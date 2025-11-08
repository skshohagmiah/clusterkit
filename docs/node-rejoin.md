# Data Synchronization on Node Rejoin

## The Problem: Stale Data After Rejoin

### Scenario Timeline

```
Time    Event                           Node-1      Node-2      Node-3
────────────────────────────────────────────────────────────────────────
T0      Initial state                   100 keys    100 keys    100 keys
        All nodes healthy               ✅          ✅          ✅

T1      Node-3 dies                     100 keys    100 keys    💀 DEAD
        Health checks fail              ✅          ✅          ❌

T2      Node-3 removed                  100 keys    100 keys    (offline)
        Partitions rebalanced           ✅          ✅          

T3      New writes (keys 101-150)       150 keys    150 keys    (offline)
        Data only on Node-1 & Node-2    ✅          ✅          

T4      Node-3 rejoins                  150 keys    150 keys    100 keys ⚠️
        Partitions rebalanced           ✅          ✅          ⚠️ STALE!

T5      Read key 125 from Node-3        ✅          ✅          ❌ NOT FOUND!
        Node-3 doesn't have it!
```

## Current ClusterKit Behavior

### ✅ What ClusterKit DOES Handle

1. **Partition Reassignment**
   ```go
   // When Node-3 rejoins, ClusterKit:
   // 1. Detects rejoin (no duplicate nodes)
   // 2. Updates node info in cluster state
   // 3. Triggers RebalancePartitions()
   // 4. Fires OnPartitionChange() hooks
   ```

2. **Hook Notifications**
   ```go
   // Your application receives:
   OnPartitionChange(partitionID, copyFrom, copyTo) {
       // partition-5 moved FROM [node-1, node-2] TO [node-3, node-1]
       // ⚠️ But ClusterKit doesn't copy the data!
   }
   ```

### ❌ What ClusterKit DOES NOT Handle

1. **Data Synchronization** - ClusterKit doesn't copy data between nodes
2. **Conflict Resolution** - No handling of conflicting updates
3. **Version Vectors** - No tracking of data versions
4. **Anti-Entropy** - No background sync to fix inconsistencies
5. **Read Repair** - No fixing stale data on reads

## Why This Happens

ClusterKit is a **coordination library**, not a **data replication library**:

```
┌─────────────────────────────────────────────────────────────┐
│  ClusterKit's Responsibility (✅ Implemented)               │
├─────────────────────────────────────────────────────────────┤
│  - Track which nodes are alive                              │
│  - Decide which partitions belong to which nodes            │
│  - Notify applications when partitions move                 │
│  - Maintain consensus on cluster state                      │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  Application's Responsibility (❌ Not Implemented)          │
├─────────────────────────────────────────────────────────────┤
│  - Actually copy data between nodes                         │
│  - Handle conflicts (last-write-wins, CRDTs, etc.)         │
│  - Implement read repair                                    │
│  - Background anti-entropy sync                             │
└─────────────────────────────────────────────────────────────┘
```

## Solutions

### Solution 1: Application-Level Sync (Current Approach)

**Your application must handle data sync in the `OnPartitionChange` hook:**

```go
// In your KV store
ck.OnPartitionChange(func(partitionID string, copyFrom, copyTo []Node) {
    if isLocalNode(copyTo) {
        // This node is receiving the partition
        for _, sourceNode := range copyFrom {
            // Fetch ALL data for this partition from source
            data := fetchPartitionData(sourceNode, partitionID)
            
            // Merge with local data
            for key, value := range data {
                localData[key] = value  // ⚠️ Overwrites local!
            }
        }
    }
})
```

**Problem:** This only works when partitions are reassigned, not for general sync!

### Solution 2: Full Data Sync on Rejoin (Recommended)

**Implement a complete sync when a node rejoins:**

```go
// When Node-3 rejoins
func (kv *KVStore) handleRejoin() {
    // 1. Get list of all partitions this node should have
    partitions := kv.ck.GetPartitionsForNode(kv.nodeID)
    
    // 2. For each partition, sync from replicas
    for _, partition := range partitions {
        // Get replica nodes for this partition
        replicas := kv.ck.GetReplicasForPartition(partition.ID)
        
        // Fetch latest data from a healthy replica
        for _, replica := range replicas {
            if replica.ID != kv.nodeID {
                data, version := fetchPartitionWithVersion(replica, partition.ID)
                
                // Merge with local data (keep newer version)
                kv.mergePartitionData(partition.ID, data, version)
                break
            }
        }
    }
}
```

### Solution 3: Version Vectors (Production-Grade)

**Track data versions to handle conflicts:**

