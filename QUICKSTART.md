# ClusterKit Quick Start Guide

Get up and running with ClusterKit in 5 minutes!

## What You'll Build

A simple 3-node distributed cluster with automatic data partitioning.

## Prerequisites

- Go 1.19 or higher
- Basic understanding of distributed systems

## Step 1: Install ClusterKit

```bash
go get github.com/skshohagmiah/clusterkit
```

## Step 2: Create Your Application

Create `main.go`:

```go
package main

import (
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Read config from environment
    nodeID := getEnv("NODE_ID", "node-1")
    httpAddr := getEnv("HTTP_ADDR", ":8080")
    raftAddr := getEnv("RAFT_ADDR", "127.0.0.1:9001")
    bootstrap := getEnv("BOOTSTRAP", "true") == "true"
    joinAddr := getEnv("JOIN_ADDR", "")
    
    // Initialize ClusterKit
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:    nodeID,
        NodeName:  "Server-" + nodeID,
        HTTPAddr:  httpAddr,
        RaftAddr:  raftAddr,
        Bootstrap: bootstrap,
        JoinAddr:  joinAddr,
        DataDir:   "./data/" + nodeID,
        Config: &clusterkit.Config{
            ClusterName:       "my-cluster",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Start ClusterKit
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()
    
    fmt.Printf("✓ Node %s started on %s\n", nodeID, httpAddr)
    fmt.Printf("✓ Raft listening on %s\n", raftAddr)
    
    // Use ClusterKit API
    demoAPI(ck)
    
    // Wait for shutdown signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan
    
    fmt.Println("\nShutting down...")
}

func demoAPI(ck *clusterkit.ClusterKit) {
    // Example: Find where to store a key
    key := "user:123"
    
    partition, err := ck.GetPartition(key)
    if err != nil {
        log.Printf("Error: %v", err)
        return
    }
    
    primary := ck.GetPrimary(partition)
    replicas := ck.GetReplicas(partition)
    
    fmt.Printf("\n📊 Key '%s' routing:\n", key)
    fmt.Printf("   Partition: %s\n", partition.ID)
    fmt.Printf("   Primary: %s (%s)\n", primary.Name, primary.IP)
    fmt.Printf("   Replicas: %d nodes\n", len(replicas))
    
    if ck.IsPrimary(partition) {
        fmt.Printf("   ✓ I AM the primary!\n")
    }
    
    if ck.IsReplica(partition) {
        fmt.Printf("   ✓ I AM a replica!\n")
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

## Step 3: Start Your Cluster

### Terminal 1: Start Node 1 (Bootstrap)
```bash
NODE_ID=node-1 \
HTTP_ADDR=:8080 \
RAFT_ADDR=127.0.0.1:9001 \
BOOTSTRAP=true \
go run main.go
```

### Terminal 2: Start Node 2
```bash
NODE_ID=node-2 \
HTTP_ADDR=:8081 \
RAFT_ADDR=127.0.0.1:9002 \
JOIN_ADDR=localhost:8080 \
BOOTSTRAP=false \
go run main.go
```

### Terminal 3: Start Node 3
```bash
NODE_ID=node-3 \
HTTP_ADDR=:8082 \
RAFT_ADDR=127.0.0.1:9003 \
JOIN_ADDR=localhost:8080 \
BOOTSTRAP=false \
go run main.go
```

## Step 4: Test Your Cluster

```bash
# Check cluster status
curl http://localhost:8080/cluster | jq

# Get partition for a key
curl "http://localhost:8080/partitions/key?key=user:123" | jq

# Check partition stats
curl http://localhost:8080/partitions/stats | jq

# Check Raft leader
curl http://localhost:8080/consensus/leader | jq
```

## Step 5: Use the API in Your Code

Now that your cluster is running, use the ClusterKit API:

```go
// Get partition for a key
partition, err := ck.GetPartition("user:123")
if err != nil {
    return err
}

// Get nodes
primary := ck.GetPrimary(partition)
replicas := ck.GetReplicas(partition)

// Check if current node should handle it
if ck.IsPrimary(partition) {
    // I'm the primary - store locally
    myDatabase.Write("user:123", userData)
    
    // Replicate to replicas
    for _, replica := range replicas {
        httpPost(replica.IP, "user:123", userData)
    }
}

if ck.IsReplica(partition) {
    // I'm a replica - store locally
    myDatabase.Write("user:123", userData)
}
```

## Next Steps

### Add a Real Database

Replace the demo with actual storage:

```go
import "database/sql"

type MyApp struct {
    ck *clusterkit.ClusterKit
    db *sql.DB
}

func (app *MyApp) Set(key, value string) error {
    partition, _ := app.ck.GetPartition(key)
    
    if app.ck.IsPrimary(partition) {
        // Store in database
        _, err := app.db.Exec("INSERT INTO kv VALUES (?, ?)", key, value)
        return err
    }
    
    // Forward to primary
    primary := app.ck.GetPrimary(partition)
    return httpPost(primary.IP, key, value)
}
```

### Add HTTP Endpoints

```go
func (app *MyApp) startHTTPServer() {
    http.HandleFunc("/set", app.handleSet)
    http.HandleFunc("/get", app.handleGet)
    http.ListenAndServe(":9080", nil)
}

func (app *MyApp) handleSet(w http.ResponseWriter, r *http.Request) {
    key := r.FormValue("key")
    value := r.FormValue("value")
    
    if err := app.Set(key, value); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    
    w.Write([]byte("OK"))
}
```

### Add Replication

```go
func (app *MyApp) replicateToNodes(key, value string, nodes []Node) error {
    var wg sync.WaitGroup
    errors := make(chan error, len(nodes))
    
    for _, node := range nodes {
        wg.Add(1)
        go func(n Node) {
            defer wg.Done()
            if err := httpPost(n.IP, key, value); err != nil {
                errors <- err
            }
        }(node)
    }
    
    wg.Wait()
    close(errors)
    
    // Check for errors
    for err := range errors {
        if err != nil {
            return err
        }
    }
    
    return nil
}
```

## Common Issues

### Issue: "no partitions available"
**Solution:** Wait a few seconds after starting the cluster. Partitions are created automatically when enough nodes join.

### Issue: Nodes can't connect
**Solution:** Check firewall settings and ensure Raft addresses are reachable.

### Issue: "failed to join cluster"
**Solution:** Ensure the `JOIN_ADDR` points to an existing node and that node is running.

## Production Checklist

Before deploying to production:

- [ ] Use persistent storage for `DataDir`
- [ ] Set appropriate `PartitionCount` (16-256)
- [ ] Configure `ReplicationFactor` based on needs
- [ ] Implement proper error handling
- [ ] Add monitoring for `/health` and `/metrics`
- [ ] Use TLS for inter-node communication
- [ ] Test node failure scenarios
- [ ] Document your replication strategy

## Examples

Check out the [example directory](./example) for:
- Complete distributed KV store implementation
- Docker Compose setup for 3-node cluster
- Production-ready configuration

## Need Help?

- 📖 [Full API Reference](./API.md)
- 📖 [README](./README.md)
- 🐛 [Report Issues](https://github.com/skshohagmiah/clusterkit/issues)
- 💬 [Discussions](https://github.com/skshohagmiah/clusterkit/discussions)

---

**You're now ready to build distributed systems with ClusterKit!** 🚀
