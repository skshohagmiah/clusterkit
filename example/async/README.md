# ASYNC Example - Primary-First Replication

This example demonstrates **asynchronous replication** with primary-first writes using ClusterKit.

## Strategy: Async Replication

**How it works:**
1. Client writes to primary
2. Primary writes locally immediately
3. Primary returns success to client (fast!)
4. Primary replicates to replicas in **background**

**Trade-offs:**
- ✅ **Low latency** - Returns immediately after primary write (~1-5ms)
- ✅ **High throughput** - No waiting for replicas
- ✅ **High durability** - Data eventually replicated to all nodes
- ⚠️ **Eventual consistency** - Brief window where replicas may be stale

## Use Cases

- User profiles
- Session data
- Shopping carts
- Social media posts
- General purpose applications (90% of use cases)

## Running the Example

### Start 3 nodes:

```bash
# Terminal 1 - Node 1 (bootstrap)
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 go run server.go client.go

# Terminal 2 - Node 2
NODE_ID=node-2 HTTP_PORT=8081 KV_PORT=9081 JOIN_ADDR=localhost:8080 go run server.go client.go

# Terminal 3 - Node 3
NODE_ID=node-3 HTTP_PORT=8082 KV_PORT=9082 JOIN_ADDR=localhost:8080 go run server.go client.go
```

### Test with client:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	// Create client
	client, _ := NewClient([]string{"localhost:8080"}, 64)

	// Write to primary (returns immediately)
	start := time.Now()
	err := client.Set("user:123", "John Doe")
	fmt.Printf("Write latency: %v\n", time.Since(start))
	// Expected: ~1-5ms (primary only, no quorum wait)

	// Read from any replica
	value, _ := client.Get("user:123")
	fmt.Printf("Value: %s\n", value)
}
```

## Architecture

```
Client
  │
  └─> Primary (write locally)
        │
        ├─> Return "OK" immediately ✓
        │
        └─> Background replication
              ├─> Replica 1 (async)
              └─> Replica 2 (async)
```

## Performance

- **Write latency**: 1-5ms (primary only)
- **Read latency**: 1-5ms (any replica)
- **Throughput**: ~5,000 writes/sec (3x faster than sync)
- **Consistency**: Eventual
- **Durability**: High (eventual replication)

## ClusterKit API Usage

```go
partition, _ := ck.GetPartition(key)

if ck.IsPrimary(partition) {
    // Store locally FIRST
    kv.data[key] = value
    
    // Return immediately to client (fast!)
    return "ok"
    
    // Replicate in BACKGROUND
    go func() {
        replicas := ck.GetReplicas(partition)
        for _, replica := range replicas {
            // Send to each replica (fire-and-forget)
        }
    }()
}
```

## Failover Behavior

**What happens if primary fails before replication?**

1. Primary writes locally and returns success
2. Primary crashes before background replication
3. ClusterKit detects failure (health checks)
4. Replica promoted to new primary (automatic rebalancing)
5. Recent writes on failed primary are lost ❌

**Mitigation:**
- Background replication is fast (usually <10ms)
- Failure window is very small
- For critical data, use SYNC mode instead

## Comparison with SYNC

| Metric | ASYNC (Primary-first) | SYNC (Quorum) |
|--------|----------------------|---------------|
| Write Latency | 1-5ms | 10-20ms |
| Consistency | Eventual | Strong |
| Durability | High (eventual) | High (immediate) |
| Throughput | 5,000 ops/sec | 1,500 ops/sec |
| Use Case | General purpose | Critical data |

## When to Use ASYNC

✅ **Use ASYNC when:**
- Speed is important
- Can tolerate brief inconsistency
- Most web applications
- User-facing features

❌ **Don't use ASYNC for:**
- Financial transactions
- Inventory counts
- Anything requiring strong consistency
