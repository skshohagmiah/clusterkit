package clusterkit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/etcd"
	"github.com/shohag/clusterkit/pkg/hash"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// PartitionManager manages partition assignments and rebalancing
type PartitionManager struct {
	mu               sync.RWMutex
	store            *etcd.Store
	config           *Config
	logger           Logger
	membership       *MembershipManager
	ring             *hash.PartitionRing
	assignments      map[int]*PartitionInfo
	localPartitions  map[int]bool
	leaderPartitions map[int]bool
	stopCh           chan struct{}
	wg               sync.WaitGroup
	rebalanceTimer   *time.Timer
}

// NewPartitionManager creates a new partition manager
func NewPartitionManager(store *etcd.Store, config *Config, membership *MembershipManager) *PartitionManager {
	return &PartitionManager{
		store:            store,
		config:           config,
		logger:           config.Logger,
		membership:       membership,
		ring:             hash.NewPartitionRing(config.Partitions, config.ReplicaFactor),
		assignments:      make(map[int]*PartitionInfo),
		localPartitions:  make(map[int]bool),
		leaderPartitions: make(map[int]bool),
		stopCh:           make(chan struct{}),
	}
}

// Start starts the partition manager
func (pm *PartitionManager) Start(ctx context.Context) error {
	// Load existing assignments
	err := pm.loadAssignments(ctx)
	if err != nil {
		return fmt.Errorf("failed to load assignments: %w", err)
	}

	// Add existing nodes to the ring
	nodes := pm.membership.GetNodes()
	for _, node := range nodes {
		pm.ring.AddNode(node.ID)
	}

	// Start watching for assignment changes
	pm.wg.Add(2)
	go pm.watchAssignments(ctx)
	go pm.rebalanceLoop(ctx)

	// Trigger initial rebalance
	pm.scheduleRebalance()

	pm.logger.Infof("Partition manager started with %d partitions", pm.config.Partitions)
	return nil
}

// Stop stops the partition manager
func (pm *PartitionManager) Stop() error {
	close(pm.stopCh)
	pm.wg.Wait()

	if pm.rebalanceTimer != nil {
		pm.rebalanceTimer.Stop()
	}

	pm.logger.Infof("Partition manager stopped")
	return nil
}

// GetPartition returns the partition ID for a given key
func (pm *PartitionManager) GetPartition(key string) int {
	return pm.ring.GetPartition(key)
}

// GetLeader returns the leader node for a partition
func (pm *PartitionManager) GetLeader(partition int) *Node {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	info, exists := pm.assignments[partition]
	if !exists || info.Leader == nil {
		return nil
	}

	return info.Leader
}

// GetReplicas returns all replica nodes for a partition
func (pm *PartitionManager) GetReplicas(partition int) []*Node {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	info, exists := pm.assignments[partition]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	replicas := make([]*Node, len(info.Replicas))
	copy(replicas, info.Replicas)
	return replicas
}

// IsLeader checks if the local node is the leader for a partition
func (pm *PartitionManager) IsLeader(partition int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.leaderPartitions[partition]
}

// IsReplica checks if the local node is a replica for a partition
func (pm *PartitionManager) IsReplica(partition int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.localPartitions[partition]
}

// GetPartitionMap returns the complete partition assignment map
func (pm *PartitionManager) GetPartitionMap() map[int][]*Node {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[int][]*Node)
	for partition, info := range pm.assignments {
		result[partition] = make([]*Node, len(info.Replicas))
		copy(result[partition], info.Replicas)
	}

	return result
}

// OnNodeJoined handles a node joining the cluster
func (pm *PartitionManager) OnNodeJoined(nodeID string) {
	pm.logger.Infof("Node joined, scheduling rebalance: %s", nodeID)
	pm.ring.AddNode(nodeID)
	pm.scheduleRebalance()
}

