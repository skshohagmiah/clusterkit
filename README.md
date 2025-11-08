# ClusterKit

**A lightweight Go library for building distributed systems**

ClusterKit handles cluster coordination (partitioning, replication, consensus) so you can focus on your application logic.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## What is ClusterKit?

ClusterKit tells you **WHERE** data should go. You decide **HOW** to store it.

**ClusterKit handles:**
- ✅ Partition assignment (which partition for a key)
- ✅ Primary/replica designation (which nodes store data)
- ✅ Cluster membership (node discovery and health)
- ✅ Rebalancing (when nodes join/leave)
- ✅ Consensus (Raft-based leader election)

**You handle:**
- 🔧 Data storage (your choice: PostgreSQL, Redis, files, etc.)
- 🔧 Data replication (your protocol: HTTP, gRPC, TCP, etc.)
- 🔧 Business logic

---

## Features

- **Simple API** - 7 methods + 1 hook
- **Minimal Config** - Only 2 required fields (NodeID, HTTPAddr)
- **Auto-Rebalancing** - Partitions rebalance when nodes change
- **Data Migration Hooks** - Get notified when to move data
- **Raft Consensus** - Built on HashiCorp Raft
- **HTTP API** - RESTful cluster info endpoint
- **Production Ready** - WAL, snapshots, persistence

---

## Zero-Downtime Failover

ClusterKit implements **client-side retry with replica fallback** for instant failover when nodes fail:

```
Normal:    Client → Primary → Success ✅
Failover:  Client → Primary ❌ → Replica → Success ✅ (<100ms)
```

**Key Benefits:**
- ⚡ **<100ms failover** - Instant retry on replicas
- 🔄 **Zero data loss** - Replicas have the data
- 🚀 **No blocking** - Operations continue during rebalancing
- 📊 **Eventual consistency** - Data syncs within 30s

### How It Works

When a primary node fails:
1. Client detects connection error immediately
2. Client retries on replica nodes (no delay)
3. Replica accepts write and returns success
4. Topology refresh triggered in background
5. Rebalancing happens without blocking operations

**Example:**
```go
// Client automatically retries on replicas
err := client.Set("user:123", "data")
// If primary fails, client tries replicas immediately
// User sees <100ms latency, not 20-30s downtime!
```

See [PARTITIONING.md](PARTITIONING.md) for detailed documentation on partitioning, replication, and failover.

---

## Data Migration & Partition Changes

ClusterKit provides a powerful hook system that notifies you when partitions are reassigned. The hook receives **ALL nodes that have the data**, allowing you to merge from multiple sources to prevent data loss.

### OnPartitionChange Hook

```go
// Register hook to handle partition changes
ck.OnPartitionChange(func(partitionID string, copyFromNodes []*Node, copyToNode *Node) {
    if copyToNode.ID != myNodeID {
        return // Not for me
    }
    
    log.Printf("📦 Migrating %s from %d nodes", partitionID, len(copyFromNodes))
    
    // Merge data from ALL source nodes
    mergedData := make(map[string]string)
    
    for _, sourceNode := range copyFromNodes {
        data := fetchDataFromNode(sourceNode, partitionID)
        
        // Merge with your conflict resolution strategy
        for key, value := range data {
            mergedData[key] = value  // Last-write-wins
            // OR: Use version numbers, timestamps, etc.
        }
    }
    
    // Store merged data
    storeData(mergedData)
    log.Printf("✅ Migrated %d keys", len(mergedData))
})
```

### Why Multiple Source Nodes?

When a primary fails and replicas accept writes during failover, different replicas may have different data. ClusterKit provides **ALL nodes that had the partition** so you can merge them:

```
Before:  partition-37: Primary=node-2 ☠️, Replicas=[node-3, node-4]
         └─ Client writes go to node-3 during failover

After:   partition-37: Primary=node-5, Replicas=[node-3, node-6]
         └─ node-5 receives: copyFromNodes=[node-3, node-4]
         └─ Merges data from BOTH to prevent loss!
```

### Conflict Resolution Strategies

**1. Last-Write-Wins (Simple)**
```go
for key, value := range data {
    mergedData[key] = value  // Latest overwrites
}
```

**2. Version-Based (Recommended)**
```go
type ValueWithVersion struct {
    Value   string
    Version int64  // Timestamp
}

for key, newValue := range data {
    existing := mergedData[key]
    if newValue.Version > existing.Version {
        mergedData[key] = newValue  // Keep newer
    }
}
```

**3. Custom Logic**
```go
for key, newValue := range data {
    existing := mergedData[key]
    mergedData[key] = myCustomMerge(existing, newValue)
}
```

