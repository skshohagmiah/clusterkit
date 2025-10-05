package clusterkit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/etcd"
	"github.com/shohag/clusterkit/pkg/hash"
)

// RebalanceStrategy defines how rebalancing should be performed
type RebalanceStrategy int

const (
	RebalanceImmediate RebalanceStrategy = iota // Immediate rebalancing
	RebalanceDelayed                            // Delayed rebalancing with debouncing
	RebalanceManual                             // Manual rebalancing only
)

// RebalanceEvent represents a rebalancing event
type RebalanceEvent struct {
	Trigger   string                 // What triggered the rebalance
	OldMap    map[int][]*Node       // Old partition assignments
	NewMap    map[int][]*Node       // New partition assignments
	Changes   []PartitionChange     // List of changes
	StartTime time.Time             // When rebalancing started
	Duration  time.Duration         // How long it took
}

// PartitionChange represents a single partition assignment change
type PartitionChange struct {
	Partition int     // Partition ID
	OldNodes  []*Node // Previous nodes
	NewNodes  []*Node // New nodes
	Action    string  // "assigned", "unassigned", "moved"
}

// RebalanceManager handles cluster rebalancing operations
type RebalanceManager struct {
	mu           sync.RWMutex
	store        *etcd.Store
	config       *Config
	logger       Logger
	membership   *MembershipManager
	ring         *hash.PartitionRing
	strategy     RebalanceStrategy
	rebalanceTimer *time.Timer
	isRebalancing bool
	lastRebalance time.Time
	stopCh       chan struct{}
	wg           sync.WaitGroup
	
	// Statistics
	rebalanceCount int
	totalDuration  time.Duration
}

// NewRebalanceManager creates a new rebalance manager
func NewRebalanceManager(store *etcd.Store, config *Config, membership *MembershipManager) *RebalanceManager {
	return &RebalanceManager{
		store:      store,
		config:     config,
		logger:     config.Logger,
		membership: membership,
		ring:       hash.NewPartitionRing(config.Partitions, config.ReplicaFactor),
		strategy:   RebalanceDelayed,
		stopCh:     make(chan struct{}),
	}
}

// Start starts the rebalance manager
func (rm *RebalanceManager) Start(ctx context.Context) error {
	// Initialize ring with existing nodes
	nodes := rm.membership.GetNodes()
	for _, node := range nodes {
		rm.ring.AddNode(node.ID)
	}

	rm.wg.Add(1)
	go rm.rebalanceLoop(ctx)

	rm.logger.Infof("Rebalance manager started with strategy: %s", rm.getStrategyName())
	return nil
}

// Stop stops the rebalance manager
func (rm *RebalanceManager) Stop() error {
	close(rm.stopCh)
	rm.wg.Wait()

	if rm.rebalanceTimer != nil {
		rm.rebalanceTimer.Stop()
	}

	rm.logger.Infof("Rebalance manager stopped")
	return nil
}

// SetStrategy sets the rebalancing strategy
func (rm *RebalanceManager) SetStrategy(strategy RebalanceStrategy) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.strategy = strategy
	rm.logger.Infof("Rebalance strategy changed to: %s", rm.getStrategyName())
}

// OnNodeJoined handles a node joining the cluster
func (rm *RebalanceManager) OnNodeJoined(nodeID string) {
	rm.logger.Infof("Node joined, triggering rebalance: %s", nodeID)
	rm.ring.AddNode(nodeID)
	rm.triggerRebalance(fmt.Sprintf("node_joined:%s", nodeID))
}

// OnNodeLeft handles a node leaving the cluster
func (rm *RebalanceManager) OnNodeLeft(nodeID string) {
	rm.logger.Infof("Node left, triggering rebalance: %s", nodeID)
	rm.ring.RemoveNode(nodeID)
	rm.triggerRebalance(fmt.Sprintf("node_left:%s", nodeID))
}

// TriggerManualRebalance manually triggers a rebalance
func (rm *RebalanceManager) TriggerManualRebalance() error {
	rm.mu.RLock()
	if rm.isRebalancing {
		rm.mu.RUnlock()
		return fmt.Errorf("rebalancing already in progress")
	}
	rm.mu.RUnlock()

	rm.triggerRebalance("manual_trigger")
	return nil
}

