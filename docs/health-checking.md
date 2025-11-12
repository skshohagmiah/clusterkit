# Health Checking in ClusterKit

Complete guide to ClusterKit's health checking and failure detection system.

---

## Overview

ClusterKit includes automatic health checking to detect and remove failed nodes from the cluster. This ensures your distributed system remains operational even when nodes crash or become unreachable.

---

## How It Works

### Basic Flow

```
Every 5 seconds:
    For each node in cluster:
        1. Send HTTP GET to /health
        2. Wait up to 2 seconds for response
        3. If success:
            - Reset failure count
            - Update last seen time
        4. If failure (timeout/error):
            - Increment failure count
            - If count >= 3:
                - Remove node from cluster
                - Trigger rebalancing
                - Fire OnNodeLeave hook
```

### Timeline Example

```
T0:  Node-3 crashes
T5:  Health check fails (1/3)
T10: Health check fails (2/3)
T15: Health check fails (3/3) → Remove node
T16: OnNodeLeave hook fires
T17: Rebalancing starts
T20: Partitions redistributed
T25: Cluster stable with 2 nodes
```

---

## Configuration

### Default Configuration

```go
ck, _ := clusterkit.New(clusterkit.Options{
    NodeID:   "node-1",
    HTTPAddr: ":8080",
    // Health checking enabled by default
})
```

### Custom Configuration

```go
ck, _ := clusterkit.New(clusterkit.Options{
    NodeID:   "node-1",
    HTTPAddr: ":8080",
    HealthCheck: clusterkit.HealthCheckConfig{
        Enabled:          true,
        Interval:         5 * time.Second,  // Check every 5s
        Timeout:          2 * time.Second,  // Request timeout
        FailureThreshold: 3,                // Remove after 3 failures
    },
})
```

### Disable Health Checking

```go
ck, _ := clusterkit.New(clusterkit.Options{
    NodeID:   "node-1",
    HTTPAddr: ":8080",
    HealthCheck: clusterkit.HealthCheckConfig{
        Enabled: false,  // Disable health checking
    },
})
```

---

## Configuration Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| **Enabled** | `true` | Enable/disable health checking |
| **Interval** | `5s` | Time between health checks |
| **Timeout** | `2s` | HTTP request timeout |
| **FailureThreshold** | `3` | Failures before removal |

### Calculating Detection Time

```
Detection Time = Interval × FailureThreshold

Default: 5s × 3 = 15 seconds
Fast:    2s × 2 = 4 seconds
Slow:    10s × 5 = 50 seconds
```

---

## Health Endpoint

ClusterKit automatically exposes a `/health` endpoint:

```bash
# Check node health
curl http://localhost:8080/health

# Response
{
  "status": "healthy",
  "node_id": "node-1",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Custom Health Checks

You can add custom health logic:

```go
// Override default health endpoint
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    // Check database connection
    if !db.Ping() {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    
    // Check disk space
    if getDiskUsage() > 90 {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    
    // All checks passed
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{
        "status": "healthy",
    })
})
```

---

## Failure Scenarios

### 1. Node Crash

```
Scenario: Node-3 process crashes

Timeline:
T0:  Node-3 crashes
T5:  Health check fails (connection refused)
T10: Health check fails
T15: Health check fails → Remove node
T16: OnNodeLeave(reason: "health_check_failure")
T17: Rebalancing starts
T25: Cluster stable

Result: Node removed, partitions redistributed ✅
```

### 2. Network Partition

```
Scenario: Network split between nodes

Timeline:
T0:  Network partition occurs
     Group A: [node-1, node-2]
     Group B: [node-3]

T5:  Group A can't reach node-3 (timeout)
T10: Group A can't reach node-3
T15: Group A removes node-3
     Group B can't reach node-1, node-2
     Group B removes node-1, node-2

Result: Split-brain scenario ⚠️
```

**Mitigation:** Use Raft quorum - only majority partition can make changes.

### 3. Slow Response

```
Scenario: Node-3 is slow but not dead

Timeline:
T0:  Node-3 response time: 3s (timeout: 2s)
T5:  Health check fails (timeout)
T10: Health check fails
T15: Health check fails → Remove node

Result: Node removed even though it's alive ⚠️
```

**Mitigation:** Increase timeout or threshold.

### 4. Graceful Shutdown

```
Scenario: Node-3 shuts down gracefully

Timeline:
T0:  Node-3 calls ck.Stop()
T1:  HTTP server stops
T2:  Raft membership updated
T3:  OnNodeLeave(reason: "graceful_shutdown")
T4:  Rebalancing starts

Result: Clean removal, no false failures ✅
```

---

## Tuning Health Checks

### Fast Detection (4 seconds)

```go
HealthCheck: clusterkit.HealthCheckConfig{
    Enabled:          true,
    Interval:         2 * time.Second,
    Timeout:          1 * time.Second,
    FailureThreshold: 2,
}
```

**Use case:** Critical systems requiring fast failover  
**Trade-off:** More false positives on slow networks

### Slow Detection (50 seconds)

```go
HealthCheck: clusterkit.HealthCheckConfig{
    Enabled:          true,
    Interval:         10 * time.Second,
    Timeout:          5 * time.Second,
    FailureThreshold: 5,
}
```

**Use case:** Stable networks, avoid false positives  
**Trade-off:** Slower failure detection

### Balanced (15 seconds) - Default

```go
HealthCheck: clusterkit.HealthCheckConfig{
    Enabled:          true,
    Interval:         5 * time.Second,
    Timeout:          2 * time.Second,
    FailureThreshold: 3,
}
```

**Use case:** Most production deployments  
**Trade-off:** Good balance

---

## Monitoring Health

### Get Health Status

```go
health := ck.HealthCheck()

