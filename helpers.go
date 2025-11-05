package clusterkit

import "context"

// ============================================
// SIMPLE API - Clean and Intuitive
// ============================================

// GetNodes returns all nodes (primary + replicas) for a partition
func (ck *ClusterKit) GetNodes(partition *Partition) []Node {
	if partition == nil {
		return []Node{}
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	var nodes []Node

	// Add primary node
	for _, node := range ck.cluster.Nodes {
		if node.ID == partition.PrimaryNode {
			nodes = append(nodes, node)
			break
		}
	}

	// Add replica nodes
	for _, replicaID := range partition.ReplicaNodes {
		for _, node := range ck.cluster.Nodes {
			if node.ID == replicaID {
				nodes = append(nodes, node)
				break
			}
		}
	}

	return nodes
}

// GetPrimary returns the primary node for a partition
func (ck *ClusterKit) GetPrimary(partition *Partition) *Node {
	if partition == nil {
		return nil
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	for _, node := range ck.cluster.Nodes {
		if node.ID == partition.PrimaryNode {
			nodeCopy := node
			return &nodeCopy
		}
	}

	return nil
}

// GetReplicas returns the replica nodes for a partition
func (ck *ClusterKit) GetReplicas(partition *Partition) []Node {
	if partition == nil {
		return []Node{}
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	var replicas []Node
	for _, replicaID := range partition.ReplicaNodes {
		for _, node := range ck.cluster.Nodes {
			if node.ID == replicaID {
				replicas = append(replicas, node)
				break
			}
		}
	}

	return replicas
}

// GetMyNodeID returns the current node's ID
func (ck *ClusterKit) GetMyNodeID() string {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	
	if len(ck.cluster.Nodes) > 0 {
		return ck.cluster.Nodes[0].ID
	}
	return ""
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

// GetPartitionWithContext returns partition for a key with context support
func (ck *ClusterKit) GetPartitionWithContext(ctx context.Context, key string) (*Partition, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return ck.GetPartition(key)
	}
}
