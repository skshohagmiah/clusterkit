package clusterkit

// ============================================
// SIMPLE API - Clean and Intuitive
// ============================================

// GetNodes returns all nodes (primary + replicas) for a partition
// Time Complexity: O(R) where R = replication factor
func (ck *ClusterKit) GetNodes(partition *Partition) []Node {
	if partition == nil {
		return []Node{}
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	nodes := make([]Node, 0, 1+len(partition.ReplicaNodes))

	// Add primary node - O(1) map lookup
	if primaryNode, exists := ck.cluster.NodeMap[partition.PrimaryNode]; exists {
		nodes = append(nodes, *primaryNode)
	}

	// Add replica nodes - O(R) where R = replication factor
	for _, replicaID := range partition.ReplicaNodes {
		if replicaNode, exists := ck.cluster.NodeMap[replicaID]; exists {
			nodes = append(nodes, *replicaNode)
		}
	}

	return nodes
}

// GetPrimary returns the primary node for a partition
// Time Complexity: O(1)
func (ck *ClusterKit) GetPrimary(partition *Partition) *Node {
	if partition == nil {
		return nil
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	// O(1) map lookup
	if primaryNode, exists := ck.cluster.NodeMap[partition.PrimaryNode]; exists {
		nodeCopy := *primaryNode
		return &nodeCopy
	}

	return nil
}

// GetReplicas returns the replica nodes for a partition
// Time Complexity: O(R) where R = replication factor
func (ck *ClusterKit) GetReplicas(partition *Partition) []Node {
	if partition == nil {
		return []Node{}
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	replicas := make([]Node, 0, len(partition.ReplicaNodes))

	// O(R) - loop through replicas with O(1) map lookup each
	for _, replicaID := range partition.ReplicaNodes {
		if replicaNode, exists := ck.cluster.NodeMap[replicaID]; exists {
			replicas = append(replicas, *replicaNode)
		}
	}

	return replicas
}

// GetMyNodeID returns the current node's ID
func (ck *ClusterKit) GetMyNodeID() string {
	return ck.nodeID
}

// IsPrimary checks if the current node is the primary for a partition
func (ck *ClusterKit) IsPrimary(partition *Partition) bool {
	if partition == nil {
		return false
	}
	myNodeID := ck.GetMyNodeID()
	return partition.PrimaryNode == myNodeID
}

// IsReplica checks if the current node is a replica for a partition
func (ck *ClusterKit) IsReplica(partition *Partition) bool {
	if partition == nil {
		return false
	}
	myNodeID := ck.GetMyNodeID()
	for _, replicaID := range partition.ReplicaNodes {
		if replicaID == myNodeID {
			return true
		}
	}
	return false
}

// ============================================
// Context-Aware Versions (Optional)
// ============================================
