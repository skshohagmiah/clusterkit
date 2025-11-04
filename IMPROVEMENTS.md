# ClusterKit - Issues & Improvements

## 🚨 Critical Issues

### 1. No Test Coverage
**Priority:** HIGH  
**Status:** Missing

**Problem:**
- Zero unit tests
- Zero integration tests
- No test coverage

**Solution:**
```bash
# Create test files
touch clusterkit_test.go
touch partitions_test.go
touch helpers_test.go
touch consensus_test.go
```

**Example Test:**
```go
func TestGetNodesForKey(t *testing.T) {
    ck := setupTestCluster(t)
    nodes, err := ck.GetNodesForKey("user:123")
    assert.NoError(t, err)
    assert.NotEmpty(t, nodes)
}
```

---

### 2. Missing Input Validation
**Priority:** HIGH  
**Status:** Bug

**Problem:**
```go
func NewClusterKit(opts Options) (*ClusterKit, error) {
    // ❌ No validation for:
    // - opts.Config (can be nil!)
    // - opts.NodeName
    // - opts.RaftAddr
}
```

**Solution:**
```go
func NewClusterKit(opts Options) (*ClusterKit, error) {
    if opts.NodeID == "" {
        return nil, fmt.Errorf("NodeID is required")
    }
    if opts.NodeName == "" {
        return nil, fmt.Errorf("NodeName is required")
    }
    if opts.HTTPAddr == "" {
        return nil, fmt.Errorf("HTTPAddr is required")
    }
    if opts.RaftAddr == "" {
        return nil, fmt.Errorf("RaftAddr is required")
    }
    if opts.Config == nil {
        return nil, fmt.Errorf("Config is required")
    }
    if opts.Config.ClusterName == "" {
        return nil, fmt.Errorf("ClusterName is required")
    }
    if opts.Config.PartitionCount <= 0 {
        return nil, fmt.Errorf("PartitionCount must be > 0")
    }
    if opts.Config.ReplicationFactor <= 0 {
        return nil, fmt.Errorf("ReplicationFactor must be > 0")
    }
    // ... rest of code
}
```

---

### 3. Cluster ID Confusion
**Priority:** MEDIUM  
**Status:** Design Issue

**Problem:**
```go
cluster := &Cluster{
    ID:   opts.NodeID,  // ❌ Wrong! This is the cluster, not the node
    Name: opts.Config.ClusterName,
}
```

**Solution:**
```go
cluster := &Cluster{
    ID:   opts.Config.ClusterName,  // ✅ Use cluster name as ID
    Name: opts.Config.ClusterName,
    // Or generate a unique cluster ID
}
```

---

### 4. HTTP Server Can't Shutdown Gracefully
**Priority:** MEDIUM  
**Status:** Bug

**Problem:**
```go
func (ck *ClusterKit) startHTTPServer() error {
    server := &http.Server{...}
    go func() {
        server.ListenAndServe()  // ❌ Can't call Shutdown()
    }()
}
```

**Solution:**
```go
type ClusterKit struct {
    httpServer *http.Server  // Add this field
    // ... other fields
}

func (ck *ClusterKit) startHTTPServer() error {
    ck.httpServer = &http.Server{
        Addr:    ck.httpAddr,
        Handler: mux,
    }
    
    go func() {
        if err := ck.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            fmt.Printf("HTTP server error: %v\n", err)
        }
    }()
    
    return nil
}

func (ck *ClusterKit) Stop() error {
    close(ck.stopChan)
    
    // Gracefully shutdown HTTP server
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := ck.httpServer.Shutdown(ctx); err != nil {
        return fmt.Errorf("failed to shutdown HTTP server: %v", err)
    }
    
    // Stop consensus manager
    ck.consensusManager.Stop()
    
    return nil
}
```

---

### 5. Unused Field: walFile
**Priority:** LOW  
**Status:** Cleanup Needed

**Problem:**
```go
type ClusterKit struct {
    walFile string  // ❌ Not used (Raft handles persistence)
}
```

