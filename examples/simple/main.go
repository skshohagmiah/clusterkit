package main

import (
	"fmt"
	"log"
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

func main() {
	fmt.Println("ClusterKit Simple Example")
	fmt.Println("=========================")

	// Create a simple configuration
	config := &clusterkit.Config{
		NodeID:        "simple-node-1",
		AdvertiseAddr: "localhost:9000",
		HTTPPort:      8080,
		GRPCPort:      9000,
		Partitions:    8,
		ReplicaFactor: 1,
		EtcdEndpoints: []string{"http://localhost:2379"},

		// Simple hooks for demonstration
		OnPartitionAssigned: func(partition int, previousOwner *clusterkit.Node) {
			fmt.Printf("✅ Partition %d assigned to this node\n", partition)
		},
		OnPartitionUnassigned: func(partition int, newOwner *clusterkit.Node) {
			fmt.Printf("❌ Partition %d unassigned from this node\n", partition)
		},
		OnLeaderElected: func(partition int) {
			fmt.Printf("👑 Became leader for partition %d\n", partition)
		},
		OnLeaderLost: func(partition int) {
			fmt.Printf("💔 Lost leadership for partition %d\n", partition)
		},
	}

	fmt.Println("Joining cluster...")
	
	// Join the cluster
	ck, err := clusterkit.Join(config)
	if err != nil {
		log.Fatalf("Failed to join cluster: %v", err)
	}
	defer func() {
		fmt.Println("Leaving cluster...")
		if err := ck.Leave(); err != nil {
			log.Printf("Error leaving cluster: %v", err)
		}
		fmt.Println("Left cluster successfully")
	}()

	fmt.Printf("✅ Successfully joined cluster as node: %s\n", ck.NodeID())

	// Display cluster information
	stats := ck.GetStats()
	fmt.Printf("\nCluster Statistics:\n")
	fmt.Printf("  - Nodes: %d\n", stats.NodeCount)
	fmt.Printf("  - Partitions: %d\n", stats.PartitionCount)
	fmt.Printf("  - Local Partitions: %d\n", stats.LocalPartitions)
	fmt.Printf("  - Leader Partitions: %d\n", stats.LeaderPartitions)
	fmt.Printf("  - Replica Factor: %d\n", stats.ReplicaFactor)

	// Test partition routing
	fmt.Printf("\nTesting partition routing:\n")
	testKeys := []string{"user:1", "user:2", "order:123", "product:456", "session:abc"}
	
	for _, key := range testKeys {
		partition := ck.GetPartition(key)
		leader := ck.GetLeader(partition)
		isLeader := ck.IsLeader(partition)
		
		leaderInfo := "no leader"
		if leader != nil {
			leaderInfo = leader.ID
		}
		
		fmt.Printf("  - Key '%s' -> Partition %d (Leader: %s, IsLocal: %v)\n", 
			key, partition, leaderInfo, isLeader)
	}

	// Wait a bit to see cluster operations
	fmt.Printf("\nRunning for 10 seconds...\n")
	time.Sleep(10 * time.Second)

	fmt.Println("\nExample completed successfully!")
}
