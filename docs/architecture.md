# ClusterKit Architecture

This document provides a detailed look at ClusterKit's internal architecture, design decisions, and how the components work together.

---

## Table of Contents

- [Overview](#overview)
- [System Architecture](#system-architecture)
- [Core Components](#core-components)
- [Data Flow](#data-flow)
- [Raft Consensus](#raft-consensus)
- [Consistent Hashing](#consistent-hashing)
- [Health Checking](#health-checking)
- [Event System](#event-system)
- [Persistence](#persistence)
- [Design Decisions](#design-decisions)

---

## Overview

ClusterKit is built on three fundamental pillars:

1. **Raft Consensus** - For distributed agreement on cluster state
2. **Consistent Hashing** - For deterministic partition assignment
3. **Event Hooks** - For application integration

```
┌──────────────────────────────────────────────────────┐
│              Application Layer                        │
│  (Your KV Store, Cache, Queue, Business Logic)       │
└───────────────────┬──────────────────────────────────┘
                    │ ClusterKit API
┌───────────────────▼──────────────────────────────────┐
│              ClusterKit Library                       │
│  ┌────────────┬────────────┬──────────────────────┐  │
│  │   Raft     │ Consistent │   Hook               │  │
│  │ Consensus  │  Hashing   │  Manager             │  │
│  └────────────┴────────────┴──────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

---

## System Architecture

### Layered Design

```
┌─────────────────────────────────────────────────────────────┐
│                    Application Layer                         │
│         (Storage, Replication, Business Logic)              │
└────────────────────┬────────────────────────────────────────┘
                     │ Public API (7 methods + hooks)
┌────────────────────▼────────────────────────────────────────┐
│                   ClusterKit Public API                      │
│  • GetPartition(key) → partition                            │
│  • GetPrimary(partition) → node                             │
│  • GetReplicas(partition) → []node                          │
│  • IsPrimary(partition) → bool                              │
│  • IsReplica(partition) → bool                              │
│  • GetNodes(partition) → []node                             │
│  • GetMyNodeID() → string                                   │
│  • OnPartitionChange, OnNodeJoin, OnNodeRejoin, etc.        │
└────────────────────┬────────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────────┐
│                  Coordination Layer                          │
│  ┌──────────────┬──────────────┬──────────────────────┐    │
│  │ Partition    │ Health       │ Hook                 │    │
│  │ Manager      │ Checker      │ Manager              │    │
│  │              │              │                      │    │
│  │ • Hash(key)  │ • Heartbeat  │ • Event queue        │    │
│  │ • Assign     │ • Timeout    │ • Goroutine pool     │    │
│  │ • Rebalance  │ • Remove     │ • 7 hook types       │    │
│  └──────┬───────┴──────┬───────┴──────────┬───────────┘    │
│         │              │                  │                 │
│  ┌──────▼──────────────▼──────────────────▼───────────┐    │
│  │           Raft Consensus Manager                    │    │
│  │  • Leader election (150-300ms)                      │    │
│  │  • Log replication                                  │    │
│  │  • State machine (add_node, remove_node, etc.)      │    │
│  │  • Membership management                            │    │
│  └──────────────────┬──────────────────────────────────┘    │
└─────────────────────┼──────────────────────────────────────┘
                      │
┌─────────────────────▼──────────────────────────────────────┐
│                  Persistence Layer                          │
│  • WAL (Write-Ahead Log) - Raft operations                 │
│  • Snapshots - Cluster state checkpoints                   │
│  • State files - JSON cluster configuration                │
│  • Directory: <data_dir>/raft/                             │
└────────────────────────────────────────────────────────────┘
```

---

## Core Components

### 1. ClusterKit Main Struct

```go
type ClusterKit struct {
    nodeID            string
    cluster           *Cluster
    consensusManager  *ConsensusManager
    hookManager       *HookManager
    healthChecker     *HealthChecker
    httpServer        *http.Server
    mu                sync.RWMutex
}
```

**Responsibilities:**
- Coordinate all subsystems
- Provide public API
- Manage cluster state
- Handle HTTP requests

### 2. Consensus Manager (Raft)

```go
type ConsensusManager struct {
    raft       *raft.Raft
    transport  *raft.NetworkTransport
    logStore   *raftboltdb.BoltStore
    stableStore *raftboltdb.BoltStore
    snapshots  *raft.FileSnapshotStore
}
```

**Responsibilities:**
- Leader election
- Log replication
- State machine application
- Membership changes

**Key Operations:**
- `AddVoter(nodeID, address)` - Add node to Raft cluster
- `RemoveServer(nodeID)` - Remove node from Raft
- `ProposeAction(action, data)` - Submit state change
- `IsLeader()` - Check leadership status

### 3. Partition Manager

```go
type PartitionMap struct {
    Partitions map[string]*Partition
}

type Partition struct {
    ID           string
    PrimaryNode  string
    ReplicaNodes []string
}
```

**Responsibilities:**
- Hash keys to partitions
- Assign partitions to nodes
- Rebalance on topology changes
- Maintain replica sets

**Algorithm:**
```
1. Hash key using MD5
2. Convert to integer
3. Modulo by partition count (64)
4. Format as "partition-{id}"
5. Look up primary/replicas
```

### 4. Health Checker

```go
type HealthChecker struct {
    nodeFailures map[string]int
    nodeLastSeen map[string]time.Time
    config       HealthCheckConfig
}
```

**Responsibilities:**
- Periodic health checks (every 5s)
- Failure detection (3 strikes)
- Automatic node removal
- Trigger rebalancing

**Flow:**
```
1. Every 5 seconds, check all nodes
2. HTTP GET to /health endpoint
3. If timeout (2s) or error → failure++
4. If failures >= 3 → remove node
5. Trigger OnNodeLeave hook
6. Initiate rebalancing
```

### 5. Hook Manager

```go
type HookManager struct {
    hooks                   []PartitionChangeHook
    nodeJoinHooks           []NodeJoinHook
    nodeRejoinHooks         []NodeRejoinHook
    nodeLeaveHooks          []NodeLeaveHook
    rebalanceStartHooks     []RebalanceStartHook
    rebalanceCompleteHooks  []RebalanceCompleteHook
    clusterHealthHooks      []ClusterHealthChangeHook
    lastPartitionState      map[string]*Partition
    lastNodeSet             map[string]bool
    lastNodeSeenTime        map[string]time.Time
    workerPool              chan struct{} // Max 50 concurrent
}
```

**Responsibilities:**
- Register hooks
- Detect state changes
- Fire events asynchronously
- Track last seen times
- Prevent duplicate events

---

## Data Flow

### Partition Lookup Flow

```
Client: "Where does key 'user:123' go?"
    ↓
1. GetPartition("user:123")
    ↓
2. MD5("user:123") = "482c811da5d5b4bc6d497ffa98491e38"
    ↓
3. Convert to int % 64 = 37
    ↓
4. partition-37
    ↓
5. Look up in PartitionMap
    ↓
6. Return: {
     Primary: node-2,
     Replicas: [node-3, node-4]
   }
```

### Node Join Flow

```
1. New node starts
    ↓
2. HTTP POST /join to bootstrap node
    ↓
3. Leader receives join request
    ↓
4. Check if rejoin (node ID exists)
    ↓
5a. New Join:                    5b. Rejoin:
    - AddVoter to Raft              - Update node info
    - OnNodeJoin hook               - OnNodeRejoin hook
    ↓                               ↓
6. ProposeAction("add_node")
    ↓
7. Raft replicates to all nodes
    ↓
8. All nodes apply state change
    ↓
9. Leader triggers rebalancing
    ↓
10. OnRebalanceStart hook
    ↓
11. Calculate new partition assignments
    ↓
12. OnPartitionChange hooks (per partition)
    ↓
13. OnRebalanceComplete hook
```

### Rebalancing Flow

```
Trigger: Node join/leave
    ↓
1. Leader calculates new distribution
    ↓
2. For each partition:
    - Old: primary=A, replicas=[B,C]
    - New: primary=A, replicas=[B,D]
    ↓
3. Detect changes:
    - D is new replica (needs data)
    ↓
4. Fire OnPartitionChange:
    - partitionID: "partition-5"
    - copyFromNodes: [A, B, C]
    - copyToNode: D
    ↓
5. Application migrates data
    ↓
6. Repeat for all changed partitions
```

---

## Raft Consensus

### Why Raft?

ClusterKit uses Raft for:
- **Strong consistency** - All nodes agree on cluster state
- **Leader election** - Automatic failover
- **Log replication** - Durable state changes
- **Membership changes** - Safe node addition/removal

### Raft Configuration

```go
config := raft.DefaultConfig()
config.LocalID = raft.ServerID(nodeID)
config.HeartbeatTimeout = 1000 * time.Millisecond
config.ElectionTimeout = 1000 * time.Millisecond
config.CommitTimeout = 50 * time.Millisecond
config.SnapshotInterval = 120 * time.Second
config.SnapshotThreshold = 8192
```

### State Machine

ClusterKit implements a Raft FSM (Finite State Machine):

```go
type ClusterFSM struct {
    cluster *Cluster
}

func (f *ClusterFSM) Apply(log *raft.Log) interface{} {
    switch log.Type {
    case raft.LogCommand:
        var cmd Command
        json.Unmarshal(log.Data, &cmd)
        
        switch cmd.Action {
        case "add_node":
            // Add node to cluster
        case "remove_node":
            // Remove node from cluster
        case "update_partitions":
            // Update partition assignments
        }
    }
}
```

### Leadership

- **Election timeout:** 150-300ms
- **Heartbeat interval:** 1s
- **Only leader** can:
  - Accept join requests
  - Trigger rebalancing
  - Propose state changes

---

## Consistent Hashing

### Algorithm

```go
func (ck *ClusterKit) GetPartition(key string) (*Partition, error) {
    // 1. Hash the key
    hash := md5.Sum([]byte(key))
    hashInt := binary.BigEndian.Uint64(hash[:])
    
    // 2. Modulo by partition count
    partitionNum := hashInt % uint64(partitionCount)
    
    // 3. Format partition ID
    partitionID := fmt.Sprintf("partition-%d", partitionNum)
    
    // 4. Look up in partition map
    return cluster.PartitionMap.Partitions[partitionID]
}
```

### Partition Assignment

When rebalancing:

```go
func assignPartitions(nodes []Node, partitions []Partition, rf int) {
    for i, partition := range partitions {
        // Round-robin primary assignment
        primaryIdx := i % len(nodes)
        partition.PrimaryNode = nodes[primaryIdx].ID
        
        // Assign replicas (next RF-1 nodes)
        for j := 1; j < rf; j++ {
            replicaIdx := (primaryIdx + j) % len(nodes)
            partition.ReplicaNodes = append(
                partition.ReplicaNodes,
                nodes[replicaIdx].ID,
            )
        }
    }
}
```

### Benefits

- **Deterministic** - Same key always goes to same partition
- **Balanced** - Even distribution across nodes
- **Minimal movement** - Only affected partitions move
- **Fast lookup** - O(1) hash calculation

---

## Health Checking

### Configuration

```go
type HealthCheckConfig struct {
    Enabled          bool
    Interval         time.Duration  // 5s
    Timeout          time.Duration  // 2s
    FailureThreshold int            // 3
}
```

### Check Process

```
Every 5 seconds:
    For each node:
        1. HTTP GET /health (timeout: 2s)
        2. If success:
            - Reset failure count
            - Update last seen time
        3. If failure:
            - Increment failure count
            - If count >= 3:
                - Remove node
                - Trigger rebalancing
```

### Failure Scenarios

| Scenario | Detection Time | Action |
|----------|----------------|--------|
| Node crash | 15s (3 × 5s) | Remove + rebalance |
| Network partition | 15s | Remove + rebalance |
| Slow response | 2s timeout | Count as failure |
| Graceful shutdown | Immediate | Clean removal |

---

## Event System

### Hook Execution

```
Event occurs (e.g., partition change)
    ↓
1. HookManager detects change
    ↓
2. Create event object with context
    ↓
3. For each registered hook:
    - Acquire semaphore slot (max 50)
    - Launch goroutine
    - Execute hook
    - Recover from panics
    - Release semaphore
```

### Event Detection

```go
func (hm *HookManager) checkPartitionChanges(current, last map[string]*Partition) {
    for partitionID, newPartition := range current {
        oldPartition := last[partitionID]
        
        // Detect primary change
        if oldPartition.PrimaryNode != newPartition.PrimaryNode {
            // Fire event
        }
        
        // Detect new replicas
        for _, newReplica := range newPartition.ReplicaNodes {
            if !contains(oldPartition.ReplicaNodes, newReplica) {
                // Fire event
            }
        }
    }
}
```

---

## Persistence

### Directory Structure

```
<data_dir>/
├── raft/
│   ├── raft.db           # Raft log + stable storage
│   ├── snapshots/        # Cluster state snapshots
│   │   ├── 1-100-1234567890
│   │   └── 2-200-1234567891
│   └── raft-transport/   # Network transport state
└── cluster-state.json    # Current cluster state (backup)
```

### Snapshot Format

```json
{
  "cluster": {
    "nodes": {
      "node-1": {"id": "node-1", "ip": ":8080", "status": "active"},
      "node-2": {"id": "node-2", "ip": ":8081", "status": "active"}
    },
    "partition_map": {
      "partitions": {
        "partition-0": {
          "id": "partition-0",
          "primary_node": "node-1",
          "replica_nodes": ["node-2", "node-3"]
        }
      }
    }
  }
}
```

### Recovery

On startup:
1. Load Raft log from disk
2. Apply all committed entries
3. Restore cluster state
4. Resume operations

---

## Design Decisions

### Why MD5 for Hashing?

- **Fast** - Optimized for speed
- **Deterministic** - Same input = same output
- **Good distribution** - Even spread across partitions
- **Not for security** - We don't need cryptographic strength

### Why 64 Partitions Default?

- **Balance** - Good distribution without overhead
- **Scalability** - Works well for 3-100 nodes
- **Rebalancing** - Reasonable migration time
- **Configurable** - Can be changed per deployment

### Why Raft Over Paxos?

- **Understandable** - Easier to reason about
- **Production-ready** - HashiCorp's implementation
- **Well-tested** - Used in Consul, etcd, etc.
- **Good tooling** - Excellent Go library

### Why Hooks Over Polling?

- **Efficient** - No wasted CPU on polling
- **Immediate** - React instantly to changes
- **Decoupled** - Clean separation of concerns
- **Flexible** - Application controls behavior

### Why HTTP Over gRPC?

- **Simple** - No protobuf compilation
- **Debuggable** - Easy to test with curl
- **Universal** - Works everywhere
- **Sufficient** - Performance is adequate

---

## Performance Characteristics

| Operation | Complexity | Notes |
|-----------|------------|-------|
| GetPartition | O(1) | Hash calculation |
| IsPrimary | O(1) | Map lookup |
| GetReplicas | O(R) | R = replication factor |
| Node Join | O(N + P) | N = nodes, P = partitions |
| Rebalancing | O(P × R) | P = partitions, R = RF |
| Health Check | O(N) | N = nodes |
| Hook Execution | O(H) | H = registered hooks |

---

## Scalability Limits

| Metric | Recommended | Maximum | Notes |
|--------|-------------|---------|-------|
| Nodes | 3-50 | 100 | Raft overhead increases |
| Partitions | 64-256 | 1024 | Rebalance time grows |
| Replication Factor | 3 | 5 | More replicas = more data |
| Hook Count | 1-10 | 50 | Goroutine pool limit |
| Cluster Size | <1GB | <10GB | Raft snapshot size |

---

## Further Reading

- [Raft Paper](https://raft.github.io/raft.pdf) - Original Raft consensus algorithm
- [Consistent Hashing](https://en.wikipedia.org/wiki/Consistent_hashing) - Wikipedia
- [HashiCorp Raft](https://github.com/hashicorp/raft) - Go implementation
- [Partitioning Guide](./partitioning.md) - ClusterKit partitioning details
- [Rebalancing Guide](./rebalancing.md) - How rebalancing works
