package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/skshohagmiah/clusterkit"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <node-id>")
	}

	nodeID := os.Args[1]

	// Parse node number for port calculation
	var nodeNum int
	fmt.Sscanf(nodeID, "node-%d", &nodeNum)
	if nodeNum == 0 {
		nodeNum = 1
	}

	httpPort := 8080 + nodeNum - 1
	httpAddr := fmt.Sprintf(":%d", httpPort)
	dataDir := fmt.Sprintf("./data/%s", nodeID)

	// Determine if this is the bootstrap node
	isBootstrap := nodeNum == 1
	var joinAddr string
	if !isBootstrap {
		joinAddr = "localhost:8080" // Join the first node
	}

	// Create ClusterKit instance
	ck, err := clusterkit.New(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          httpAddr,
		DataDir:           dataDir,
		Bootstrap:         isBootstrap,
		JoinAddr:          joinAddr,
		PartitionCount:    16,
		ReplicationFactor: 3,
		HealthCheck: clusterkit.HealthCheckConfig{
			Enabled:          true,
			Interval:         5 * time.Second,
			Timeout:          2 * time.Second,
			FailureThreshold: 3,
		},
	})
	if err != nil {
		log.Fatalf("Failed to create ClusterKit: %v", err)
	}

	// Start ClusterKit
	if err := ck.Start(); err != nil {
		log.Fatalf("Failed to start ClusterKit: %v", err)
	}
	defer ck.Stop()

	log.Printf("✅ Node %s started on %s", nodeID, httpAddr)

	// Wait for cluster to stabilize
	time.Sleep(5 * time.Second)

	// Bootstrap node: Set custom data
	if isBootstrap {
		log.Println("\n🔧 Bootstrap node: Setting custom data...")

		// Example 1: Feature flags
		featureFlags := map[string]bool{
			"new_ui":       true,
			"beta_feature": false,
			"dark_mode":    true,
		}
		flagsJSON, _ := json.Marshal(featureFlags)

		if err := ck.SetCustomData("feature_flags", flagsJSON); err != nil {
			log.Printf("❌ Failed to set feature flags: %v", err)
		} else {
			log.Printf("✅ Set feature flags: %v", featureFlags)
		}

		// Example 2: Application config
		appConfig := map[string]interface{}{
			"max_connections": 100,
			"timeout_seconds": 30,
			"api_version":     "v2",
		}
		configJSON, _ := json.Marshal(appConfig)

		if err := ck.SetCustomData("app_config", configJSON); err != nil {
			log.Printf("❌ Failed to set app config: %v", err)
		} else {
			log.Printf("✅ Set app config: %v", appConfig)
		}

		// Example 3: Global counter
		counter := map[string]int{"request_count": 0}
		counterJSON, _ := json.Marshal(counter)

		if err := ck.SetCustomData("global_counter", counterJSON); err != nil {
			log.Printf("❌ Failed to set counter: %v", err)
		} else {
			log.Printf("✅ Set global counter: %v", counter)
		}

		log.Println("✅ Custom data set successfully!")
	}

	// All nodes: Read custom data after a delay
	time.Sleep(8 * time.Second)

	log.Println("\n📖 Reading custom data from cluster...")

	// List all keys
	keys := ck.ListCustomDataKeys()
	log.Printf("📋 Available custom data keys: %v", keys)

	// Read feature flags
	if flagsData, err := ck.GetCustomData("feature_flags"); err == nil {
		var flags map[string]bool
		json.Unmarshal(flagsData, &flags)
		log.Printf("🚩 Feature flags: %v", flags)

		// Use feature flags in application logic
		if flags["new_ui"] {
			log.Println("   ➡️  New UI is ENABLED")
		}
		if flags["dark_mode"] {
			log.Println("   ➡️  Dark mode is ENABLED")
		}
	} else {
		log.Printf("⚠️  Feature flags not found: %v", err)
	}

	// Read app config
	if configData, err := ck.GetCustomData("app_config"); err == nil {
		var config map[string]interface{}
		json.Unmarshal(configData, &config)
		log.Printf("⚙️  App config: %v", config)
	} else {
		log.Printf("⚠️  App config not found: %v", err)
	}

	// Read counter
	if counterData, err := ck.GetCustomData("global_counter"); err == nil {
		var counter map[string]int
		json.Unmarshal(counterData, &counter)
		log.Printf("🔢 Global counter: %v", counter)
	} else {
		log.Printf("⚠️  Global counter not found: %v", err)
	}

	// Demonstrate data consistency across nodes
	log.Printf("\n✅ Node %s successfully read custom data from cluster!", nodeID)
	log.Println("   All nodes have access to the same custom data via Raft consensus")

	// Keep running
	log.Println("\n🔄 Node running... Press Ctrl+C to stop")
	select {}
}
