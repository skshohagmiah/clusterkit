# ClusterKit

> A lightweight, production-ready distributed cluster coordination library for Go

ClusterKit provides **cluster coordination** (nodes, partitions, consensus) while letting you handle your own data storage and replication logic. Built on **HashiCorp Raft** for strong consistency.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## ✨ Features

- 🎯 **Cluster Coordination** - Automatic node discovery and membership management
- 📦 **Partition Management** - Consistent hashing for data distribution
- 🔄 **Raft Consensus** - Production-grade consensus using HashiCorp Raft
- 🎭 **Leader Election** - Automatic leader election and failover
- 🔍 **Simple API** - Just 7 core methods + 1 hook
- 🪝 **Partition Change Hooks** - Automatic notifications for data migration
- 🌐 **HTTP API** - RESTful endpoints for cluster management
- 💾 **State Persistence** - WAL and snapshots for crash recovery
- 📊 **Metrics & Health** - Built-in monitoring endpoints

## 🎯 What ClusterKit Does

**Think of ClusterKit as GPS for your distributed system** - it tells you WHERE data should go, YOU decide HOW to store it.

**ClusterKit Provides:**
- ✅ Which partition a key belongs to
- ✅ Which nodes (primary + replicas) should store the data
- ✅ Whether current node is primary or replica
- ✅ Leader election and consensus
- ✅ Notifications when partitions change (for data migration)

**You Implement:**
- 🔧 Data storage (PostgreSQL, Redis, RocksDB, etc.)
- 🔧 Data replication (HTTP, gRPC, etc.)
- 🔧 Data migration logic
- 🔧 Business logic

## Installation

```bash
go get github.com/skshohagmiah/clusterkit
```

## Quick Start

### 1. Initialize ClusterKit

```go
package main

import "github.com/skshohagmiah/clusterkit"

func main() {
    // Create first node (bootstrap)
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:    "node-1",
        NodeName:  "Server-1",
        HTTPAddr:  ":8080",
        RaftAddr:  "127.0.0.1:9001",
        Bootstrap: true,  // First node
        DataDir:   "./data",
        Config: &clusterkit.Config{
            ClusterName:       "my-app",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    
    ck.Start()
    defer ck.Stop()
    
    // Your application logic here...
}
```

### 2. Use the Simple API

```go
// Step 1: Get partition for a key
partition, err := ck.GetPartition("user:123")

// Step 2: Get nodes
primary := ck.GetPrimary(partition)
replicas := ck.GetReplicas(partition)
allNodes := ck.GetNodes(partition)  // primary + replicas

// Step 3: Check if current node should handle it
if ck.IsPrimary(partition) {
    storeLocally(key, value)
}

if ck.IsReplica(partition) {
    storeLocally(key, value)
}

// Step 4: Forward to other nodes
for _, replica := range replicas {
    if replica.ID != ck.GetMyNodeID() {
        httpPost(replica, key, value)
    }
}
```

## Complete API Reference

ClusterKit has just **7 core methods + 1 hook**:

### Core Methods

```go
// 1. Get partition for a key
partition, err := ck.GetPartition(key string) (*Partition, error)

// 2. Get primary node
primary := ck.GetPrimary(partition *Partition) *Node

// 3. Get replica nodes
replicas := ck.GetReplicas(partition *Partition) []Node

// 4. Get all nodes (primary + replicas)
nodes := ck.GetNodes(partition *Partition) []Node

// 5. Check if current node is primary
isPrimary := ck.IsPrimary(partition *Partition) bool

// 6. Check if current node is replica
isReplica := ck.IsReplica(partition *Partition) bool

// 7. Get current node ID
myNodeID := ck.GetMyNodeID() string
```

### Partition Change Hook

```go
// 8. Register hook for partition changes (data migration)
ck.OnPartitionChange(func(partitionID string, copyFrom *Node, copyTo *Node) {
    // partitionID: Which partition changed
    // copyFrom: Node to copy data from (has the data)
    // copyTo: Node that needs the data (YOU if this is your node)
    
    if copyTo.ID == myNodeID && copyFrom != nil {
        // Copy data from copyFrom node
        fetchDataFrom(copyFrom.IP, partitionID)
    }
})
```

That's it! No complex APIs, no confusion.

## Real-World Example: Distributed KV Store

