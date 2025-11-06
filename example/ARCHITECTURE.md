# Architecture: Smart Client vs Simple Server

## The Correct Design

### ✅ Smart Client (Handles Routing & Replication)

The **client** uses ClusterKit to:
1. Get partition for a key
2. Get primary and replica nodes
3. Route requests directly to correct nodes
4. Implement replication strategy (sync/async)

### ✅ Simple Server (Just Stores Data)

The **server** is dumb:
1. Receives write request → stores data
2. Receives read request → returns data
3. No routing, no forwarding, no replication logic

---

## Why This Design?

### ❌ Wrong: Server-Side Routing

```go
// BAD: Server checks if it's primary/replica
func (server *KVStore) handleSet(key, value) {
    partition := ck.GetPartition(key)
    
    if ck.IsPrimary(partition) {
        // Store and replicate
    } else if ck.IsReplica(partition) {
        // Just store
    } else {
        // Forward to primary
    }
}
```

**Problems:**
- ❌ Extra network hop (client → wrong node → correct node)
- ❌ Server complexity
- ❌ Wasted resources

### ✅ Correct: Client-Side Routing

```go
// GOOD: Client routes directly
func (client *Client) Set(key, value) {
    partition := getPartitionID(key)
    
    // Get correct nodes from topology
    primary := topology.GetPrimary(partition)
    replicas := topology.GetReplicas(partition)
    
    // Route directly to correct nodes
    writeToNode(primary, key, value)
    writeToNodes(replicas, key, value)
}

// Server is simple
func (server *KVStore) handleSet(key, value) {
    data[key] = value  // Just store it!
    return "ok"
}
```

**Benefits:**
- ✅ Direct routing (no extra hops)
- ✅ Simple server
- ✅ Client controls strategy

---

## SYNC Example (Quorum)

### Client (Smart)
```go
func (c *Client) Set(key, value string) error {
    // 1. Get partition and nodes
    partition := c.getPartitionID(key)
    allNodes := c.topology.GetAllNodes(partition)
    
    // 2. Write to ALL nodes in parallel
    for _, node := range allNodes {
        go writeToNode(node, key, value)
    }
    
    // 3. Wait for QUORUM (2/3)
    if successCount >= quorum {
        return nil  // Success!
    }
}
```

### Server (Simple)
```go
func (kv *KVStore) handleSet(key, value string) {
    kv.data[key] = value  // Just store!
    return "ok"
}
```

---

## ASYNC Example (Primary-First)

### Client (Smart)
```go
func (c *Client) Set(key, value string) error {
    // 1. Get partition and nodes
    partition := c.getPartitionID(key)
    primary := c.topology.GetPrimary(partition)
    replicas := c.topology.GetReplicas(partition)
    
    // 2. Write to PRIMARY first (blocking)
    writeToNode(primary, key, value)
    
    // 3. Return immediately (fast!)
    
    // 4. Replicate in BACKGROUND
    go func() {
        for _, replica := range replicas {
            writeToNode(replica, key, value)
        }
    }()
    
    return nil
}
```

### Server (Simple)
```go
func (kv *KVStore) handleSet(key, value string) {
    kv.data[key] = value  // Just store!
    return "ok"
}
```

---

## How ClusterKit Fits In

ClusterKit provides the **topology information** to the client:

```go
// Client fetches topology from ClusterKit
resp := http.Get("http://node1:8080/partitions")
resp := http.Get("http://node1:8080/cluster")

// Now client knows:
// - Which partition each key belongs to
// - Which nodes are primary/replicas for each partition
// - Node addresses

// Client uses this to route directly
primary := topology.Partitions["partition-5"].PrimaryNode
replicas := topology.Partitions["partition-5"].ReplicaNodes
```

---

## Network Flow

### ❌ Wrong (Server-Side Routing)
```
Client → Node 2 (wrong node)
           ↓
         Node 2 checks: "I'm not primary"
           ↓
         Node 2 → Node 1 (primary)
           ↓
         Node 1 stores data
           ↓
         Node 1 → Node 2 (response)
           ↓
         Node 2 → Client (response)

Total: 4 network hops!
```

### ✅ Correct (Client-Side Routing)
```
Client → Node 1 (primary) ✓
           ↓
         Node 1 stores data
           ↓
         Node 1 → Client (response)

Total: 2 network hops!
```

---

## Summary

| Component | Responsibility |
|-----------|---------------|
| **ClusterKit** | Provides topology (partitions, nodes) |
| **Client** | Gets topology, routes to correct nodes, implements replication strategy |
| **Server** | Just stores/retrieves data |

**Key Principle:** Keep servers simple, make clients smart!
