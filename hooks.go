package clusterkit

import (
	"fmt"
	"sync"
	"time"
)

// ============================================
// Event Structures
// ============================================

// PartitionChangeEvent represents a partition assignment change
type PartitionChangeEvent struct {
	PartitionID   string    // The partition that changed (e.g., "partition-5")
	CopyFromNodes []*Node   // Nodes to copy data from (all nodes that have the data)
	CopyToNode    *Node     // Node that needs the data (YOU)
	ChangeReason  string    // "node_join", "node_leave", "rebalance", "manual"
	OldPrimary    string    // Previous primary node ID
	NewPrimary    string    // New primary node ID
	Timestamp     time.Time // When the change occurred
}

// NodeJoinEvent represents a node joining the cluster
type NodeJoinEvent struct {
	Node          *Node     // The joining node
	ClusterSize   int       // Total nodes after join
	IsBootstrap   bool      // Is this the first node?
	Timestamp     time.Time // When the node joined
}

// NodeRejoinEvent represents a node rejoining after being offline
type NodeRejoinEvent struct {
	Node                  *Node         // The rejoining node
	OfflineDuration       time.Duration // How long it was offline
	LastSeenAt            time.Time     // When it was last seen
	PartitionsBeforeLeave []string      // Partitions it had before leaving
	Timestamp             time.Time     // When it rejoined
}

// NodeLeaveEvent represents a node leaving the cluster
type NodeLeaveEvent struct {
	Node              *Node     // Full node info (ID, IP, Name, Status)
	Reason            string    // "health_check_failure", "graceful_shutdown", "removed_by_admin"
	PartitionsOwned   []string  // Partitions this node was primary for
	PartitionsReplica []string  // Partitions this node was replica for
	Timestamp         time.Time // When it left
}

// RebalanceEvent represents a rebalancing operation
type RebalanceEvent struct {
	Trigger          string    // "node_join", "node_leave", "manual"
	TriggerNodeID    string    // Which node caused it
	PartitionsToMove int       // How many partitions will move
	NodesAffected    []string  // Which nodes are affected
	Timestamp        time.Time // When rebalance started
}

// ClusterHealthEvent represents cluster health status change
type ClusterHealthEvent struct {
	HealthyNodes     int       // Number of healthy nodes
	UnhealthyNodes   int       // Number of unhealthy nodes
	TotalNodes       int       // Total nodes in cluster
	Status           string    // "healthy", "degraded", "critical"
	UnhealthyNodeIDs []string  // IDs of unhealthy nodes
	Timestamp        time.Time // When health changed
}

// ============================================
// Hook Function Types
// ============================================

// PartitionChangeHook is called when a partition assignment changes
type PartitionChangeHook func(event *PartitionChangeEvent)

// NodeJoinHook is called when a new node joins the cluster
type NodeJoinHook func(event *NodeJoinEvent)

// NodeRejoinHook is called when an existing node rejoins after being offline
// This fires BEFORE rebalancing - use for preparation (e.g., clearing stale data)
type NodeRejoinHook func(event *NodeRejoinEvent)

// NodeLeaveHook is called when a node leaves or is removed from the cluster
type NodeLeaveHook func(event *NodeLeaveEvent)

// RebalanceStartHook is called when a rebalance operation starts
type RebalanceStartHook func(event *RebalanceEvent)

// RebalanceCompleteHook is called when a rebalance operation completes
type RebalanceCompleteHook func(event *RebalanceEvent, duration time.Duration)

// ClusterHealthChangeHook is called when cluster health status changes
type ClusterHealthChangeHook func(event *ClusterHealthEvent)

// HookManager manages all cluster event hooks
type HookManager struct {
	hooks                   []PartitionChangeHook
	nodeJoinHooks           []NodeJoinHook
	nodeRejoinHooks         []NodeRejoinHook
	nodeLeaveHooks          []NodeLeaveHook
	rebalanceStartHooks     []RebalanceStartHook
	rebalanceCompleteHooks  []RebalanceCompleteHook
	clusterHealthHooks      []ClusterHealthChangeHook
	lastPartitionState      map[string]*Partition // partitionID -> Partition
	lastNodeSet             map[string]bool       // nodeID -> exists
	lastNodeSeenTime        map[string]time.Time  // nodeID -> last seen timestamp
	lastHealthStatus        string                // Last cluster health status
	mu                      sync.RWMutex
	workerPool              chan struct{} // Semaphore for limiting concurrent hook executions
}

// NewHookManager creates a new hook manager
func newHookManager() *HookManager {
	// Create a worker pool with max 50 concurrent hook executions
	workerPool := make(chan struct{}, 50)
	return &HookManager{
		hooks:                  make([]PartitionChangeHook, 0),
		nodeJoinHooks:          make([]NodeJoinHook, 0),
		nodeRejoinHooks:        make([]NodeRejoinHook, 0),
		nodeLeaveHooks:         make([]NodeLeaveHook, 0),
		rebalanceStartHooks:    make([]RebalanceStartHook, 0),
		rebalanceCompleteHooks: make([]RebalanceCompleteHook, 0),
		clusterHealthHooks:     make([]ClusterHealthChangeHook, 0),
		lastPartitionState:     make(map[string]*Partition),
		lastNodeSet:            make(map[string]bool),
		lastNodeSeenTime:       make(map[string]time.Time),
		lastHealthStatus:       "healthy",
		workerPool:             workerPool,
	}
}