```go
type DistributedKV struct {
    ck    *clusterkit.ClusterKit
    store map[string]string
    mu    sync.RWMutex
}

func (kv *DistributedKV) Set(key, value string) error {
    // Step 1: Get partition
    partition, err := kv.ck.GetPartition(key)
    if err != nil {
        return err
    }
    
    // Step 2: Get nodes
    primary := kv.ck.GetPrimary(partition)
    replicas := kv.ck.GetReplicas(partition)
    
    // Step 3: Send to primary
    if kv.ck.IsPrimary(partition) {
        // I'm the primary - store locally
        kv.mu.Lock()
        kv.store[key] = value
        kv.mu.Unlock()
    } else {
        // Forward to primary
        httpPost(primary, key, value)
    }
    
    // Step 4: Send to replicas
    if kv.ck.IsReplica(partition) {
        // I'm a replica - store locally
        kv.mu.Lock()
        kv.store[key] = value
        kv.mu.Unlock()
    }
    
    // Forward to other replicas
    for _, replica := range replicas {
        if replica.ID != kv.ck.GetMyNodeID() {
            httpPost(replica, key, value)
        }
    }
    
    return nil
}

func (kv *DistributedKV) Get(key string) (string, error) {
    // Simple: just check local store
    kv.mu.RLock()
    value, exists := kv.store[key]
    kv.mu.RUnlock()
    
    if exists {
        return value, nil
    }
    return "", fmt.Errorf("key not found")
}
```

See the [example](./example) directory for a complete working implementation.

## Handling Data Migration

When nodes join or leave the cluster, partitions are reassigned. Use the `OnPartitionChange` hook to automatically migrate data:

```go
// Register the hook during initialization
ck.OnPartitionChange(func(partitionID string, copyFrom *Node, copyTo *Node) {
    myNodeID := ck.GetMyNodeID()
    
    // Only act if I'm the target node
    if copyTo == nil || copyTo.ID != myNodeID {
        return
    }
    
    fmt.Printf("I need data for partition %s\n", partitionID)
    
    // Copy data from source node
    if copyFrom != nil {
        fmt.Printf("Copying from %s (%s)\n", copyFrom.ID, copyFrom.IP)
        go copyPartitionData(partitionID, copyFrom)
    } else {
        fmt.Printf("No source (I already have the data)\n")
    }
})

func copyPartitionData(partitionID string, fromNode *Node) {
    // 1. Fetch all keys for this partition from the source node
    url := fmt.Sprintf("http://%s/keys?partition=%s", fromNode.IP, partitionID)
    keys := httpGet(url)
    
    // 2. Copy each key
    for _, key := range keys {
        value := httpGet(fmt.Sprintf("http://%s/get?key=%s", fromNode.IP, key))
        localStore[key] = value
    }
    
    fmt.Printf("✓ Copied %d keys for partition %s\n", len(keys), partitionID)
}
```

### When Hooks Fire

**Node Dies:**
```
Before: partition-5 → Node 1 (primary), Node 2 (replica), Node 3 (replica)
Node 1 dies ❌
After:  partition-5 → Node 2 (NEW primary), Node 3 (replica), Node 4 (NEW replica)

Hook fires on Node 4:
  partitionID: "partition-5"
  copyFrom: &Node{ID: "node-2", IP: ":8081"}  ← Copy from here!
  copyTo: &Node{ID: "node-4", IP: ":8083"}    ← That's me!
```

**Node Joins:**
```
3 nodes → 4 nodes join → Partitions rebalanced

Hook fires multiple times for affected partitions:
  - partition-3 moves to Node 4 → Copy from Node 1
  - partition-7 moves to Node 4 → Copy from Node 2
  - partition-12 moves to Node 4 → Copy from Node 3
```

**Key Points:**
- ✅ Hook fires on ALL nodes, but only the target node acts
- ✅ `copyFrom` is always a live node with the data
- ✅ `copyTo` is the node that needs the data
- ✅ Hook runs in goroutine (non-blocking)
- ✅ With RF≥2, you never lose data (replicas have copies)

## Starting a 3-Node Cluster

### Node 1 (Bootstrap)
```bash
NODE_ID=node-1 \
NODE_NAME=Server-1 \
HTTP_ADDR=:8080 \
RAFT_ADDR=127.0.0.1:9001 \
BOOTSTRAP=true \
DATA_DIR=./data/node1 \
go run main.go
```

### Node 2
```bash
NODE_ID=node-2 \
NODE_NAME=Server-2 \
HTTP_ADDR=:8081 \
RAFT_ADDR=127.0.0.1:9002 \
JOIN_ADDR=localhost:8080 \
DATA_DIR=./data/node2 \
go run main.go
```

### Node 3
```bash
NODE_ID=node-3 \
NODE_NAME=Server-3 \
HTTP_ADDR=:8082 \
RAFT_ADDR=127.0.0.1:9003 \
JOIN_ADDR=localhost:8080 \
DATA_DIR=./data/node3 \
go run main.go
```

## HTTP API Endpoints

ClusterKit provides built-in HTTP endpoints:

```bash
# Cluster info
GET /cluster

# Partitions
GET /partitions
GET /partitions/stats
GET /partitions/key?key=<key>

# Consensus
GET /consensus/leader
GET /consensus/stats

# Health
GET /health
GET /health/detailed
GET /metrics
```

## Configuration Options

