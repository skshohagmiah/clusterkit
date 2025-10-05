package main

import (
	"fmt"
	"log"
	"os"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("ClusterKit - Distributed Clustering Made Simple")
		fmt.Println("")
		fmt.Println("Usage:")
		fmt.Println("  go run main.go <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  example    Run the KV store example")
		fmt.Println("  version    Show version information")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  go run examples/kv-store/main.go")
		fmt.Println("")
		fmt.Println("Environment Variables:")
		fmt.Println("  NODE_ID         - Unique node identifier (default: kv-node-1)")
		fmt.Println("  ADVERTISE_ADDR  - Address other nodes connect to (default: localhost:9000)")
		fmt.Println("  HTTP_PORT       - HTTP port (default: 8080)")
		fmt.Println("  GRPC_PORT       - gRPC port (default: 9000)")
		fmt.Println("  ETCD_ENDPOINTS  - etcd endpoints (default: http://localhost:2379)")
		return
	}

	command := os.Args[1]

	switch command {
	case "version":
		showVersion()
	case "example":
		runExample()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

func showVersion() {
	fmt.Println("ClusterKit v1.0.0")
	fmt.Println("Distributed Clustering Made Simple")
	fmt.Println("Built with Go")
}

func runExample() {
	fmt.Println("To run the KV store example:")
	fmt.Println("  cd examples/kv-store")
	fmt.Println("  go run main.go")
	fmt.Println("")
	fmt.Println("Make sure etcd is running:")
	fmt.Println("  docker run -d -p 2379:2379 -p 2380:2380 \\")
	fmt.Println("    --name etcd quay.io/coreos/etcd:v3.5.10 \\")
	fmt.Println("    /usr/local/bin/etcd \\")
	fmt.Println("    --data-dir=/etcd-data \\")
	fmt.Println("    --listen-client-urls http://0.0.0.0:2379 \\")
	fmt.Println("    --advertise-client-urls http://0.0.0.0:2379 \\")
	fmt.Println("    --listen-peer-urls http://0.0.0.0:2380 \\")
	fmt.Println("    --initial-advertise-peer-urls http://0.0.0.0:2380 \\")
	fmt.Println("    --initial-cluster default=http://0.0.0.0:2380 \\")
	fmt.Println("    --initial-cluster-token tkn \\")
	fmt.Println("    --initial-cluster-state new")
}

// Simple example of using ClusterKit
func simpleExample() {
	config := &clusterkit.Config{
		NodeID:        "example-node",
		AdvertiseAddr: "localhost:9000",
		HTTPPort:      8080,
		GRPCPort:      9000,
		Partitions:    16,
		ReplicaFactor: 2,
	}

	ck, err := clusterkit.Join(config)
	if err != nil {
		log.Fatalf("Failed to join cluster: %v", err)
	}
	defer ck.Leave()

	fmt.Printf("Joined cluster as node: %s\n", ck.NodeID())
	fmt.Printf("Cluster stats: %+v\n", ck.GetStats())
}