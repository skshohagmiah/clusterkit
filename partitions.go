package clusterkit

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// CreatePartitions initializes partitions and assigns them to nodes
func (ck *ClusterKit) CreatePartitions() error {
	ck.mu.Lock()
	// Note: Lock is manually released before Raft call to avoid deadlock

	if ck.cluster.PartitionMap == nil {
		ck.cluster.PartitionMap = &PartitionMap{
			Partitions: make(map[string]*Partition),
		}
	}

	partitionCount := ck.cluster.Config.PartitionCount
	replicationFactor := ck.cluster.Config.ReplicationFactor

	if len(ck.cluster.Nodes) < replicationFactor {
		return fmt.Errorf("not enough nodes (%d) for replication factor (%d)",
			len(ck.cluster.Nodes), replicationFactor)
	}

	// Create partitions
	for i := 0; i < partitionCount; i++ {
		partitionID := fmt.Sprintf("partition-%d", i)

		// Assign primary and replica nodes using consistent hashing
		primaryNode, replicaNodes := ck.assignNodesToPartition(partitionID, replicationFactor)

		partition := &Partition{
			ID:           partitionID,
			PrimaryNode:  primaryNode,
			ReplicaNodes: replicaNodes,
		}

		ck.cluster.PartitionMap.Partitions[partitionID] = partition
	}

	// Prepare partition data for Raft proposal
	partitionData := make(map[string]interface{})
	partitionData["partitions"] = make(map[string]interface{})

	partitions := partitionData["partitions"].(map[string]interface{})
	for id, partition := range ck.cluster.PartitionMap.Partitions {
		partitions[id] = map[string]interface{}{
			"id":            partition.ID,
			"primary_node":  partition.PrimaryNode,
			"replica_nodes": partition.ReplicaNodes,
		}
	}

	// Release lock before calling Raft (which may block)
	ck.mu.Unlock()

	// Use Raft consensus to replicate partition creation
	if err := ck.consensusManager.ProposeAction("create_partitions", partitionData); err != nil {
		return fmt.Errorf("failed to propose partition creation: %v", err)
	}

	// Save state after consensus
	return ck.saveState()
}

// assignNodesToPartition assigns nodes to a partition using consistent hashing
func (ck *ClusterKit) assignNodesToPartition(partitionID string, replicationFactor int) (string, []string) {
	if len(ck.cluster.Nodes) == 0 {
		return "", []string{}
	}

	// Sort nodes by their hash with the partition ID for consistent assignment
	type nodeScore struct {
		nodeID string
		score  uint32
	}

	scores := make([]nodeScore, 0, len(ck.cluster.Nodes))
	for _, node := range ck.cluster.Nodes {
		hash := hashKey(partitionID + node.ID)
		scores = append(scores, nodeScore{nodeID: node.ID, score: hash})
	}

	// Sort by score
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})

	// Primary is the first node
	primaryNode := scores[0].nodeID

	// Replicas are the next nodes
	replicaNodes := make([]string, 0, replicationFactor-1)
	for i := 1; i < len(scores) && i < replicationFactor; i++ {
		replicaNodes = append(replicaNodes, scores[i].nodeID)
	}

	return primaryNode, replicaNodes
}

// GetPartition determines which partition a key belongs to
func (ck *ClusterKit) GetPartition(key string) (*Partition, error) {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	if ck.cluster.PartitionMap == nil || len(ck.cluster.PartitionMap.Partitions) == 0 {
		return nil, fmt.Errorf("no partitions available")
	}

	// Hash the key to determine partition
	hash := hashKey(key)
	partitionIndex := int(hash) % ck.cluster.Config.PartitionCount
	partitionID := fmt.Sprintf("partition-%d", partitionIndex)

	partition, exists := ck.cluster.PartitionMap.Partitions[partitionID]
	if !exists {
		return nil, fmt.Errorf("partition %s not found", partitionID)
	}

	return partition, nil
}

