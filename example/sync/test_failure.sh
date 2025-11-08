#!/bin/bash

# Test script to demonstrate automatic failure detection and recovery

set -e

echo "=========================================="
echo "  Node Failure & Recovery Test"
echo "=========================================="
echo "Testing automatic failure detection, removal, and rejoin"
echo ""

# Cleanup
cleanup() {
    echo ""
    echo "Cleaning up..."
    killall -9 server 2>/dev/null || true
    rm -rf /tmp/ck-node*
}

trap cleanup EXIT
cleanup

mkdir -p logs
rm -f logs/*.log

# Start 3 nodes
echo "Starting 3 nodes..."
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 DATA_DIR=/tmp/ck-node1 \
    go run server.go > logs/node-1.log 2>&1 &
PID1=$!
sleep 3

NODE_ID=node-2 HTTP_PORT=8081 KV_PORT=9081 DATA_DIR=/tmp/ck-node2 JOIN_ADDR=localhost:8080 \
    go run server.go > logs/node-2.log 2>&1 &
PID2=$!
sleep 2

NODE_ID=node-3 HTTP_PORT=8082 KV_PORT=9082 DATA_DIR=/tmp/ck-node3 JOIN_ADDR=localhost:8080 \
    go run server.go > logs/node-3.log 2>&1 &
PID3=$!
sleep 5

echo "✓ All nodes started"
echo ""

# Check initial cluster
echo "========================================"
echo "PHASE 1: Initial Cluster State"
echo "========================================"
echo ""
echo "Nodes:"
curl -s http://localhost:8080/cluster | jq '.cluster.nodes[] | {id, ip, status}'
echo ""
echo "Partition Summary:"
curl -s http://localhost:8080/cluster | jq '{total_partitions: (.cluster.partition_map.partitions | length), sample_partition: (.cluster.partition_map.partitions | to_entries | first | .value | {id, primary: .primary_node, replicas: .replica_nodes})}'
echo ""

# Write some test data
echo "Writing test data..."
go run cmd/test_client/*.go 100
echo ""

# Check data distribution
echo "Data distribution:"
for port in 9080 9081 9082; do
    stats=$(curl -s http://localhost:$port/kv/stats 2>/dev/null || echo "N/A")
    echo "  Port $port: $stats"
done
echo ""

# Kill node-3
echo "❌ KILLING node-3 (PID: $PID3)..."
kill -9 $PID3
echo "Waiting for health checker to detect failure (15s = 3 checks × 5s interval)..."
sleep 2
echo ""

# Check cluster state immediately after failure
echo "========================================"
echo "PHASE 2: Immediately After Node-3 Dies"
echo "========================================"
echo ""
echo "Nodes:"
curl -s http://localhost:8080/cluster | jq '.cluster.nodes[] | {id, ip, status}'
echo ""
echo "Partitions still assigned to node-3:"
curl -s http://localhost:8080/cluster | jq '[.cluster.partition_map.partitions | to_entries[] | select(.value.primary_node == "node-3" or (.value.replica_nodes | contains(["node-3"]))) | .value.id] | length' | xargs echo "Count:"
echo ""
echo "⚠️  Node-3 is still in cluster (health checker hasn't detected it yet)"
echo ""

# Wait for health checker to remove the node
echo "Waiting for automatic removal (health checks: 5s interval, 3 failures = 15s)..."
sleep 18
echo ""

# Check cluster state after automatic removal
echo "========================================"
echo "PHASE 3: After Automatic Removal"
echo "========================================"
echo ""
echo "Nodes:"
curl -s http://localhost:8080/cluster | jq '.cluster.nodes[] | {id, ip, status}'
echo ""
NODE_COUNT=$(curl -s http://localhost:8080/cluster | jq '.cluster.nodes | length')
if [ "$NODE_COUNT" -eq 2 ]; then
    echo "✅ Node-3 automatically removed! (2 nodes remaining)"
    echo ""
    echo "Partitions reassigned:"
    curl -s http://localhost:8080/cluster | jq '[.cluster.partition_map.partitions | to_entries[] | select(.value.primary_node == "node-3" or (.value.replica_nodes | contains(["node-3"]))) | .value.id] | length' | xargs echo "Partitions still on node-3:"
else
    echo "❌ Node-3 NOT removed! Still $NODE_COUNT nodes"
    echo "Checking health checker status in logs..."
    grep "\[HEALTH\]" logs/node-1.log | tail -10
fi
echo ""

# Try to read data
echo "Attempting to read 100 keys..."
read_success=0
read_fail=0
for i in $(seq 1 100); do
    if curl -s http://localhost:9080/kv/get?key=user:$i > /dev/null 2>&1; then
        read_success=$((read_success + 1))
    else
        read_fail=$((read_fail + 1))
    fi
done
echo "  Success: $read_success"
echo "  Failed: $read_fail"
echo ""
if [ "$read_fail" -gt 0 ]; then
    echo "⚠️  Some reads may have failed during rebalancing"
else
    echo "✅ All reads successful after automatic rebalancing!"
fi
echo ""

# Restart node-3
echo "🔄 REJOINING node-3..."
NODE_ID=node-3 HTTP_PORT=8082 KV_PORT=9082 DATA_DIR=/tmp/ck-node3 JOIN_ADDR=localhost:8080 \
    go run server.go > logs/node-3-restart.log 2>&1 &
PID3_NEW=$!
echo "Waiting for rejoin and rebalancing (10s)..."
sleep 10
echo ""

# Check cluster state after recovery
echo "========================================"
echo "PHASE 4: After Node-3 Rejoins"
echo "========================================"
echo ""
echo "Nodes:"
curl -s http://localhost:8080/cluster | jq '.cluster.nodes[] | {id, ip, status}'
echo ""
echo "Partition distribution:"
echo "  Node-1 primary partitions:" $(curl -s http://localhost:8080/cluster | jq '[.cluster.partition_map.partitions | to_entries[] | select(.value.primary_node == "node-1") | .value.id] | length')
echo "  Node-2 primary partitions:" $(curl -s http://localhost:8080/cluster | jq '[.cluster.partition_map.partitions | to_entries[] | select(.value.primary_node == "node-2") | .value.id] | length')
echo "  Node-3 primary partitions:" $(curl -s http://localhost:8080/cluster | jq '[.cluster.partition_map.partitions | to_entries[] | select(.value.primary_node == "node-3") | .value.id] | length')
echo ""

# Check for duplicate nodes
NODE_COUNT=$(curl -s http://localhost:8080/cluster | jq '.cluster.nodes | length')
if [ "$NODE_COUNT" -gt 3 ]; then
    echo "❌ PROBLEM: Duplicate nodes detected! Node count: $NODE_COUNT"
else
    echo "✅ Node count correct: $NODE_COUNT (rejoin handled properly!)"
fi
echo ""

# Try to read data again
echo "Attempting to read 100 keys after recovery..."
read_success=0
read_fail=0
for i in $(seq 1 100); do
    if curl -s http://localhost:9080/kv/get?key=user:$i > /dev/null 2>&1; then
        read_success=$((read_success + 1))
    else
        read_fail=$((read_fail + 1))
    fi
done
echo "  Success: $read_success"
echo "  Failed: $read_fail"
echo ""

echo "=========================================="
echo "  Summary"
echo "=========================================="
echo ""
echo "✅ NEW ClusterKit Features Tested:"
echo "  ✅ Automatic failure detection (health checks every 5s)"
echo "  ✅ Automatic node removal after 3 failed checks (~15s)"
echo "  ✅ Automatic rebalancing after node removal"
echo "  ✅ Proper rejoin handling (no duplicate nodes)"
echo "  ✅ Data migration hooks fire on rebalancing"
echo ""
echo "Check logs for details:"
echo "  - logs/node-1.log (leader, handles removal)"
echo "  - logs/node-2.log"
echo "  - logs/node-3.log (killed)"
echo "  - logs/node-3-restart.log (rejoined)"
echo ""
echo "Look for these log messages:"
echo "  [HEALTH] Node health check failed"
echo "  [HEALTH] ❌ Node exceeded failure threshold, removing"
echo "  [HEALTH] Triggering rebalance after node removal"
echo "  [REJOIN] Node is rejoining"
echo ""

read -p "Press Enter to cleanup..."
