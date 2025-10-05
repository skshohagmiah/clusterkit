# ClusterKit 🧠

**Distributed Clustering Made Simple**

A lightweight, embeddable clustering library for Go that provides automatic node membership, partitioning, replication management, and leader routing — all built on top of etcd.

Build scalable distributed systems like databases, caches, message brokers, or stream processors by simply embedding ClusterKit into your application.

---

## 📋 Table of Contents

- [Overview](#-overview)
- [Features](#-features)
- [Architecture](#%EF%B8%8F-architecture)
- [Installation](#-installation)
- [Quick Start](#-quick-start)
- [API Reference](#-api-reference)
- [Hooks & Lifecycle](#-hooks--lifecycle)
- [Communication](#-communication)
- [Project Structure](#-project-structure)
- [Configuration](#%EF%B8%8F-configuration)
- [Examples](#-examples)
- [Best Practices](#-best-practices)
- [Observability](#-observability)
- [Troubleshooting](#-troubleshooting)

---

## 🚀 Overview

ClusterKit is an **embeddable library**, not a standalone service. You import it into your Go application and it handles all the clustering complexity.

### What ClusterKit Does

✅ Manages cluster topology (nodes joining/leaving)  
✅ Assigns partitions to nodes using consistent hashing  
✅ Elects partition leaders automatically  
✅ Notifies your app via hooks when topology changes  
✅ Provides routing APIs to find the right node for any key  
✅ Uses etcd internally for coordination (transparent to you)  

### What ClusterKit Doesn't Do

❌ Store your data (you use any storage: RocksDB, Badger, PostgreSQL, etc.)  
❌ Implement replication protocol (you choose: async, Raft, chain replication)  
❌ Handle application logic  

**You write the data layer. ClusterKit handles the cluster layer.**

---

## ✨ Features

- **Zero-Config Clustering** — Just call `Join()` with node info
- **Consistent Hash Partitioning** — Uniform key distribution, minimal rebalancing
- **Configurable Replication** — Set replica factor (1-N replicas per partition)
- **Per-Partition Leaders** — Automatic leader election with sub-second failover
- **Real-time Updates** — Apps get topology changes instantly via etcd watch
- **Hook-based Migration** — Full control over data movement between nodes
- **HTTP & gRPC Support** — Built-in support for both protocols
- **Eventually Consistent** — No coordination overhead for reads/writes
- **Embedded etcd Support** — Can use external or embedded etcd
- **Prometheus Metrics** — Built-in observability

---

## 🏗️ Architecture

```
                    ┌─────────────────────────────────┐
                    │         etcd Cluster            │
                    │  (Metadata, Membership, Leaders) │
                    └─────────────────────────────────┘
                                   ▲
                    Control Plane  │  <100 ops/sec
                                   │
        ┌──────────────────────────┴──────────────────────────┐
        │                                                      │
        ▼                                                      ▼
┌───────────────┐  HTTP/gRPC   ┌───────────────┐  HTTP/gRPC  ┌───────────────┐
│   Your App    │◄────────────►│   Your App    │◄───────────►│   Your App    │
│   + ClusterKit│              │   + ClusterKit│             │   + ClusterKit│
│               │              │               │             │               │
│ Partitions:   │              │ Partitions:   │             │ Partitions:   │
│  0 (Leader)   │              │  1 (Leader)   │             │  2 (Leader)   │
│  1 (Replica)  │              │  2 (Replica)  │             │  0 (Replica)  │
└───────┬───────┘              └───────┬───────┘             └───────┬───────┘
        │                              │                             │
        └──────────────────────────────┴─────────────────────────────┘
                    Your Data Layer (RocksDB, etc.)
```

### Separation of Concerns

| Layer | Responsibility | Backing Store | Frequency |
|-------|----------------|---------------|-----------|
| **Control Plane** | Membership, partition map, leader election | etcd | Low (~100 ops/sec) |
| **Data Plane** | Actual reads/writes, replication | Your storage | High (millions ops/sec) |

---

## 📦 Installation

```bash
go get github.com/yourusername/clusterkit
```

**Prerequisites:**
- Go 1.22+
- etcd 3.5+ cluster accessible (or ClusterKit can use embedded etcd)

---

## 🚀 Quick Start

### Step 1: Import and Join Cluster

```go
package main

import (
    "log"
    "github.com/yourusername/clusterkit"
)

func main() {
    // Embed ClusterKit in your application
    ck, err := clusterkit.Join(&clusterkit.Config{
        NodeID:        "node1",
        AdvertiseAddr: "10.0.0.1:9000",
        HTTPPort:      8080,
        GRPCPort:      9000,
        Partitions:    32,
        ReplicaFactor: 3,
        
        // Hooks to handle topology changes
        OnPartitionAssigned:   handlePartitionAssigned,
        OnPartitionUnassigned: handlePartitionUnassigned,
        OnLeaderElected:       handleBecameLeader,
        OnLeaderLost:          handleLostLeadership,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer ck.Leave()

    log.Println("Node joined cluster!")
    
    // Start your application logic
    startMyApp(ck)
}

func handlePartitionAssigned(pt int, prevOwner *clusterkit.Node) {
    log.Printf("Partition %d assigned to me", pt)
    // Pull data from previous owner if exists
}

func handlePartitionUnassigned(pt int, newOwner *clusterkit.Node) {
    log.Printf("Partition %d moving to %s", pt, newOwner.ID)
    // Push data to new owner
}

func handleBecameLeader(pt int) {
    log.Printf("Became leader for partition %d", pt)
}

func handleLostLeadership(pt int) {
    log.Printf("Lost leadership for partition %d", pt)
}
```

### Step 2: Route Requests

```go
func (s *KVService) Set(key, value string) error {
    // Get partition for this key
    pt := ck.GetPartition(key)
    
    // Check if this node is the leader
    if ck.IsLeader(pt) {
        // Handle locally
        s.storage.Set(pt, key, value)
        
        // Replicate to followers
        replicas := ck.GetReplicas(pt)
        for _, replica := range replicas {
            if replica.ID != ck.NodeID() {
                s.replicateTo(replica, key, value)
            }
        }
        return nil
    }
    
    // Forward to leader
    leader := ck.GetLeader(pt)
    return s.forwardToLeader(leader, key, value)
}
```

### Step 3: Client Routing

```go
// Client library (embedded in your client app)
client := clusterkit.NewClient(&clusterkit.ClientConfig{
    EtcdEndpoints: []string{"http://etcd:2379"},
})

// Route request to correct node
key := "user:1234"
pt := client.GetPartition(key)
leader := client.GetLeader(pt)

// Make request
conn := grpc.Dial(leader.GRPCAddr())
resp, _ := conn.Set(ctx, &SetRequest{Key: key, Value: "data"})
```

---

## 📚 API Reference

### Core APIs

```go
// Join the cluster (embed in your app)
func Join(cfg *Config) (*ClusterKit, error)

// Leave the cluster gracefully
func (ck *ClusterKit) Leave() error

// Get partition ID for a key
func (ck *ClusterKit) GetPartition(key string) int

// Get leader node for a partition
func (ck *ClusterKit) GetLeader(partition int) *Node

// Get all replica nodes for a partition
func (ck *ClusterKit) GetReplicas(partition int) []*Node

// Check if this node is leader for partition
func (ck *ClusterKit) IsLeader(partition int) bool

// Check if this node is a replica for partition
func (ck *ClusterKit) IsReplica(partition int) bool

// Get all alive nodes in cluster
func (ck *ClusterKit) GetNodes() []*Node

// Get this node's ID
func (ck *ClusterKit) NodeID() string

// Get partition map (for debugging)
func (ck *ClusterKit) GetPartitionMap() map[int][]*Node
```

### Client APIs

```go
// Create a client (embed in client app)
func NewClient(cfg *ClientConfig) (*Client, error)

// Same routing APIs as server
func (c *Client) GetPartition(key string) int
func (c *Client) GetLeader(partition int) *Node
func (c *Client) GetReplicas(partition int) []*Node
func (c *Client) GetNodes() []*Node
func (c *Client) Close() error
```

### Node Object

```go
type Node struct {
    ID            string
    Addr          string
    HTTPEndpoint  string
    GRPCEndpoint  string
    Metadata      map[string]string
}

func (n *Node) HTTPAddr() string  // Returns http://host:port
func (n *Node) GRPCAddr() string  // Returns host:port
```

---

## 🪝 Hooks & Lifecycle

Hooks are where you implement your data migration logic:

```go
type Config struct {
    // Called when partition assigned to this node
    OnPartitionAssigned func(partition int, previousOwner *Node)
    
    // Called when partition being removed from this node
    OnPartitionUnassigned func(partition int, newOwner *Node)
    
    // Called when this node becomes leader
    OnLeaderElected func(partition int)
    
    // Called when this node loses leadership
    OnLeaderLost func(partition int)
    
    // Called when new replica added
    OnReplicaAdded func(partition int, node *Node)
    
    // Called when replica removed
    OnReplicaRemoved func(partition int, node *Node)
}
```

### Complete Migration Example

```go
func handlePartitionAssigned(pt int, prev *clusterkit.Node) {
    log.Printf("Partition %d assigned", pt)
    
    // Initialize partition storage
    myPartitions[pt] = &PartitionState{}
    
    if prev == nil {
        // New partition, no migration needed
        myPartitions[pt].Ready = true
        return
    }
    
    // Pull data from previous owner
    conn, _ := grpc.Dial(prev.GRPCAddr())
    client := pb.NewMigrationServiceClient(conn)
    stream, _ := client.StreamPartition(ctx, &pb.StreamRequest{
        Partition: int32(pt),
    })
    
    // Receive and store data
    for {
        data, err := stream.Recv()
        if err == io.EOF {
            break
        }
        myDB.Put(pt, data.Key, data.Value)
    }
    
    myPartitions[pt].Ready = true
    log.Printf("Migration complete for partition %d", pt)
}

func handlePartitionUnassigned(pt int, newOwner *clusterkit.Node) {
    log.Printf("Partition %d moving to %s", pt, newOwner.ID)
    
    // Mark as not ready
    myPartitions[pt].Ready = false
    
    // Push data to new owner (or let them pull)
    // Your choice based on your protocol
    
    // Delete local data after migration
    myDB.DeletePartition(pt)
    delete(myPartitions, pt)
}
```

---

## 🔌 Communication

ClusterKit supports both HTTP and gRPC for inter-node communication.

### HTTP

```go
ck, _ := clusterkit.Join(&clusterkit.Config{
    HTTPPort: 8080,
    // ...
})

// Use HTTP client
httpClient := ck.HTTPClient()
node := ck.GetLeader(partition)

resp, _ := httpClient.Post(
    fmt.Sprintf("%s/api/replicate", node.HTTPAddr()),
    "application/json",
    body,
)
```

### gRPC

```go
ck, _ := clusterkit.Join(&clusterkit.Config{
    GRPCPort: 9000,
    // ...
})

// Use gRPC connection pool
grpcPool := ck.GRPCPool()
node := ck.GetLeader(partition)

conn, _ := grpcPool.Get(node.GRPCAddr())
client := pb.NewYourServiceClient(conn)
resp, _ := client.YourMethod(ctx, req)
```

### Built-in Migration Service

ClusterKit provides a built-in gRPC service for partition migration:

```protobuf
service MigrationService {
    rpc StreamPartition(StreamRequest) returns (stream KeyValue);
    rpc ReceivePartition(stream KeyValue) returns (MigrationResponse);
}
```

---

## 📁 Project Structure

```
clusterkit/
├── go.mod
├── go.sum
├── README.md
│
├── pkg/
│   ├── clusterkit/              # Core library
│   │   ├── clusterkit.go       # Main API
│   │   ├── membership.go       # Node registration
│   │   ├── partition.go        # Partitioning logic
│   │   ├── leader.go           # Leader election
│   │   ├── watcher.go          # etcd watch handler
│   │   ├── rebalance.go        # Rebalancing
│   │   └── types.go
│   │
│   ├── client/                  # Client library
│   │   ├── client.go
│   │   └── cache.go
│   │
│   ├── hash/                    # Consistent hashing
│   │   └── consistent.go
│   │
│   ├── transport/               # Communication
│   │   ├── http/
│   │   │   ├── server.go
│   │   │   └── client.go
│   │   ├── grpc/
│   │   │   ├── server.go
│   │   │   ├── client.go
│   │   │   └── pool.go
│   │   └── migration/
│   │       ├── migration.proto
│   │       └── service.go
│   │
│   ├── etcd/                    # etcd integration
│   │   ├── store.go
│   │   ├── lease.go
│   │   └── watch.go
│   │
│   └── metrics/                 # Prometheus metrics
│       └── metrics.go
│
├── examples/
│   ├── kv-store/                # KV store example
│   ├── queue/                   # Queue example
│   └── cache/                   # Cache example
│
└── internal/
    └── util/
```

---

## ⚙️ Configuration

### Node Configuration

```go
type Config struct {
    // Identity
    NodeID        string              // Unique node identifier
    AdvertiseAddr string              // Address other nodes connect to
    
    // Ports
    HTTPPort      int                 // HTTP port (default: 8080)
    GRPCPort      int                 // gRPC port (default: 9000)
    
    // Cluster
    Partitions    int                 // Number of partitions (default: 256)
    ReplicaFactor int                 // Replicas per partition (default: 3)
    
    // etcd (optional - auto-discovered)
    EtcdEndpoints []string            // etcd endpoints
    EtcdPrefix    string              // Key prefix (default: /clusterkit)
    
    // Timeouts
    HeartbeatInterval time.Duration   // Heartbeat (default: 5s)
    SessionTTL        time.Duration   // Session TTL (default: 10s)
    RebalanceDelay    time.Duration   // Rebalance debounce (default: 5s)
    
    // Hooks
    OnPartitionAssigned   func(partition int, previousOwner *Node)
    OnPartitionUnassigned func(partition int, newOwner *Node)
    OnLeaderElected       func(partition int)
    OnLeaderLost          func(partition int)
    OnReplicaAdded        func(partition int, node *Node)
    OnReplicaRemoved      func(partition int, node *Node)
    
    // Optional
    Metadata      map[string]string   // Custom metadata
    Logger        Logger              // Custom logger
    MetricsPort   int                 // Metrics port (default: 2112)
}
```

### Client Configuration

```go
type ClientConfig struct {
    EtcdEndpoints   []string
    EtcdPrefix      string
    CacheEnabled    bool
    CacheTTL        time.Duration
    DialTimeout     time.Duration
    RequestTimeout  time.Duration
}
```

---

## 💡 Examples

### Distributed KV Store

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
    "sync"
    
    "github.com/yourusername/clusterkit"
)

type KVStore struct {
    ck        *clusterkit.ClusterKit
    data      map[int]map[string]string
    mu        sync.RWMutex
}

func main() {
    store := &KVStore{
        data: make(map[int]map[string]string),
    }
    
    // Embed ClusterKit
    ck, _ := clusterkit.Join(&clusterkit.Config{
        NodeID:        "kv-node1",
        AdvertiseAddr: "localhost:9000",
        HTTPPort:      8080,
        Partitions:    16,
        ReplicaFactor: 2,
        
        OnPartitionAssigned:   store.onAssigned,
        OnPartitionUnassigned: store.onUnassigned,
    })
    store.ck = ck
    
    // Start HTTP API
    http.HandleFunc("/set", store.handleSet)
    http.HandleFunc("/get", store.handleGet)
    http.ListenAndServe(":8080", nil)
}

func (s *KVStore) handleSet(w http.ResponseWriter, r *http.Request) {
    key := r.URL.Query().Get("key")
    value := r.URL.Query().Get("value")
    
    pt := s.ck.GetPartition(key)
    
    if !s.ck.IsLeader(pt) {
        // Forward to leader
        leader := s.ck.GetLeader(pt)
        http.Redirect(w, r, fmt.Sprintf("%s/set?key=%s&value=%s", 
            leader.HTTPAddr(), key, value), http.StatusTemporaryRedirect)
        return
    }
    
    // Store locally
    s.mu.Lock()
    if s.data[pt] == nil {
        s.data[pt] = make(map[string]string)
    }
    s.data[pt][key] = value
    s.mu.Unlock()
    
    // Replicate to followers
    s.replicate(pt, key, value)
    
    w.WriteHeader(http.StatusOK)
}

func (s *KVStore) handleGet(w http.ResponseWriter, r *http.Request) {
    key := r.URL.Query().Get("key")
    pt := s.ck.GetPartition(key)
    
    s.mu.RLock()
    value, ok := s.data[pt][key]
    s.mu.RUnlock()
    
    if !ok {
        http.NotFound(w, r)
        return
    }
    
    w.Write([]byte(value))
}

func (s *KVStore) onAssigned(pt int, prev *clusterkit.Node) {
    log.Printf("Partition %d assigned", pt)
    s.mu.Lock()
    s.data[pt] = make(map[string]string)
    s.mu.Unlock()
    
    if prev != nil {
        s.pullData(pt, prev)
    }
}

func (s *KVStore) onUnassigned(pt int, newOwner *clusterkit.Node) {
    log.Printf("Partition %d moving", pt)
    s.mu.Lock()
    delete(s.data, pt)
    s.mu.Unlock()
}

func (s *KVStore) replicate(pt int, key, value string) {
    replicas := s.ck.GetReplicas(pt)
    for _, replica := range replicas {
        if replica.ID == s.ck.NodeID() {
            continue
        }
        
        go func(node *clusterkit.Node) {
            http.Post(
                fmt.Sprintf("%s/replicate?key=%s&value=%s", 
                    node.HTTPAddr(), key, value),
                "application/json", nil,
            )
        }(replica)
    }
}

func (s *KVStore) pullData(pt int, node *clusterkit.Node) {
    // Pull data from previous owner
    resp, _ := http.Get(fmt.Sprintf("%s/dump?partition=%d", 
        node.HTTPAddr(), pt))
    defer resp.Body.Close()
    
    var data map[string]string
    json.NewDecoder(resp.Body).Decode(&data)
    
    s.mu.Lock()
    s.data[pt] = data
    s.mu.Unlock()
}
```

---

## 🎯 Best Practices

### 1. Partition Sizing

- **Too few**: Limited parallelism
- **Too many**: Higher overhead
- **Recommended**: `partitions = nodes * 8 to 32`

```go
// For 10 nodes
Partitions: 256  // 25.6 partitions per node
```

### 2. Replica Factor

```go
ReplicaFactor: 3  // Standard (tolerates 2 failures)
```

### 3. Data Migration

Stream data in chunks with rate limiting:

```go
func handlePartitionAssigned(pt int, prev *Node) {
    limiter := rate.NewLimiter(1000, 1000) // 1000 keys/sec
    
    for offset := 0; ; offset += 1000 {
        chunk := pullChunk(prev, pt, offset, 1000)
        if len(chunk) == 0 {
            break
        }
        
        for _, kv := range chunk {
            limiter.Wait(ctx)
            myDB.Put(pt, kv.Key, kv.Value)
        }
    }
}
```

### 4. Error Handling

Always handle errors gracefully:

```go
ck, err := clusterkit.Join(cfg)
if err != nil {
    log.Fatalf("Failed to join cluster: %v", err)
}

defer func() {
    if err := ck.Leave(); err != nil {
        log.Printf("Error leaving cluster: %v", err)
    }
}()
```

---

## 🔍 Observability

### Prometheus Metrics

```go
// Node metrics
clusterkit_nodes_total
clusterkit_partitions_assigned{node_id}
clusterkit_partitions_leader{node_id}

// Rebalancing metrics
clusterkit_rebalance_total
clusterkit_rebalance_duration_seconds
clusterkit_partition_migrations_total

// etcd metrics
clusterkit_etcd_operations_total{op}
clusterkit_etcd_operation_duration_seconds{op}
```

### Health Checks

```go
func handleHealth(w http.ResponseWriter, r *http.Request) {
    if !ck.IsHealthy() {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status":     "healthy",
        "node_id":    ck.NodeID(),
        "partitions": len(ck.GetPartitionMap()),
    })
}
```

---

## 🐛 Troubleshooting

### Nodes Not Discovering Each Other

```bash
# Check etcd connectivity
etcdctl --endpoints=http://localhost:2379 get /clusterkit/nodes --prefix

# Watch for registrations
etcdctl watch /clusterkit/nodes --prefix
```

### Partition Rebalancing Not Happening

- Wait for `RebalanceDelay` (default 5s)
- Check logs for errors
- Verify etcd is accessible

### Slow Data Migration

Add rate limiting in hooks:

```go
limiter := rate.NewLimiter(1000, 1000)
for data := range stream {
    limiter.Wait(ctx)
    myDB.Put(pt, data.Key, data.Value)
}
```

### etcd Storage Full

```bash
# Compact and defragment
etcdctl compact $(etcdctl endpoint status --write-out="json" | jq -r '.[0].Status.header.revision')
etcdctl defrag
```

---

## 📄 License

Apache License 2.0

---

## 🙏 Acknowledgments

- Built on [etcd](https://etcd.io/) for coordination
- Inspired by Kafka, Cassandra, and Redis Cluster
- Consistent hashing from [groupcache](https://github.com/golang/groupcache)

---

**ClusterKit — Embed clustering into your Go applications with ease**#   c l u s t e r k i t 
 
 
