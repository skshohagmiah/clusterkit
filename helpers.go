package clusterkit

import "fmt"

// GetNodesForKey returns all nodes (primary + replicas) that should store this key
// This is the main method developers should use - no need to check if you're primary!
func (ck *ClusterKit) GetNodesForKey(key string) ([]Node, error) {
	partition, err := ck.GetPartitionForKey(key)
	if err != nil {
		return nil, err
	}

	var nodes []Node

	// Add primary node
	primaryNode, err := ck.cluster.GetNodeByID(partition.PrimaryNode)
	if err != nil {
		return nil, fmt.Errorf("primary node not found: %v", err)
	}
	nodes = append(nodes, *primaryNode)

	// Add replica nodes
	for _, replicaID := range partition.ReplicaNodes {
		replicaNode, err := ck.cluster.GetNodeByID(replicaID)
		if err != nil {
			continue // Skip if replica not found
		}
		nodes = append(nodes, *replicaNode)
	}

	return nodes, nil
}

// GetPrimaryNodeForKey returns the primary node for a key
func (ck *ClusterKit) GetPrimaryNodeForKey(key string) (*Node, error) {
	partition, err := ck.GetPartitionForKey(key)
	if err != nil {
		return nil, err
	}

	return ck.cluster.GetNodeByID(partition.PrimaryNode)
}

// GetReplicaNodesForKey returns all replica nodes for a key
func (ck *ClusterKit) GetReplicaNodesForKey(key string) ([]Node, error) {
	partition, err := ck.GetPartitionForKey(key)
	if err != nil {
		return nil, err
	}

	var replicas []Node
	for _, replicaID := range partition.ReplicaNodes {
		node, err := ck.cluster.GetNodeByID(replicaID)
		if err != nil {
			continue
		}
		replicas = append(replicas, *node)
	}

	return replicas, nil
}

// AmIResponsibleFor checks if the current node should handle this key
// Returns: (shouldHandle bool, role string, allNodes []Node)
// role: "primary", "replica", or ""
func (ck *ClusterKit) AmIResponsibleFor(key string) (bool, string, []Node, error) {
	partition, err := ck.GetPartitionForKey(key)
	if err != nil {
		return false, "", nil, err
	}

	myNodeID := ck.cluster.ID
	
	// Get all nodes for this key
	allNodes, err := ck.GetNodesForKey(key)
	if err != nil {
		return false, "", nil, err
	}

	// Check if primary
	if partition.PrimaryNode == myNodeID {
		return true, "primary", allNodes, nil
	}

	// Check if replica
	for _, replicaID := range partition.ReplicaNodes {
		if replicaID == myNodeID {
			return true, "replica", allNodes, nil
		}
	}

	return false, "", allNodes, nil
}

// GetMyNodeInfo returns information about the current node
func (ck *ClusterKit) GetMyNodeInfo() Node {
	if len(ck.cluster.Nodes) > 0 {
		return ck.cluster.Nodes[0] // First node is always self
	}
	return Node{
		ID:     ck.cluster.ID,
		IP:     ck.httpAddr,
		Status: "active",
	}
}
