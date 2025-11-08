# ClusterKit Rebalancing Behavior

## Current Implementation Status

### ✅ Node JOIN - Fully Automated
```
1. New node sends /join request to leader
2. Leader adds node to Raft cluster
3. Leader proposes "add_node" action through Raft
4. All nodes update their cluster state
5. Leader triggers RebalancePartitions()
6. Partitions redistributed with consistent hashing
7. OnPartitionChange() hooks fire
8. Application receives migration events
9. Application copies data from old to new nodes
```

**Result:** New node automatically gets its share of partitions and data.

### ✅ Node LEAVE/FAILURE - NOW AUTOMATED!

```
1. Node dies/crashes
2. ✅ Health checker detects failure (checks every 5s)
3. ✅ After 3 failed checks (~15s), leader removes node
4. ✅ Node removed from Raft cluster
5. ✅ Partitions automatically rebalanced
6. ✅ Data migration hooks fire
7. ✅ Cluster continues operating
```

**Result:** Cluster automatically recovers from node failures!

## What Happens in Each Scenario

### Scenario 1: Node Joins
```bash
# Start 3 nodes
./run.sh 3 16 3

# Cluster state:
# - 3 nodes
# - 16 partitions
# - Each partition has 3 replicas (primary + 2 replicas)
# - Partitions evenly distributed

# Add 4th node (in another terminal)
NODE_ID=node-4 HTTP_PORT=8083 KV_PORT=9083 \
  JOIN_ADDR=localhost:8080 DATA_DIR=/tmp/ck-node4 \
  go run server.go

# What happens:
# 1. node-4 joins cluster ✅
# 2. Automatic rebalancing triggers ✅
# 3. ~25% of partitions move to node-4 ✅
# 4. Data migration hooks fire ✅
# 5. KV store copies data to node-4 ✅
```

### Scenario 2: Node Dies (Current Behavior)
```bash
# Kill node-3
kill -9 <node-3-pid>

# What happens:
# 1. Node-3 stops responding ❌
# 2. ClusterKit doesn't detect it ❌
# 3. Node-3 still in cluster.Nodes[] ❌
# 4. Partitions still assigned to node-3 ❌
# 5. Client requests to node-3 partitions TIMEOUT ❌
# 6. NO automatic failover ❌
# 7. NO rebalancing ❌

# Manual fix required:
curl -X POST http://localhost:8080/remove-node -d '{"node_id":"node-3"}'
# ⚠️ This endpoint doesn't exist yet!
```

### Scenario 3: Dead Node Recovers (Current Behavior)
```bash
# Restart node-3 with same ID
NODE_ID=node-3 HTTP_PORT=8082 KV_PORT=9082 \
  JOIN_ADDR=localhost:8080 DATA_DIR=/tmp/ck-node3 \
  go run server.go

# What happens:
# 1. Node-3 tries to join ⚠️
# 2. Raft sees it already exists ⚠️
# 3. Behavior is UNDEFINED ❌
# 4. Might cause duplicate nodes ❌
# 5. Might cause Raft errors ❌

# Expected behavior (not implemented):
# 1. Detect it's a rejoin (same node ID)
# 2. Update node IP/address
# 3. Sync Raft state
# 4. Resume normal operation
```

## Code Analysis

### What's Implemented

**RebalancePartitions() in partition.go:**
```go
func (ck *ClusterKit) RebalancePartitions() error {
    // Gets current nodes
    nodes := ck.cluster.Nodes
    
    // Redistributes partitions using consistent hashing
    for _, partition := range partitions {
        newPrimary := consistentHash(partition.ID, nodes)
        newReplicas := getNextN(newPrimary, replicationFactor-1)
        
        // Detects changes
        if primaryChanged || replicasChanged {
            // Fires OnPartitionChange hooks
            ck.hookManager.notifyPartitionChange(...)
        }
    }
}
```

**OnPartitionChange() Hook:**
```go
// Application registers callback
ck.OnPartitionChange(func(partitionID, copyFrom, copyTo) {
    // Application handles data migration
    // Fetches data from old nodes
    // Copies to new node
})
```

### What's Missing

