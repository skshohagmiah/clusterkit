# ClusterKit Hooks Guide

Complete reference for ClusterKit's event hook system.

---

## Overview

ClusterKit provides 7 lifecycle hooks that notify your application of cluster events:

1. **OnPartitionChange** - Partition assignments change
2. **OnNodeJoin** - New node joins cluster
3. **OnNodeRejoin** - Node returns after being offline
4. **OnNodeLeave** - Node leaves or fails
5. **OnRebalanceStart** - Rebalancing begins
6. **OnRebalanceComplete** - Rebalancing finishes
7. **OnClusterHealthChange** - Health status changes

---

## Hook Reference

### 1. OnPartitionChange

**Triggered when:** Partitions are reassigned during rebalancing

**Event Structure:**
```go
type PartitionChangeEvent struct {
    PartitionID   string    // e.g., "partition-5"
    CopyFromNodes []*Node   // Nodes that have the data
    CopyToNode    *Node     // Node that needs the data
    ChangeReason  string    // "node_join", "node_leave", "rebalance"
    OldPrimary    string    // Previous primary node ID
    NewPrimary    string    // New primary node ID
    Timestamp     time.Time // When the change occurred
}
```

**Example:**
```go
ck.OnPartitionChange(func(event *clusterkit.PartitionChangeEvent) {
    if event.CopyToNode.ID != myNodeID {
        return // Not for me
    }
    
    log.Printf("Migrating partition %s (reason: %s)", 
        event.PartitionID, event.ChangeReason)
    
    // Fetch data from source nodes
    for _, source := range event.CopyFromNodes {
        data := fetchPartitionData(source, event.PartitionID)
        mergeData(data)
    }
})
```

