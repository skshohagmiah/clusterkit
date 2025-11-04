# ClusterKit Quick Start Guide

This guide will help you get ClusterKit running in 5 minutes.

## Step 1: Install ClusterKit

```bash
go get github.com/skshohagmiah/clusterkit
```

## Step 2: Create Your Application

Create a new file `main.go`:

```go
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
    
    var knownNodesList []string
    if knownNodes != "" {
        knownNodesList = []string{knownNodes}
    }

    // Initialize ClusterKit
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:       nodeID,
        NodeName:     fmt.Sprintf("Server-%s", nodeID),
        HTTPAddr:     httpAddr,
        KnownNodes:   knownNodesList,
        DataDir:      fmt.Sprintf("./data/%s", nodeID),
        SyncInterval: 5 * time.Second,
        Config: &clusterkit.Config{
            ClusterName:       "my-cluster",
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

    fmt.Printf("✓ ClusterKit started on %s\n", httpAddr)
    fmt.Printf("✓ Node ID: %s\n", nodeID)

    // Your application logic here
    go runApp(ck)

    // Wait for shutdown signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    fmt.Println("\nShutting down...")
    ck.Stop()
}

func runApp(ck *clusterkit.ClusterKit) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        cluster := ck.GetCluster()
        fmt.Printf("\n=== Cluster Status ===\n")
        fmt.Printf("Nodes in cluster: %d\n", len(cluster.Nodes))
        for _, node := range cluster.Nodes {
            fmt.Printf("  • %s at %s [%s]\n", node.Name, node.IP, node.Status)
        }
        fmt.Printf("=====================\n\n")
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

## Step 3: Run Multiple Instances

### Terminal 1 - Start Node 1 (First Node)
```bash
NODE_ID=node-1 HTTP_ADDR=:8080 go run main.go
```

### Terminal 2 - Start Node 2 (Joins Node 1)
```bash
NODE_ID=node-2 HTTP_ADDR=:8081 KNOWN_NODES=localhost:8080 go run main.go
```

### Terminal 3 - Start Node 3 (Joins the Cluster)
```bash
NODE_ID=node-3 HTTP_ADDR=:8082 KNOWN_NODES=localhost:8080 go run main.go
```

## Step 4: Verify the Cluster

You should see output like this on each node:

```
✓ ClusterKit started on :8080
✓ Node ID: node-1
ClusterKit started on :8080

=== Cluster Status ===
Nodes in cluster: 3
  • Server-node-1 at :8080 [active]
  • Server-node-2 at :8081 [active]
  • Server-node-3 at :8082 [active]
=====================
```

## Step 5: Test the HTTP API

Check cluster health:
```bash
curl http://localhost:8080/health
```

Get cluster state:
```bash
curl http://localhost:8080/cluster
```

## What's Happening?

1. **Node 1** starts and creates a new cluster
2. **Node 2** connects to Node 1 and joins the cluster
3. **Node 3** connects to Node 1 and joins the cluster
4. All nodes sync state every 5 seconds via HTTP
5. Each node saves its state to disk in `./data/node-X/`
6. If a node restarts, it loads its previous state from disk

## Data Persistence

Check the data directories:
```bash
ls -la data/node-1/
# cluster-state.json  - Current cluster state
# wal.log            - Write-ahead log
```

View the cluster state:
```bash
cat data/node-1/cluster-state.json | jq
```

## Next Steps

- **Add your application logic** in the `runApp()` function
- **Use the cluster state** to coordinate work across nodes
- **Implement partitioning** to distribute data
- **Add health checks** to detect failed nodes
- **Scale horizontally** by adding more nodes

## Common Issues

### Port Already in Use
Change the `HTTP_ADDR` to a different port:
```bash
HTTP_ADDR=:9000 go run main.go
```

### Cannot Connect to Known Node
Make sure the first node is running before starting subsequent nodes.

### State Not Syncing
Check that:
- All nodes are on the same network
- Firewalls allow HTTP traffic
- Node addresses are correct

## Production Deployment

For production:
1. Use unique `NodeID` for each instance (e.g., hostname, UUID)
2. Configure `KnownNodes` with multiple seed nodes for redundancy
3. Set appropriate `SyncInterval` based on your needs
4. Store `DataDir` on persistent storage
5. Monitor the `/health` endpoint
6. Implement graceful shutdown to save state

## Learn More

- [README.md](./README.md) - Full documentation
- [ARCHITECTURE.md](./ARCHITECTURE.md) - Architecture details
- [example/](./example/) - Complete example application