**1. Health Checking:**
```go
// ❌ NOT IMPLEMENTED
type HealthChecker struct {
    interval time.Duration
    timeout  time.Duration
}

func (hc *HealthChecker) MonitorNodes() {
    for {
        for _, node := range cluster.Nodes {
            if !isHealthy(node) {
                // Mark as unhealthy
                // Trigger removal after N failures
            }
        }
        time.Sleep(interval)
    }
}
```

**2. Automatic Node Removal:**
```go
// ❌ NOT IMPLEMENTED
func (ck *ClusterKit) RemoveNode(nodeID string) error {
    // 1. Remove from Raft cluster
    // 2. Propose "remove_node" through Raft
    // 3. Trigger rebalancing
    // 4. Migrate partitions away
    return nil
}
```

**3. Graceful Shutdown:**
```go
// ❌ NOT IMPLEMENTED
func (ck *ClusterKit) Leave() error {
    // 1. Mark self as leaving
    // 2. Trigger rebalancing
    // 3. Wait for data migration
    // 4. Remove from cluster
    // 5. Shutdown Raft
    return nil
}
```

**4. Rejoin Handling:**
```go
// ❌ NOT IMPLEMENTED
func (ck *ClusterKit) handleRejoin(nodeID string) error {
    // 1. Detect it's a rejoin (node ID exists)
    // 2. Update node address
    // 3. Sync Raft log
    // 4. Resume normal operation
    return nil
}
```

## Production Requirements

### Critical Missing Features

1. **Failure Detection**
   - Heartbeat mechanism
   - Configurable timeout
   - Gossip protocol (like Consul)

2. **Automatic Failover**
   - Promote replica to primary
   - Update partition map
   - Notify applications

3. **Graceful Shutdown**
   - Drain connections
   - Migrate data
   - Remove from cluster

4. **Split-Brain Prevention**
   - Quorum-based decisions
   - Fencing mechanisms
   - Network partition handling

5. **Node Recovery**
   - Detect rejoin vs new node
   - Sync state from cluster
   - Resume operations

## Workarounds (Current)

### Manual Node Removal
```bash
# 1. Identify dead node
curl http://localhost:8080/cluster | jq '.cluster.nodes'

# 2. Manually remove from Raft (requires code change)
# Add this endpoint to sync.go:
func (ck *ClusterKit) handleRemoveNode(w http.ResponseWriter, r *http.Request) {
    var req struct {
        NodeID string `json:"node_id"`
    }
    json.NewDecoder(r.Body).Decode(&req)
    
    // Remove from Raft
    cm.raft.RemoveServer(raft.ServerID(req.NodeID), 0, 0)
    
    // Propose removal
    cm.ProposeAction("remove_node", map[string]interface{}{
        "id": req.NodeID,
    })
    
    // Trigger rebalancing
    ck.RebalancePartitions()
}

# 3. Call the endpoint
curl -X POST http://localhost:8080/remove-node \
  -d '{"node_id":"node-3"}'
```

## Comparison with Production Systems

### Consul
- ✅ Automatic failure detection (gossip)
- ✅ Automatic failover
- ✅ Graceful leave
- ✅ Automatic rejoin
- ✅ Split-brain prevention

### etcd
- ✅ Automatic failure detection (heartbeat)
- ✅ Automatic member removal (after timeout)
- ✅ Graceful shutdown
- ✅ Member recovery
- ✅ Learner nodes for safe rejoin

### ClusterKit
- ✅ Manual join
- ❌ No failure detection
- ❌ No automatic removal
- ❌ No graceful leave
- ❌ No rejoin handling

## Conclusion

**ClusterKit handles:**
- ✅ Node addition with automatic rebalancing
- ✅ Data migration hooks for applications

**ClusterKit does NOT handle:**
- ❌ Node failure detection
- ❌ Automatic node removal
- ❌ Failover and replica promotion
- ❌ Graceful shutdown
- ❌ Node recovery/rejoin

**For production use, you MUST implement:**
1. Health checking and failure detection
2. Automatic node removal after N failed checks
3. Graceful shutdown with data migration
4. Rejoin detection and state sync
5. Split-brain prevention
