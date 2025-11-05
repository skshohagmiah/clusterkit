# ClusterKit

> A lightweight, production-ready distributed cluster coordination library for Go

ClusterKit provides **cluster coordination** (nodes, partitions, consensus) while letting you handle your own data storage and replication logic. Built on **HashiCorp Raft** for strong consistency.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## ✨ Features

- 🎯 **Cluster Coordination** - Automatic node discovery and membership management
- 📦 **Partition Management** - Consistent hashing for data distribution
- 🔄 **Raft Consensus** - Production-grade consensus using HashiCorp Raft
- 🎭 **Leader Election** - Automatic leader election
- 🔍 **Simple API** - Just 7 methods to learn
- 🌐 **HTTP API** - RESTful endpoints for cluster management
- 💾 **State Persistence** - WAL and snapshots for crash recovery

## 🎯 What ClusterKit Does

**Think of ClusterKit as GPS for your distributed system** - it tells you WHERE data should go, YOU decide HOW to store it.

**ClusterKit Provides:**
- ✅ Which partition a key belongs to
- ✅ Which nodes (primary + replicas) should store the data
- ✅ Whether current node is primary or replica
- ✅ Leader election and consensus

**You Implement:**
- 🔧 Data storage (PostgreSQL, Redis, MongoDB, etc.)
- 🔧 Data replication (HTTP, gRPC, etc.)
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

ClusterKit has just **7 simple methods**:

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

- ✅ Use environment variables for configuration
- ✅ Set appropriate `PartitionCount` (16-256 recommended)
- ✅ Set `ReplicationFactor` based on availability needs
- ✅ Use persistent storage for `DataDir`
- ✅ Monitor `/health` and `/metrics` endpoints
- ✅ Implement proper error handling and retries
- ✅ Use TLS for production deployments

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
