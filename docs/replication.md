# Replication Strategies in ClusterKit

This guide explains different replication strategies you can implement with ClusterKit and their trade-offs.

---

## Overview

ClusterKit provides **partition assignment** and **replica discovery**, but **you control** the replication strategy. This gives you complete flexibility to choose the right consistency model for your use case.

---

## Replication Strategies

### 1. Client-Side SYNC (Quorum-Based) 🔒

**Best for:** Strong consistency, financial transactions, inventory

#### How It Works

```
Client writes to ALL nodes (primary + replicas) in parallel
Wait for quorum (2/3) to acknowledge
Return success only if quorum reached
```

#### Implementation

```go
func (client *Client) Set(key, value string) error {
    // 1. Get partition and nodes
    partition := client.ck.GetPartition(key)
    nodes := client.ck.GetNodes(partition)  // [primary, replica1, replica2]
    
    // 2. Write to all nodes in parallel
    results := make(chan error, len(nodes))
    for _, node := range nodes {
        go func(n *Node) {
            results <- client.writeToNode(n, key, value)
        }(node)
    }
    
    // 3. Wait for quorum (2/3)
    quorum := (len(nodes) / 2) + 1
    successes := 0
    for i := 0; i < len(nodes); i++ {
        if err := <-results; err == nil {
            successes++
            if successes >= quorum {
                return nil  // Success!
            }
        }
    }
    
    return errors.New("quorum not reached")
}
```

#### Characteristics

| Aspect | Value |
|--------|-------|
| **Consistency** | Strong (quorum) |
| **Latency** | Medium (wait for 2/3 nodes) |
| **Throughput** | 3,750 ops/sec |
| **Durability** | High (data on 2+ nodes) |
| **Complexity** | Medium |

#### Pros & Cons

✅ **Pros:**
- Data on multiple nodes before success
- Survives node failures
- Strong consistency guarantees
- Read from any replica

❌ **Cons:**
- Higher latency (wait for quorum)
- More network traffic
- Client complexity

---

### 2. Client-Side ASYNC (Primary-First) ⚡

**Best for:** High throughput, streaming, analytics, caching

#### How It Works

```
Client writes to PRIMARY only
Return immediately
Replicate to replicas asynchronously in background
```

#### Implementation

```go
func (client *Client) Set(key, value string) error {
    // 1. Get partition and primary
    partition := client.ck.GetPartition(key)
    primary := client.ck.GetPrimary(partition)
    
    // 2. Write to primary (blocking)
    if err := client.writeToNode(primary, key, value); err != nil {
        return err
    }
    
    // 3. Replicate to replicas (async)
    replicas := client.ck.GetReplicas(partition)
    for _, replica := range replicas {
        go client.writeToNode(replica, key, value)  // Fire and forget
    }
    
    return nil  // Return immediately
}
```

#### Characteristics

| Aspect | Value |
|--------|-------|
| **Consistency** | Eventual |
| **Latency** | Low (primary only) |
| **Throughput** | 12,500 ops/sec |
| **Durability** | Medium (primary has data) |
| **Complexity** | Low |

#### Pros & Cons

✅ **Pros:**
- Fastest approach
- High throughput
- Simple implementation
- Low latency

❌ **Cons:**
- Eventual consistency
- Replicas lag behind
- Data loss if primary fails before replication

---

### 3. Server-Side Routing 🌐

**Best for:** Simple clients, web/mobile apps, HTTP-only environments

#### How It Works

```
Client sends to ANY node
Server checks: Am I primary? Am I replica?
If neither, forward to primary
Server handles replication
```

#### Implementation

```go
func (server *Server) handleSet(w http.ResponseWriter, r *http.Request) {
    key := r.FormValue("key")
    value := r.FormValue("value")
    
    // 1. Get partition
    partition, _ := server.ck.GetPartition(key)
    
    // 2. Am I the primary?
    if server.ck.IsPrimary(partition) {
        // Store locally
        server.data[key] = value
        
        // Replicate to replicas
        replicas := server.ck.GetReplicas(partition)
        for _, replica := range replicas {
            go server.replicateToNode(replica, key, value)
        }
        
        w.WriteHeader(http.StatusOK)
        return
    }
    
    // 3. Am I a replica?
    if server.ck.IsReplica(partition) {
        // Just store it
        server.data[key] = value
        w.WriteHeader(http.StatusOK)
        return
    }
    
    // 4. Forward to primary
    primary := server.ck.GetPrimary(partition)
    server.forwardToPrimary(primary, key, value)
}
```

#### Characteristics

| Aspect | Value |
|--------|-------|
| **Consistency** | Configurable |
| **Latency** | Medium (extra hop) |
| **Throughput** | 8,000 ops/sec |
| **Durability** | Depends on strategy |
| **Complexity** | Medium (server-side) |

#### Pros & Cons

✅ **Pros:**
- Simple HTTP clients
- No SDK required
- Server controls strategy
- Traditional architecture

❌ **Cons:**
- Extra network hop
- Server complexity
- Lower throughput

---

## Consistency Models

### Strong Consistency (Quorum)

```
Write: Wait for 2/3 nodes
Read: Read from any node (all have same data)

Timeline:
T0: Write to node-1, node-2, node-3
T1: Wait for 2/3 ACKs
T2: Return success
T3: All nodes have data

Result: All reads see latest write ✅
```

### Eventual Consistency (Async)

```
Write: Write to primary, return immediately
Read: Read from primary (or stale replica)

Timeline:
T0: Write to primary
T1: Return success
T2: Replicate to replica-1 (async)
T3: Replicate to replica-2 (async)

Result: Reads may see stale data for ~100ms ⚠️
```