**See:** [Complete migration example in README](../README.md#1-onpartitionchange---partition-assignment-changes)

---

### 2. OnNodeJoin

**Triggered when:** A brand new node joins the cluster

**Event Structure:**
```go
type NodeJoinEvent struct {
    Node        *Node     // The joining node
    ClusterSize int       // Total nodes after join
    IsBootstrap bool      // Is this the first node?
    Timestamp   time.Time // When the node joined
}
```

**Example:**
```go
ck.OnNodeJoin(func(event *clusterkit.NodeJoinEvent) {
    log.Printf("Node %s joined (cluster size: %d)", 
        event.Node.ID, event.ClusterSize)
    
    if event.IsBootstrap {
        initializeClusterResources()
    }
    
    updateMonitoring(event.ClusterSize)
})
```

---

### 3. OnNodeRejoin

**Triggered when:** A node that was previously in the cluster returns

**Event Structure:**
```go
type NodeRejoinEvent struct {
    Node                  *Node         // The rejoining node
    OfflineDuration       time.Duration // How long it was offline
    LastSeenAt            time.Time     // When it was last seen
    PartitionsBeforeLeave []string      // Partitions it had before
    Timestamp             time.Time     // When it rejoined
}
```

**Example:**
```go
ck.OnNodeRejoin(func(event *clusterkit.NodeRejoinEvent) {
    if event.Node.ID == myNodeID {
        log.Printf("I'm rejoining after %v offline", event.OfflineDuration)
        
        // Clear stale data
        clearAllLocalData()
        
        // Wait for OnPartitionChange to sync fresh data
    }
})
```

**Important:** This fires BEFORE rebalancing. Use it to clear stale data, then let `OnPartitionChange` handle the actual sync.

---

### 4. OnNodeLeave

**Triggered when:** A node is removed from the cluster

**Event Structure:**
```go
type NodeLeaveEvent struct {
    Node              *Node     // Full node info
    Reason            string    // "health_check_failure", "graceful_shutdown"
    PartitionsOwned   []string  // Partitions it was primary for
    PartitionsReplica []string  // Partitions it was replica for
    Timestamp         time.Time // When it left
}
```

**Example:**
```go
ck.OnNodeLeave(func(event *clusterkit.NodeLeaveEvent) {
    log.Printf("Node %s left (reason: %s)", event.Node.ID, event.Reason)
    
    // Clean up connections
    closeConnectionTo(event.Node.IP)
    
    // Alert if critical
    if event.Reason == "health_check_failure" {
        alertOps("Node failed: " + event.Node.ID)
    }
})
```

---

### 5. OnRebalanceStart

**Triggered when:** Partition rebalancing begins

**Event Structure:**
```go
type RebalanceEvent struct {
    Trigger          string    // "node_join", "node_leave", "manual"
    TriggerNodeID    string    // Which node caused it
    PartitionsToMove int       // How many partitions will move
    NodesAffected    []string  // Which nodes are affected
    Timestamp        time.Time // When rebalance started
}
```

**Example:**
```go
ck.OnRebalanceStart(func(event *clusterkit.RebalanceEvent) {
    log.Printf("Rebalance starting (trigger: %s, partitions: %d)", 
        event.Trigger, event.PartitionsToMove)
    
    // Pause background jobs
    pauseBackgroundJobs()
    
    // Increase timeouts
    increaseOperationTimeouts()
})
```

---

### 6. OnRebalanceComplete

**Triggered when:** Partition rebalancing finishes

**Parameters:**
```go
func(event *RebalanceEvent, duration time.Duration)
```

**Example:**
```go
ck.OnRebalanceComplete(func(event *clusterkit.RebalanceEvent, duration time.Duration) {
    log.Printf("Rebalance completed in %v", duration)
    
    // Resume background jobs
    resumeBackgroundJobs()
    
    // Reset timeouts
    resetOperationTimeouts()
    
    // Record metrics
    recordRebalanceDuration(duration)
})
```

---

### 7. OnClusterHealthChange

**Triggered when:** Overall cluster health status changes

**Event Structure:**
```go
type ClusterHealthEvent struct {
    HealthyNodes     int       // Number of healthy nodes
    UnhealthyNodes   int       // Number of unhealthy nodes
    TotalNodes       int       // Total nodes in cluster
    Status           string    // "healthy", "degraded", "critical"
    UnhealthyNodeIDs []string  // IDs of unhealthy nodes
    Timestamp        time.Time // When health changed
}
```

**Example:**
```go
ck.OnClusterHealthChange(func(event *clusterkit.ClusterHealthEvent) {
    log.Printf("Cluster health: %s (%d/%d healthy)", 
        event.Status, event.HealthyNodes, event.TotalNodes)
    
    if event.Status == "critical" {
        alertOps("Cluster in critical state!")
        enableReadOnlyMode()
    }
})
```

---

## Hook Execution

### Async Execution

All hooks run in **background goroutines**:

```go
// Hooks don't block your application
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    // This runs in a goroutine
    time.Sleep(10 * time.Second)  // Won't block anything
})
```

### Concurrency Limit

Maximum **50 concurrent hook executions**:

```go
// If 50 hooks are running, new ones wait
workerPool := make(chan struct{}, 50)
```

### Panic Recovery

Hooks are isolated - panics won't crash your app:

```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    panic("oops!")  // Recovered automatically
})
// Your application continues running ✅
```

---

## Best Practices

### 1. Check Node ID

```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    // Always check if event is for you
    if event.CopyToNode.ID != myNodeID {
        return
    }
    
    // Handle event
})
```

### 2. Handle Errors Gracefully

```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    if err := migratePartition(event); err != nil {
        log.Printf("Migration failed: %v", err)
        // Don't panic - log and continue
    }
})
```

### 3. Use Timeouts

```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    migratePartitionWithContext(ctx, event)
})
```

### 4. Avoid Blocking Operations

```go
// ❌ Bad - blocks hook execution
ck.OnNodeJoin(func(event *NodeJoinEvent) {
    time.Sleep(1 * time.Hour)  // Blocks worker pool slot
})

// ✅ Good - spawn separate goroutine for long operations
ck.OnNodeJoin(func(event *NodeJoinEvent) {
    go func() {
        time.Sleep(1 * time.Hour)  // Doesn't block
    }()
})
```

### 5. Idempotent Operations

```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    // Make operations idempotent
    // (safe to run multiple times)
    
    if alreadyMigrated(event.PartitionID) {
        return
    }
    
    migratePartition(event)
})
```

---

## Common Patterns

### Pattern 1: Rejoin with Data Sync

```go
type KVStore struct {
    isRejoining bool
    rejoinMu    sync.Mutex
}

// Step 1: Clear stale data on rejoin
ck.OnNodeRejoin(func(event *NodeRejoinEvent) {
    if event.Node.ID == myNodeID {
        kv.rejoinMu.Lock()
        kv.isRejoining = true
        kv.rejoinMu.Unlock()
        
        // Clear stale data
        kv.data = make(map[string]string)
    }
})

// Step 2: Sync fresh data after rebalance
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    if event.CopyToNode.ID != myNodeID {
        return
    }
    
    // Sync partition data
    syncPartition(event)
    
    // Clear rejoin flag after first partition
    kv.rejoinMu.Lock()
    if kv.isRejoining {
        kv.isRejoining = false
        log.Println("Rejoin complete")
    }
    kv.rejoinMu.Unlock()
})
```

### Pattern 2: Metrics Collection

```go
type Metrics struct {
    rebalanceCount    int
    rebalanceDuration time.Duration
    mu                sync.Mutex
}

ck.OnRebalanceStart(func(event *RebalanceEvent) {
    metrics.mu.Lock()
    metrics.rebalanceCount++
    metrics.mu.Unlock()
})

ck.OnRebalanceComplete(func(event *RebalanceEvent, duration time.Duration) {
    metrics.mu.Lock()
    metrics.rebalanceDuration += duration
    metrics.mu.Unlock()
    
    // Export to monitoring system
    prometheus.RecordRebalance(duration)
})
```

### Pattern 3: Circuit Breaker

```go
type CircuitBreaker struct {
    failures int
    open     bool
}

ck.OnClusterHealthChange(func(event *ClusterHealthEvent) {
    if event.Status == "critical" {
        // Open circuit breaker
        cb.open = true
        log.Println("Circuit breaker OPEN - rejecting requests")
    } else if event.Status == "healthy" {
        // Close circuit breaker
        cb.open = false
        cb.failures = 0
        log.Println("Circuit breaker CLOSED - accepting requests")
    }
})
```

---

## Troubleshooting

### Hook Not Firing

**Check:**
1. Is hook registered before `ck.Start()`?
2. Is event actually occurring?
3. Check logs for panic recovery messages

```go
// ❌ Wrong - registered after Start()
ck.Start()
ck.OnPartitionChange(hook)  // Too late!

// ✅ Correct - register before Start()
ck.OnPartitionChange(hook)
ck.Start()
```

### Hook Panicking

**Debug:**
```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Hook panic: %v\n%s", r, debug.Stack())
        }
    }()
    
    // Your code
})
```

### Slow Hook Execution

**Profile:**
```go
ck.OnPartitionChange(func(event *PartitionChangeEvent) {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        if duration > 5*time.Second {
            log.Printf("Slow hook: %v", duration)
        }
    }()
    
    // Your code
})
```

---

## Examples

See complete working examples:

- **[SYNC Mode](../example/sync/server.go)** - Full hook usage
- **[Node Rejoin](../example/sync/server.go#L40-L68)** - Rejoin handling
- **[Partition Migration](../example/sync/server.go#L286-L361)** - Data migration

---

## Further Reading

- [Architecture](./architecture.md) - How hooks fit into the system
- [Node Rejoin](./node-rejoin.md) - Detailed rejoin handling
- [README Hooks Section](../README.md#-event-hooks-system) - Quick reference
