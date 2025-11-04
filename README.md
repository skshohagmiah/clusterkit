# ClusterKit

> A lightweight, production-ready distributed cluster coordination library for Go

ClusterKit provides **cluster coordination** (nodes, partitions, consensus) while letting you handle your own data storage and replication logic. Built on **HashiCorp Raft** for strong consistency.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🎯 **Cluster Coordination** | Automatic node discovery and membership management |
| 📦 **Partition Management** | Consistent hashing for data distribution across nodes |
| 🔄 **Raft Consensus** | Production-grade consensus using HashiCorp Raft |
| 🎭 **Leader Election** | Automatic leader election for coordinated operations |
| 🔍 **Replica Discovery** | Find primary and replica nodes for any key |
| 🌐 **HTTP API** | RESTful endpoints for cluster management |
| 💾 **State Persistence** | WAL and snapshots for crash recovery |

## 🎯 What ClusterKit Does vs What You Do

**ClusterKit Provides:**
- ✅ Node membership and discovery
- ✅ Partition assignments (which node handles which data)
- ✅ Replica node discovery
- ✅ Leader election and consensus
- ✅ Cluster state synchronization

**You Implement:**
- 🔧 Data storage (PostgreSQL, Redis, MongoDB, etc.)
- 🔧 Data replication (sync/async, quorum, etc.)
- 🔧 Business logic and data models

**Think of ClusterKit as a GPS for your distributed system** - it tells you where data should go, you decide how to store it.

## Installation

```bash
go get github.com/skshohagmiah/clusterkit
```

## Quick Start

### Running Multiple Nodes

**Node 1 (Bootstrap - First Node):**
```go
package main

import (
    "log"
    "time"
    
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:       "node-1",
        NodeName:     "Server-1",
        HTTPAddr:     ":8080",
        Bootstrap:    true,  // First node bootstraps the cluster
        DataDir:      "./data/node1",
        SyncInterval: 5 * time.Second,
        Config: &clusterkit.Config{
            ClusterName:       "my-cluster",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    
    // Your application logic here
    select {}
}
```

**Node 2 (Joins Node 1):**
```go
package main

import (
    "log"
    "time"
    
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:       "node-2",
        NodeName:     "Server-2",
        HTTPAddr:     ":8081",
        JoinAddr:     "localhost:8080",  // Join via node-1
        DataDir:      "./data/node2",
        SyncInterval: 5 * time.Second,
        Config: &clusterkit.Config{
            ClusterName:       "my-cluster",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    
    // Your application logic here
    select {}
}
```

**Node 3 (Joins the Cluster):**
```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:       "node-3",
    NodeName:     "Server-3",
    HTTPAddr:     ":8082",
    JoinAddr:     "localhost:8080",  // Can join via any existing node
    DataDir:      "./data/node3",
    SyncInterval: 5 * time.Second,
    Config: &clusterkit.Config{
        ClusterName:       "my-cluster",
        PartitionCount:    16,
        ReplicationFactor: 3,
    },
})
```

## How It Works

1. **Initialization** - Each application instance initializes ClusterKit with a unique Node ID
2. **HTTP Server** - ClusterKit starts an HTTP server for inter-node communication
3. **Discovery** - New nodes connect to known nodes and exchange cluster state
4. **State Sync** - Nodes periodically sync state with each other (default: 5 seconds)
5. **Persistence** - Each node saves cluster state to disk in `cluster-state.json`
6. **WAL Logging** - All operations are logged to `wal.log` for durability

## API Reference

### Creating a ClusterKit Instance

```go
type Options struct {
    NodeID        string        // Unique ID for this node
    NodeName      string        // Human-readable name
    HTTPAddr      string        // Address to listen on (e.g., ":8080")
    KnownNodes    []string      // List of known node addresses
    DataDir       string        // Directory to store state and WAL
    SyncInterval  time.Duration // How often to sync with other nodes
    Config        *Config       // Cluster configuration
}

ck, err := clusterkit.NewClusterKit(options)
```

### Starting ClusterKit

```go
err := ck.Start()
```

### Getting Cluster State

```go
cluster := ck.GetCluster()
fmt.Printf("Cluster: %s\n", cluster.Name)
fmt.Printf("Total Nodes: %d\n", len(cluster.Nodes))
```

### Stopping ClusterKit

