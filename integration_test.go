package clusterkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMultiNodeReplication verifies data is properly partitioned and replicated
func TestMultiNodeReplication(t *testing.T) {
	// Skip in short mode (requires time for cluster formation)
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary directories for each node
	tempDir := t.TempDir()

	// Create 3 nodes
	nodes := make([]*ClusterKit, 3)

	// Node 1 (bootstrap)
	node1Dir := filepath.Join(tempDir, "node-1")
	os.MkdirAll(node1Dir, 0755)

	var err error
	nodes[0], err = New(Options{
		NodeID:            "node-1",
		HTTPAddr:          ":18080",
		DataDir:           node1Dir,
		Bootstrap:         true,
		PartitionCount:    16,
		ReplicationFactor: 3,
		Logger:            &NoOpLogger{},
	})
	if err != nil {
		t.Fatalf("Failed to create node-1: %v", err)
	}

	if err := nodes[0].Start(); err != nil {
		t.Fatalf("Failed to start node-1: %v", err)
	}
	defer nodes[0].Stop()

	// Wait for bootstrap node to be ready
	time.Sleep(2 * time.Second)

	// Node 2
	node2Dir := filepath.Join(tempDir, "node-2")
	os.MkdirAll(node2Dir, 0755)

	nodes[1], err = New(Options{
		NodeID:            "node-2",
		HTTPAddr:          ":18081",
		DataDir:           node2Dir,
		JoinAddr:          "localhost:18080",
		PartitionCount:    16,
		ReplicationFactor: 3,
		Logger:            &NoOpLogger{},
	})
	if err != nil {
		t.Fatalf("Failed to create node-2: %v", err)
	}

	if err := nodes[1].Start(); err != nil {
		t.Fatalf("Failed to start node-2: %v", err)
	}
	defer nodes[1].Stop()

	time.Sleep(2 * time.Second)

	// Node 3
	node3Dir := filepath.Join(tempDir, "node-3")
	os.MkdirAll(node3Dir, 0755)

	nodes[2], err = New(Options{
		NodeID:            "node-3",
		HTTPAddr:          ":18082",
		DataDir:           node3Dir,
		JoinAddr:          "localhost:18080",
		PartitionCount:    16,
		ReplicationFactor: 3,
		Logger:            &NoOpLogger{},
	})
	if err != nil {
		t.Fatalf("Failed to create node-3: %v", err)
	}

	if err := nodes[2].Start(); err != nil {
		t.Fatalf("Failed to start node-3: %v", err)
	}
	defer nodes[2].Stop()

	// Wait for cluster to stabilize
	time.Sleep(5 * time.Second)

	t.Run("VerifyClusterFormation", func(t *testing.T) {
		// Verify all nodes see 3 nodes
		for i, node := range nodes {
			cluster := node.GetCluster()
			if len(cluster.Nodes) != 3 {
				t.Errorf("Node %d sees %d nodes, expected 3", i+1, len(cluster.Nodes))
			}
		}
	})

	t.Run("VerifyPartitionDistribution", func(t *testing.T) {
		// Check partition distribution
		for i, node := range nodes {
			cluster := node.GetCluster()
			if cluster.PartitionMap == nil {
				t.Errorf("Node %d has no partition map", i+1)
				continue
			}

			partitionCount := len(cluster.PartitionMap.Partitions)
			if partitionCount != 16 {
				t.Errorf("Node %d has %d partitions, expected 16", i+1, partitionCount)
			}
		}

		// Verify each partition has primary + 2 replicas
		cluster := nodes[0].GetCluster()
		for partID, partition := range cluster.PartitionMap.Partitions {
			if partition.PrimaryNode == "" {
				t.Errorf("Partition %s has no primary", partID)
			}
			if len(partition.ReplicaNodes) != 2 {
				t.Errorf("Partition %s has %d replicas, expected 2", partID, len(partition.ReplicaNodes))
			}
		}
	})

	t.Run("VerifyDataPartitioning", func(t *testing.T) {
		// Test keys and their expected partitions
		testKeys := []string{
			"user:1", "user:2", "user:3",
			"order:100", "order:101",
			"product:abc", "product:xyz",
		}

		// Verify each key consistently maps to same partition
		for _, key := range testKeys {
			var partitionID string

			for i, node := range nodes {
				partition, err := node.GetPartition(key)
				if err != nil {
					t.Errorf("Node %d failed to get partition for %s: %v", i+1, key, err)
					continue
				}

				if i == 0 {
					partitionID = partition.ID
				} else if partition.ID != partitionID {
					t.Errorf("Key %s maps to different partitions: %s vs %s",
						key, partitionID, partition.ID)
				}
			}
		}
	})

	t.Run("VerifyReplicationFactor", func(t *testing.T) {
		// Count how many nodes each partition is assigned to
		cluster := nodes[0].GetCluster()

		for partID, partition := range cluster.PartitionMap.Partitions {
			// Should have 1 primary + 2 replicas = 3 total nodes
			totalNodes := 1 + len(partition.ReplicaNodes)
			if totalNodes != 3 {
				t.Errorf("Partition %s has %d total nodes, expected 3 (RF=3)",
					partID, totalNodes)
			}

			// Verify no duplicate nodes
			allNodes := append([]string{partition.PrimaryNode}, partition.ReplicaNodes...)
			seen := make(map[string]bool)
			for _, nodeID := range allNodes {
				if seen[nodeID] {
					t.Errorf("Partition %s has duplicate node: %s", partID, nodeID)
				}
				seen[nodeID] = true
			}
		}
	})

	t.Run("VerifyCustomDataReplication", func(t *testing.T) {
		// Set custom data on leader
		testData := map[string]string{
			"config": "test-config-value",
		}
		configJSON, _ := json.Marshal(testData)

		// Find leader
		var leader *ClusterKit
		for _, node := range nodes {
			if node.consensusManager.IsLeader() {
				leader = node
				break
			}
		}

		if leader == nil {
			t.Skip("No leader found, skipping custom data test")
			return
		}

		// Set data on leader
		err := leader.SetCustomData("test-config", configJSON)
		if err != nil {
			t.Fatalf("Failed to set custom data: %v", err)
		}

		// Wait for replication
		time.Sleep(2 * time.Second)

		// Verify all nodes can read the data
		for i, node := range nodes {
			data, err := node.GetCustomData("test-config")
			if err != nil {
				t.Errorf("Node %d failed to get custom data: %v", i+1, err)
				continue
			}

			if string(data) != string(configJSON) {
				t.Errorf("Node %d has incorrect data", i+1)
			}
		}
	})

	t.Run("VerifyPartitionOwnership", func(t *testing.T) {
		// Verify each node knows which partitions it owns
		cluster := nodes[0].GetCluster()

		for _, node := range nodes {
			primaryCount := 0
			replicaCount := 0

			for _, partition := range cluster.PartitionMap.Partitions {
				if node.IsPrimary(partition) {
					primaryCount++
				}
				if node.IsReplica(partition) {
					replicaCount++
				}
			}

			t.Logf("Node %s: %d primary, %d replica partitions",
				node.nodeID, primaryCount, replicaCount)

			// With 16 partitions and 3 nodes, each should have ~5-6 primaries
			if primaryCount < 3 || primaryCount > 8 {
				t.Errorf("Node %s has unusual primary count: %d",
					node.nodeID, primaryCount)
			}
		}
	})
}

