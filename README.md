# ClusterKit

**A lightweight, production-ready distributed cluster coordination library for Go**

ClusterKit is your "GPS for distributed systems" - it tells you **WHERE** data should go, while you decide **HOW** to store it. Built on HashiCorp Raft for strong consistency, ClusterKit handles all the complex coordination logic so you can focus on building your distributed application.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## 🎯 What is ClusterKit?

ClusterKit provides **cluster coordination** without dictating your data storage or replication strategy. Think of it as the foundation layer that handles:

- ✅ **Partition Management** - Consistent hashing to determine which partition owns a key
- ✅ **Node Discovery** - Automatic cluster membership and health monitoring
- ✅ **Leader Election** - Raft-based consensus for cluster decisions
- ✅ **Rebalancing** - Automatic partition redistribution when nodes join/leave
- ✅ **Event Hooks** - Rich notifications for partition changes, node lifecycle events
- ✅ **Failure Detection** - Automatic health checking and node removal
- ✅ **Rejoin Handling** - Smart detection and data sync for returning nodes

**You control:**
- 🔧 Data storage (PostgreSQL, Redis, files, memory, etc.)
- 🔧 Replication protocol (HTTP, gRPC, TCP, etc.)
- 🔧 Consistency model (strong, eventual, causal, etc.)
- 🔧 Business logic

---

## ✨ Key Features

### Core Capabilities
- **Simple API** - 7 core methods + rich event hooks
- **Minimal Configuration** - Only 2 required fields (NodeID, HTTPAddr)
- **Production-Ready** - WAL, snapshots, crash recovery, metrics
- **Health Checking** - Automatic failure detection and node removal
- **Smart Rejoin** - Detects returning nodes and triggers data sync

### Event System
- **Rich Context** - Events include timestamps, reasons, offline duration, partition ownership
- **7 Lifecycle Hooks** - OnPartitionChange, OnNodeJoin, OnNodeRejoin, OnNodeLeave, OnRebalanceStart, OnRebalanceComplete, OnClusterHealthChange
- **Async Execution** - Hooks run in background goroutines (max 50 concurrent)
- **Panic Recovery** - Hooks are isolated and won't crash your application

### Distributed Coordination
- **Raft Consensus** - Built on HashiCorp Raft for strong consistency
- **Consistent Hashing** - MD5-based partition assignment
- **Configurable Replication** - Set replication factor (default: 3)
- **HTTP API** - RESTful endpoints for cluster information

---

## 📦 Installation

```bash
go get github.com/skshohagmiah/clusterkit
```

---

## 🚀 Quick Start

### Bootstrap Node (First Node)

```go
package main

import (
    "log"
    "time"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Create first node - only 2 fields required!
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-1",
        HTTPAddr: ":8080",
        // Optional: Enable health checking
        HealthCheck: clusterkit.HealthCheckConfig{
            Enabled:          true,
            Interval:         5 * time.Second,
            Timeout:          2 * time.Second,
            FailureThreshold: 3,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()
    
    log.Println("✅ Bootstrap node started on :8080")
    
    select {} // Keep running
}
```

### Additional Nodes (Join Cluster)

```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:   "node-2",
    HTTPAddr: ":8081",
    JoinAddr: "localhost:8080", // Bootstrap node address
    HealthCheck: clusterkit.HealthCheckConfig{
        Enabled:          true,
        Interval:         5 * time.Second,
        Timeout:          2 * time.Second,
        FailureThreshold: 3,
    },
})
```

---

## 📚 API Reference

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

### Cluster Operations

```go
// Get cluster information
cluster := ck.GetCluster() *Cluster

// Trigger manual rebalancing
err := ck.RebalancePartitions() error

// Get metrics
metrics := ck.GetMetrics() *Metrics

// Health check
health := ck.HealthCheck() *HealthStatus
```

---

## 🎣 Event Hooks System

ClusterKit provides a comprehensive event system with rich context for all cluster lifecycle events.

### 1. OnPartitionChange - Partition Assignment Changes

**Triggered when:** Partitions are reassigned due to rebalancing

```go
ck.OnPartitionChange(func(event *clusterkit.PartitionChangeEvent) {
    // Only act if I'm the destination node
    if event.CopyToNode.ID != myNodeID {
        return
    }
    
    log.Printf("📦 Partition %s moving (reason: %s)", 
        event.PartitionID, event.ChangeReason)
    log.Printf("   From: %d nodes", len(event.CopyFromNodes))
    log.Printf("   Primary changed: %s → %s", event.OldPrimary, event.NewPrimary)
    
    // Fetch and merge data from all source nodes
    for _, source := range event.CopyFromNodes {
        data := fetchPartitionData(source, event.PartitionID)
        mergeData(data)
    }
})
```