// GetReplicaNodes returns all replica nodes for a given key
// This is a convenience function for developers to implement replication
func (ck *ClusterKit) GetReplicaNodes(key string) ([]Node, error) {
	partition, err := ck.GetPartition(key)
	if err != nil {
		return nil, err
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	replicaNodes := make([]Node, 0, len(partition.ReplicaNodes))
	for _, replicaNodeID := range partition.ReplicaNodes {
		node, err := ck.cluster.GetNodeByID(replicaNodeID)
		if err != nil {
			continue // Skip if node not found
		}
		replicaNodes = append(replicaNodes, *node)
	}

	return replicaNodes, nil
}

// GetPrimaryNode returns the primary node for a given key
func (ck *ClusterKit) GetPrimaryNode(key string) (*Node, error) {
	partition, err := ck.GetPartition(key)
	if err != nil {
		return nil, err
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	return ck.cluster.GetNodeByID(partition.PrimaryNode)
}

// GetAllNodesForKey returns primary + all replica nodes for a key
func (ck *ClusterKit) GetAllNodesForKey(key string) (primary *Node, replicas []Node, err error) {
	partition, err := ck.GetPartition(key)
	if err != nil {
		return nil, nil, err
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	// Get primary node
	primary, err = ck.cluster.GetNodeByID(partition.PrimaryNode)
	if err != nil {
		return nil, nil, err
	}

	// Get replica nodes
	replicas = make([]Node, 0, len(partition.ReplicaNodes))
	for _, replicaNodeID := range partition.ReplicaNodes {
		node, err := ck.cluster.GetNodeByID(replicaNodeID)
		if err != nil {
			continue // Skip if node not found
		}
		replicas = append(replicas, *node)
	}

	return primary, replicas, nil
}

// GetPartitionsForNode returns all partitions where the node is primary or replica
func (ck *ClusterKit) GetPartitionsForNode(nodeID string) []*Partition {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	partitions := make([]*Partition, 0)

	if ck.cluster.PartitionMap == nil {
		return partitions
	}

	for _, partition := range ck.cluster.PartitionMap.Partitions {
		if partition.PrimaryNode == nodeID {
			partitions = append(partitions, partition)
			continue
		}

		for _, replicaNode := range partition.ReplicaNodes {
			if replicaNode == nodeID {
				partitions = append(partitions, partition)
				break
			}
		}
	}

	return partitions
}

// RebalancePartitions redistributes partitions when nodes join or leave
func (ck *ClusterKit) RebalancePartitions() error {
	ck.mu.Lock()

	if ck.cluster.PartitionMap == nil {
		ck.mu.Unlock()
		return fmt.Errorf("partition map not initialized")
	}

	replicationFactor := ck.cluster.Config.ReplicationFactor
	if len(ck.cluster.Nodes) < replicationFactor {
		ck.mu.Unlock()
		return fmt.Errorf("not enough nodes for replication factor")
	}

	// Store old assignments before rebalancing
	oldAssignments := make(map[string]*Partition)
	for id, partition := range ck.cluster.PartitionMap.Partitions {
		oldAssignments[id] = &Partition{
			ID:           partition.ID,
			PrimaryNode:  partition.PrimaryNode,
			ReplicaNodes: append([]string{}, partition.ReplicaNodes...),
		}
	}

	// Reassign nodes to each partition
	for partitionID, partition := range ck.cluster.PartitionMap.Partitions {
		primaryNode, replicaNodes := ck.assignNodesToPartition(partitionID, replicationFactor)
		partition.PrimaryNode = primaryNode
		partition.ReplicaNodes = replicaNodes
	}

	// Propose rebalance through Raft consensus
	partitionData := make(map[string]interface{})
	partitionData["partitions"] = make(map[string]interface{})

	partitions := partitionData["partitions"].(map[string]interface{})
	for id, partition := range ck.cluster.PartitionMap.Partitions {
		partitions[id] = map[string]interface{}{
			"id":            partition.ID,
			"primary_node":  partition.PrimaryNode,
			"replica_nodes": partition.ReplicaNodes,
		}
	}

	ck.mu.Unlock()

	// Use Raft consensus
	if err := ck.consensusManager.ProposeAction("rebalance_partitions", partitionData); err != nil {
		return fmt.Errorf("failed to propose rebalance: %v", err)
	}

	// Trigger partition change hooks after rebalancing
	ck.notifyPartitionChanges(oldAssignments)

	// Also save locally
	ck.saveState()

	return nil
}

// triggerRebalance triggers automatic partition rebalancing
func (ck *ClusterKit) triggerRebalance() {
	fmt.Println("[REBALANCE] Triggering automatic partition rebalancing...")

	// Wait a bit for cluster to stabilize
	// time.Sleep(2 * time.Second)

	if err := ck.RebalancePartitions(); err != nil {
		fmt.Printf("[REBALANCE] ✗ Failed to rebalance: %v\n", err)
	} else {
		fmt.Println("[REBALANCE] ✓ Partition rebalancing complete")
	}
}

// notifyPartitionChanges compares old and new assignments and triggers hooks
func (ck *ClusterKit) notifyPartitionChanges(oldAssignments map[string]*Partition) {
	ck.mu.RLock()
	currentPartitions := ck.cluster.PartitionMap.Partitions
	cluster := ck.cluster
	ck.mu.RUnlock()

	// Use the hook manager to detect and notify changes
	ck.hookManager.checkPartitionChanges(currentPartitions, cluster)

	fmt.Println("[REBALANCE] ✓ Partition change notifications sent")
}

// ListPartitions returns all partitions
func (ck *ClusterKit) ListPartitions() []*Partition {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	partitions := make([]*Partition, 0)

	if ck.cluster.PartitionMap == nil {
		return partitions
	}

	for _, partition := range ck.cluster.PartitionMap.Partitions {
		partitions = append(partitions, partition)
	}

	return partitions
}

// PartitionStats returns statistics about partitions
type PartitionStats struct {
	TotalPartitions   int            `json:"total_partitions"`
	PartitionsPerNode map[string]int `json:"partitions_per_node"`
	ReplicasPerNode   map[string]int `json:"replicas_per_node"`
}

// GetPartitionStats returns partition distribution statistics
func (ck *ClusterKit) GetPartitionStats() *PartitionStats {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	stats := &PartitionStats{
		TotalPartitions:   0,
		PartitionsPerNode: make(map[string]int),
		ReplicasPerNode:   make(map[string]int),
	}

	if ck.cluster.PartitionMap == nil {
		return stats
	}

	stats.TotalPartitions = len(ck.cluster.PartitionMap.Partitions)

	for _, partition := range ck.cluster.PartitionMap.Partitions {
		// Count primary assignments
		stats.PartitionsPerNode[partition.PrimaryNode]++

		// Count replica assignments
		for _, replicaNode := range partition.ReplicaNodes {
			stats.ReplicasPerNode[replicaNode]++
		}
	}

	return stats
}

// hashKey generates a consistent hash for a key
func hashKey(key string) uint32 {
	hash := md5.Sum([]byte(key))
	return binary.BigEndian.Uint32(hash[:4])
}

// PartitionManager provides thread-safe partition operations
type PartitionManager struct {
	partitions map[string]*Partition
	mu         sync.RWMutex
}

// NewPartitionManager creates a new partition manager
func NewPartitionManager() *PartitionManager {
	return &PartitionManager{
		partitions: make(map[string]*Partition),
	}
}

// Add adds a partition
func (pm *PartitionManager) Add(partition *Partition) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.partitions[partition.ID] = partition
}

// Get retrieves a partition
func (pm *PartitionManager) Get(partitionID string) (*Partition, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	partition, exists := pm.partitions[partitionID]
	return partition, exists
}

// Remove removes a partition
func (pm *PartitionManager) Remove(partitionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.partitions, partitionID)
}

// List returns all partitions
func (pm *PartitionManager) List() []*Partition {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	partitions := make([]*Partition, 0, len(pm.partitions))
	for _, partition := range pm.partitions {
		partitions = append(partitions, partition)
	}
	return partitions
}