// OnPartitionChange registers a callback for partition changes
func (ck *ClusterKit) OnPartitionChange(hook PartitionChangeHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.hooks = append(ck.hookManager.hooks, hook)
}

// OnNodeJoin registers a callback for when a NEW node joins
func (ck *ClusterKit) OnNodeJoin(hook NodeJoinHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.nodeJoinHooks = append(ck.hookManager.nodeJoinHooks, hook)
}

// OnNodeRejoin registers a callback for when a node REJOINS after being offline
func (ck *ClusterKit) OnNodeRejoin(hook NodeRejoinHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.nodeRejoinHooks = append(ck.hookManager.nodeRejoinHooks, hook)
}

// OnNodeLeave registers a callback for when a node leaves/is removed
func (ck *ClusterKit) OnNodeLeave(hook NodeLeaveHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.nodeLeaveHooks = append(ck.hookManager.nodeLeaveHooks, hook)
}

// OnRebalanceStart registers a callback for when rebalancing starts
func (ck *ClusterKit) OnRebalanceStart(hook RebalanceStartHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.rebalanceStartHooks = append(ck.hookManager.rebalanceStartHooks, hook)
}

// OnRebalanceComplete registers a callback for when rebalancing completes
func (ck *ClusterKit) OnRebalanceComplete(hook RebalanceCompleteHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.rebalanceCompleteHooks = append(ck.hookManager.rebalanceCompleteHooks, hook)
}

// OnClusterHealthChange registers a callback for cluster health changes
func (ck *ClusterKit) OnClusterHealthChange(hook ClusterHealthChangeHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.clusterHealthHooks = append(ck.hookManager.clusterHealthHooks, hook)
}

// checkPartitionChanges detects and notifies about partition changes
func (hm *HookManager) checkPartitionChanges(currentPartitions map[string]*Partition, cluster *Cluster) {
	if len(hm.hooks) == 0 {
		return // No hooks registered
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// First time - just save the state
	if len(hm.lastPartitionState) == 0 {
		hm.lastPartitionState = hm.copyPartitionMap(currentPartitions)
		return
	}

	// Detect changes
	changes := hm.detectChanges(hm.lastPartitionState, currentPartitions, cluster)

	// Notify hooks with goroutine pooling
	for _, change := range changes {
		for _, hook := range hm.hooks {
			// Acquire semaphore slot (blocks if pool is full)
			hm.workerPool <- struct{}{}

			// Execute hook in goroutine
			go func(h PartitionChangeHook, event *PartitionChangeEvent) {
				defer func() {
					// Release semaphore slot
					<-hm.workerPool
					// Recover from panics in hooks
					if r := recover(); r != nil {
						fmt.Printf("Hook panic recovered: %v\n", r)
					}
				}()
				h(event)
			}(hook, &change)
		}
	}

	// Update state
	if len(changes) > 0 {
		hm.lastPartitionState = hm.copyPartitionMap(currentPartitions)
	}
}

// detectChanges compares old and new partition states
func (hm *HookManager) detectChanges(old, new map[string]*Partition, cluster *Cluster) []PartitionChangeEvent {
	var changes []PartitionChangeEvent

	for partitionID, newPartition := range new {
		oldPartition, exists := old[partitionID]
		if !exists {
			// New partition created - not a change, skip
			continue
		}

		// Find nodes that need data (new primary or new replicas)
		nodesToNotify := make(map[string]bool)

		// Check if primary changed
		if oldPartition.PrimaryNode != newPartition.PrimaryNode {
			nodesToNotify[newPartition.PrimaryNode] = true
		}

		// Check for new replicas
		oldReplicaSet := make(map[string]bool)
		for _, r := range oldPartition.ReplicaNodes {
			oldReplicaSet[r] = true
		}

		for _, newReplicaID := range newPartition.ReplicaNodes {
			if !oldReplicaSet[newReplicaID] {
				// This is a NEW replica that needs data
				nodesToNotify[newReplicaID] = true
			}
		}

		// Create a change notification for each node that needs data
		for nodeID := range nodesToNotify {
			// Collect ALL nodes that have the data (OLD primary + OLD replicas)
			copyFromNodes := []*Node{}

			// IMPORTANT: Only use nodes that ALREADY HAD the data AND are NOT also migrating
			// This ensures we copy from stable nodes with complete data

			// Add old primary if still alive and not the target node
			if oldPartition.PrimaryNode != nodeID {
				if node := getNodeByID(cluster, oldPartition.PrimaryNode); node != nil {
					copyFromNodes = append(copyFromNodes, node)
				}
			}

			// Add all old replicas that are NOT also being promoted/changed
			for _, replicaID := range oldPartition.ReplicaNodes {
				if replicaID != nodeID {
					// Check if this replica is also being promoted to primary
					// If so, skip it as a source (it's also migrating)
					if replicaID == newPartition.PrimaryNode && replicaID != oldPartition.PrimaryNode {
						// This replica is being promoted to primary, skip as source
						continue
					}

					if node := getNodeByID(cluster, replicaID); node != nil {
						// Check if not already in list
						alreadyAdded := false
						for _, n := range copyFromNodes {
							if n.ID == replicaID {
								alreadyAdded = true
								break
							}
						}
						if !alreadyAdded {
							copyFromNodes = append(copyFromNodes, node)
						}
					}
				}
			}

			// Find the target node object
			copyToNodeObj := getNodeByID(cluster, nodeID)

			changes = append(changes, PartitionChangeEvent{
				PartitionID:   partitionID,
				CopyToNode:    copyToNodeObj,
				CopyFromNodes: copyFromNodes,
				ChangeReason:  "rebalance", // Default reason, can be enhanced
				OldPrimary:    oldPartition.PrimaryNode,
				NewPrimary:    newPartition.PrimaryNode,
				Timestamp:     time.Now(),
			})
		}
	}

	return changes
}

// replicasChanged checks if replica sets are different
func (hm *HookManager) replicasChanged(old, new []string) bool {
	if len(old) != len(new) {
		return true
	}

	oldSet := make(map[string]bool)
	for _, r := range old {
		oldSet[r] = true
	}

	for _, r := range new {
		if !oldSet[r] {
			return true
		}
	}

	return false
}

// copyPartitionMap creates a deep copy of partition map
func (hm *HookManager) copyPartitionMap(partitions map[string]*Partition) map[string]*Partition {
	result := make(map[string]*Partition)
	for id, partition := range partitions {
		replicas := make([]string, len(partition.ReplicaNodes))
		copy(replicas, partition.ReplicaNodes)

		result[id] = &Partition{
			ID:           partition.ID,
			PrimaryNode:  partition.PrimaryNode,
			ReplicaNodes: replicas,
		}
	}
	return result
}

// getNodeByID finds a node by ID in the cluster
func getNodeByID(cluster *Cluster, nodeID string) *Node {
	for i := range cluster.Nodes {
		if cluster.Nodes[i].ID == nodeID {
			return &cluster.Nodes[i]
		}
	}
	return nil
}

// notifyNodeJoin triggers node join hooks
func (hm *HookManager) notifyNodeJoin(node *Node, clusterSize int, isBootstrap bool) {
	hm.mu.RLock()
	hooks := make([]NodeJoinHook, len(hm.nodeJoinHooks))
	copy(hooks, hm.nodeJoinHooks)
	hm.mu.RUnlock()

	event := &NodeJoinEvent{
		Node:        node,
		ClusterSize: clusterSize,
		IsBootstrap: isBootstrap,
		Timestamp:   time.Now(),
	}

	for _, hook := range hooks {
		go func(h NodeJoinHook, e *NodeJoinEvent) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeJoin hook panic recovered: %v\n", r)
				}
			}()
			h(e)
		}(hook, event)
	}
}