**Event Structure:**
```go
type PartitionChangeEvent struct {
    PartitionID   string    // e.g., "partition-5"
    CopyFromNodes []*Node   // Nodes that have the data
    CopyToNode    *Node     // Node that needs the data
    ChangeReason  string    // "node_join", "node_leave", "rebalance"
    OldPrimary    string    // Previous primary node ID
    NewPrimary    string    // New primary node ID
    Timestamp     time.Time // When the change occurred
}
```

**Use Cases:**
- Migrate data when partitions move
- Update local indexes
- Trigger background sync jobs

---

### 2. OnNodeJoin - New Node Joins Cluster

**Triggered when:** A brand new node joins the cluster

```go
ck.OnNodeJoin(func(event *clusterkit.NodeJoinEvent) {
    log.Printf("🎉 Node %s joined (cluster size: %d)", 
        event.Node.ID, event.ClusterSize)
    
    if event.IsBootstrap {
        log.Println("   This is the bootstrap node - initializing cluster")
        initializeSchema()
    }
    
    // Update monitoring dashboards
    updateNodeCount(event.ClusterSize)
})
```

**Event Structure:**
```go
type NodeJoinEvent struct {
    Node        *Node     // The joining node
    ClusterSize int       // Total nodes after join
    IsBootstrap bool      // Is this the first node?
    Timestamp   time.Time // When the node joined
}
```

**Use Cases:**
- Initialize cluster-wide resources on bootstrap
- Update monitoring/alerting systems
- Trigger capacity planning checks

---

### 3. OnNodeRejoin - Node Returns After Being Offline

**Triggered when:** A node that was previously in the cluster rejoins

```go
ck.OnNodeRejoin(func(event *clusterkit.NodeRejoinEvent) {
    if event.Node.ID == myNodeID {
        log.Printf("🔄 I'm rejoining after %v offline", event.OfflineDuration)
        log.Printf("   Last seen: %v", event.LastSeenAt)
        log.Printf("   Had %d partitions before leaving", 
            len(event.PartitionsBeforeLeave))
        
        // Clear stale data
        clearAllLocalData()
        
        // Wait for OnPartitionChange to sync fresh data
        log.Println("   Ready for partition reassignment")
    } else {
        log.Printf("📡 Node %s rejoined after %v", 
            event.Node.ID, event.OfflineDuration)
    }
})
```

**Event Structure:**
```go
type NodeRejoinEvent struct {
    Node                  *Node         // The rejoining node
    OfflineDuration       time.Duration // How long it was offline
    LastSeenAt            time.Time     // When it was last seen
    PartitionsBeforeLeave []string      // Partitions it had before
    Timestamp             time.Time     // When it rejoined
}
```

**Use Cases:**
- Clear stale local data before rebalancing
- Decide sync strategy based on offline duration
- Log rejoin events for debugging
- Alert if offline duration was too long

**Important:** This hook fires BEFORE rebalancing. Use it to prepare (clear data), then let `OnPartitionChange` handle the actual data sync with correct partition assignments.

---

### 4. OnNodeLeave - Node Leaves or Fails

**Triggered when:** A node is removed from the cluster (failure or graceful shutdown)

```go
ck.OnNodeLeave(func(event *clusterkit.NodeLeaveEvent) {
    log.Printf("❌ Node %s left (reason: %s)", 
        event.Node.ID, event.Reason)
    log.Printf("   Owned %d partitions (primary)", len(event.PartitionsOwned))
    log.Printf("   Replicated %d partitions", len(event.PartitionsReplica))
    
    // Clean up connections
    closeConnectionTo(event.Node.IP)
    
    // Alert if critical
    if event.Reason == "health_check_failure" && len(event.PartitionsOwned) > 10 {
        alertOps("High partition loss - node failed!")
    }
})
```

**Event Structure:**
```go
type NodeLeaveEvent struct {
    Node              *Node     // Full node info
    Reason            string    // "health_check_failure", "graceful_shutdown", "removed_by_admin"
    PartitionsOwned   []string  // Partitions this node was primary for
    PartitionsReplica []string  // Partitions this node was replica for
    Timestamp         time.Time // When it left
}
```

**Use Cases:**
- Clean up network connections
- Alert operations team
- Update capacity planning
- Log failure events

---

### 5. OnRebalanceStart - Rebalancing Begins