### When Hooks Are Triggered

- ✅ Node joins cluster (new partitions assigned)
- ✅ Node leaves cluster (partitions reassigned)
- ✅ Primary node fails (replica promoted to primary)
- ✅ Rebalancing completes (partition ownership changes)

**Important:** Hooks run in **background goroutines** (max 50 concurrent). Your application continues serving requests during migration.

---

## Installation

```bash
go get github.com/skshohagmiah/clusterkit
```

---

## Quick Start

### Bootstrap Node (First Node)

```go
package main

import (
    "log"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Create first node - only 2 fields required!
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-1",
        HTTPAddr: ":8080",
        // Bootstrap: true (auto-detected for first node)
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()
    
    log.Println("✅ Bootstrap node started on :8080")
    
    // Your application logic here
    select {} // Keep running
}
```

### Additional Nodes (Join Cluster)

```go
package main

import (
    "log"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Create node that joins existing cluster
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-2",
        HTTPAddr: ":8081",
        JoinAddr: "localhost:8080", // Bootstrap node address
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()
    
    log.Println("✅ Node-2 joined cluster via :8080")
    
    select {}
}
```

### Using the API

```go
package main

import (
    "fmt"
    "log"
    "sync"
    "github.com/skshohagmiah/clusterkit"
)

type KVStore struct {
    ck    *clusterkit.ClusterKit
    data  map[string]string
    mu    sync.RWMutex
}

func (kv *KVStore) Set(key, value string) error {
    // 1. Get partition for this key
    partition, err := kv.ck.GetPartition(key)
    if err != nil {
        return err
    }
    
    // 2. Am I the primary for this partition?
    if kv.ck.IsPrimary(partition) {
        // YES - Store locally
        kv.mu.Lock()
        kv.data[key] = value
        kv.mu.Unlock()
        
        fmt.Printf("✅ Stored %s=%s (I'm primary)\n", key, value)
        
        // Replicate to replicas
        replicas := kv.ck.GetReplicas(partition)
        for _, replica := range replicas {
            go kv.replicateToNode(replica, key, value)
        }
        return nil
    }
    
    // 3. Am I a replica?
    if kv.ck.IsReplica(partition) {
        // YES - Just store it
        kv.mu.Lock()
        kv.data[key] = value
        kv.mu.Unlock()
        
        fmt.Printf("✅ Stored %s=%s (I'm replica)\n", key, value)
        return nil
    }
    
    // 4. I'm neither - forward to primary
    primary := kv.ck.GetPrimary(partition)
    fmt.Printf("⏩ Forwarding to primary: %s\n", primary.ID)
    return kv.forwardToPrimary(primary, key, value)
}

func (kv *KVStore) Get(key string) (string, error) {
    // 1. Get partition for this key
    partition, err := kv.ck.GetPartition(key)
    if err != nil {
        return "", err
    }
    
    // 2. Am I primary or replica for this partition?
    if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
        // YES - Read locally
        kv.mu.RLock()
        defer kv.mu.RUnlock()
        
        value, exists := kv.data[key]
        if !exists {
            return "", fmt.Errorf("key not found")
        }
        return value, nil
    }
    
    // 3. I don't own this partition - forward to primary
    primary := kv.ck.GetPrimary(partition)
    return kv.readFromNode(primary, key)
}

func main() {
    ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-1",
        HTTPAddr: ":8080",
    })
    ck.Start()
    defer ck.Stop()
    
    kv := &KVStore{
        ck:   ck,
        data: make(map[string]string),
    }
    
    // Use the KV store
    kv.Set("user:123", "John Doe")
    kv.Set("user:456", "Jane Smith")
    
    value, _ := kv.Get("user:123")
    fmt.Println("Value:", value)
}
```

---

## API Reference

ClusterKit has **7 core methods + 1 hook**:

### Core Methods

```go
// 1. Get partition for a key
partition, err := ck.GetPartition(key string) (*Partition, error)

// 2. Get primary node for partition
primary := ck.GetPrimary(partition *Partition) *Node

// 3. Get replica nodes for partition
replicas := ck.GetReplicas(partition *Partition) []Node

// 4. Get all nodes (primary + replicas)
nodes := ck.GetNodes(partition *Partition) []Node

// 5. Check if I'm the primary
isPrimary := ck.IsPrimary(partition *Partition) bool

// 6. Check if I'm a replica
isReplica := ck.IsReplica(partition *Partition) bool

// 7. Get my node ID
myID := ck.GetMyNodeID() string
```

### Data Migration Hook