```go
type VersionedValue struct {
    Value   string
    Version VectorClock  // {node-1: 5, node-2: 3, node-3: 2}
    Updated time.Time
}

// When Node-3 rejoins with stale data
func (kv *KVStore) syncWithVersions() {
    for key, localValue := range kv.data {
        // Get latest version from replicas
        replicaValue := fetchFromReplica(key)
        
        // Compare versions
        if replicaValue.Version.IsNewerThan(localValue.Version) {
            // Replica has newer data
            kv.data[key] = replicaValue
        } else if localValue.Version.IsNewerThan(replicaValue.Version) {
            // Local has newer data (shouldn't happen after offline)
            // Push to replicas
            pushToReplicas(key, localValue)
        } else {
            // Conflict! Need resolution strategy
            resolved := resolveConflict(localValue, replicaValue)
            kv.data[key] = resolved
        }
    }
}
```

### Solution 4: Read Repair (Lazy Sync)

**Fix stale data when reads happen:**

```go
func (kv *KVStore) Get(key string) (string, error) {
    // 1. Read from local storage
    localValue, localVersion := kv.getLocal(key)
    
    // 2. Read from replicas
    replicaValues := kv.readFromReplicas(key)
    
    // 3. Compare versions
    newestValue := localValue
    newestVersion := localVersion
    
    for _, rv := range replicaValues {
        if rv.Version > newestVersion {
            newestValue = rv.Value
            newestVersion = rv.Version
        }
    }
    
    // 4. If local is stale, update it (read repair)
    if newestVersion > localVersion {
        kv.setLocal(key, newestValue, newestVersion)
    }
    
    return newestValue, nil
}
```

### Solution 5: Anti-Entropy (Background Sync)

**Periodically sync all data in background:**

```go
func (kv *KVStore) startAntiEntropy() {
    ticker := time.NewTicker(1 * time.Minute)
    
    go func() {
        for range ticker.C {
            // Get all partitions this node owns
            partitions := kv.ck.GetPartitionsForNode(kv.nodeID)
            
            for _, partition := range partitions {
                // Compare with replicas
                replicas := kv.ck.GetReplicasForPartition(partition.ID)
                
                for _, replica := range replicas {
                    if replica.ID != kv.nodeID {
                        // Exchange Merkle tree hashes
                        localHash := kv.getPartitionHash(partition.ID)
                        replicaHash := fetchPartitionHash(replica, partition.ID)
                        
                        if localHash != replicaHash {
                            // Sync the differences
                            kv.syncPartition(replica, partition.ID)
                        }
                    }
                }
            }
        }
    }()
}
```

## What Happens in Your Current Example

Let's trace through your sync example:

```go
// In example/sync/server.go
func (kv *KVStore) handlePartitionChange(partitionID string, copyFrom, copyTo []Node) {
    // This is called when partitions are reassigned
    
    // 1. Fetch data from old nodes
    for _, sourceNode := range copyFromNodes {
        url := fmt.Sprintf("http://%s/kv/migrate?partition=%s", sourceNode.IP, partitionID)
        // ... fetch data ...
    }
    
    // 2. Merge into local storage
    for key, value := range mergedData {
        kv.data[key] = value  // ⚠️ Simple overwrite!
    }
}
```

**Problem:** This only runs when partitions are reassigned, NOT on every rejoin!

## Test Demonstrating the Problem

Let me create a test that shows the stale data issue:

```bash
#!/bin/bash
# test_stale_data.sh

# 1. Start 3 nodes
# 2. Write 100 keys (keys 1-100)
# 3. Kill node-3
# 4. Wait for removal
# 5. Write 50 MORE keys (keys 101-150) ← Node-3 doesn't have these!
# 6. Rejoin node-3
# 7. Try to read key 125 from node-3 ← WILL FAIL!
```

## Recommended Implementation

For your ClusterKit library, I recommend adding a **sync helper** that applications can use:

```go
// In clusterkit.go
type SyncManager struct {
    ck *ClusterKit
}

// SyncPartition helps applications sync data for a partition
func (sm *SyncManager) SyncPartition(partitionID string, fetchFunc FetchDataFunc) error {
    // 1. Get replicas for this partition
    partition := sm.ck.GetPartition(partitionID)
    
    // 2. Determine which replica has the freshest data
    // 3. Call application's fetch function
    // 4. Return data to application for merging
    
    return nil
}

// Application uses it like:
ck.SyncManager().SyncPartition("partition-5", func(sourceNode Node) (map[string]string, error) {
    // Application-specific logic to fetch data
    return fetchFromNode(sourceNode)
})
```

## Summary

| Scenario | ClusterKit Handles | Application Must Handle |
|----------|-------------------|------------------------|
| Node joins | ✅ Add to cluster | ✅ Fetch initial data |
| Node dies | ✅ Remove from cluster | ❌ Nothing |
| Node rejoins | ✅ Update node info | ❌ **Sync stale data!** |
| Partition moves | ✅ Notify via hooks | ✅ Copy data |
| Conflicting writes | ❌ Not tracked | ❌ **Need to implement!** |

**Bottom Line:** ClusterKit tells you WHERE data should be, but YOU must implement HOW to sync it when nodes rejoin!
