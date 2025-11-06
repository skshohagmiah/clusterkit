package clusterkit

import (
	"fmt"
	"sync"
)

// PartitionChange represents a change in partition assignment
type PartitionChange struct {
	PartitionID string // The partition that changed (e.g., "partition-5")
	CopyFromNode *Node // Node to copy data from (nil if no copy needed)
	CopyToNode   *Node // Node that needs the data (YOU)
}

// PartitionChangeHook is a callback function for partition changes
type PartitionChangeHook func(partitionID string, copyFrom *Node, copyTo *Node)

// HookManager manages partition change hooks
type HookManager struct {
	hooks              []PartitionChangeHook
	lastPartitionState map[string]*Partition // partitionID -> Partition
	mu                 sync.RWMutex
	workerPool         chan struct{} // Semaphore for limiting concurrent hook executions
}

// NewHookManager creates a new hook manager
func newHookManager() *HookManager {
	// Create a worker pool with max 50 concurrent hook executions
	workerPool := make(chan struct{}, 50)
	return &HookManager{
		hooks:              make([]PartitionChangeHook, 0),
		lastPartitionState: make(map[string]*Partition),
		workerPool:         workerPool,
	}
}

// OnPartitionChange registers a callback for partition changes
func (ck *ClusterKit) OnPartitionChange(hook PartitionChangeHook) {
	ck.hookManager.mu.Lock()
	defer ck.hookManager.mu.Unlock()
	ck.hookManager.hooks = append(ck.hookManager.hooks, hook)
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
			go func(h PartitionChangeHook, partID string, from, to *Node) {
				defer func() {
					// Release semaphore slot
					<-hm.workerPool
					// Recover from panics in hooks
					if r := recover(); r != nil {
						fmt.Printf("Hook panic recovered: %v\n", r)
					}
				}()
				h(partID, from, to)
			}(hook, change.PartitionID, change.CopyFromNode, change.CopyToNode)
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
			// Find a node to copy from
			copyFromNode := ""
			
			// Try old replicas first (they have the data)
			for _, replicaID := range oldPartition.ReplicaNodes {
				if replicaID != nodeID {
					copyFromNode = replicaID
					break
				}
			}
			
			// If no old replica, try old primary
			if copyFromNode == "" && oldPartition.PrimaryNode != nodeID {
				copyFromNode = oldPartition.PrimaryNode
			}
			
			// If still nothing, try new replicas
			if copyFromNode == "" {
				for _, replicaID := range newPartition.ReplicaNodes {
					if replicaID != nodeID {
						copyFromNode = replicaID
						break
					}
				}
			}
			
			// Find the actual Node objects
			var copyToNodeObj *Node
			var copyFromNodeObj *Node
			
			for i := range cluster.Nodes {
				if cluster.Nodes[i].ID == nodeID {
					copyToNodeObj = &cluster.Nodes[i]
				}
				if cluster.Nodes[i].ID == copyFromNode {
					copyFromNodeObj = &cluster.Nodes[i]
				}
			}
			
			changes = append(changes, PartitionChange{
				PartitionID:  partitionID,
				CopyToNode:   copyToNodeObj,
				CopyFromNode: copyFromNodeObj,
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
