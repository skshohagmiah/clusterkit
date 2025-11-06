# ClusterKit Examples

This folder contains two complete examples showing different replication strategies using ClusterKit.

## Examples

### 1. [SYNC - Quorum-Based Replication](./sync/) (Client-Side)

**Strategy:** Client writes to all nodes, waits for quorum

- ✅ Strong consistency
- ✅ High durability (immediate)
- ❌ Higher latency (~10-20ms)
- **Use for:** Financial data, critical transactions

### 2. [ASYNC - Primary-First Replication](./async/) (Client-Side)

**Strategy:** Client writes to primary, replicates in background

- ✅ Low latency (~1-5ms)
- ✅ High throughput (3x faster)
- ⚠️ Eventual consistency
- **Use for:** User data, general purpose (90% of use cases)

### 3. [Server-Side Partition Handling](./server-side/)

**Strategy:** Server checks primary/replica and handles routing

- ✅ Simple clients (no topology knowledge)
- ✅ Server handles replication
- ✅ Partition change hooks for data migration
- **Use for:** Web/mobile apps, traditional request/response

## Quick Comparison

| Feature | SYNC | ASYNC |
|---------|------|-------|
| **Write Latency** | 10-20ms | 1-5ms |
| **Throughput** | 1,500 ops/sec | 5,000 ops/sec |
| **Consistency** | Strong (quorum) | Eventual |
| **Durability** | High (immediate) | High (eventual) |
| **Best For** | Critical data | General purpose |

## How ClusterKit is Used

Both examples use ClusterKit's simple API:

```go
// Get partition for a key
partition, _ := ck.GetPartition("user:123")

// Check if I'm the primary
if ck.IsPrimary(partition) {
    // Store data locally
    kv.data[key] = value
    
    // Get replicas for replication
    replicas := ck.GetReplicas(partition)
    
    // SYNC: Wait for quorum
    // ASYNC: Replicate in background
}

// Check if I'm a replica
if ck.IsReplica(partition) {
    // Just store locally (primary handles replication)
    kv.data[key] = value
}

// Get primary node
primary := ck.GetPrimary(partition)
// Forward request to primary if needed
```

## What ClusterKit Provides

ClusterKit is a **coordination library** that tells you:

- ✅ Which partition a key belongs to
- ✅ Which nodes (primary + replicas) should store the data
- ✅ Whether current node is primary or replica
- ✅ Automatic rebalancing when nodes join/leave
- ✅ Leader election and consensus (Raft)

## What You Implement

You decide HOW to store and replicate:

- 🔧 Data storage (in-memory, disk, database)
- 🔧 Replication strategy (sync, async, quorum)
- 🔧 Network protocol (HTTP, gRPC, TCP)
- 🔧 Business logic

## Running Examples

Each example has its own README with detailed instructions:

```bash
# Run SYNC example
cd sync/
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 go run server.go client.go

# Run ASYNC example
cd async/
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 go run server.go client.go
```

## Architecture

Both examples implement a distributed key-value store:

```
┌─────────────┐
│   Client    │ (smart routing to correct nodes)
└──────┬──────┘
       │
       ├──────────┬──────────┬──────────┐
       ▼          ▼          ▼          ▼
   ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐
   │Node 1│  │Node 2│  │Node 3│  │Node 4│
   │ KV   │  │ KV   │  │ KV   │  │ KV   │
   └──┬───┘  └──┬───┘  └──┬───┘  └──┬───┘
      │         │         │         │
      └─────────┴─────────┴─────────┘
            ClusterKit
       (partition management)
```

## Key Differences

### SYNC (Quorum)
```go
// Primary writes to all nodes and WAITS
for _, node := range allNodes {
    response := writeToNode(node, key, value)
    successCount++
}

if successCount >= quorum {
    return "ok"  // Waited for 2/3 nodes
}
```

### ASYNC (Primary-first)
```go
// Primary writes locally and RETURNS immediately
kv.data[key] = value
return "ok"  // Fast response!

// Background replication (fire-and-forget)
go func() {
    for _, replica := range replicas {
        writeToNode(replica, key, value)
    }
}()
```

## Choosing a Strategy

**Use SYNC when:**
- Data is critical (money, inventory)
- Strong consistency required
- Can tolerate higher latency

**Use ASYNC when:**
- Speed matters
- General purpose application
- Can tolerate brief inconsistency
- Most web applications (90% of cases)

## Learn More

- [ClusterKit Documentation](../README.md)
- [SYNC Example Details](./sync/README.md)
- [ASYNC Example Details](./async/README.md)