**Triggered when:** Partition rebalancing operation starts

```go
ck.OnRebalanceStart(func(event *clusterkit.RebalanceEvent) {
    log.Printf("⚖️  Rebalance starting (trigger: %s)", event.Trigger)
    log.Printf("   Triggered by: %s", event.TriggerNodeID)
    log.Printf("   Partitions to move: %d", event.PartitionsToMove)
    log.Printf("   Nodes affected: %v", event.NodesAffected)
    
    // Pause background jobs during rebalance
    pauseBackgroundJobs()
    
    // Increase operation timeouts
    increaseTimeouts()
})
```

**Event Structure:**
```go
type RebalanceEvent struct {
    Trigger          string    // "node_join", "node_leave", "manual"
    TriggerNodeID    string    // Which node caused it
    PartitionsToMove int       // How many partitions will move
    NodesAffected    []string  // Which nodes are affected
    Timestamp        time.Time // When rebalance started
}
```

---

### 6. OnRebalanceComplete - Rebalancing Finishes

**Triggered when:** Partition rebalancing operation completes

```go
ck.OnRebalanceComplete(func(event *clusterkit.RebalanceEvent, duration time.Duration) {
    log.Printf("✅ Rebalance completed in %v", duration)
    log.Printf("   Moved %d partitions", event.PartitionsToMove)
    
    // Resume background jobs
    resumeBackgroundJobs()
    
    // Reset timeouts
    resetTimeouts()
    
    // Update metrics
    recordRebalanceDuration(duration)
})
```

---

### 7. OnClusterHealthChange - Cluster Health Status Changes

**Triggered when:** Overall cluster health status changes

```go
ck.OnClusterHealthChange(func(event *clusterkit.ClusterHealthEvent) {
    log.Printf("🏥 Cluster health: %s", event.Status)
    log.Printf("   Healthy: %d/%d nodes", event.HealthyNodes, event.TotalNodes)
    
    if event.Status == "critical" {
        log.Printf("   Unhealthy nodes: %v", event.UnhealthyNodeIDs)
        alertOps("Cluster in critical state!")
        enableReadOnlyMode()
    } else if event.Status == "healthy" {
        log.Println("   All systems operational")
        disableReadOnlyMode()
    }
})
```

**Event Structure:**
```go
type ClusterHealthEvent struct {
    HealthyNodes     int       // Number of healthy nodes
    UnhealthyNodes   int       // Number of unhealthy nodes
    TotalNodes       int       // Total nodes in cluster
    Status           string    // "healthy", "degraded", "critical"
    UnhealthyNodeIDs []string  // IDs of unhealthy nodes
    Timestamp        time.Time // When health changed
}
```

---

## 🔄 Understanding Cluster Lifecycle

### Scenario 1: Node Join

```
1. New node starts and sends join request
   ↓
2. OnNodeJoin fires
   - Event includes: node info, cluster size, bootstrap flag
   ↓
3. OnRebalanceStart fires
   - Event includes: trigger reason, partitions to move
   ↓
4. Partitions are reassigned
   ↓
5. OnPartitionChange fires (multiple times, once per partition)
   - Event includes: partition ID, source nodes, destination node, reason
   ↓
6. OnRebalanceComplete fires
   - Event includes: duration, partitions moved
```

**Your Application:**
- In `OnNodeJoin`: Log event, update monitoring
- In `OnPartitionChange`: Migrate data for assigned partitions
- In `OnRebalanceComplete`: Resume normal operations

---

### Scenario 2: Node Failure & Removal

```
1. Health checker detects node failure (3 consecutive failures)
   ↓
2. OnNodeLeave fires
   - Event includes: node info, reason="health_check_failure", partitions owned
   ↓
3. OnRebalanceStart fires
   ↓
4. Partitions are reassigned to remaining nodes
   ↓
5. OnPartitionChange fires (for each reassigned partition)
   ↓
6. OnRebalanceComplete fires
```

**Your Application:**
- In `OnNodeLeave`: Clean up connections, alert ops team
- In `OnPartitionChange`: Take ownership of reassigned partitions
- Data already exists on replicas, so migration is fast!

---

### Scenario 3: Node Rejoin (After Failure)

```
1. Failed node restarts and rejoins
   ↓
2. OnNodeRejoin fires
   - Event includes: node info, offline duration, partitions before leave
   ↓
3. Your app clears stale local data
   ↓
4. OnRebalanceStart fires
   ↓
5. Partitions are reassigned (may be different than before!)
   ↓
6. OnPartitionChange fires (for each assigned partition)
   - Your app fetches fresh data from replicas
   ↓
7. OnRebalanceComplete fires
```