// notifyNodeRejoin triggers node rejoin hooks
func (hm *HookManager) notifyNodeRejoin(node *Node, partitionsBeforeLeave []string) {
	hm.mu.RLock()
	hooks := make([]NodeRejoinHook, len(hm.nodeRejoinHooks))
	copy(hooks, hm.nodeRejoinHooks)
	
	// Calculate offline duration
	lastSeen := hm.lastNodeSeenTime[node.ID]
	offlineDuration := time.Since(lastSeen)
	if lastSeen.IsZero() {
		offlineDuration = 0 // Unknown
	}
	hm.mu.RUnlock()

	event := &NodeRejoinEvent{
		Node:                  node,
		OfflineDuration:       offlineDuration,
		LastSeenAt:            lastSeen,
		PartitionsBeforeLeave: partitionsBeforeLeave,
		Timestamp:             time.Now(),
	}

	for _, hook := range hooks {
		go func(h NodeRejoinHook, e *NodeRejoinEvent) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeRejoin hook panic recovered: %v\n", r)
				}
			}()
			h(e)
		}(hook, event)
	}
}

// notifyNodeLeave triggers node leave hooks
func (hm *HookManager) notifyNodeLeave(node *Node, reason string, partitionsOwned, partitionsReplica []string) {
	hm.mu.RLock()
	hooks := make([]NodeLeaveHook, len(hm.nodeLeaveHooks))
	copy(hooks, hm.nodeLeaveHooks)
	
	// Update last seen time
	hm.lastNodeSeenTime[node.ID] = time.Now()
	hm.mu.RUnlock()

	event := &NodeLeaveEvent{
		Node:              node,
		Reason:            reason,
		PartitionsOwned:   partitionsOwned,
		PartitionsReplica: partitionsReplica,
		Timestamp:         time.Now(),
	}

	for _, hook := range hooks {
		go func(h NodeLeaveHook, e *NodeLeaveEvent) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeLeave hook panic recovered: %v\n", r)
				}
			}()
			h(e)
		}(hook, event)
	}
}
