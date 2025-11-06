# SYNC Example - Quorum-Based Replication

This example demonstrates **synchronous replication** with quorum writes using ClusterKit.

## Strategy: Quorum Writes

**How it works:**
1. Client writes to primary
2. Primary writes locally + replicates to ALL replicas
3. Primary waits for **quorum (2/3 nodes)** to acknowledge
4. Returns success to client

**Trade-offs:**
- ✅ **Strong consistency** - Data guaranteed on 2+ nodes before success
- ✅ **High durability** - Survives 1 node failure
- ❌ **Higher latency** - Must wait for multiple nodes (~10-20ms)

## Use Cases

- Financial transactions
- Inventory management  
- Critical user data
- Anything requiring strong consistency

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

	// Write with quorum (waits for 2/3 nodes)
	start := time.Now()
	err := client.Set("user:123", "John Doe")
	fmt.Printf("Write latency: %v\n", time.Since(start))
	// Expected: ~10-20ms (waits for quorum)

	// Read from any replica
	value, _ := client.Get("user:123")
	fmt.Printf("Value: %s\n", value)
}
```

## Architecture

```
Client
  │
  ├─> Primary (write + replicate)
  │     ├─> Replica 1 (parallel)
  │     └─> Replica 2 (parallel)
  │
  └─> Wait for 2/3 ACKs ✓
```

## Performance

- **Write latency**: 10-20ms (quorum wait)
- **Read latency**: 1-5ms (any replica)
- **Throughput**: ~1,500 writes/sec
- **Consistency**: Strong (quorum)
- **Durability**: High (2+ copies)

## ClusterKit API Usage

```go
partition, _ := ck.GetPartition(key)

if ck.IsPrimary(partition) {
    // Store locally
    kv.data[key] = value
    
    // Replicate to ALL nodes
    nodes := ck.GetNodes(partition)
    for _, node := range nodes {
        // Send to each node
    }
    
    // Wait for quorum
    if successCount >= quorum {
        return "ok"
    }
}
```

## Comparison with ASYNC

| Metric | SYNC (Quorum) | ASYNC (Primary-first) |
|--------|---------------|----------------------|
| Write Latency | 10-20ms | 1-5ms |
| Consistency | Strong | Eventual |
| Durability | High | High (eventual) |
| Use Case | Critical data | General purpose |
