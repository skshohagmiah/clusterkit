# Partition Management in ClusterKit

ClusterKit provides built-in support for data partitioning across your cluster nodes. This guide explains how to use partitions effectively.

## What are Partitions?

Partitions divide your data across multiple nodes in the cluster. Each partition:
- Has a unique ID
- Is assigned to a **primary node** (handles writes)
- Has **replica nodes** (handle reads and provide redundancy)
- Uses **consistent hashing** for stable assignment

## Key Concepts

### Partition Count
The number of partitions in your cluster (configured via `PartitionCount`). More partitions = better distribution but more overhead.

**Recommendation**: 16-64 partitions for most use cases

### Replication Factor
How many nodes store each partition (configured via `ReplicationFactor`). Higher replication = better availability but more storage.

**Recommendation**: 2-3 replicas for production

### Consistent Hashing
Keys are hashed to determine which partition they belong to. This ensures:
- Same key always maps to same partition
- Even distribution across partitions
- Minimal data movement when nodes join/leave

## Basic Usage

### 1. Configure Partitions

```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:   "node-1",
    HTTPAddr: ":8080",
    Config: &clusterkit.Config{
        ClusterName:       "my-cluster",
        PartitionCount:    16,  // 16 partitions
        ReplicationFactor: 3,   // 3 copies of each partition
    },
})
```

### 2. Create Partitions

After nodes join the cluster:

```go
// Wait for enough nodes
cluster := ck.GetCluster()
if len(cluster.Nodes) >= cluster.Config.ReplicationFactor {
    err := ck.CreatePartitions()
    if err != nil {
        log.Fatal(err)
    }
}
```

### 3. Find Partition for a Key

```go
// Determine which partition a key belongs to
partition, err := ck.GetPartitionForKey("user:1001")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Key belongs to: %s\n", partition.ID)
fmt.Printf("Primary node: %s\n", partition.PrimaryNode)
fmt.Printf("Replica nodes: %v\n", partition.ReplicaNodes)
```

### 4. Get Partitions for Current Node

```go
// Find which partitions this node is responsible for
myPartitions := ck.GetPartitionsForNode(ck.GetCluster().ID)

for _, partition := range myPartitions {
    if partition.PrimaryNode == ck.GetCluster().ID {
        fmt.Printf("Primary: %s\n", partition.ID)
    } else {
        fmt.Printf("Replica: %s\n", partition.ID)
    }
}
```

### 5. Get Partition Statistics

```go
stats := ck.GetPartitionStats()

fmt.Printf("Total partitions: %d\n", stats.TotalPartitions)
for nodeID, count := range stats.PartitionsPerNode {
    fmt.Printf("Node %s: %d primary partitions\n", nodeID, count)
}
```

## API Reference

### CreatePartitions()
```go
err := ck.CreatePartitions()
```
Creates and assigns partitions to nodes. Should be called once after cluster has enough nodes.

### GetPartitionForKey(key string)
```go
partition, err := ck.GetPartitionForKey("user:123")
```
Returns the partition that a given key belongs to.

### GetPartition(partitionID string)
```go
partition, err := ck.GetPartition("partition-0")
```
Returns a specific partition by ID.

### GetPartitionsForNode(nodeID string)
```go
partitions := ck.GetPartitionsForNode("node-1")
```
Returns all partitions where the node is primary or replica.

### ListPartitions()
```go
partitions := ck.ListPartitions()
```
Returns all partitions in the cluster.

### GetPartitionStats()
```go
stats := ck.GetPartitionStats()
```
Returns statistics about partition distribution.

### RebalancePartitions()
```go
err := ck.RebalancePartitions()
```
Redistributes partitions when nodes join or leave the cluster.

## HTTP Endpoints

### GET /partitions
Returns all partitions:
```bash
curl http://localhost:8080/partitions
```

Response:
```json
{
  "partitions": [
    {
      "id": "partition-0",
      "primary_node": "node-1",
      "replica_nodes": ["node-2", "node-3"]
    }
  ],
  "count": 16
}
```

### GET /partitions/stats
Returns partition statistics:
```bash
curl http://localhost:8080/partitions/stats
```

