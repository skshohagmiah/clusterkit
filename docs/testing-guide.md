# Testing Data Partitioning and Replication

## Quick Test

Run the integration test to verify everything works:

```bash
cd /home/shohagmiah/Personal/clusterkit
./test-integration.sh
```

Or run directly with Go:

```bash
go test -v -run TestMultiNodeReplication -timeout 60s
```

---

## What Gets Tested

### 1. Cluster Formation ✅
- Creates 3 nodes (node-1, node-2, node-3)
- Verifies all nodes see each other
- Confirms cluster membership

### 2. Partition Distribution ✅
- Verifies 16 partitions created
- Checks each partition has a primary node
- Confirms partition map consistency across nodes

### 3. Replication Factor ✅
- Verifies each partition has 1 primary + 2 replicas
- Confirms no duplicate node assignments
- Validates RF=3 configuration

### 4. Data Partitioning ✅
- Tests multiple keys (`user:1`, `order:100`, `product:abc`)
- Verifies same key maps to same partition on all nodes
- Confirms consistent hashing works correctly

### 5. Custom Data Replication ✅
- Sets custom data on leader node
- Waits for Raft replication
- Verifies all nodes can read the same data
- Confirms data consistency via Raft consensus

### 6. Partition Balancing ✅
- Counts primary partitions per node
- Counts replica partitions per node
- Verifies even distribution (no node has 0 or all partitions)

---

## Manual Testing

### Start a 3-Node Cluster

**Terminal 1 - Node 1 (Bootstrap)**:
```bash
cd example/sync
go run server.go -node-id node-1 -http :8080 -raft :9080
```

**Terminal 2 - Node 2**:
```bash
cd example/sync
go run server.go -node-id node-2 -http :8081 -raft :9081 -join localhost:8080
```

**Terminal 3 - Node 3**:
```bash
cd example/sync
go run server.go -node-id node-3 -http :8082 -raft :9082 -join localhost:8080
```

### Verify Cluster State

```bash
# Check cluster on node-1
curl http://localhost:8080/cluster | jq

# Check cluster on node-2
curl http://localhost:8081/cluster | jq

# Check cluster on node-3
curl http://localhost:8082/cluster | jq
```

**Expected Output**:
```json
{
  "cluster": {
    "nodes": [
      {"id": "node-1", "ip": ":8080", "status": "active"},
      {"id": "node-2", "ip": ":8081", "status": "active"},
      {"id": "node-3", "ip": ":8082", "status": "active"}
    ],
    "partition_map": {
      "partitions": {
        "partition-0": {
          "primary_node": "node-1",
          "replica_nodes": ["node-2", "node-3"]
        },
        ...
      }
    }
  }
}
```

### Test Data Partitioning

```bash
# Set data on node-1
curl -X POST http://localhost:8080/set \
  -d '{"key":"user:123","value":"John Doe"}'

# Get from node-2 (should work if node-2 has the partition)
curl http://localhost:8081/get?key=user:123

# Get from node-3
curl http://localhost:8082/get?key=user:123
```

### Verify Partition Distribution

```bash
# Get partition stats from each node
curl http://localhost:8080/partitions/stats | jq
curl http://localhost:8081/partitions/stats | jq
curl http://localhost:8082/partitions/stats | jq
```

**Expected Output**:
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

### Test Custom Data Replication

```bash
# Set custom data on leader (find leader first)
curl http://localhost:8080/consensus/leader

# Set on leader
curl -X POST http://localhost:8080/custom-data/config \
  -H "Content-Type: application/json" \
  -d '{"max_connections": 100}'

# Wait 2 seconds for replication

# Read from all nodes
curl http://localhost:8080/custom-data/config
curl http://localhost:8081/custom-data/config
curl http://localhost:8082/custom-data/config

# All should return the same data
```

---

## Verification Checklist

Use this checklist to manually verify everything works:

- [ ] **Cluster Formation**
  - [ ] All 3 nodes start successfully
  - [ ] Each node sees 3 nodes in cluster
  - [ ] Leader is elected

- [ ] **Partitions**
  - [ ] 16 partitions created
  - [ ] Each partition has 1 primary
  - [ ] Each partition has 2 replicas
  - [ ] All nodes have same partition map

- [ ] **Data Partitioning**
  - [ ] Same key maps to same partition on all nodes
  - [ ] Keys are distributed across partitions
  - [ ] Hash function is consistent

- [ ] **Replication**
  - [ ] Data written to primary
  - [ ] Data readable from replicas
  - [ ] Custom data replicated via Raft

- [ ] **Balancing**
  - [ ] Partitions evenly distributed
  - [ ] No node has 0 partitions
  - [ ] No node has all partitions

---

## Troubleshooting

### Nodes Can't See Each Other

**Problem**: Nodes show only 1 node in cluster

**Solution**:
- Check join address is correct
- Verify ports are not blocked
- Check firewall rules
- Look for Raft connection errors in logs

### Partitions Not Created

**Problem**: Partition count is 0

**Solution**:
- Wait for leader election (3-5 seconds)
- Check replication factor ≤ node count
- Verify bootstrap node started first
- Check logs for errors

### Data Not Replicated

**Problem**: Custom data not visible on all nodes

**Solution**:
- Verify you're writing to the leader
- Wait 2-3 seconds for Raft replication
- Check Raft is healthy: `curl /consensus/stats`
- Verify all nodes are in Raft cluster

### Uneven Distribution

**Problem**: One node has most partitions

**Solution**:
- This is expected with consistent hashing
- Distribution improves with more partitions
- Try increasing partition count to 64 or 128

---

## Understanding the Output

### Integration Test Output

```
=== RUN   TestMultiNodeReplication
=== RUN   TestMultiNodeReplication/VerifyClusterFormation
=== RUN   TestMultiNodeReplication/VerifyPartitionDistribution
=== RUN   TestMultiNodeReplication/VerifyDataPartitioning
=== RUN   TestMultiNodeReplication/VerifyReplicationFactor
=== RUN   TestMultiNodeReplication/VerifyCustomDataReplication
=== RUN   TestMultiNodeReplication/VerifyPartitionOwnership
    integration_test.go:XXX: Node node-1: 5 primary, 11 replica partitions
    integration_test.go:XXX: Node node-2: 6 primary, 10 replica partitions
    integration_test.go:XXX: Node node-3: 5 primary, 11 replica partitions
--- PASS: TestMultiNodeReplication (15.23s)
```

**What This Means**:
- ✅ All 6 sub-tests passed
- ✅ Partitions distributed: 5, 6, 5 (balanced)
- ✅ Replicas distributed: 11, 10, 11 (balanced)
- ✅ Total time: 15 seconds (includes cluster formation)

---

## Performance Expectations

- **Cluster Formation**: 3-5 seconds
- **Partition Creation**: 1-2 seconds
- **Rebalancing**: 2-3 seconds
- **Custom Data Replication**: 1-2 seconds (via Raft)
- **Key Lookup**: < 1ms (O(1) hash + map lookup)

---

## Next Steps

1. ✅ Run integration test: `./test-integration.sh`
2. ✅ Start manual 3-node cluster
3. ✅ Verify partition distribution
4. ✅ Test data operations
5. ✅ Monitor Raft consensus
6. ✅ Test node failure scenarios

Happy testing! 🎉
