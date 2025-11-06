# Partitioning & Replication in ClusterKit

## Table of Contents

1. [Overview](#overview)
2. [How Partitioning Works](#how-partitioning-works)
3. [How Replication Works](#how-replication-works)
4. [Data Migration](#data-migration)
5. [Consistent Hashing](#consistent-hashing)
6. [Rebalancing](#rebalancing)
7. [Edge Cases & Solutions](#edge-cases--solutions)

---

## Overview

ClusterKit uses **consistent hashing** to distribute data across nodes in a cluster. This document explains the complete lifecycle of data partitioning, replication, and migration.

### Key Concepts

- **Partition**: A logical shard of your data (e.g., `partition-0`, `partition-1`, ...)
- **Primary Node**: The authoritative node for a partition (handles writes)
- **Replica Nodes**: Backup nodes that store copies of the data
- **Replication Factor**: Number of copies of each partition (default: 3)
- **Partition Count**: Total number of partitions (default: 64)

---

## How Partitioning Works

### 1. Key-to-Partition Mapping

ClusterKit uses **MD5 hashing** to map keys to partitions:

```go
// Step 1: Hash the key
hash := md5.Sum([]byte("user:123"))
hashValue := binary.BigEndian.Uint32(hash[:4])
// hashValue = 2847561234

// Step 2: Modulo to get partition number
partitionNum := hashValue % 64  // 64 partitions
// partitionNum = 18

// Step 3: Format partition ID
partitionID := fmt.Sprintf("partition-%d", partitionNum)
// partitionID = "partition-18"
```

**Why MD5?**
- Fast (microseconds)
- Uniform distribution
- Deterministic (same key → same partition)
- Available in Go standard library

### 2. Partition-to-Node Assignment

Each partition is assigned to nodes using **consistent hashing**:

```go
// For partition-18, hash with each node ID
scores := []struct{nodeID string, score uint32}{}

for _, node := range nodes {
    // Combine partition ID + node ID
    combined := "partition-18" + "node-1"
    hash := md5.Sum([]byte(combined))
    score := binary.BigEndian.Uint32(hash[:4])
    scores = append(scores, {nodeID: "node-1", score: score})
}

// Sort by score (lowest to highest)
sort.Slice(scores, func(i, j int) bool {
    return scores[i].score < scores[j].score
})

// Assign roles
primary := scores[0].nodeID       // Lowest score = primary
replica1 := scores[1].nodeID      // 2nd lowest = replica 1
replica2 := scores[2].nodeID      // 3rd lowest = replica 2
```

**Example:**
```
Cluster: [node-1, node-2, node-3, node-4, node-5]
Partition: partition-18
Replication Factor: 3

Hashing Results:
  partition-18 + node-1 → hash: 4829384756
  partition-18 + node-2 → hash: 2345678901 ← Lowest (PRIMARY)
  partition-18 + node-3 → hash: 3456789012 ← 2nd (REPLICA 1)
  partition-18 + node-4 → hash: 5678901234
  partition-18 + node-5 → hash: 3567890123 ← 3rd (REPLICA 2)

Assignment:
  partition-18: Primary=node-2, Replicas=[node-3, node-5]
```

### 3. Complete Flow

```
User Request: Set("user:123", "John")
     ↓
1. Hash key → partition-18
     ↓
2. Get partition-18 assignment
     ↓
3. Primary=node-2, Replicas=[node-3, node-5]
     ↓
4. Write to node-2 (primary)
     ↓
5. Replicate to node-3 and node-5
     ↓
6. Return success
```

---

## How Replication Works

ClusterKit supports **three replication strategies**:

### 1. SYNC (Quorum-Based)

**Strong consistency** - Wait for majority before returning success.

```go
// Client writes to ALL nodes (primary + replicas)
allNodes := [primary, replica1, replica2]

successCount := 0
for _, node := range allNodes {
    if writeToNode(node, key, value) == nil {
        successCount++
    }
}

// Require quorum (2 out of 3)
if successCount >= 2 {
    return success  // ✅ Quorum reached
}
return error  // ❌ Quorum failed
```

**Characteristics:**
- ✅ Strong consistency (reads always see latest write)
- ✅ Survives node failures (as long as quorum alive)
- ❌ Higher latency (waits for multiple nodes)
- ❌ Lower throughput (blocking writes)

**Use Case:** Financial systems, inventory management

### 2. ASYNC (Primary-First)

**Low latency** - Write to primary, replicate in background.

```go
// 1. Write to primary (blocking)
if err := writeToPrimary(key, value); err != nil {
    return err
}

// 2. Replicate to replicas (background)
go func() {
    for _, replica := range replicas {
        writeToNode(replica, key, value)
        // Ignore errors - eventual consistency
    }
}()

return success  // ✅ Return immediately
```

**Characteristics:**
- ✅ Low latency (only waits for primary)
- ✅ High throughput (non-blocking)
- ❌ Eventual consistency (replicas lag behind)
- ❌ Data loss risk if primary dies before replication

**Use Case:** Social media feeds, analytics, caching

### 3. Server-Side Routing

**Centralized** - Server handles routing and replication.

```go
// Client sends to any node
client.Post("http://any-node/kv/set", data)

// Server checks ownership
if IsPrimary(partition) {
    // I'm primary - store and replicate
    storeLocally(key, value)
    replicateToReplicas(key, value)
    return success
}

if IsReplica(partition) {
    // I'm replica - just store
    storeLocally(key, value)
    return success
}

// I don't own it - forward to primary
forwardToPrimary(key, value)
```

**Characteristics:**
- ✅ Simple client (no routing logic)
- ✅ Centralized control
- ❌ Extra network hop (forwarding)
- ❌ Server becomes bottleneck

**Use Case:** Simple applications, microservices

---

## Data Migration

### When Migration Happens

Data migration is triggered when partition ownership changes:

1. **Node joins** → Partitions rebalanced → Some move to new node
2. **Node leaves** → Partitions reassigned → Data moved from dead node
3. **Primary fails** → Replica promoted → New nodes need data

### Migration Process

#### Step 1: Detect Changes

```go
// ClusterKit detects partition changes
oldState: partition-18: Primary=node-2, Replicas=[node-3, node-4]
newState: partition-18: Primary=node-3, Replicas=[node-4, node-5]

Changes detected:
  - node-3: replica → primary (needs data)
  - node-5: none → replica (needs data)
```

#### Step 2: Trigger Hooks

```go
// ClusterKit calls your hook
OnPartitionChange("partition-18", copyFromNodes, copyToNode)

// For node-3 (promoted to primary):
copyFromNodes = [node-4]  // Old replica (has data)
copyToNode = node-3

// For node-5 (new replica):
copyFromNodes = [node-3, node-4]  // Old replicas
copyToNode = node-5
```

**Why multiple sources?**
- Different replicas may have different data (during failover)
- Merging ensures no data loss
- Allows conflict resolution

#### Step 3: Fetch Data

```go
func handlePartitionChange(partitionID string, copyFromNodes []*Node, copyToNode *Node) {
    if copyToNode.ID != myNodeID {
        return  // Not for me
    }
    
    // Fetch data from ALL sources
    mergedData := make(map[string]ValueWithVersion)
    
    for _, sourceNode := range copyFromNodes {
        // HTTP request to source node
        url := fmt.Sprintf("http://%s/kv/migrate?partition=%s", 
            sourceNode.IP, partitionID)
        resp, _ := http.Get(url)
        
        var data map[string]ValueWithVersion
        json.NewDecoder(resp.Body).Decode(&data)
        
        // Merge with conflict resolution
        for key, newValue := range data {
            existing := mergedData[key]
            if newValue.Version > existing.Version {
                mergedData[key] = newValue  // Keep newer
            }
        }
    }
    
    // Store merged data
    for key, value := range mergedData {
        kv.data[key] = value
    }
}
```

#### Step 4: Serve Migration Endpoint

```go
func handleMigrate(w http.ResponseWriter, r *http.Request) {
    partitionID := r.URL.Query().Get("partition")
    
    // Collect all keys for this partition
    partitionData := make(map[string]ValueWithVersion)
    
    for key, value := range kv.data {
        partition, _ := kv.ck.GetPartition(key)
        if partition.ID == partitionID {
            partitionData[key] = value
        }
    }
    
    // Return data
    json.NewEncoder(w).Encode(map[string]interface{}{
        "partition_id": partitionID,
        "data":         partitionData,
        "count":        len(partitionData),
    })
}
```

### Migration Timeline

```
t=0s:   Node-2 dies
        └─ Raft detects failure

t=5s:   Rebalancing triggered
        └─ New partition map calculated
        └─ partition-18: Primary=node-3, Replicas=[node-4, node-5]

t=6s:   Hooks triggered
        └─ node-3 starts migrating from node-4
        └─ node-5 starts migrating from node-3, node-4

t=6-30s: Migration in progress
        └─ Data copied in background
        └─ Operations continue normally

t=30s:  Migration complete
        └─ All nodes have consistent data
```

**Important:** Migration happens in **background**. Operations continue during migration!

---

## Consistent Hashing

### Why Consistent Hashing?

Traditional hashing (`hash(key) % nodeCount`) has a problem:

```
Initial: 5 nodes
  key "user:123" → hash % 5 = node-3

After node-6 joins: 6 nodes
  key "user:123" → hash % 6 = node-1  ❌ Different node!
  
Result: ALL keys need to be remapped! 💥
```

Consistent hashing solves this:

```
Initial: 5 nodes
  partition-18 → [node-2, node-3, node-5]

After node-6 joins: 6 nodes
  partition-18 → [node-2, node-3, node-5]  ✅ Same nodes!
  partition-42 → [node-6, node-1, node-4]  ← Only some partitions change
  
Result: Only ~1/6 of partitions remapped ✅
```

### How It Works

1. **Fixed partitions**: 64 partitions (never changes)
2. **Hash partition + node**: Deterministic assignment
3. **Sort by hash**: Lowest scores win
4. **Node changes**: Only affects some partitions

**Example:**

```
Partitions: [partition-0, partition-1, ..., partition-63]
Nodes: [node-1, node-2, node-3, node-4, node-5]

For partition-18:
  partition-18 + node-1 → score: 4829384756
  partition-18 + node-2 → score: 2345678901 ← Primary
  partition-18 + node-3 → score: 3456789012 ← Replica 1
  partition-18 + node-4 → score: 5678901234
  partition-18 + node-5 → score: 3567890123 ← Replica 2

Node-6 joins:
  partition-18 + node-6 → score: 4123456789
  
Sorted scores:
  node-2: 2345678901 ← Still primary ✅
  node-3: 3456789012 ← Still replica 1 ✅
  node-5: 3567890123 ← Still replica 2 ✅
  node-6: 4123456789
  node-1: 4829384756
  
Result: partition-18 assignment unchanged!
```

### Benefits

- ✅ **Minimal data movement** - Only ~1/N partitions move when node joins/leaves
- ✅ **Deterministic** - Same input → same output
- ✅ **Balanced** - Even distribution across nodes
- ✅ **Scalable** - Works with any number of nodes

---

## Rebalancing

### When Rebalancing Happens

1. **Node joins** - New node added to cluster
2. **Node leaves** - Node gracefully removed
3. **Node dies** - Node failure detected by Raft

### Rebalancing Algorithm

```go
func RebalancePartitions() {
    // 1. Get current state
    oldPartitions := getCurrentPartitions()
    
    // 2. Recalculate assignments for ALL partitions
    for partitionID := range partitions {
        // Use consistent hashing
        primary, replicas := assignNodesToPartition(partitionID, replicationFactor)
        
        partitions[partitionID].PrimaryNode = primary
        partitions[partitionID].ReplicaNodes = replicas
    }
    
    // 3. Propose through Raft (consensus)
    raft.ProposeAction("rebalance_partitions", partitions)
    
    // 4. Detect changes and trigger hooks
    notifyPartitionChanges(oldPartitions, newPartitions)
}
```

### Example: Node Joins

```
Before (5 nodes):
  partition-18: Primary=node-2, Replicas=[node-3, node-5]
  partition-42: Primary=node-1, Replicas=[node-4, node-5]

Node-6 joins (6 nodes):
  
Recalculate partition-18:
  partition-18 + node-1 → 4829384756
  partition-18 + node-2 → 2345678901 ← Primary (unchanged)
  partition-18 + node-3 → 3456789012 ← Replica 1 (unchanged)
  partition-18 + node-4 → 5678901234
  partition-18 + node-5 → 3567890123 ← Replica 2 (unchanged)
  partition-18 + node-6 → 4123456789
  
Result: partition-18 unchanged ✅

Recalculate partition-42:
  partition-42 + node-1 → 1234567890 ← Primary (unchanged)
  partition-42 + node-2 → 5678901234
  partition-42 + node-3 → 2345678901 ← NEW Replica 1
  partition-42 + node-4 → 3456789012 ← Replica 2 (was Replica 1)
  partition-42 + node-5 → 6789012345
  partition-42 + node-6 → 4567890123
  
Result: partition-42 changed!
  Old: Primary=node-1, Replicas=[node-4, node-5]
  New: Primary=node-1, Replicas=[node-3, node-4]
  
Migration needed:
  - node-3 needs data (new replica)
  - node-5 can delete data (no longer replica)
```

### Example: Node Dies

```
Before (5 nodes):
  partition-18: Primary=node-2 ☠️, Replicas=[node-3, node-4]

Node-2 dies (4 nodes remain):
  
Recalculate partition-18 (without node-2):
  partition-18 + node-1 → 4829384756
  partition-18 + node-3 → 3456789012 ← Lowest (NEW Primary)
  partition-18 + node-4 → 5678901234 ← 2nd (Replica 1)
  partition-18 + node-5 → 3567890123 ← 3rd (NEW Replica 2)
  
Result:
  Old: Primary=node-2, Replicas=[node-3, node-4]
  New: Primary=node-3, Replicas=[node-4, node-5]
  
Changes:
  - node-3: replica → primary (already has data ✅)
  - node-4: replica → replica (no change ✅)
  - node-5: none → replica (needs data from node-3 or node-4)
```

---

## Edge Cases & Solutions

### 1. Replica Promoted During Failover

**Problem:**
```
Old: Primary=node-2 ☠️, Replicas=[node-3, node-4]
New: Primary=node-3, Replicas=[node-4, node-5]

node-5 needs data. Should it copy from node-3?
❌ NO! node-3 is also migrating (replica → primary)
```

**Solution:**
```go
// Filter out nodes that are also being promoted
if replicaID == newPrimary && replicaID != oldPrimary {
    continue  // Skip this replica as source
}

// node-5 copies from node-4 only (stable source)
copyFromNodes = [node-4]  ✅
```

### 2. Multiple Replicas Have Different Data

**Problem:**
```
During failover:
  - Client writes to node-3 (replica)
  - Client writes to node-4 (replica)
  - Different data on each replica!

After rebalancing:
  - node-5 needs data
  - Which replica to copy from? 🤔
```

**Solution:**
```go
// Copy from ALL replicas and MERGE
copyFromNodes = [node-3, node-4]

mergedData := make(map[string]ValueWithVersion)
for _, source := range copyFromNodes {
    data := fetchFrom(source)
    
    for key, newValue := range data {
        existing := mergedData[key]
        if newValue.Version > existing.Version {
            mergedData[key] = newValue  // Keep newer
        }
    }
}
```

### 3. Not Enough Nodes for Replication Factor

**Problem:**
```
Replication Factor: 3
Nodes: 2 (not enough!)
```

**Solution:**
```go
if len(nodes) < replicationFactor {
    return fmt.Errorf("not enough nodes for replication factor")
}

// OR: Adjust replication factor dynamically
actualReplication := min(replicationFactor, len(nodes))
```

### 4. Partition Count Too Small

**Problem:**
```
Partitions: 4
Nodes: 10
Result: Uneven distribution (some nodes get 0 partitions)
```

**Solution:**
```
Rule of thumb: partitions >= nodes * 10

Example:
  10 nodes → 100+ partitions
  100 nodes → 1000+ partitions
  
ClusterKit default: 64 partitions (good for up to 6 nodes)
```

### 5. Migration During High Load

**Problem:**
```
Migration copies 1GB of data
High load: 1000 req/s
Migration blocks operations? 💥
```

**Solution:**
```go
// Migration runs in background goroutine
go func() {
    migratePartition(partitionID, sources)
}()

// Operations continue normally
handleRequest()  // Not blocked!

// Rate limiting (optional)
limiter := rate.NewLimiter(rate.Limit(100), 100)  // 100 req/s
limiter.Wait(ctx)
fetchDataFromSource()
```

---

## Best Practices

### 1. Choose Appropriate Partition Count

```go
// Rule: partitions = nodes * 10 to 20
nodes := 10
partitions := nodes * 15  // 150 partitions

ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    PartitionCount: partitions,
})
```

### 2. Set Replication Factor Based on Availability Needs

```go
// Survives N-1 node failures
replicationFactor := 3  // Survives 2 failures

ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    ReplicationFactor: replicationFactor,
})
```

### 3. Implement Versioning for Conflict Resolution

```go
type ValueWithVersion struct {
    Value     string
    Version   int64  // Unix nanoseconds
    WrittenBy string // Node ID
}

// Always include version in your data
kv.data[key] = ValueWithVersion{
    Value:     value,
    Version:   time.Now().UnixNano(),
    WrittenBy: myNodeID,
}
```

### 4. Monitor Migration Progress

```go
func handlePartitionChange(partitionID string, sources []*Node, target *Node) {
    start := time.Now()
    
    // ... migration logic ...
    
    duration := time.Since(start)
    log.Printf("✅ Migrated %s in %v (%d keys)", 
        partitionID, duration, keyCount)
    
    // Metrics
    migrationDuration.Observe(duration.Seconds())
    migrationKeys.Add(float64(keyCount))
}
```

### 5. Test Failure Scenarios

```bash
# Test node failure
kill <node-pid>

# Test network partition
iptables -A INPUT -p tcp --dport 8080 -j DROP

# Test slow migration
tc qdisc add dev eth0 root netem delay 100ms

# Test high load during migration
wrk -t12 -c400 -d30s http://localhost:8080/kv/set
```

---

## Summary

| Concept | How It Works | Key Benefit |
|---------|--------------|-------------|
| **Partitioning** | MD5 hash → modulo → partition ID | Deterministic key routing |
| **Consistent Hashing** | Hash partition+node → sort → assign | Minimal data movement |
| **Replication** | Primary + N replicas per partition | High availability |
| **Migration** | Merge from ALL old nodes | No data loss |
| **Rebalancing** | Recalculate on node change | Automatic recovery |

**ClusterKit handles the hard parts (partitioning, replication, migration) so you can focus on your application logic!** 🚀
