# Server-Side Example

This example demonstrates **server-side partition handling** where the server uses ClusterKit APIs to:
- Check if it's primary or replica for a partition
- Handle replication from primary to replicas
- Forward requests to the correct node
- Handle data migration when nodes join/leave

## Key Difference from Client-Side

### Client-Side (sync/async folders)
- ✅ Client routes directly to correct nodes
- ✅ Client handles replication strategy
- ✅ Server is dumb (just stores data)

### Server-Side (this folder)
- ✅ Client sends to any node
- ✅ Server checks if it's primary/replica
- ✅ Server forwards to primary if needed
- ✅ Server handles replication

## ClusterKit APIs Used

```go
// Check partition ownership
partition, _ := ck.GetPartition(key)
isPrimary := ck.IsPrimary(partition)
isReplica := ck.IsReplica(partition)

// Get nodes for replication
primary := ck.GetPrimary(partition)
replicas := ck.GetReplicas(partition)

// Get my partitions
myPartitions := ck.GetPartitionsForNode(nodeID)

// Register for partition changes
ck.OnPartitionChange(func(partitionID string, copyFrom, copyTo *Node) {
    // Handle data migration
})
```

## How It Works

### Write Flow:
```
Client → Any Node
           ↓
        Am I primary? 
           ↓
        YES → Store locally + replicate to replicas
           ↓
        NO → Am I replica?
           ↓
        YES → Store locally (primary handles replication)
           ↓
        NO → Forward to primary
```

### Read Flow:
```
Client → Any Node
           ↓
        Am I primary/replica?
           ↓
        YES → Serve from local storage
           ↓
        NO → Forward to primary
```

### Partition Change Flow:
```
Node joins/leaves
    ↓
ClusterKit rebalances partitions
    ↓
Hook triggered: OnPartitionChange(partitionID, copyFrom, copyTo)
    ↓
Server migrates data from copyFrom to copyTo
```

## Running the Example

```bash
./run.sh
```

This will:
1. Start 10 nodes
2. Perform 1000 write/read operations
3. Show partition distribution
4. Demonstrate server-side routing

## Example Output

```
[KV-node-1] Received SET for key=user:123
[KV-node-1] I'm PRIMARY for partition-5
[KV-node-1] Storing locally and replicating to 2 replicas
[KV-node-1] Replicated to node-2 (replica)
[KV-node-1] Replicated to node-3 (replica)

[KV-node-4] Received SET for key=user:456
[KV-node-4] I'm NOT primary/replica for partition-12
[KV-node-4] Forwarding to primary: node-2

[KV-node-5] Partition change detected
[KV-node-5] Partition partition-8: Need to copy data from node-1
[KV-node-5] Data migration completed
```

## When to Use Server-Side

**Use server-side when:**
- Clients are simple (web browsers, mobile apps)
- Don't want clients to know cluster topology
- Need centralized routing logic
- Traditional request/response pattern

**Use client-side when:**
- Clients are sophisticated (backend services)
- Want to minimize network hops
- Need maximum performance
- Direct routing is acceptable

## Comparison

| Feature | Server-Side | Client-Side |
|---------|-------------|-------------|
| **Client Complexity** | Simple | Smart |
| **Network Hops** | 2 (if forwarding) | 1 (direct) |
| **Latency** | Higher (forwarding) | Lower (direct) |
| **Server Logic** | Complex | Simple |
| **Use Case** | Web/mobile apps | Backend services |