fmt.Printf("Status: %s\n", health.Status)
fmt.Printf("Healthy Nodes: %d/%d\n", health.HealthyNodes, health.TotalNodes)
fmt.Printf("Unhealthy Nodes: %v\n", health.UnhealthyNodeIDs)
```

### HTTP Endpoint

```bash
curl http://localhost:8080/health/detailed

# Response
{
  "status": "healthy",
  "healthy_nodes": 3,
  "total_nodes": 3,
  "unhealthy_nodes": [],
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Hook Integration

```go
ck.OnClusterHealthChange(func(event *clusterkit.ClusterHealthEvent) {
    log.Printf("Cluster health: %s", event.Status)
    log.Printf("Healthy: %d/%d nodes", event.HealthyNodes, event.TotalNodes)
    
    if event.Status == "critical" {
        // Alert operations team
        alertOps("Cluster in critical state!")
        
        // Enable read-only mode
        enableReadOnlyMode()
    }
})
```

---

## Best Practices

### 1. Set Appropriate Timeouts

```go
// ❌ Too aggressive - false positives
Interval:         1 * time.Second,
Timeout:          500 * time.Millisecond,
FailureThreshold: 2,

// ✅ Balanced
Interval:         5 * time.Second,
Timeout:          2 * time.Second,
FailureThreshold: 3,

// ✅ Conservative - avoid false positives
Interval:         10 * time.Second,
Timeout:          5 * time.Second,
FailureThreshold: 5,
```

### 2. Implement Custom Health Checks

```go
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    // Check all critical dependencies
    checks := []HealthCheck{
        checkDatabase(),
        checkDiskSpace(),
        checkMemory(),
        checkCPU(),
    }
    
    for _, check := range checks {
        if !check.Healthy {
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(check)
            return
        }
    }
    
    w.WriteHeader(http.StatusOK)
})
```

### 3. Monitor Health Metrics

```go
// Track health check failures
var healthCheckFailures = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "clusterkit_health_check_failures_total",
        Help: "Total health check failures",
    },
    []string{"node_id"},
)

ck.OnNodeLeave(func(event *clusterkit.NodeLeaveEvent) {
    if event.Reason == "health_check_failure" {
        healthCheckFailures.WithLabelValues(event.Node.ID).Inc()
    }
})
```

### 4. Handle Split-Brain

```go
// Only allow operations if we have quorum
func (ck *ClusterKit) HasQuorum() bool {
    cluster := ck.GetCluster()
    healthyNodes := countHealthyNodes(cluster)
    return healthyNodes > (len(cluster.Nodes) / 2)
}

// In your application
if !ck.HasQuorum() {
    return errors.New("no quorum - cluster partitioned")
}
```

### 5. Graceful Shutdown

```go
func main() {
    ck, _ := clusterkit.New(options)
    ck.Start()
    
    // Handle shutdown signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    <-sigChan
    log.Println("Shutting down gracefully...")
    
    // Stop ClusterKit cleanly
    ck.Stop()  // Triggers graceful removal
}
```

---

## Troubleshooting

### False Positives

**Symptom:** Healthy nodes being removed

**Causes:**
- Network latency spikes
- GC pauses
- CPU overload
- Timeout too aggressive

**Solutions:**
```go
// Increase timeout
Timeout: 5 * time.Second,

// Increase threshold
FailureThreshold: 5,

// Increase interval
Interval: 10 * time.Second,
```

### Slow Detection

**Symptom:** Failed nodes not removed quickly

**Causes:**
- Interval too long
- Threshold too high

**Solutions:**
```go
// Decrease interval
Interval: 2 * time.Second,

// Decrease threshold
FailureThreshold: 2,
```

### Split-Brain

**Symptom:** Multiple nodes think they're the leader

**Causes:**
- Network partition
- Raft quorum lost

**Solutions:**
- Use odd number of nodes (3, 5, 7)
- Ensure Raft quorum (majority)
- Monitor Raft leadership

---

## Health Check Flow Diagram

```
┌─────────────────────────────────────────────────────────┐
│                  Health Checker                          │
│                                                          │
│  Every 5 seconds:                                        │
│  ┌────────────────────────────────────────────────┐    │
│  │ For each node in cluster:                      │    │
│  │                                                 │    │
│  │  1. HTTP GET /health (timeout: 2s)             │    │
│  │     ↓                                           │    │
│  │  2. Response OK?                                │    │
│  │     ├─ YES → Reset failure count                │    │
│  │     │         Update last seen                  │    │
│  │     │                                           │    │
│  │     └─ NO → Increment failure count            │    │
│  │              ↓                                   │    │
│  │           3. Failures >= 3?                     │    │
│  │              ├─ YES → Remove node               │    │
│  │              │         Fire OnNodeLeave         │    │
│  │              │         Trigger rebalancing      │    │
│  │              │                                   │    │
│  │              └─ NO → Continue monitoring        │    │
│  └────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

---

## Examples

See health checking in action:

- **[SYNC Example](../example/sync/run.sh)** - 10-node cluster with health checking
- **[Test Failure Script](../example/sync/test_failure.sh)** - Simulate node failures

---

## Further Reading

- [Architecture](./architecture.md) - How health checking fits in
- [Node Rejoin](./node-rejoin.md) - What happens when nodes return
- [Raft Consensus](https://raft.github.io/) - Understanding Raft quorum