### Read-Your-Writes (Sticky Sessions)

```
Write: Write to primary
Read: Always read from same node (session affinity)

Timeline:
T0: Client writes to primary
T1: Client reads from primary
T2: Client always routes to primary

Result: Client sees own writes ✅
```

---

## Failure Handling

### Primary Failure (SYNC Mode)

```
1. Client writes to [primary, replica-1, replica-2]
2. Primary fails ❌
3. Client gets 2/3 ACKs from replicas ✅
4. Write succeeds!
5. ClusterKit detects failure (15s)
6. Promotes replica to primary
7. Client refreshes topology
8. Future writes go to new primary
```

**Result:** Zero data loss, <100ms failover ✅

### Primary Failure (ASYNC Mode)

```
1. Client writes to primary
2. Primary ACKs ✅
3. Primary fails before replicating ❌
4. Data lost! ❌

OR:

1. Client writes to primary
2. Primary replicates to replicas
3. Primary fails ❌
4. Replicas have data ✅
5. Promote replica to primary
6. Data preserved ✅
```

**Result:** Possible data loss if failure before replication ⚠️

### Network Partition

```
Scenario: Cluster splits into [node-1] and [node-2, node-3]

SYNC Mode:
- Writes to minority partition fail (no quorum) ✅
- Writes to majority partition succeed ✅
- Strong consistency maintained ✅

ASYNC Mode:
- Both partitions accept writes ❌
- Split-brain scenario ❌
- Requires conflict resolution ⚠️
```

---

## Choosing a Strategy

### Decision Matrix

| Your Requirement | Best Strategy |
|------------------|---------------|
| **Strong consistency** | Client-Side SYNC |
| **Maximum throughput** | Client-Side ASYNC |
| **Simple clients** | Server-Side |
| **Financial transactions** | Client-Side SYNC |
| **Streaming/Analytics** | Client-Side ASYNC |
| **User sessions** | Client-Side ASYNC |
| **Inventory management** | Client-Side SYNC |
| **Caching** | Client-Side ASYNC |
| **Web/Mobile apps** | Server-Side |

### Trade-off Table

| Strategy | Consistency | Latency | Throughput | Complexity |
|----------|-------------|---------|------------|------------|
| **SYNC** | Strong | Medium | Medium | Medium |
| **ASYNC** | Eventual | Low | High | Low |
| **Server-Side** | Configurable | Medium | Medium | Medium |

---

## Advanced Patterns

### Hybrid: Critical + Non-Critical Data

```go
func (client *Client) Set(key, value string, critical bool) error {
    if critical {
        // Use SYNC for critical data
        return client.syncWrite(key, value)
    } else {
        // Use ASYNC for non-critical data
        return client.asyncWrite(key, value)
    }
}
```

### Read Repair

```go
func (client *Client) Get(key string) (string, error) {
    partition := client.ck.GetPartition(key)
    nodes := client.ck.GetNodes(partition)
    
    // Read from all nodes
    values := make(map[string]int)
    for _, node := range nodes {
        value, _ := client.readFromNode(node, key)
        values[value]++
    }
    
    // Find most common value
    mostCommon := findMostCommon(values)
    
    // Repair nodes with stale data
    for _, node := range nodes {
        value, _ := client.readFromNode(node, key)
        if value != mostCommon {
            client.writeToNode(node, key, mostCommon)  // Repair
        }
    }
    
    return mostCommon, nil
}
```

### Version Vectors (Conflict Resolution)

```go
type ValueWithVersion struct {
    Value   string
    Version int64  // Timestamp or logical clock
}

func (server *Server) handleSet(key string, newValue ValueWithVersion) {
    existing := server.data[key]
    
    if newValue.Version > existing.Version {
        // New value is newer
        server.data[key] = newValue
    } else {
        // Keep existing (newer)
    }
}
```

---

## Best Practices

### 1. Choose Based on Use Case

```go
// Financial transactions → SYNC
bankingClient := NewSyncClient(ck)

// User sessions → ASYNC
sessionClient := NewAsyncClient(ck)

// Web API → Server-Side
http.HandleFunc("/api", serverSideHandler)
```

### 2. Handle Failures Gracefully

```go
func (client *Client) SetWithRetry(key, value string) error {
    for attempt := 0; attempt < 3; attempt++ {
        if err := client.Set(key, value); err == nil {
            return nil
        }
        
        // Refresh topology and retry
        client.refreshTopology()
        time.Sleep(100 * time.Millisecond)
    }
    return errors.New("max retries exceeded")
}
```

### 3. Monitor Replication Lag

```go
func (server *Server) getReplicationLag() time.Duration {
    primary := server.getPrimaryTimestamp()
    replica := server.getReplicaTimestamp()
    return primary.Sub(replica)
}
```

### 4. Use Timeouts

```go
client := &http.Client{
    Timeout: 2 * time.Second,  // Prevent hanging
}
```

---

## Examples

See complete working examples:

- **[SYNC Mode](../example/sync/)** - Quorum-based replication
- **[ASYNC Mode](../example/async/)** - Primary-first replication
- **[Server-Side](../example/server-side/)** - Server routing

---

## Further Reading

- [Consistency Models](https://jepsen.io/consistency) - Jepsen consistency guide
- [CAP Theorem](https://en.wikipedia.org/wiki/CAP_theorem) - Trade-offs
- [Quorum Consensus](https://en.wikipedia.org/wiki/Quorum_(distributed_computing)) - Wikipedia
- [Eventual Consistency](https://www.allthingsdistributed.com/2008/12/eventually_consistent.html) - Werner Vogels