```go
type Options struct {
    NodeID       string   // Unique node identifier
    NodeName     string   // Human-readable name
    HTTPAddr     string   // HTTP server address (e.g., ":8080")
    RaftAddr     string   // Raft address (e.g., "127.0.0.1:9001")
    JoinAddr     string   // Address of node to join (empty for bootstrap)
    Bootstrap    bool     // True for first node
    DataDir      string   // Directory for Raft data
    Config       *Config  // Cluster configuration
}

type Config struct {
    ClusterName       string  // Cluster identifier
    PartitionCount    int     // Number of partitions (default: 16)
    ReplicationFactor int     // Number of replicas (default: 3)
}
```

## Architecture

```
┌─────────────────────────────────────────┐
│         Your Application                │
│  (HTTP Server, gRPC, Database, etc.)    │
├─────────────────────────────────────────┤
│           ClusterKit Library            │
│  ┌──────────┬──────────┬──────────┐    │
│  │Partition │   Raft   │   HTTP   │    │
│  │ Manager  │Consensus │   API    │    │
│  └──────────┴──────────┴──────────┘    │
└─────────────────────────────────────────┘
         │         │         │
    ┌────┴────┬────┴────┬────┴────┐
    │ Node 1  │ Node 2  │ Node 3  │
    └─────────┴─────────┴─────────┘
```

## Use Cases

ClusterKit is perfect for building:

### ✅ **Distributed Databases**
```go
// Your new database "MyDB"
ck := clusterkit.NewClusterKit(opts)
partition, _ := ck.GetPartition("users:123")
primary := ck.GetPrimary(partition)
// Store data on primary, replicate to replicas
```

### ✅ **Distributed Caches**
```go
// Your new cache "FastCache"
ck := clusterkit.NewClusterKit(opts)
partition, _ := ck.GetPartition("session:abc")
if ck.IsPrimary(partition) {
    cache.Set(key, value)
}
```

### ✅ **Distributed Queues**
```go
// Your new queue "ReliableQ"
ck := clusterkit.NewClusterKit(opts)
partition, _ := ck.GetPartition("queue:orders")
primary := ck.GetPrimary(partition)
// Send messages to primary
```

### ✅ **Distributed File Systems**
```go
// Your new FS "CloudFS"
ck := clusterkit.NewClusterKit(opts)
partition, _ := ck.GetPartition("file:document.pdf")
nodes := ck.GetNodes(partition)
// Store file chunks across nodes
```

### ✅ **Distributed Key-Value Stores**
```go
// Your new KV store "FastKV"
ck := clusterkit.NewClusterKit(opts)
// Add persistent storage (RocksDB, BadgerDB)
// Add quorum writes
// Add conflict resolution
```

**Perfect for:**
- 🎓 Learning distributed systems
- 🚀 Startups building infrastructure
- 🔬 Research projects
- 💡 Side projects
- 🏢 Custom distributed solutions

## Why ClusterKit?

**Before ClusterKit:**
```go
// Complex: Manual partition calculation, node discovery, consensus...
hash := md5.Sum([]byte(key))
partitionID := int(hash) % 16
nodes := lookupNodes(partitionID)  // How?
primary := electPrimary(nodes)      // How?
replicas := getReplicas(nodes)      // How?
// ... 100+ lines of cluster management code
```

**With ClusterKit:**
```go
// Simple: Just ask ClusterKit!
partition, _ := ck.GetPartition(key)
primary := ck.GetPrimary(partition)
replicas := ck.GetReplicas(partition)
// Done! Focus on your business logic.
```

## Production Checklist

### ClusterKit Configuration
- ✅ Use environment variables for configuration
- ✅ Set appropriate `PartitionCount` (16-256 recommended)
- ✅ Set `ReplicationFactor` ≥ 3 for high availability
- ✅ Use persistent storage for `DataDir`
- ✅ Monitor `/health` and `/metrics` endpoints
- ✅ Use TLS for production deployments

### Your Application
- ✅ Implement durable storage (RocksDB, BadgerDB, etc.)
- ✅ Register `OnPartitionChange` hook for data migration
- ✅ Implement proper error handling and retries
- ✅ Add batching for replication (don't send one key at a time)
- ✅ Add rate limiting for migrations
- ✅ Verify data after migration
- ✅ Clean up old data after successful migration
- ✅ Add metrics and monitoring
- ✅ Test failure scenarios (kill nodes, network partitions)

## Examples

- [Distributed KV Store](./example) - Complete working example
- [Docker Setup](./example/docker) - Run 3-node cluster with Docker

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Support

- 📖 [Documentation](https://github.com/skshohagmiah/clusterkit/wiki)
- 🐛 [Issue Tracker](https://github.com/skshohagmiah/clusterkit/issues)
- 💬 [Discussions](https://github.com/skshohagmiah/clusterkit/discussions)

---

**Made with ❤️ for developers who want simple, production-ready cluster coordination**