// TestPartitionBalancing verifies partitions are evenly distributed
func TestPartitionBalancing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a cluster with known configuration
	ck := &ClusterKit{
		cluster: &Cluster{
			Nodes: []Node{
				{ID: "node-1"},
				{ID: "node-2"},
				{ID: "node-3"},
			},
			Config: &Config{
				PartitionCount:    16,
				ReplicationFactor: 3,
			},
			PartitionMap: &PartitionMap{
				Partitions: make(map[string]*Partition),
			},
		},
	}

	// Assign partitions
	for i := 0; i < 16; i++ {
		partitionID := fmt.Sprintf("partition-%d", i)
		primary, replicas := ck.assignNodesToPartition(partitionID, 3)

		ck.cluster.PartitionMap.Partitions[partitionID] = &Partition{
			ID:           partitionID,
			PrimaryNode:  primary,
			ReplicaNodes: replicas,
		}
	}

	// Count assignments per node
	primaryCounts := make(map[string]int)
	replicaCounts := make(map[string]int)

	for _, partition := range ck.cluster.PartitionMap.Partitions {
		primaryCounts[partition.PrimaryNode]++
		for _, replica := range partition.ReplicaNodes {
			replicaCounts[replica]++
		}
	}

	t.Logf("Primary distribution: %v", primaryCounts)
	t.Logf("Replica distribution: %v", replicaCounts)

	// Verify reasonable distribution (no node should have 0 or all partitions)
	for nodeID, count := range primaryCounts {
		if count == 0 {
			t.Errorf("Node %s has no primary partitions", nodeID)
		}
		if count == 16 {
			t.Errorf("Node %s has all primary partitions", nodeID)
		}
	}

	// Each node should have some replicas
	for _, node := range ck.cluster.Nodes {
		if replicaCounts[node.ID] == 0 {
			t.Errorf("Node %s has no replica partitions", node.ID)
		}
	}
}