// OnNodeLeft handles a node leaving the cluster
func (pm *PartitionManager) OnNodeLeft(nodeID string) {
	pm.logger.Infof("Node left, scheduling rebalance: %s", nodeID)
	pm.ring.RemoveNode(nodeID)
	pm.scheduleRebalance()
}

// loadAssignments loads existing partition assignments from etcd
func (pm *PartitionManager) loadAssignments(ctx context.Context) error {
	assignments, err := pm.store.List(ctx, "partitions/")
	if err != nil {
		return fmt.Errorf("failed to list partition assignments: %w", err)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	localNodeID := pm.membership.GetLocalNode().ID

	for key, data := range assignments {
		var partition int
		_, err = fmt.Sscanf(key, "partitions/%d", &partition)
		if err != nil {
			pm.logger.Errorf("Failed to parse partition key %s: %v", key, err)
			continue
		}

		var info PartitionInfo
		err = json.Unmarshal(data, &info)
		if err != nil {
			pm.logger.Errorf("Failed to unmarshal partition %d: %v", partition, err)
			continue
		}

		pm.assignments[partition] = &info

		// Check if local node is involved
		if info.Leader != nil && info.Leader.ID == localNodeID {
			pm.leaderPartitions[partition] = true
			pm.localPartitions[partition] = true
		}

		for _, replica := range info.Replicas {
			if replica.ID == localNodeID {
				pm.localPartitions[partition] = true
				break
			}
		}
	}

	pm.logger.Infof("Loaded %d partition assignments", len(pm.assignments))
	return nil
}

// watchAssignments watches for partition assignment changes
func (pm *PartitionManager) watchAssignments(ctx context.Context) {
	defer pm.wg.Done()

	watchCh := pm.store.Watch(ctx, "partitions/")
	pm.logger.Infof("Started watching partition assignments")

	for {
		select {
		case <-pm.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				pm.logger.Warnf("Partition assignment watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				pm.handleAssignmentEvent(event)
			}
		}
	}
}

// handleAssignmentEvent handles a partition assignment change event
func (pm *PartitionManager) handleAssignmentEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	var partition int
	_, err := fmt.Sscanf(key, "%s/partitions/%d", pm.store.Prefix, &partition)
	if err != nil {
		pm.logger.Errorf("Failed to parse partition key %s: %v", key, err)
		return
	}

	localNodeID := pm.membership.GetLocalNode().ID

	pm.mu.Lock()
	defer pm.mu.Unlock()

	switch event.Type {
	case clientv3.EventTypePut:
		var info PartitionInfo
		err := json.Unmarshal(event.Kv.Value, &info)
		if err != nil {
			pm.logger.Errorf("Failed to unmarshal partition %d: %v", partition, err)
			return
		}

		oldInfo := pm.assignments[partition]
		pm.assignments[partition] = &info

		// Handle leadership changes
		wasLeader := pm.leaderPartitions[partition]
		isLeader := info.Leader != nil && info.Leader.ID == localNodeID

		if !wasLeader && isLeader {
			pm.leaderPartitions[partition] = true
			pm.localPartitions[partition] = true
			if pm.config.OnLeaderElected != nil {
				go pm.config.OnLeaderElected(partition)
			}
		} else if wasLeader && !isLeader {
			delete(pm.leaderPartitions, partition)
			if pm.config.OnLeaderLost != nil {
				go pm.config.OnLeaderLost(partition)
			}
		}

		// Handle partition assignment changes
		wasAssigned := pm.localPartitions[partition]
		isAssigned := pm.isLocalNodeInReplicas(&info)

		if !wasAssigned && isAssigned {
			pm.localPartitions[partition] = true
			var prevOwner *Node
			if oldInfo != nil && oldInfo.Leader != nil {
				prevOwner = oldInfo.Leader
			}
			if pm.config.OnPartitionAssigned != nil {
				go pm.config.OnPartitionAssigned(partition, prevOwner)
			}
		} else if wasAssigned && !isAssigned {
			delete(pm.localPartitions, partition)
			var newOwner *Node
			if info.Leader != nil {
				newOwner = info.Leader
			}
			if pm.config.OnPartitionUnassigned != nil {
				go pm.config.OnPartitionUnassigned(partition, newOwner)
			}
		}

	case clientv3.EventTypeDelete:
		oldInfo := pm.assignments[partition]
		delete(pm.assignments, partition)

		if pm.leaderPartitions[partition] {
			delete(pm.leaderPartitions, partition)
			if pm.config.OnLeaderLost != nil {
				go pm.config.OnLeaderLost(partition)
			}
		}

		if pm.localPartitions[partition] {
			delete(pm.localPartitions, partition)
			var newOwner *Node
			if oldInfo != nil && oldInfo.Leader != nil {
				newOwner = oldInfo.Leader
			}
			if pm.config.OnPartitionUnassigned != nil {
				go pm.config.OnPartitionUnassigned(partition, newOwner)
			}
		}
	}
}