```go
err := ck.Stop() // Saves state before shutdown
```

## Environment Variables Example

You can use environment variables to configure nodes:

```bash
# Node 1 (Bootstrap)
NODE_ID=node-1 NODE_NAME=Server-1 HTTP_ADDR=:8080 BOOTSTRAP=true DATA_DIR=./data/node1 go run main.go

# Node 2 (Join)
NODE_ID=node-2 NODE_NAME=Server-2 HTTP_ADDR=:8081 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node2 go run main.go

# Node 3 (Join)
NODE_ID=node-3 NODE_NAME=Server-3 HTTP_ADDR=:8082 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node3 go run main.go
```

## HTTP Endpoints

ClusterKit exposes these endpoints for inter-node communication:

### Cluster Management
- `GET /health` - Health check
- `POST /join` - Node registration
- `POST /sync` - State synchronization
- `GET /cluster` - Get cluster state

### Partition Management
- `GET /partitions` - List all partitions
- `GET /partitions/stats` - Get partition statistics
- `GET /partitions/key?key=<key>` - Get partition for a specific key

### Consensus & Leader Election
- `GET /consensus/leader` - Get current leader information
- `GET /consensus/stats` - Get consensus statistics (term, leader, etc.)

## Data Persistence

Each node stores:
- **cluster-state.json** - Current cluster state (nodes, partitions, config)
- **wal.log** - Write-ahead log of all operations

These files are stored in the `DataDir` specified in options.

## Using ClusterKit with Your Data

ClusterKit provides partition information - you implement storage and replication:

```go
// 1. Create partitions
ck.CreatePartitions()

// 2. Find partition for your data
partition, _ := ck.GetPartitionForKey("user:1001")

// 3. Implement your storage logic
if partition.PrimaryNode == myNodeID {
    // This node is primary - write to your database
    myDB.Write("user:1001", userData)
    
    // Replicate to replica nodes (your implementation)
    for _, replicaNode := range partition.ReplicaNodes {
        sendToNode(replicaNode, "user:1001", userData)
    }
} else {
    // Forward to primary node
    forwardToPrimary(partition.PrimaryNode, "user:1001", userData)
}

// 4. Read from any replica
if isNodeInPartition(myNodeID, partition) {
    data := myDB.Read("user:1001")
} else {
    data := readFromNode(partition.PrimaryNode, "user:1001")
}
```

### Example: Distributed Key-Value Store

```go
type DistributedKV struct {
    ck       *clusterkit.ClusterKit
    localDB  map[string][]byte  // Your storage (Redis, etc.)
}

func (kv *DistributedKV) Set(key string, value []byte) error {
    // Check if this node should handle the key
    isPrimary, _ := kv.ck.IsPrimaryForKey(key)
    
    if isPrimary {
        // Store locally
        kv.localDB[key] = value
        
        // Get replica nodes and replicate
        replicas, _ := kv.ck.GetReplicaNodes(key)
        for _, replica := range replicas {
            kv.replicateToNode(replica, key, value)
        }
        return nil
    }
    
    // Forward to primary
    primary, _ := kv.ck.GetPrimaryNode(key)
    return kv.forwardToPrimary(primary, key, value)
}

func (kv *DistributedKV) Get(key string) ([]byte, error) {
    // Check if this node has the data
    shouldHandle, role, _ := kv.ck.ShouldHandleKey(key)
    
    if shouldHandle {
        // Read from local storage
        return kv.localDB[key], nil
    }
    
    // Forward to a node that has it
    primary, _ := kv.ck.GetPrimaryNode(key)
    return kv.readFromNode(primary, key)
}
```

### Helper Functions

ClusterKit provides convenient functions to work with partitions:

```go
// Get all replica nodes for a key
replicas, _ := ck.GetReplicaNodes("user:123")
for _, node := range replicas {
    fmt.Printf("Replica: %s at %s\n", node.Name, node.IP)
}

// Get primary node for a key
primary, _ := ck.GetPrimaryNode("user:123")
fmt.Printf("Primary: %s at %s\n", primary.Name, primary.IP)

// Get all nodes (primary + replicas) for a key
primary, replicas, _ := ck.GetAllNodesForKey("user:123")

// Check if current node is primary
isPrimary, _ := ck.IsPrimaryForKey("user:123")

// Check if current node is replica
isReplica, _ := ck.IsReplicaForKey("user:123")

// Check if current node should handle key
shouldHandle, role, _ := ck.ShouldHandleKey("user:123")
// role is "primary", "replica", or ""
```