**Your Application:**
- In `OnNodeRejoin`: Clear ALL stale data (important!)
- In `OnPartitionChange`: Fetch fresh data for NEW partition assignments
- Don't assume you'll get the same partitions you had before!

**Why clear data?**
- Node was offline - data is stale
- Partition assignments may have changed
- Other nodes have the latest data
- Clean slate ensures consistency

---

## 💡 Complete Example: Building a Distributed KV Store

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"
    "time"
    
    "github.com/skshohagmiah/clusterkit"
)

type KVStore struct {
    ck          *clusterkit.ClusterKit
    data        map[string]string
    mu          sync.RWMutex
    nodeID      string
    isRejoining bool
    rejoinMu    sync.Mutex
}

func NewKVStore(ck *clusterkit.ClusterKit, nodeID string) *KVStore {
    kv := &KVStore{
        ck:     ck,
        data:   make(map[string]string),
        nodeID: nodeID,
    }
    
    // Register all hooks
    ck.OnPartitionChange(func(event *clusterkit.PartitionChangeEvent) {
        kv.handlePartitionChange(event)
    })
    
    ck.OnNodeRejoin(func(event *clusterkit.NodeRejoinEvent) {
        if event.Node.ID == kv.nodeID {
            kv.handleRejoin(event)
        }
    })
    
    ck.OnNodeLeave(func(event *clusterkit.NodeLeaveEvent) {
        log.Printf("[KV] Node %s left (reason: %s, partitions: %d)", 
            event.Node.ID, event.Reason, 
            len(event.PartitionsOwned)+len(event.PartitionsReplica))
    })
    
    return kv
}

func (kv *KVStore) handleRejoin(event *clusterkit.NodeRejoinEvent) {
    kv.rejoinMu.Lock()
    defer kv.rejoinMu.Unlock()
    
    if kv.isRejoining {
        return // Already rejoining
    }
    
    kv.isRejoining = true
    
    log.Printf("[KV] 🔄 Rejoining after %v offline", event.OfflineDuration)
    log.Printf("[KV] 🗑️  Clearing stale data")
    
    // Clear ALL stale data
    kv.mu.Lock()
    kv.data = make(map[string]string)
    kv.mu.Unlock()
    
    log.Printf("[KV] ✅ Ready for partition reassignment")
}

func (kv *KVStore) handlePartitionChange(event *clusterkit.PartitionChangeEvent) {
    if event.CopyToNode.ID != kv.nodeID {
        return
    }
    
    // Check if rejoining
    kv.rejoinMu.Lock()
    isRejoining := kv.isRejoining
    kv.rejoinMu.Unlock()
    
    if len(event.CopyFromNodes) == 0 {
        log.Printf("[KV] New partition %s assigned", event.PartitionID)
        return
    }
    
    log.Printf("[KV] 🔄 Migrating partition %s (reason: %s)", 
        event.PartitionID, event.ChangeReason)
    
    // Fetch data from source nodes
    for _, source := range event.CopyFromNodes {
        data := kv.fetchPartitionData(source, event.PartitionID)
        
        kv.mu.Lock()
        for key, value := range data {
            kv.data[key] = value
        }
        kv.mu.Unlock()
        
        log.Printf("[KV] ✅ Migrated %d keys from %s", len(data), source.ID)
        break // Successfully migrated
    }
    
    // Clear rejoin flag after first partition
    if isRejoining {
        kv.rejoinMu.Lock()
        kv.isRejoining = false
        kv.rejoinMu.Unlock()
        log.Printf("[KV] ✅ Rejoin complete")
    }
}

func (kv *KVStore) fetchPartitionData(node *clusterkit.Node, partitionID string) map[string]string {
    url := fmt.Sprintf("http://%s/migrate?partition=%s", node.IP, partitionID)
    resp, err := http.Get(url)
    if err != nil {
        return nil
    }
    defer resp.Body.Close()
    
    var result map[string]string
    json.NewDecoder(resp.Body).Decode(&result)
    return result
}

func (kv *KVStore) Set(key, value string) error {
    partition, err := kv.ck.GetPartition(key)
    if err != nil {
        return err
    }
    
    if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
        kv.mu.Lock()
        kv.data[key] = value
        kv.mu.Unlock()
        return nil
    }
    
    // Forward to primary
    primary := kv.ck.GetPrimary(partition)
    return kv.forwardToPrimary(primary, key, value)
}