// isLocalNodeInReplicas checks if the local node is in the replica list
func (pm *PartitionManager) isLocalNodeInReplicas(info *PartitionInfo) bool {
	localNodeID := pm.membership.GetLocalNode().ID
	for _, replica := range info.Replicas {
		if replica.ID == localNodeID {
			return true
		}
	}
	return false
}

// rebalanceLoop handles periodic rebalancing
func (pm *PartitionManager) rebalanceLoop(ctx context.Context) {
	defer pm.wg.Done()

	for {
		select {
		case <-pm.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// scheduleRebalance schedules a rebalance operation
func (pm *PartitionManager) scheduleRebalance() {
	if pm.rebalanceTimer != nil {
		pm.rebalanceTimer.Stop()
	}

	pm.rebalanceTimer = time.AfterFunc(pm.config.RebalanceDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pm.rebalance(ctx)
	})
}

// rebalance performs partition rebalancing
func (pm *PartitionManager) rebalance(ctx context.Context) {
	pm.logger.Infof("Starting partition rebalance")

	// Get current nodes
	nodes := pm.membership.GetNodes()
	if len(nodes) == 0 {
		pm.logger.Warnf("No nodes available for rebalancing")
		return
	}

	// Calculate new assignments
	newAssignments := pm.calculateAssignments(nodes)

	// Apply changes
	pm.applyAssignments(ctx, newAssignments)

	pm.logger.Infof("Partition rebalance completed")
}

// calculateAssignments calculates new partition assignments
func (pm *PartitionManager) calculateAssignments(nodes []*Node) map[int]*PartitionInfo {
	assignments := make(map[int]*PartitionInfo)

	// Create node map for quick lookup
	nodeMap := make(map[string]*Node)
	for _, node := range nodes {
		nodeMap[node.ID] = node
	}

	// Calculate assignments for each partition
	for partition := 0; partition < pm.config.Partitions; partition++ {
		nodeIDs := pm.ring.GetNodes(partition)
		
		if len(nodeIDs) == 0 {
			continue
		}

		// Convert node IDs to Node objects
		replicas := make([]*Node, 0, len(nodeIDs))
		for _, nodeID := range nodeIDs {
			if node, exists := nodeMap[nodeID]; exists {
				replicas = append(replicas, node)
			}
		}

		if len(replicas) == 0 {
			continue
		}

		// First replica is the leader
		assignments[partition] = &PartitionInfo{
			ID:       partition,
			Leader:   replicas[0],
			Replicas: replicas,
		}
	}

	return assignments
}

// applyAssignments applies new partition assignments to etcd
func (pm *PartitionManager) applyAssignments(ctx context.Context, assignments map[int]*PartitionInfo) {
	for partition, info := range assignments {
		key := fmt.Sprintf("partitions/%d", partition)
		err := pm.store.Put(ctx, key, info)
		if err != nil {
			pm.logger.Errorf("Failed to store assignment for partition %d: %v", partition, err)
		}
	}
}