## Using Consensus & Leader Election

ClusterKit provides simple leader election for coordinating cluster-wide operations:

```go
// Get consensus manager
cm := ck.GetConsensusManager()

// Check if this node is the leader
if cm.IsLeader() {
    // Only leader performs certain operations
    fmt.Println("I am the leader!")
    
    // Example: Only leader creates partitions
    ck.CreatePartitions()
}

// Get current leader information
leader, err := cm.GetLeader()
if err == nil {
    fmt.Printf("Leader: %s at %s (term %d)\n", 
        leader.LeaderName, leader.LeaderIP, leader.Term)
}

// Wait for leader election to complete
err = cm.WaitForLeader(10 * time.Second)

// Get consensus stats
stats := cm.GetStats()
fmt.Printf("Term: %d, Leader: %s, Is Leader: %v\n",
    stats.CurrentTerm, stats.CurrentLeader, stats.IsLeader)
```

### Use Cases for Consensus

**1. Coordinated Operations**
```go
// Only leader should perform cluster-wide operations
if cm.IsLeader() {
    ck.RebalancePartitions()
}
```

**2. Distributed Locks**
```go
// Use leader for distributed coordination
if cm.IsLeader() {
    // Perform operation that should only happen once
    runMigration()
}
```

**3. Configuration Changes**
```go
// Only leader updates cluster configuration
if cm.IsLeader() {
    updateClusterConfig(newConfig)
}
```

**Note:** ClusterKit uses **HashiCorp Raft** for production-grade consensus. Raft provides:
- Strong consistency guarantees
- Automatic leader election
- Log replication across nodes
- Crash recovery with snapshots
- Proven algorithm used in production systems (Consul, etcd, etc.)

For detailed partition documentation, see [PARTITIONS.md](./PARTITIONS.md)

## 📚 API Reference

### Initialization

#### `NewClusterKit(options Options) (*ClusterKit, error)`

Create a new ClusterKit instance.

```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:       "node-1",           // Unique node identifier
    NodeName:     "Server-1",         // Human-readable name
    HTTPAddr:     ":8080",            // HTTP listen address
    RaftAddr:     "127.0.0.1:9001",   // Raft bind address
    Bootstrap:    true,               // true for first node only
    JoinAddr:     "",                 // Address to join (empty for bootstrap)
    DataDir:      "./data/node1",     // Data directory
    SyncInterval: 5 * time.Second,    // State sync interval
    Config: &clusterkit.Config{
        ClusterName:       "my-cluster",
        PartitionCount:    16,
        ReplicationFactor: 3,
    },
})
```

#### `Start() error`

Start the ClusterKit instance (HTTP server, Raft, discovery).

```go
if err := ck.Start(); err != nil {
    log.Fatal(err)
}
```

#### `Stop() error`

Gracefully shutdown ClusterKit.

```go
ck.Stop()
```

---

### Partition Management

#### `CreatePartitions() error`

Create partitions across the cluster (leader only).

```go
if ck.GetConsensusManager().IsLeader() {
    err := ck.CreatePartitions()
}
```

#### `GetPartitionForKey(key string) (*Partition, error)`

Get the partition assignment for a specific key.

```go
partition, err := ck.GetPartitionForKey("user:123")
// Returns: {ID: "partition-5", PrimaryNode: "node-2", ReplicaNodes: ["node-1", "node-3"]}
```

#### `GetPrimaryNode(key string) (*Node, error)`

Get the primary node for a key.

```go
primary, err := ck.GetPrimaryNode("user:123")
// Returns: {ID: "node-2", Name: "Server-2", IP: ":8081", Status: "active"}
```

#### `GetReplicaNodes(key string) ([]Node, error)`

Get all replica nodes for a key.

```go
replicas, err := ck.GetReplicaNodes("user:123")
// Returns: [{ID: "node-1", ...}, {ID: "node-3", ...}]
```

#### `GetAllNodesForKey(key string) (*Node, []Node, error)`

Get primary and all replica nodes for a key.

```go
primary, replicas, err := ck.GetAllNodesForKey("user:123")
```

#### `IsPrimaryForKey(key string) (bool, error)`

