package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	clusterkit "github.com/skshohagmiah/clusterkit"
)

func main() {
	// Get node configuration from environment or use defaults
	nodeID := getEnv("NODE_ID", "node-1")
	nodeName := getEnv("NODE_NAME", "Server-1")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	raftAddr := getEnv("RAFT_ADDR", "127.0.0.1:9001")
	dataDir := getEnv("DATA_DIR", "./data")

	// Join address - address of any existing node (empty for first node)
	joinAddr := getEnv("JOIN_ADDR", "")

	// Bootstrap flag - set to true for the first node
	bootstrap := getEnv("BOOTSTRAP", "false") == "true"

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:       nodeID,
		NodeName:     nodeName,
		HTTPAddr:     httpAddr,
		RaftAddr:     raftAddr,
		JoinAddr:     joinAddr,
		Bootstrap:    bootstrap,
		DataDir:      dataDir,
		SyncInterval: 5 * time.Second,
		Config: &clusterkit.Config{
			ClusterName:       "my-app-cluster",
			PartitionCount:    16,
			ReplicationFactor: 3,
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize ClusterKit: %v", err)
	}

	// Start ClusterKit
	if err := ck.Start(); err != nil {
		log.Fatalf("Failed to start ClusterKit: %v", err)
	}

	fmt.Printf("Application started with ClusterKit\n")
	fmt.Printf("Node ID: %s\n", nodeID)
	fmt.Printf("HTTP Address: %s\n", httpAddr)
	fmt.Printf("Data Directory: %s\n", dataDir)

	// Your application logic here
	go runApplication(ck)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	if err := ck.Stop(); err != nil {
		log.Printf("Error stopping ClusterKit: %v", err)
	}
}

func runApplication(ck *clusterkit.ClusterKit) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cluster := ck.GetCluster()
		fmt.Printf("\n=== Cluster Status ===\n")
		fmt.Printf("Cluster: %s\n", cluster.Name)
		fmt.Printf("Total Nodes: %d\n", len(cluster.Nodes))
		for _, node := range cluster.Nodes {
			fmt.Printf("  - %s (%s) at %s [%s]\n", node.Name, node.ID, node.IP, node.Status)
		}
		fmt.Printf("====================\n\n")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