**Solution:**
Remove `walFile` field and related code since Raft handles all persistence.

---

## 💡 Recommended Improvements

### 1. Add Metrics/Monitoring
**Priority:** MEDIUM

```go
type Metrics struct {
    NodeCount       int
    PartitionCount  int
    RequestCount    int64
    ErrorCount      int64
    LastSync        time.Time
}

func (ck *ClusterKit) GetMetrics() *Metrics {
    return &Metrics{
        NodeCount:      len(ck.cluster.Nodes),
        PartitionCount: len(ck.cluster.PartitionMap.Partitions),
        LastSync:       ck.lastSyncTime,
    }
}
```

### 2. Add Context Support
**Priority:** MEDIUM

```go
func (ck *ClusterKit) StartWithContext(ctx context.Context) error {
    // Use context for graceful shutdown
}

func (ck *ClusterKit) GetNodesForKeyWithContext(ctx context.Context, key string) ([]Node, error) {
    // Respect context cancellation
}
```

### 3. Add Logging Interface
**Priority:** MEDIUM

```go
type Logger interface {
    Info(msg string, args ...interface{})
    Error(msg string, args ...interface{})
    Debug(msg string, args ...interface{})
}

type ClusterKit struct {
    logger Logger  // Allow custom logger
}
```

### 4. Add Health Check Details
**Priority:** LOW

```go
type HealthStatus struct {
    Healthy       bool
    NodeID        string
    IsLeader      bool
    NodeCount     int
    PartitionCount int
    RaftState     string
    LastSync      time.Time
}

func (ck *ClusterKit) HealthCheck() *HealthStatus {
    // Return detailed health info
}
```

### 5. Add Partition Rebalancing Strategy
**Priority:** LOW

```go
type RebalanceStrategy interface {
    Rebalance(nodes []Node, partitions []*Partition) error
}

type RoundRobinStrategy struct{}
type ConsistentHashStrategy struct{}
```

### 6. Add Node Metadata
**Priority:** LOW

```go
type Node struct {
    ID       string
    Name     string
    IP       string
    Status   string
    Metadata map[string]string  // ✅ Add custom metadata
    Region   string             // ✅ For multi-region support
    Zone     string             // ✅ For zone-aware placement
}
```

### 7. Add Retry Logic
**Priority:** MEDIUM

```go
func (ck *ClusterKit) joinNodeWithRetry(nodeAddr string, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        if err := ck.joinNode(nodeAddr); err == nil {
            return nil
        }
        time.Sleep(time.Second * time.Duration(i+1))
    }
    return fmt.Errorf("failed to join after %d retries", maxRetries)
}
```

---

## 📊 Performance Improvements

### 1. Connection Pooling
Use HTTP connection pooling for inter-node communication

### 2. Batch Operations
Batch partition assignments instead of one-by-one

### 3. Caching
Cache partition lookups for frequently accessed keys

---

## 🔒 Security Improvements

### 1. TLS Support
```go
type Options struct {
    TLSEnabled bool
    CertFile   string
    KeyFile    string
}
```

### 2. Authentication
```go
type Options struct {
    AuthToken string
}
```

### 3. Rate Limiting
Add rate limiting for HTTP endpoints

---

## 📝 Documentation Improvements

### 1. Add Godoc Examples
```go
// Example usage:
//  ck, _ := clusterkit.NewClusterKit(...)
//  nodes, _ := ck.GetNodesForKey("user:123")
```

### 2. Add Architecture Diagram
Create visual diagrams for:
- Cluster topology
- Partition distribution
- Raft consensus flow

### 3. Add Troubleshooting Guide
Common issues and solutions

---

## ✅ Priority Order

1. **HIGH:** Add input validation
2. **HIGH:** Add tests (unit + integration)
3. **MEDIUM:** Fix HTTP server shutdown
4. **MEDIUM:** Add context support
5. **MEDIUM:** Add retry logic
6. **LOW:** Remove unused walFile
7. **LOW:** Add metrics
8. **LOW:** Add logging interface
