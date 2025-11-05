# ClusterKit API Reference

## Core API (7 Methods)

ClusterKit provides a simple, intuitive API with just 7 methods. That's all you need!

---

### 1. `GetPartition(key string) (*Partition, error)`

Get the partition responsible for a key.

**Parameters:**
- `key` - The key to look up (e.g., "user:123", "order:456")

**Returns:**
- `*Partition` - The partition object containing primary and replica node IDs
- `error` - Error if partitions not initialized

**Example:**
```go
partition, err := ck.GetPartition("user:123")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Partition ID: %s\n", partition.ID)
fmt.Printf("Primary Node: %s\n", partition.PrimaryNode)
fmt.Printf("Replicas: %v\n", partition.ReplicaNodes)
```

---

### 2. `GetPrimary(partition *Partition) *Node`

Get the primary node for a partition.

**Parameters:**
- `partition` - The partition object from `GetPartition()`

**Returns:**
- `*Node` - The primary node object with ID, IP, Name, etc.

**Example:**
```go
partition, _ := ck.GetPartition("user:123")
primary := ck.GetPrimary(partition)

fmt.Printf("Primary Node ID: %s\n", primary.ID)
fmt.Printf("Primary Node IP: %s\n", primary.IP)
fmt.Printf("Primary Node Name: %s\n", primary.Name)

// Send data to primary
httpPost(primary.IP, key, value)
```

---

### 3. `GetReplicas(partition *Partition) []Node`

Get all replica nodes for a partition.

**Parameters:**
- `partition` - The partition object from `GetPartition()`

**Returns:**
- `[]Node` - Slice of replica node objects

**Example:**
```go
partition, _ := ck.GetPartition("user:123")
replicas := ck.GetReplicas(partition)

fmt.Printf("Number of replicas: %d\n", len(replicas))

// Send data to all replicas
for _, replica := range replicas {
    httpPost(replica.IP, key, value)
}
```

---

### 4. `GetNodes(partition *Partition) []Node`

Get ALL nodes (primary + replicas) for a partition.

**Parameters:**
- `partition` - The partition object from `GetPartition()`

**Returns:**
- `[]Node` - Slice containing primary node followed by replica nodes

**Example:**
```go
partition, _ := ck.GetPartition("user:123")
allNodes := ck.GetNodes(partition)

fmt.Printf("Total nodes: %d\n", len(allNodes))

// Send data to all nodes (primary + replicas)
for _, node := range allNodes {
    httpPost(node.IP, key, value)
}
```

---

### 5. `IsPrimary(partition *Partition) bool`

Check if the current node is the primary for a partition.

**Parameters:**
- `partition` - The partition object from `GetPartition()`

**Returns:**
- `bool` - `true` if current node is the primary, `false` otherwise

**Example:**
```go
partition, _ := ck.GetPartition("user:123")

if ck.IsPrimary(partition) {
    // I'm the primary - store locally
    db.Write(key, value)
    
    // Replicate to replicas
    replicas := ck.GetReplicas(partition)
    for _, replica := range replicas {
        httpPost(replica.IP, key, value)
    }
} else {
    // I'm not the primary - forward to primary
    primary := ck.GetPrimary(partition)
    httpPost(primary.IP, key, value)
}
```

---

### 6. `IsReplica(partition *Partition) bool`

Check if the current node is a replica for a partition.

**Parameters:**
- `partition` - The partition object from `GetPartition()`

**Returns:**
- `bool` - `true` if current node is a replica, `false` otherwise

**Example:**
```go
partition, _ := ck.GetPartition("user:123")

if ck.IsReplica(partition) {
    // I'm a replica - store locally
    db.Write(key, value)
}
```

---

### 7. `GetMyNodeID() string`

Get the current node's ID.

**Returns:**
- `string` - The node ID (e.g., "node-1", "node-2")

**Example:**
```go
myNodeID := ck.GetMyNodeID()
fmt.Printf("I am node: %s\n", myNodeID)

// Use it to skip sending to self
for _, node := range allNodes {
    if node.ID != myNodeID {
        httpPost(node.IP, key, value)
    }
}
```

---

## Data Structures

### Node

