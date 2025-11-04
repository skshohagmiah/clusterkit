# ClusterKit Architecture

## Overview

ClusterKit is designed as an embeddable library that developers can include in their applications to add distributed clustering capabilities. Each application instance runs its own ClusterKit instance, and all instances communicate via HTTP to maintain a synchronized global state.

## Core Components

### 1. ClusterKit Instance (`clusterkit.go`)
- Main entry point for the library
- Manages cluster lifecycle (initialization, start, stop)
- Coordinates state persistence and synchronization
- Each application instance has its own ClusterKit instance

### 2. State Management (`types.go`)
- **Node**: Represents a single server/instance in the cluster
- **Partition**: Data partitioning information
- **PartitionMap**: Maps partitions to nodes
- **Cluster**: Global cluster state containing all nodes and partitions
- **Config**: Cluster configuration (replication factor, partition count, etc.)

### 3. HTTP Communication (`sync.go`)
- **Endpoints**:
  - `/health` - Health checks
  - `/join` - New node registration
  - `/sync` - State synchronization between nodes
  - `/cluster` - Query cluster state
- All inter-node communication happens over HTTP
- RESTful API for easy debugging and monitoring

### 4. Persistence Layer (`wal.go`)
- **cluster-state.json**: Current cluster state snapshot
- **wal.log**: Write-Ahead Log for all operations
- Each node maintains its own local copy on disk
- State is loaded on startup and saved on shutdown

### 5. Node Management (`servers.go`)
- Add/remove nodes from cluster
- Query node information
- Node lifecycle management

## How It Works

### Initialization Flow

```
Developer's Application
        ↓
Initialize ClusterKit with Options
        ↓
ClusterKit creates local Cluster instance
        ↓
Load existing state from disk (if available)
        ↓
Start HTTP server for inter-node communication
        ↓
Discover and join known nodes
        ↓
Start periodic state synchronization
```

### Node Discovery & Joining

```
New Node (Node 2)
        ↓
Configured with KnownNodes: ["node-1:8080"]
        ↓
Sends POST /join to node-1:8080
        ↓
Node 1 receives join request
        ↓
Node 1 adds Node 2 to its cluster state
        ↓
Node 1 returns full cluster state to Node 2
        ↓
Node 2 updates its local state with cluster info
        ↓
Both nodes save state to disk
```

### State Synchronization

```
Every SyncInterval (default: 5 seconds)
        ↓
Each node sends POST /sync to all other nodes
        ↓
Payload contains: current node info + full cluster state
        ↓
Receiving node merges incoming state with local state
        ↓
Receiving node saves updated state to disk
        ↓
Result: Eventually consistent cluster state across all nodes
```

### Data Flow Example

**3-Node Cluster:**

```
Application Instance 1          Application Instance 2          Application Instance 3
        ↓                               ↓                               ↓
ClusterKit (Node 1)             ClusterKit (Node 2)             ClusterKit (Node 3)
   HTTP: :8080                     HTTP: :8081                     HTTP: :8082
        ↓                               ↓                               ↓
   cluster-state.json              cluster-state.json              cluster-state.json
   wal.log                         wal.log                         wal.log
        ↓                               ↓                               ↓
        └───────────────HTTP Sync───────┴───────────HTTP Sync──────────┘
```

## State Consistency Model

ClusterKit uses **eventual consistency**:

1. Each node maintains its own local state
2. Nodes periodically sync with all other nodes
3. State changes are merged using simple last-write-wins
4. WAL provides durability for crash recovery
5. On restart, nodes reload state and re-sync with cluster

## Usage Pattern

### Developer Integration

```go
// In your application's main.go
package main

import (
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Initialize ClusterKit
    ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:     "unique-node-id",
        HTTPAddr:   ":8080",
        KnownNodes: []string{"other-node:8080"},
        DataDir:    "./cluster-data",
        Config: &clusterkit.Config{
            ClusterName:       "my-app",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    
    // Start clustering
    ck.Start()
    
    // Your application logic
    runYourApplication()
    
    // Cleanup
    defer ck.Stop()
}
```

### Multi-Instance Deployment

When deploying multiple instances:

1. **First Instance**: Start with empty `KnownNodes`
2. **Subsequent Instances**: Point to at least one existing node
3. **All Instances**: Use unique `NodeID` and `HTTPAddr`
4. **All Instances**: Share same `ClusterName` in Config

## Key Design Decisions

### Why HTTP?
- Simple, debuggable protocol
- Easy to monitor and troubleshoot
- Works across different network topologies
- No complex binary protocols

### Why Local State Files?
- Fast local reads without network calls
- Survives node restarts
- Simple backup and recovery
- No dependency on external databases

### Why Eventual Consistency?
- Simpler implementation
- Better availability
- Acceptable for most clustering use cases
- Can be extended for stronger consistency if needed

## Future Enhancements

Potential areas for extension:
- Leader election for coordination
- Partition assignment algorithms
- Data replication implementation
- Health monitoring and failure detection
- Automatic rebalancing
- gRPC support for faster communication
- Consensus protocols (Raft, Paxos)
