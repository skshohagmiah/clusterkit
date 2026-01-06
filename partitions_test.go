package clusterkit

import (
	"testing"
)

// TestHashKeyConsistency verifies hash function produces consistent results
func TestHashKeyConsistency(t *testing.T) {
	key := "test-key-123"

	// Hash same key multiple times
	hash1 := hashKey(key)
	hash2 := hashKey(key)
	hash3 := hashKey(key)

	if hash1 != hash2 || hash2 != hash3 {
		t.Errorf("Hash function not consistent: %d, %d, %d", hash1, hash2, hash3)
	}
}

// TestHashKeyDistribution verifies hash distributes keys reasonably
func TestHashKeyDistribution(t *testing.T) {
	partitionCount := 16
	keys := []string{
		"user:1", "user:2", "user:3", "user:4", "user:5",
		"order:1", "order:2", "order:3", "order:4", "order:5",
		"product:1", "product:2", "product:3", "product:4", "product:5",
	}

	distribution := make(map[int]int)

	for _, key := range keys {
		hash := hashKey(key)
		partition := int(hash) % partitionCount
		distribution[partition]++
	}

	// Check that keys are distributed (not all in one partition)
	if len(distribution) < 5 {
		t.Errorf("Poor hash distribution: only %d partitions used out of %d", len(distribution), partitionCount)
	}
}

// TestGetPartition verifies partition lookup
func TestGetPartition(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			Config: &Config{
				PartitionCount: 16,
			},
			PartitionMap: &PartitionMap{
				Partitions: make(map[string]*Partition),
			},
		},
	}

	// Create test partitions
	for i := 0; i < 16; i++ {
		partitionID := "partition-" + string(rune('0'+i))
		ck.cluster.PartitionMap.Partitions[partitionID] = &Partition{
			ID:          partitionID,
			PrimaryNode: "node-1",
		}
	}

	// Test partition lookup
	partition, err := ck.GetPartition("test-key")
	if err != nil {
		t.Fatalf("GetPartition failed: %v", err)
	}

	if partition == nil {
		t.Fatal("GetPartition returned nil partition")
	}

	// Verify same key always maps to same partition
	partition2, _ := ck.GetPartition("test-key")
	if partition.ID != partition2.ID {
		t.Errorf("Same key mapped to different partitions: %s vs %s", partition.ID, partition2.ID)
	}
}

// TestPartitionAssignment verifies node assignment logic
func TestPartitionAssignment(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			Nodes: []Node{
				{ID: "node-1"},
				{ID: "node-2"},
				{ID: "node-3"},
			},
		},
	}

	// Test assignment with replication factor 3
	primary, replicas := ck.assignNodesToPartition("partition-0", 3)

	if primary == "" {
		t.Error("Primary node not assigned")
	}

	if len(replicas) != 2 {
		t.Errorf("Expected 2 replicas, got %d", len(replicas))
	}

	// Verify no duplicates
	allNodes := append([]string{primary}, replicas...)
	seen := make(map[string]bool)
	for _, nodeID := range allNodes {
		if seen[nodeID] {
			t.Errorf("Duplicate node in assignment: %s", nodeID)
		}
		seen[nodeID] = true
	}
}

// TestIsPrimary verifies primary node check
func TestIsPrimary(t *testing.T) {
	ck := &ClusterKit{
		nodeID: "node-1",
	}

	partition := &Partition{
		ID:          "partition-0",
		PrimaryNode: "node-1",
	}

	if !ck.IsPrimary(partition) {
		t.Error("IsPrimary returned false for primary node")
	}

	partition.PrimaryNode = "node-2"
	if ck.IsPrimary(partition) {
		t.Error("IsPrimary returned true for non-primary node")
	}
}

// TestIsReplica verifies replica node check
func TestIsReplica(t *testing.T) {
	ck := &ClusterKit{
		nodeID: "node-2",
	}

	partition := &Partition{
		ID:           "partition-0",
		PrimaryNode:  "node-1",
		ReplicaNodes: []string{"node-2", "node-3"},
	}

	if !ck.IsReplica(partition) {
		t.Error("IsReplica returned false for replica node")
	}

	ck.nodeID = "node-4"
	if ck.IsReplica(partition) {
		t.Error("IsReplica returned true for non-replica node")
	}
}