Check if current node is the primary for a key.

```go
isPrimary, err := ck.IsPrimaryForKey("user:123")
if isPrimary {
    // This node should handle writes
}
```

#### `IsReplicaForKey(key string) (bool, error)`

Check if current node is a replica for a key.

```go
isReplica, err := ck.IsReplicaForKey("user:123")
if isReplica {
    // This node has a replica copy
}
```

#### `ShouldHandleKey(key string) (bool, string, error)`

Check if current node should handle a key (primary or replica).

```go
shouldHandle, role, err := ck.ShouldHandleKey("user:123")
// role: "primary", "replica", or ""
if shouldHandle {
    data := myDB.Read(key)
}
```

#### `ListPartitions() []*Partition`

Get all partitions in the cluster.

```go
partitions := ck.ListPartitions()
for _, p := range partitions {
    fmt.Printf("Partition %s: Primary=%s\n", p.ID, p.PrimaryNode)
}
```

#### `GetPartitionStats() *PartitionStats`

Get partition distribution statistics.

```go
stats := ck.GetPartitionStats()
// Returns: {TotalPartitions: 16, PartitionsPerNode: {...}, ReplicasPerNode: {...}}
```

#### `RebalancePartitions() error`

Rebalance partitions across nodes (leader only).

```go
if ck.GetConsensusManager().IsLeader() {
    err := ck.RebalancePartitions()
}
```

---

### Cluster Information

#### `GetCluster() *Cluster`

Get current cluster state.

```go
cluster := ck.GetCluster()
fmt.Printf("Cluster: %s, Nodes: %d\n", cluster.Name, len(cluster.Nodes))
```

---

### Consensus & Leadership

#### `GetConsensusManager() *ConsensusManager`

Get the consensus manager for leader operations.

```go
cm := ck.GetConsensusManager()
```

#### `IsLeader() bool`

Check if current node is the Raft leader.

```go
if cm.IsLeader() {
    // Perform leader-only operations
    ck.CreatePartitions()
}
```

#### `GetLeader() (*LeaderInfo, error)`

Get current leader information.

```go
leader, err := cm.GetLeader()
// Returns: {LeaderID: "node-1", LeaderName: "Server-1", LeaderIP: ":8080", Term: 5}
```

#### `WaitForLeader(timeout time.Duration) error`

Wait for leader election to complete.

```go
err := cm.WaitForLeader(10 * time.Second)
```

#### `GetStats() *ConsensusStats`

Get Raft consensus statistics.

```go
stats := cm.GetStats()
// Returns: {State: "Leader", Term: 5, LastLogIndex: 42, CommitIndex: 42, ...}
```

---

### Complete Example

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Initialize ClusterKit
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:    "node-1",
        NodeName:  "Server-1",
        HTTPAddr:  ":8080",
        RaftAddr:  "127.0.0.1:9001",
        Bootstrap: true,
        DataDir:   "./data/node1",
        Config: &clusterkit.Config{
            ClusterName:       "my-app",
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

    // Wait for leader election
    cm := ck.GetConsensusManager()
    cm.WaitForLeader(10 * time.Second)

    // Create partitions (leader only)
    if cm.IsLeader() {
        ck.CreatePartitions()
    }

    // Use ClusterKit to route data
    key := "user:123"
    
    // Get partition info
    partition, _ := ck.GetPartitionForKey(key)
    fmt.Printf("Key %s belongs to partition %s\n", key, partition.ID)
    
    // Check if this node should handle the key
    isPrimary, _ := ck.IsPrimaryForKey(key)
    if isPrimary {
        // Write to your database
        myDB.Write(key, data)
        
        // Replicate to replica nodes
        replicas, _ := ck.GetReplicaNodes(key)
        for _, replica := range replicas {
            sendToNode(replica, key, data)
        }
    } else {
        // Forward to primary
        primary, _ := ck.GetPrimaryNode(key)
        forwardTo(primary, key, data)
    }
}
```

## 📖 Examples

- **[Basic Example](./example)** - Simple multi-node cluster setup
- **[Partitions Demo](./example/partitions-demo)** - Complete partition management example

## 📚 Documentation

- **[ARCHITECTURE.md](./ARCHITECTURE.md)** - System design and architecture
- **[PARTITIONS.md](./PARTITIONS.md)** - Detailed partition management guide

## License

MIT License