```go
// Register hook during initialization
ck.OnPartitionChange(func(partitionID string, copyFrom, copyTo *Node) {
    // Only act if I'm the destination node
    if copyTo == nil || copyTo.ID != ck.GetMyNodeID() {
        return
    }
    
    if copyFrom == nil {
        log.Printf("📦 New partition %s assigned (no data to copy)\n", partitionID)
        return
    }
    
    log.Printf("🔄 Migrating partition %s from %s\n", partitionID, copyFrom.ID)
    
    // Copy data from source node
    go migratePartitionData(partitionID, copyFrom)
})
```

**Complete Migration Example:**

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"
)

type KVStore struct {
    ck    *clusterkit.ClusterKit
    data  map[string]string
    mu    sync.RWMutex
}

func NewKVStore(ck *clusterkit.ClusterKit) *KVStore {
    kv := &KVStore{
        ck:   ck,
        data: make(map[string]string),
    }
    
    // Register migration hook
    ck.OnPartitionChange(func(partitionID string, copyFrom, copyTo *clusterkit.Node) {
        if copyTo == nil || copyTo.ID != ck.GetMyNodeID() {
            return
        }
        
        if copyFrom != nil {
            kv.migratePartition(partitionID, copyFrom)
        }
    })
    
    return kv
}

