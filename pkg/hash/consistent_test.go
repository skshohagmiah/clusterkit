package hash

import (
	"fmt"
	"testing"
)

func TestRingBasicOperations(t *testing.T) {
	ring := NewRing(3)

	// Test empty ring
	if ring.Size() != 0 {
		t.Errorf("Expected empty ring size to be 0, got %d", ring.Size())
	}

	if ring.Get("test") != "" {
		t.Errorf("Expected empty ring to return empty string, got %s", ring.Get("test"))
	}

	// Add nodes
	ring.Add("node1")
	ring.Add("node2")
	ring.Add("node3")

	if ring.Size() != 3 {
		t.Errorf("Expected ring size to be 3, got %d", ring.Size())
	}

	// Test key assignment
	key := "test-key"
	node := ring.Get(key)
	if node == "" {
		t.Errorf("Expected non-empty node for key %s", key)
	}

	// Same key should always return same node
	for i := 0; i < 10; i++ {
		if ring.Get(key) != node {
			t.Errorf("Key %s should always map to same node", key)
		}
	}

	// Remove node
	ring.Remove("node2")
	if ring.Size() != 2 {
		t.Errorf("Expected ring size to be 2 after removal, got %d", ring.Size())
	}

	// Add duplicate node (should be ignored)
	ring.Add("node1")
	if ring.Size() != 2 {
		t.Errorf("Expected ring size to remain 2 after duplicate add, got %d", ring.Size())
	}
}

func TestRingGetN(t *testing.T) {
	ring := NewRing(3)
	ring.Add("node1")
	ring.Add("node2")
	ring.Add("node3")

	// Test GetN
	nodes := ring.GetN("test-key", 2)
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}

	// Should not contain duplicates
	seen := make(map[string]bool)
	for _, node := range nodes {
		if seen[node] {
			t.Errorf("GetN returned duplicate node: %s", node)
		}
		seen[node] = true
	}

	// Test GetN with more replicas than nodes
	allNodes := ring.GetN("test-key", 5)
	if len(allNodes) != 3 {
		t.Errorf("Expected 3 nodes when asking for 5, got %d", len(allNodes))
	}
}

func TestPartitionRing(t *testing.T) {
	partitions := 8
	replicas := 2
	pr := NewPartitionRing(partitions, replicas)

	// Add nodes
	pr.AddNode("node1")
	pr.AddNode("node2")

	// Test partition assignment
	for i := 0; i < partitions; i++ {
		nodes := pr.GetNodes(i)
		if len(nodes) == 0 {
			t.Errorf("Partition %d should have assigned nodes", i)
		}
		if len(nodes) > replicas {
			t.Errorf("Partition %d has more nodes (%d) than replicas (%d)", i, len(nodes), replicas)
		}
	}

	// Test key to partition mapping
	key := "test-key"
	partition1 := pr.GetPartition(key)
	partition2 := pr.GetPartition(key)
	if partition1 != partition2 {
		t.Errorf("Same key should always map to same partition")
	}

	if partition1 < 0 || partition1 >= partitions {
		t.Errorf("Partition %d is out of range [0, %d)", partition1, partitions)
	}

	// Test leader assignment
	leader := pr.GetLeader(partition1)
	if leader == "" {
		t.Errorf("Partition %d should have a leader", partition1)
	}

	// Leader should be first in the node list
	nodes := pr.GetNodes(partition1)
	if len(nodes) > 0 && nodes[0] != leader {
		t.Errorf("Leader should be first node in partition assignment")
	}
}

func TestPartitionRingRebalancing(t *testing.T) {
	partitions := 4
	replicas := 2
	pr := NewPartitionRing(partitions, replicas)

	// Start with one node
	pr.AddNode("node1")
	
	// All partitions should be assigned to node1
	for i := 0; i < partitions; i++ {
		nodes := pr.GetNodes(i)
		if len(nodes) != 1 || nodes[0] != "node1" {
			t.Errorf("With single node, partition %d should be assigned only to node1", i)
		}
	}

	// Add second node
	pr.AddNode("node2")

	// Now partitions should be distributed
	node1Count := 0
	node2Count := 0
	
	for i := 0; i < partitions; i++ {
		nodes := pr.GetNodes(i)
		for _, node := range nodes {
			if node == "node1" {
				node1Count++
			} else if node == "node2" {
				node2Count++
			}
		}
	}

	// Both nodes should have some partitions
	if node1Count == 0 || node2Count == 0 {
		t.Errorf("Both nodes should have partition assignments after rebalancing")
	}

	// Remove a node
	pr.RemoveNode("node1")

	// All partitions should now be assigned to node2
	for i := 0; i < partitions; i++ {
		nodes := pr.GetNodes(i)
		if len(nodes) != 1 || nodes[0] != "node2" {
			t.Errorf("After removing node1, partition %d should be assigned only to node2", i)
		}
	}
}

func TestConsistentDistribution(t *testing.T) {
	ring := NewRing(150) // More virtual nodes for better distribution
	
	// Add nodes
	nodeCount := 5
	for i := 0; i < nodeCount; i++ {
		ring.Add(fmt.Sprintf("node%d", i))
	}

	// Test distribution with many keys
	keyCount := 1000
	distribution := make(map[string]int)

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("key%d", i)
		node := ring.Get(key)
		distribution[node]++
	}

	// Check that distribution is reasonably even
	expectedPerNode := keyCount / nodeCount
	tolerance := expectedPerNode / 2 // 50% tolerance

	for node, count := range distribution {
		if count < expectedPerNode-tolerance || count > expectedPerNode+tolerance {
			t.Logf("Node %s has %d keys (expected ~%d)", node, count, expectedPerNode)
		}
	}

	// All nodes should have some keys
	if len(distribution) != nodeCount {
		t.Errorf("Expected %d nodes in distribution, got %d", nodeCount, len(distribution))
	}
}