func (kv *KVStore) Get(key string) (string, error) {
    partition, err := kv.ck.GetPartition(key)
    if err != nil {
        return "", err
    }
    
    if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
        kv.mu.RLock()
        defer kv.mu.RUnlock()
        
        value, exists := kv.data[key]
        if !exists {
            return "", fmt.Errorf("key not found")
        }
        return value, nil
    }
    
    // Forward to primary
    primary := kv.ck.GetPrimary(partition)
    return kv.readFromPrimary(primary, key)
}

func main() {
    ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:   "node-1",
        HTTPAddr: ":8080",
        HealthCheck: clusterkit.HealthCheckConfig{
            Enabled:          true,
            Interval:         5 * time.Second,
            Timeout:          2 * time.Second,
            FailureThreshold: 3,
        },
    })
    ck.Start()
    defer ck.Stop()
    
    kv := NewKVStore(ck, "node-1")
    
    // Use the KV store
    kv.Set("user:123", "John Doe")
    value, _ := kv.Get("user:123")
    fmt.Println("Value:", value)
    
    select {}
}
```

---

## 🏗️ Configuration Options

### Minimal (2 required fields)

```go
ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:   "node-1",  // Required
    HTTPAddr: ":8080",   // Required
})
```

### Production Configuration

```go
ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    // Required
    NodeID:   "node-1",
    HTTPAddr: ":8080",
    
    // Cluster Formation
    JoinAddr:  "node-1:8080", // For non-bootstrap nodes
    Bootstrap: false,          // Auto-detected
    
    // Partitioning
    PartitionCount:    64,  // More partitions = better distribution
    ReplicationFactor: 3,   // Survive 2 node failures
    
    // Storage
    DataDir: "/var/lib/clusterkit",
    
    // Health Checking
    HealthCheck: clusterkit.HealthCheckConfig{
        Enabled:          true,
        Interval:         5 * time.Second,  // Check every 5s
        Timeout:          2 * time.Second,  // Request timeout
        FailureThreshold: 3,                // Remove after 3 failures
    },
})
```

---

## 📊 HTTP API

ClusterKit exposes RESTful endpoints:

```bash
# Get cluster state
curl http://localhost:8080/cluster

# Get metrics
curl http://localhost:8080/metrics

# Get detailed health
curl http://localhost:8080/health/detailed

# Check if ready
curl http://localhost:8080/ready
```

---

## 🧪 Running Examples

ClusterKit includes 3 complete examples:

```bash
# SYNC - Strong consistency (quorum-based)
cd example/sync && ./run.sh

# ASYNC - Maximum throughput (eventual consistency)
cd example/async && ./run.sh

# Server-Side - Simple HTTP clients
cd example/server-side && ./run.sh
```

Each example demonstrates:
- ✅ Cluster formation (10 nodes)
- ✅ Data distribution (1000 keys)
- ✅ Automatic rebalancing
- ✅ Health checking and failure recovery
- ✅ Node rejoin handling

---

## 🐳 Docker Deployment

### docker-compose.yml

```yaml
version: '3.8'

services:
  node-1:
    image: your-registry/clusterkit:latest
    environment:
      - NODE_ID=node-1
      - HTTP_PORT=8080
      - DATA_DIR=/data
    ports:
      - "8080:8080"
    volumes:
      - node1-data:/data

  node-2:
    image: your-registry/clusterkit:latest
    environment:
      - NODE_ID=node-2
      - HTTP_PORT=8080
      - JOIN_ADDR=node-1:8080
      - DATA_DIR=/data
    ports:
      - "8081:8080"
    volumes:
      - node2-data:/data
    depends_on:
      - node-1

volumes:
  node1-data:
  node2-data:
```

---

## 🎓 Learn More

- **[PARTITIONING.md](PARTITIONING.md)** - Deep dive into partitioning and replication
- **[REBALANCING_BEHAVIOR.md](REBALANCING_BEHAVIOR.md)** - How rebalancing works
- **[DATA_SYNC_ON_REJOIN.md](DATA_SYNC_ON_REJOIN.md)** - Handling stale data on rejoin
- **[Examples](./example/)** - 3 complete working implementations

---

## 🤝 Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

---

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

---

## 🌟 Why ClusterKit?

**Simple** - 7 methods + hooks, minimal config  
**Flexible** - Bring your own storage and replication  
**Production-Ready** - Raft consensus, health checking, metrics  
**Well-Documented** - Comprehensive guides and examples  
**Battle-Tested** - Used in production distributed systems

Start building your distributed system today! 🚀