// Migrate partition data from another node
func (kv *KVStore) migratePartition(partitionID string, fromNode *clusterkit.Node) {
    log.Printf("🔄 Starting migration of %s from %s\n", partitionID, fromNode.ID)
    
    // 1. Fetch all keys for this partition from source node
    url := fmt.Sprintf("http://%s/migrate?partition=%s", fromNode.IP, partitionID)
    resp, err := http.Get(url)
    if err != nil {
        log.Printf("❌ Migration failed: %v\n", err)
        return
    }
    defer resp.Body.Close()
    
    // 2. Decode migration data
    var migrationData struct {
        PartitionID string            `json:"partition_id"`
        Keys        map[string]string `json:"keys"`
        Count       int               `json:"count"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&migrationData); err != nil {
        log.Printf("❌ Failed to decode: %v\n", err)
        return
    }
    
    // 3. Import all keys
    kv.mu.Lock()
    for key, value := range migrationData.Keys {
        kv.data[key] = value
    }
    kv.mu.Unlock()
    
    log.Printf("✅ Migrated %d keys for partition %s\n", migrationData.Count, partitionID)
}

// HTTP endpoint to export partition data (for migration)
func (kv *KVStore) handleMigrate(w http.ResponseWriter, r *http.Request) {
    partitionID := r.URL.Query().Get("partition")
    if partitionID == "" {
        http.Error(w, "partition required", http.StatusBadRequest)
        return
    }
    
    // Collect all keys for this partition
    kv.mu.RLock()
    partitionKeys := make(map[string]string)
    for key, value := range kv.data {
        partition, err := kv.ck.GetPartition(key)
        if err == nil && partition.ID == partitionID {
            partitionKeys[key] = value
        }
    }
    kv.mu.RUnlock()
    
    // Return data
    json.NewEncoder(w).Encode(map[string]interface{}{
        "partition_id": partitionID,
        "keys":         partitionKeys,
        "count":        len(partitionKeys),
    })
}

func main() {
    ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-1",
        HTTPAddr: ":8080",
    })
    ck.Start()
    defer ck.Stop()
    
    kv := NewKVStore(ck)
    
    // Register migration endpoint
    http.HandleFunc("/migrate", kv.handleMigrate)
    go http.ListenAndServe(":9080", nil)
    
    select {}
}
```

**When it fires:**
- **Node joins** → Partitions rebalance → Hook fires on new node
- **Node dies** → Partitions reassigned → Hook fires on replacement nodes
- **You get:** partition ID, source node (copyFrom), destination node (copyTo)

---

## Examples

ClusterKit provides **3 complete examples** showing different replication strategies:

### 1. Client-Side SYNC (Quorum-Based)

**Best for:** Strong consistency, financial transactions

Client writes to all nodes (primary + replicas) and waits for quorum (2/3).

**How it works:**
```go
// Client writes to ALL nodes (primary + replicas)
client.Set("key", "value")

// Internally:
// 1. Get partition nodes from ClusterKit topology
// 2. Write to primary + replicas in parallel
// 3. Wait for quorum (2/3 nodes)
// 4. Return success only if quorum reached
```

**Key Points:**
- ✅ Data on 2+ nodes before success
- ✅ Survives node failures
- ✅ Server is simple (just stores data)
- ✅ Client handles routing and quorum

**Consistency:** Strong (quorum - data on 2+ nodes before success)  
**Use Case:** Banking, inventory, critical data

[View Complete Example →](./example/sync/)

---

### 2. Client-Side ASYNC (Primary-First) ⚡

**Best for:** Maximum throughput, Kafka-like streaming

Client writes to primary, returns immediately. Replication happens in background.

**How it works:**
```go
// Client writes to PRIMARY only, returns immediately
client.Set("key", "value")  // Returns fast!

// Internally:
// 1. Get primary node from ClusterKit topology
// 2. Write to primary (blocking - but fast!)
// 3. Return success immediately
// 4. Replicate to replicas in background (async)
```

**Key Points:**
- ⚡ Fastest approach - returns immediately
- ✅ Primary has data right away
- ✅ Replicas catch up in background
- ✅ Server is simple (just stores data)
- ✅ Client handles routing and replication

**Consistency:** Eventual (primary has data immediately, replicas catch up)  
**Use Case:** Streaming, analytics, user sessions, caching

[View Complete Example →](./example/async/)

---

### 3. Server-Side Routing

**Best for:** Simple clients, web/mobile apps

Client sends to any node. Server handles routing and replication.

**How it works:**
```bash
# Client sends to ANY node (no SDK needed)
curl -X POST http://any-node:10080/kv/set \
  -d '{"key":"test","value":"hello"}'

# Server checks:
# - Am I primary? → Store + replicate to replicas
# - Am I replica? → Just store
# - Neither? → Forward to primary
```

**Key Points:**
- 🌐 Simple HTTP clients (no SDK)
- ✅ Server handles all routing logic
- ✅ Server controls replication strategy
- ✅ Traditional request/response pattern

**Consistency:** Configurable (server decides)  
**Use Case:** Web/mobile apps, simple HTTP clients

[View Complete Example →](./example/server-side/)

---

## Which Strategy Should You Choose?

| Your Use Case | Best Choice | Why? |
|--------------|-------------|------|
| Financial transactions | Client-Side SYNC 🔒 | Strong consistency required |
| Kafka-like streaming | Client-Side ASYNC ⚡ | High throughput needed |
| User sessions/cache | Client-Side ASYNC ⚡ | Fast writes, eventual consistency OK |
| Inventory management | Client-Side SYNC 🔒 | Cannot lose data |
| Real-time analytics | Client-Side ASYNC ⚡ | Speed over consistency |
| Web/mobile apps | Server-Side 🌐 | Simple HTTP clients |
| Microservices | Client-Side ASYNC ⚡ | Direct routing, no extra hops |

---

## Running the Examples

```bash
# SYNC - Strong consistency
cd example/sync && ./run.sh

# ASYNC - Maximum speed
cd example/async && ./run.sh

# Server-Side - Simple clients
cd example/server-side && ./run.sh
```

Each example runs a 6-10 node cluster with 1000 operations and shows:
- ✅ Cluster formation
- ✅ Data replication
- ✅ Automatic migration
- ✅ Performance metrics

---

## How It Works

### Architecture

```
┌─────────────────────────────────────┐
│         Your Application            │
│  (KV Store, Cache, Queue, etc.)     │
├─────────────────────────────────────┤
│          ClusterKit API             │
│  GetPartition() IsPrimary() etc.    │
├─────────────────────────────────────┤
│        ClusterKit Core              │
│  ┌──────────┬──────────┬─────────┐  │
│  │ Raft     │ Partition│ HTTP    │  │
│  │ Consensus│ Manager  │ API     │  │
│  └──────────┴──────────┴─────────┘  │
└─────────────────────────────────────┘
```

### Data Flow Example

```
1. Client: "Where does 'user:123' go?"
   ClusterKit: "Partition-5 → Node-2 (primary), Node-3, Node-4 (replicas)"

2. Your App: "Am I primary for partition-5?"
   ClusterKit: "Yes, you're node-2"

3. Your App: Stores data locally, replicates to node-3 and node-4

4. Node-5 joins cluster
   ClusterKit: Rebalances partitions, fires OnPartitionChange hook

5. Your App: Migrates data to node-5 based on hook notification
```

---

## Configuration

### Minimal (2 fields required)

```go
ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:   "node-1",  // Required
    HTTPAddr: ":8080",   // Required
})
```

### Production

```go
ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:            "node-1",
    HTTPAddr:          ":8080",
    DataDir:           "/var/lib/clusterkit",
    PartitionCount:    64,           // More partitions = better distribution
    ReplicationFactor: 3,            // Survive 2 node failures
    JoinAddr:          "node-1:8080", // For non-bootstrap nodes
})
```

---

## HTTP API

ClusterKit exposes a RESTful API for cluster information:

```bash
# Get cluster state
curl http://localhost:8080/cluster

# Response
{
  "cluster": {
    "nodes": {
      "node-1": {"id": "node-1", "ip": ":8080"},
      "node-2": {"id": "node-2", "ip": ":8081"}
    },
    "partition_map": {
      "partition-0": {
        "id": "partition-0",
        "primary_node": "node-1",
        "replica_nodes": ["node-2", "node-3"]
      }
    }
  },
  "hash_config": {
    "algorithm": "md5",
    "modulo": 64,
    "format": "partition-%d"
  }
}
```

---

## Use Cases

ClusterKit is perfect for building:

- **Distributed KV Stores** - Like Redis Cluster
- **Distributed Caches** - Like Memcached clusters
- **Message Queues** - Like Kafka
- **Session Stores** - Distributed session management
- **File Storage** - Distributed file systems
- **Time-Series DBs** - Partitioned time-series data
- **Custom Systems** - Any distributed data system

---

## Running with Docker

### Dockerfile

```dockerfile
FROM golang:1.21-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o server ./cmd/server

EXPOSE 8080

CMD ["./server"]
```

### Server with Environment Variables

```go
package main

import (
    "log"
    "os"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Read config from environment variables
    nodeID := os.Getenv("NODE_ID")
    httpPort := os.Getenv("HTTP_PORT")
    joinAddr := os.Getenv("JOIN_ADDR")
    dataDir := os.Getenv("DATA_DIR")
    
    if nodeID == "" || httpPort == "" {
        log.Fatal("NODE_ID and HTTP_PORT required")
    }
    
    if dataDir == "" {
        dataDir = "/data"
    }
    
    // Create ClusterKit instance
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   nodeID,
        HTTPAddr: ":" + httpPort,
        DataDir:  dataDir,
        JoinAddr: joinAddr,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()
    
    log.Printf("✅ Node %s started on port %s\n", nodeID, httpPort)
    
    // Your application logic here
    select {}
}
```

### docker-compose.yml

```yaml
version: '3.8'

services:
  node-1:
    build: .
    container_name: clusterkit-node-1
    environment:
      - NODE_ID=node-1
      - HTTP_PORT=8080
      - DATA_DIR=/data
    ports:
      - "8080:8080"
    volumes:
      - node1-data:/data
    networks:
      - clusterkit

  node-2:
    build: .
    container_name: clusterkit-node-2
    environment:
      - NODE_ID=node-2
      - HTTP_PORT=8080
      - JOIN_ADDR=node-1:8080
      - DATA_DIR=/data
    ports:
      - "8081:8080"
    volumes:
      - node2-data:/data
    networks:
      - clusterkit
    depends_on:
      - node-1

  node-3:
    build: .
    container_name: clusterkit-node-3
    environment:
      - NODE_ID=node-3
      - HTTP_PORT=8080
      - JOIN_ADDR=node-1:8080
      - DATA_DIR=/data
    ports:
      - "8082:8080"
    volumes:
      - node3-data:/data
    networks:
      - clusterkit
    depends_on:
      - node-1

volumes:
  node1-data:
  node2-data:
  node3-data:

networks:
  clusterkit:
    driver: bridge
```

### Running the Cluster

```bash
# Start all nodes
docker-compose up -d

# Check cluster status
curl http://localhost:8080/cluster | jq

# Scale to 5 nodes
docker-compose up -d --scale node-2=5

# View logs
docker-compose logs -f

# Stop cluster
docker-compose down
```

### Kubernetes Example

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: clusterkit
spec:
  serviceName: clusterkit
  replicas: 3
  selector:
    matchLabels:
      app: clusterkit
  template:
    metadata:
      labels:
        app: clusterkit
    spec:
      containers:
      - name: clusterkit
        image: your-registry/clusterkit:latest
        env:
        - name: NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: HTTP_PORT
          value: "8080"
        - name: JOIN_ADDR
          value: "clusterkit-0.clusterkit:8080"
        - name: DATA_DIR
          value: "/data"
        ports:
        - containerPort: 8080
          name: http
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
---
apiVersion: v1
kind: Service
metadata:
  name: clusterkit
spec:
  clusterIP: None
  selector:
    app: clusterkit
  ports:
  - port: 8080
    name: http
```

---

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Learn More

- [Complete Examples](./example/) - 3 working implementations
- [Architecture Guide](./example/ARCHITECTURE.md) - Deep dive
- [Docker Setup](./example/DOCKER.md) - Run with Docker Compose
# Flin