// triggerRebalance triggers a rebalance based on the current strategy
func (rm *RebalanceManager) triggerRebalance(trigger string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	switch rm.strategy {
	case RebalanceImmediate:
		go rm.performRebalance(trigger)
	case RebalanceDelayed:
		rm.scheduleDelayedRebalance(trigger)
	case RebalanceManual:
		rm.logger.Infof("Rebalance triggered but strategy is manual: %s", trigger)
	}
}

// scheduleDelayedRebalance schedules a delayed rebalance with debouncing
func (rm *RebalanceManager) scheduleDelayedRebalance(trigger string) {
	if rm.rebalanceTimer != nil {
		rm.rebalanceTimer.Stop()
	}

	rm.rebalanceTimer = time.AfterFunc(rm.config.RebalanceDelay, func() {
		rm.performRebalance(trigger)
	})

	rm.logger.Debugf("Scheduled delayed rebalance in %v (trigger: %s)", rm.config.RebalanceDelay, trigger)
}

// performRebalance performs the actual rebalancing
func (rm *RebalanceManager) performRebalance(trigger string) {
	rm.mu.Lock()
	if rm.isRebalancing {
		rm.mu.Unlock()
		rm.logger.Warnf("Rebalance already in progress, skipping")
		return
	}
	rm.isRebalancing = true
	rm.mu.Unlock()

	defer func() {
		rm.mu.Lock()
		rm.isRebalancing = false
		rm.mu.Unlock()
	}()

	startTime := time.Now()
	rm.logger.Infof("Starting rebalance (trigger: %s)", trigger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get current assignments
	oldAssignments, err := rm.getCurrentAssignments(ctx)
	if err != nil {
		rm.logger.Errorf("Failed to get current assignments: %v", err)
		return
	}

	// Calculate new assignments
	newAssignments := rm.calculateNewAssignments()

	// Calculate changes
	changes := rm.calculateChanges(oldAssignments, newAssignments)
	if len(changes) == 0 {
		rm.logger.Infof("No rebalancing needed")
		return
	}

	// Apply changes
	err = rm.applyChanges(ctx, newAssignments)
	if err != nil {
		rm.logger.Errorf("Failed to apply rebalance changes: %v", err)
		return
	}

	duration := time.Since(startTime)
	
	// Update statistics
	rm.mu.Lock()
	rm.rebalanceCount++
	rm.totalDuration += duration
	rm.lastRebalance = startTime
	rm.mu.Unlock()

	rm.logger.Infof("Rebalance completed in %v (%d changes)", duration, len(changes))

	// Create rebalance event
	event := RebalanceEvent{
		Trigger:   trigger,
		OldMap:    oldAssignments,
		NewMap:    newAssignments,
		Changes:   changes,
		StartTime: startTime,
		Duration:  duration,
	}

	rm.notifyRebalanceComplete(event)
}

// getCurrentAssignments gets current partition assignments from etcd
func (rm *RebalanceManager) getCurrentAssignments(ctx context.Context) (map[int][]*Node, error) {
	assignments, err := rm.store.List(ctx, "partitions/")
	if err != nil {
		return nil, fmt.Errorf("failed to list partitions: %w", err)
	}

	result := make(map[int][]*Node)
	for key, data := range assignments {
		var partition int
		_, err = fmt.Sscanf(key, "partitions/%d", &partition)
		if err != nil {
			continue
		}

		var info PartitionInfo
		err = json.Unmarshal(data, &info)
		if err != nil {
			continue
		}

		result[partition] = info.Replicas
	}

	return result, nil
}

// calculateNewAssignments calculates new partition assignments
func (rm *RebalanceManager) calculateNewAssignments() map[int][]*Node {
	nodes := rm.membership.GetNodes()
	nodeMap := make(map[string]*Node)
	for _, node := range nodes {
		nodeMap[node.ID] = node
	}

	assignments := make(map[int][]*Node)
	for partition := 0; partition < rm.config.Partitions; partition++ {
		nodeIDs := rm.ring.GetNodes(partition)
		
		var replicas []*Node
		for _, nodeID := range nodeIDs {
			if node, exists := nodeMap[nodeID]; exists {
				replicas = append(replicas, node)
			}
		}

		if len(replicas) > 0 {
			assignments[partition] = replicas
		}
	}

	return assignments
}

// calculateChanges calculates the differences between old and new assignments
func (rm *RebalanceManager) calculateChanges(oldMap, newMap map[int][]*Node) []PartitionChange {
	var changes []PartitionChange

	// Check all partitions
	allPartitions := make(map[int]bool)
	for partition := range oldMap {
		allPartitions[partition] = true
	}
	for partition := range newMap {
		allPartitions[partition] = true
	}

	for partition := range allPartitions {
		oldNodes := oldMap[partition]
		newNodes := newMap[partition]

		if !rm.nodeListsEqual(oldNodes, newNodes) {
			action := "moved"
			if len(oldNodes) == 0 {
				action = "assigned"
			} else if len(newNodes) == 0 {
				action = "unassigned"
			}

			changes = append(changes, PartitionChange{
				Partition: partition,
				OldNodes:  oldNodes,
				NewNodes:  newNodes,
				Action:    action,
			})
		}
	}

	return changes
}

// nodeListsEqual checks if two node lists are equal
func (rm *RebalanceManager) nodeListsEqual(a, b []*Node) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, node := range a {
		aMap[node.ID] = true
	}

	for _, node := range b {
		if !aMap[node.ID] {
			return false
		}
	}

	return true
}