```go
type Node struct {
    ID     string  // Unique node identifier (e.g., "node-1")
    IP     string  // Node address (e.g., ":8080", "192.168.1.10:8080")
    Port   int     // Port number (optional, usually in IP)
    Name   string  // Human-readable name (e.g., "Server-1")
    Status string  // Node status (e.g., "active", "inactive")
}
```

### Partition

```go
type Partition struct {
    ID           string    // Partition identifier (e.g., "partition-0")
    PrimaryNode  string    // Primary node ID
    ReplicaNodes []string  // Replica node IDs
}
```

---

## Complete Usage Pattern

Here's the recommended pattern for using ClusterKit:

```go
func HandleRequest(key, value string) error {
    // 1. Get partition
    partition, err := ck.GetPartition(key)
    if err != nil {
        return err
    }
    
    // 2. Get nodes
    primary := ck.GetPrimary(partition)
    replicas := ck.GetReplicas(partition)
    
    // 3. Handle primary
    if ck.IsPrimary(partition) {
        // Store locally
        storeLocally(key, value)
    } else {
        // Forward to primary
        sendToNode(primary, key, value)
    }
    
    // 4. Handle replicas
    if ck.IsReplica(partition) {
        // Store locally
        storeLocally(key, value)
    }
    
    // Forward to other replicas
    myNodeID := ck.GetMyNodeID()
    for _, replica := range replicas {
        if replica.ID != myNodeID {
            sendToNode(replica, key, value)
        }
    }
    
    return nil
}
```

---

## Context-Aware Version

For operations that need cancellation/timeout support:

```go
// Get partition with context
partition, err := ck.GetPartitionWithContext(ctx, key)
if err != nil {
    return err
}
```

---

## HTTP API Endpoints

ClusterKit also provides HTTP endpoints for cluster management:

### Cluster Information
- `GET /cluster` - Get cluster state
- `GET /health` - Health check
- `GET /health/detailed` - Detailed health status
- `GET /metrics` - Cluster metrics

### Partitions
- `GET /partitions` - List all partitions
- `GET /partitions/stats` - Partition statistics
- `GET /partitions/key?key=<key>` - Get partition for a key

### Consensus
- `GET /consensus/leader` - Get current Raft leader
- `GET /consensus/stats` - Raft statistics

---

## Best Practices

1. **Always check errors** from `GetPartition()`
2. **Use `IsPrimary()` and `IsReplica()`** instead of manual ID comparison
3. **Cache partition lookups** if you're handling the same key repeatedly
4. **Handle node failures** - implement retries and fallbacks
5. **Use context** for operations that can timeout

---

## Common Patterns

### Pattern 1: Write to Primary, Read from Any
```go
// Write
partition, _ := ck.GetPartition(key)
if ck.IsPrimary(partition) {
    db.Write(key, value)
} else {
    forwardToPrimary(key, value)
}

// Read
partition, _ := ck.GetPartition(key)
if ck.IsPrimary(partition) || ck.IsReplica(partition) {
    return db.Read(key)
}
return forwardToAnyNode(key)
```

### Pattern 2: Write to All, Read from Local
```go
// Write to all nodes
partition, _ := ck.GetPartition(key)
nodes := ck.GetNodes(partition)
for _, node := range nodes {
    sendToNode(node, key, value)
}

// Read from local
return db.Read(key)
```

### Pattern 3: Quorum Writes
```go
partition, _ := ck.GetPartition(key)
nodes := ck.GetNodes(partition)

// Write to majority
quorum := (len(nodes) / 2) + 1
successCount := 0

for _, node := range nodes {
    if sendToNode(node, key, value) == nil {
        successCount++
        if successCount >= quorum {
            return nil  // Quorum achieved
        }
    }
}

return errors.New("quorum not achieved")
```

---

## Migration Guide

If you're upgrading from an older version:

**Old API:**
```go
nodes, _ := ck.GetNodesForKey(key)
shouldHandle, role, _, _ := ck.AmIResponsibleFor(key)
```

**New API:**
```go
partition, _ := ck.GetPartition(key)
nodes := ck.GetNodes(partition)
isPrimary := ck.IsPrimary(partition)
isReplica := ck.IsReplica(partition)
```

---

**Questions?** Open an issue on [GitHub](https://github.com/skshohagmiah/clusterkit/issues)