Response:
```json
{
  "total_partitions": 16,
  "partitions_per_node": {
    "node-1": 5,
    "node-2": 6,
    "node-3": 5
  },
  "replicas_per_node": {
    "node-1": 11,
    "node-2": 10,
    "node-3": 11
  }
}
```

### GET /partitions/key?key=<key>
Returns partition for a specific key:
```bash
curl http://localhost:8080/partitions/key?key=user:1001
```

Response:
```json
{
  "id": "partition-3",
  "primary_node": "node-2",
  "replica_nodes": ["node-1", "node-3"]
}
```

## Use Cases

### 1. Distributed Key-Value Store

```go
// Store data
key := "user:1001"
partition, _ := ck.GetPartitionForKey(key)

// Write to primary node
if partition.PrimaryNode == myNodeID {
    storeLocally(key, value)
} else {
    forwardToPrimary(partition.PrimaryNode, key, value)
}

// Replicate to replicas
for _, replicaNode := range partition.ReplicaNodes {
    replicateTo(replicaNode, key, value)
}
```

### 2. Distributed Task Queue

```go
// Assign task to partition
taskID := "task:12345"
partition, _ := ck.GetPartitionForKey(taskID)

// Process on primary node
if partition.PrimaryNode == myNodeID {
    processTask(taskID)
}
```

### 3. Sharded Database

```go
// Route database queries
userID := "user:5001"
partition, _ := ck.GetPartitionForKey(userID)

// Query the right shard
if partition.PrimaryNode == myNodeID {
    return queryLocalDB(userID)
} else {
    return queryRemoteDB(partition.PrimaryNode, userID)
}
```

## Best Practices

### 1. Choose Appropriate Partition Count
- **Too few**: Uneven distribution, limited scalability
- **Too many**: Overhead in management
- **Rule of thumb**: 2-4x the number of nodes

### 2. Set Replication Factor Based on Needs
- **RF=1**: No redundancy (not recommended for production)
- **RF=2**: Can tolerate 1 node failure
- **RF=3**: Can tolerate 2 node failures (recommended)

### 3. Create Partitions After Cluster Stabilizes
```go
// Wait for minimum nodes
if len(cluster.Nodes) >= cluster.Config.ReplicationFactor {
    ck.CreatePartitions()
}
```

### 4. Rebalance When Topology Changes
```go
// After adding/removing nodes
ck.RebalancePartitions()
```

### 5. Use Consistent Key Naming
```go
// Good: Predictable partitioning
"user:1001"
"order:5001"
"product:abc123"

// Avoid: Random keys that don't partition well
uuid.New().String()
```

## Example: Complete Application

See [example/partitions-demo/](./example/partitions-demo/) for a complete working example.

Run it:
```bash
# Terminal 1
cd example/partitions-demo
NODE_ID=node-1 HTTP_ADDR=:8080 go run main.go

# Terminal 2
NODE_ID=node-2 HTTP_ADDR=:8081 KNOWN_NODES=localhost:8080 go run main.go

# Terminal 3
NODE_ID=node-3 HTTP_ADDR=:8082 KNOWN_NODES=localhost:8080 go run main.go
```

## Advanced Topics

### Custom Partition Assignment
For custom partition assignment logic, you can extend the `assignNodesToPartition` function.

### Partition Migration
When rebalancing, data needs to be migrated between nodes. Implement your migration logic based on your data store.

### Monitoring Partitions
Use the `/partitions/stats` endpoint to monitor partition distribution and detect imbalances.

### Handling Node Failures
When a primary node fails:
1. Promote a replica to primary
2. Rebalance to maintain replication factor
3. Implement health checks to detect failures

## Troubleshooting

### Partitions Not Created
- Ensure enough nodes joined (>= ReplicationFactor)
- Check logs for errors
- Verify `CreatePartitions()` was called

### Uneven Distribution
- Increase partition count
- Check node IDs are unique
- Verify consistent hashing is working

### Replication Issues
- Ensure ReplicationFactor <= number of nodes
- Check network connectivity between nodes
- Verify state synchronization is working

## Next Steps

- Implement data replication logic
- Add partition migration on rebalance
- Set up health checks and failover
- Monitor partition metrics
