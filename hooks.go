package clusterkit

import (
	"fmt"
	"sync"
)

// PartitionChange represents a change in partition assignment
type PartitionChange struct {
	PartitionID   string  // The partition that changed (e.g., "partition-5")
	CopyFromNodes []*Node // Nodes to copy data from (all nodes that have the data)
	CopyToNode    *Node   // Node that needs the data (YOU)
}

// PartitionChangeHook is a callback function for partition changes
type PartitionChangeHook func(partitionID string, copyFromNodes []*Node, copyToNode *Node)

// NodeJoinHook is called when a new node joins the cluster
type NodeJoinHook func(node *Node)

// NodeRejoinHook is called when an existing node rejoins after being offline
// This fires BEFORE rebalancing - use for preparation (e.g., clearing stale data)
type NodeRejoinHook func(node *Node)

// NodeRejoinCompleteHook is called AFTER a node rejoins AND partitions are rebalanced
// This is when you should sync data (partitions are finalized)
type NodeRejoinCompleteHook func(node *Node)

// NodeLeaveHook is called when a node leaves or is removed from the cluster
type NodeLeaveHook func(nodeID string)

// HookManager manages partition change hooks
type HookManager struct {
	hooks              []PartitionChangeHook
	nodeJoinHooks      []NodeJoinHook
	nodeRejoinHooks    []NodeRejoinHook
	nodeLeaveHooks     []NodeLeaveHook
	lastPartitionState map[string]*Partition // partitionID -> Partition
	lastNodeSet        map[string]bool       // nodeID -> exists
	mu                 sync.RWMutex
	workerPool         chan struct{} // Semaphore for limiting concurrent hook executions
}

// NewHookManager creates a new hook manager
func newHookManager() *HookManager {
	// Create a worker pool with max 50 concurrent hook executions
	workerPool := make(chan struct{}, 50)
	return &HookManager{
		hooks:              make([]PartitionChangeHook, 0),
		nodeJoinHooks:      make([]NodeJoinHook, 0),
		nodeRejoinHooks:    make([]NodeRejoinHook, 0),
		nodeLeaveHooks:     make([]NodeLeaveHook, 0),
		lastPartitionState: make(map[string]*Partition),
		lastNodeSet:        make(map[string]bool),
		workerPool:         workerPool,
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
			go func(h PartitionChangeHook, partID string, fromNodes []*Node, to *Node) {
				defer func() {
					// Release semaphore slot
					<-hm.workerPool
					// Recover from panics in hooks
					if r := recover(); r != nil {
						fmt.Printf("Hook panic recovered: %v\n", r)
					}
				}()
				h(partID, fromNodes, to)
			}(hook, change.PartitionID, change.CopyFromNodes, change.CopyToNode)
		}
	}

	// Update state
	if len(changes) > 0 {
		hm.lastPartitionState = hm.copyPartitionMap(currentPartitions)
	}
}

// detectChanges compares old and new partition states
func (hm *HookManager) detectChanges(old, new map[string]*Partition, cluster *Cluster) []PartitionChange {
	var changes []PartitionChange

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

			changes = append(changes, PartitionChange{
				PartitionID:   partitionID,
				CopyToNode:    copyToNodeObj,
				CopyFromNodes: copyFromNodes,
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
func (hm *HookManager) notifyNodeJoin(node *Node) {
	hm.mu.RLock()
	hooks := make([]NodeJoinHook, len(hm.nodeJoinHooks))
	copy(hooks, hm.nodeJoinHooks)
	hm.mu.RUnlock()

	for _, hook := range hooks {
		go func(h NodeJoinHook, n *Node) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeJoin hook panic recovered: %v\n", r)
				}
			}()
			h(n)
		}(hook, node)
	}
}

// notifyNodeRejoin triggers node rejoin hooks
func (hm *HookManager) notifyNodeRejoin(node *Node) {
	hm.mu.RLock()
	hooks := make([]NodeRejoinHook, len(hm.nodeRejoinHooks))
	copy(hooks, hm.nodeRejoinHooks)
	hm.mu.RUnlock()

	for _, hook := range hooks {
		go func(h NodeRejoinHook, n *Node) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeRejoin hook panic recovered: %v\n", r)
				}
			}()
			h(n)
		}(hook, node)
	}
}

// notifyNodeLeave triggers node leave hooks
func (hm *HookManager) notifyNodeLeave(nodeID string) {
	hm.mu.RLock()
	hooks := make([]NodeLeaveHook, len(hm.nodeLeaveHooks))
	copy(hooks, hm.nodeLeaveHooks)
	hm.mu.RUnlock()

	for _, hook := range hooks {
		go func(h NodeLeaveHook, id string) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("NodeLeave hook panic recovered: %v\n", r)
				}
			}()
			h(id)
		}(hook, nodeID)
	}
}