// applyChanges applies new assignments to etcd
func (rm *RebalanceManager) applyChanges(ctx context.Context, assignments map[int][]*Node) error {
	for partition, replicas := range assignments {
		if len(replicas) == 0 {
			continue
		}

		info := &PartitionInfo{
			ID:       partition,
			Leader:   replicas[0], // First replica is leader
			Replicas: replicas,
		}

		key := fmt.Sprintf("partitions/%d", partition)
		err := rm.store.Put(ctx, key, info)
		if err != nil {
			return fmt.Errorf("failed to store assignment for partition %d: %w", partition, err)
		}
	}

	return nil
}

// notifyRebalanceComplete notifies about rebalance completion
func (rm *RebalanceManager) notifyRebalanceComplete(event RebalanceEvent) {
	// This could be extended to send notifications to external systems
	rm.logger.Infof("Rebalance event: %s (%d changes in %v)", 
		event.Trigger, len(event.Changes), event.Duration)
}

// rebalanceLoop handles periodic rebalancing tasks
func (rm *RebalanceManager) rebalanceLoop(ctx context.Context) {
	defer rm.wg.Done()

	ticker := time.NewTicker(5 * time.Minute) // Periodic health check
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.performHealthCheck()
		}
	}
}

// performHealthCheck performs periodic health checks
func (rm *RebalanceManager) performHealthCheck() {
	rm.mu.RLock()
	if rm.isRebalancing {
		rm.mu.RUnlock()
		return
	}
	rm.mu.RUnlock()

	// Check if any partitions are unassigned
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	assignments, err := rm.getCurrentAssignments(ctx)
	if err != nil {
		rm.logger.Errorf("Health check failed: %v", err)
		return
	}

	unassigned := 0
	for partition := 0; partition < rm.config.Partitions; partition++ {
		if len(assignments[partition]) == 0 {
			unassigned++
		}
	}

	if unassigned > 0 {
		rm.logger.Warnf("Health check found %d unassigned partitions", unassigned)
		rm.triggerRebalance("health_check")
	}
}

// GetStats returns rebalancing statistics
func (rm *RebalanceManager) GetStats() RebalanceStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	avgDuration := time.Duration(0)
	if rm.rebalanceCount > 0 {
		avgDuration = rm.totalDuration / time.Duration(rm.rebalanceCount)
	}

	return RebalanceStats{
		Strategy:         rm.getStrategyName(),
		RebalanceCount:   rm.rebalanceCount,
		LastRebalance:    rm.lastRebalance,
		AverageDuration:  avgDuration,
		IsRebalancing:    rm.isRebalancing,
	}
}

// getStrategyName returns the name of the current strategy
func (rm *RebalanceManager) getStrategyName() string {
	switch rm.strategy {
	case RebalanceImmediate:
		return "immediate"
	case RebalanceDelayed:
		return "delayed"
	case RebalanceManual:
		return "manual"
	default:
		return "unknown"
	}
}

// RebalanceStats represents rebalancing statistics
type RebalanceStats struct {
	Strategy        string        `json:"strategy"`
	RebalanceCount  int           `json:"rebalance_count"`
	LastRebalance   time.Time     `json:"last_rebalance"`
	AverageDuration time.Duration `json:"average_duration"`
	IsRebalancing   bool          `json:"is_rebalancing"`
}
