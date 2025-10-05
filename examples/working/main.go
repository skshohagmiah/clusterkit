package main

import (
	"fmt"
	"log"
	"os"

	"github.com/shohag/clusterkit/pkg/simple"
)

func main() {
	fmt.Println("🚀 ClusterKit Working Example")
	fmt.Println("==============================")

	// Create a simple ClusterKit instance
	config := &simple.Config{
		NodeID:        getEnv("NODE_ID", "working-node-1"),
		AdvertiseAddr: getEnv("ADVERTISE_ADDR", "localhost:9000"),
		Partitions:    16,
		ReplicaFactor: 2,
	}

	ck := simple.NewSimpleClusterKit(config)
	fmt.Printf("✅ Node '%s' created\n", ck.NodeID())

	// Simulate adding more nodes to the cluster
	ck.AddNode("node-2", "localhost:9001")
	ck.AddNode("node-3", "localhost:9002")
	fmt.Println("✅ Added additional nodes to cluster")

	// Show cluster information
	nodes := ck.GetNodes()
	fmt.Printf("📊 Cluster has %d nodes:\n", len(nodes))
	for _, node := range nodes {
		fmt.Printf("  - %s (%s)\n", node.ID, node.Addr)
	}

	// Demonstrate key routing
	fmt.Println("\n🎯 Key Routing Examples:")
	testKeys := []string{
		"user:alice",
		"user:bob", 
		"order:12345",
		"product:laptop",
		"session:abc123",
	}

	for _, key := range testKeys {
		partition := ck.GetPartition(key)
		leader := ck.GetLeader(partition)
		replicas := ck.GetReplicas(partition)
		
		fmt.Printf("  Key '%s':\n", key)
		fmt.Printf("    Partition: %d\n", partition)
		fmt.Printf("    Leader: %s\n", leader.ID)
		fmt.Printf("    Replicas: ")
		for i, replica := range replicas {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(replica.ID)
		}
		fmt.Println()
		
		if ck.IsLeader(partition) {
			fmt.Printf("    ✅ This node is the leader\n")
		} else {
			fmt.Printf("    ➡️  Forward to %s\n", leader.ID)
		}
		fmt.Println()
	}

	// Show cluster statistics
	stats := ck.GetStats()
	fmt.Println("📈 Cluster Statistics:")
	fmt.Printf("  Total Nodes: %v\n", stats["node_count"])
	fmt.Printf("  Total Partitions: %v\n", stats["partition_count"])
	fmt.Printf("  Replica Factor: %v\n", stats["replica_factor"])
	fmt.Printf("  Local Partitions: %v\n", stats["local_partitions"])
	fmt.Printf("  Leader Partitions: %v\n", stats["leader_partitions"])

	// Demonstrate partition distribution
	fmt.Println("\n📦 Partition Distribution:")
	partitionMap := make(map[string]int)
	
	for i := 0; i < 16; i++ {
		leader := ck.GetLeader(i)
		partitionMap[leader.ID]++
	}
	
	for nodeID, count := range partitionMap {
		fmt.Printf("  %s: %d partitions\n", nodeID, count)
	}

	// Simulate node failure
	fmt.Println("\n💥 Simulating node failure...")
	ck.RemoveNode("node-2")
	fmt.Println("✅ Removed node-2 from cluster")
	
	// Show updated distribution
	fmt.Println("\n📦 Updated Partition Distribution:")
	partitionMap = make(map[string]int)
	
	for i := 0; i < 16; i++ {
		leader := ck.GetLeader(i)
		if leader != nil {
			partitionMap[leader.ID]++
		}
	}
	
	for nodeID, count := range partitionMap {
		fmt.Printf("  %s: %d partitions\n", nodeID, count)
	}

	fmt.Println("\n✨ ClusterKit Working Example Complete!")
	fmt.Println("\nThis demonstrates:")
	fmt.Println("  ✅ Consistent hash-based partitioning")
	fmt.Println("  ✅ Leader election per partition")
	fmt.Println("  ✅ Replica assignment")
	fmt.Println("  ✅ Key routing to correct nodes")
	fmt.Println("  ✅ Automatic rebalancing on node changes")
	fmt.Println("  ✅ Cluster statistics and monitoring")

	log.Println("Example completed successfully!")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
