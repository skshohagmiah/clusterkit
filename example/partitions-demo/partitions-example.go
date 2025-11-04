package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skshohagmiah/clusterkit"
)

func main() {
	// Get configuration from environment
	nodeID := getEnv("NODE_ID", "node-1")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	knownNodes := getEnv("KNOWN_NODES", "")

	// Bootstrap flag - set to true for the first node
	bootstrap := getEnv("BOOTSTRAP", "false") == "true"

	// Initialize ClusterKit with partition configuration
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:       nodeID,
		NodeName:     fmt.Sprintf("Server-%s", nodeID),
		HTTPAddr:     httpAddr,
		JoinAddr:     knownNodes,
		Bootstrap:    bootstrap,
		DataDir:      fmt.Sprintf("./data/%s", nodeID),
		SyncInterval: 5 * time.Second,
		Config: &clusterkit.Config{
			ClusterName:       "partition-demo",
			PartitionCount:    16,  // 16 partitions
			ReplicationFactor: 2,   // Each partition replicated to 2 nodes
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize ClusterKit: %v", err)
	}

	// Start ClusterKit
	if err := ck.Start(); err != nil {
		log.Fatalf("Failed to start ClusterKit: %v", err)
	}

	fmt.Printf("✓ ClusterKit started on %s\n", httpAddr)
	fmt.Printf("✓ Node ID: %s\n", nodeID)

	// Wait for cluster to stabilize
	time.Sleep(3 * time.Second)

	// Create partitions (only first node should do this, or check if already created)
	cluster := ck.GetCluster()
	if len(cluster.Nodes) >= cluster.Config.ReplicationFactor {
		if len(ck.ListPartitions()) == 0 {
			fmt.Println("\n📦 Creating partitions...")
			if err := ck.CreatePartitions(); err != nil {
				log.Printf("Failed to create partitions: %v", err)
			} else {
				fmt.Println("✓ Partitions created successfully")
			}
		}
	}

	// Demonstrate partition usage
	go demonstratePartitions(ck)

	// Show partition stats periodically
	go showPartitionStats(ck)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	ck.Stop()
}

func demonstratePartitions(ck *clusterkit.ClusterKit) {
	time.Sleep(5 * time.Second) // Wait for partitions to be created

	// Example keys to partition
	keys := []string{
		"user:1001",
		"user:1002",
		"order:5001",
		"order:5002",
		"product:abc123",
		"product:xyz789",
	}

	fmt.Println("\n=== Partition Assignment Demo ===")
	for _, key := range keys {
		partition, err := ck.GetPartitionForKey(key)
		if err != nil {
			fmt.Printf("Error getting partition for %s: %v\n", key, err)
			continue
		}

		fmt.Printf("Key: %-20s → Partition: %-15s Primary: %-10s Replicas: %v\n",
			key, partition.ID, partition.PrimaryNode, partition.ReplicaNodes)
	}
	fmt.Println("================================\n")
}

func showPartitionStats(ck *clusterkit.ClusterKit) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := ck.GetPartitionStats()
		cluster := ck.GetCluster()

		fmt.Println("\n=== Cluster & Partition Status ===")
		fmt.Printf("Cluster: %s\n", cluster.Name)
		fmt.Printf("Total Nodes: %d\n", len(cluster.Nodes))
		fmt.Printf("Total Partitions: %d\n", stats.TotalPartitions)
		fmt.Println("\nNodes:")
		for _, node := range cluster.Nodes {
			primaryCount := stats.PartitionsPerNode[node.ID]
			replicaCount := stats.ReplicasPerNode[node.ID]
			fmt.Printf("  • %s (%s) - Primary: %d, Replicas: %d\n",
				node.Name, node.ID, primaryCount, replicaCount)
		}

		// Show which partitions this node is responsible for
		myPartitions := ck.GetPartitionsForNode(ck.GetCluster().ID)
		if len(myPartitions) > 0 {
			fmt.Printf("\nMy Partitions (%d):\n", len(myPartitions))
			for _, p := range myPartitions {
				role := "replica"
				if p.PrimaryNode == ck.GetCluster().ID {
					role = "primary"
				}
				fmt.Printf("  • %s [%s]\n", p.ID, role)
			}
		}
		fmt.Println("==================================\n")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
