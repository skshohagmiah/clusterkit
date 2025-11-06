#!/bin/bash

# SYNC Example - Quorum-Based Replication
# Demonstrates strong consistency with quorum writes

set -e

NUM_NODES=${1:-10}
PARTITIONS=${2:-64}
REPLICATION=${3:-3}

echo "=========================================="
echo "  SYNC Example - Quorum-Based KV Store"
echo "=========================================="
echo "Nodes: $NUM_NODES"
echo "Partitions: $PARTITIONS"
echo "Replication Factor: $REPLICATION"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    pkill -f "NODE_ID=node-" || true
    rm -rf /tmp/ck-node* /tmp/kv-*.log
    echo "✓ Cleanup complete"
}

trap cleanup EXIT

# Clean start
cleanup

# Start nodes
echo "Starting $NUM_NODES nodes..."
echo ""

for i in $(seq 1 $NUM_NODES); do
    NODE_ID="node-$i"
    HTTP_PORT=$((8080 + i - 1))
    KV_PORT=$((9080 + i - 1))
    DATA_DIR="/tmp/ck-node$i"
    
    mkdir -p $DATA_DIR
    
    if [ $i -eq 1 ]; then
        # Bootstrap node
        echo "🚀 Starting $NODE_ID (bootstrap) on ports $HTTP_PORT/$KV_PORT"
        NODE_ID=$NODE_ID HTTP_PORT=$HTTP_PORT KV_PORT=$KV_PORT \
        PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
        DATA_DIR=$DATA_DIR \
        go run server.go > /tmp/kv-node$i.log 2>&1 &
    else
        # Join existing cluster
        echo "🔗 Starting $NODE_ID (joining) on ports $HTTP_PORT/$KV_PORT"
        NODE_ID=$NODE_ID HTTP_PORT=$HTTP_PORT KV_PORT=$KV_PORT \
        JOIN_ADDR=localhost:8080 \
        PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
        DATA_DIR=$DATA_DIR \
        go run server.go > /tmp/kv-node$i.log 2>&1 &
    fi
    
    sleep 2
done

echo ""
echo "✓ All nodes started"
echo ""
echo "Waiting for cluster to stabilize (20s)..."
sleep 20

# Verify all nodes joined
echo ""
echo "Verifying cluster membership..."
JOINED_NODES=$(curl -s http://localhost:8080/cluster 2>/dev/null | jq -r '.cluster.nodes | length' 2>/dev/null || echo "0")
echo "Nodes in cluster: $JOINED_NODES / $NUM_NODES"

if [ "$JOINED_NODES" -lt "$NUM_NODES" ]; then
    echo "⚠️  Warning: Not all nodes joined successfully"
    echo "Check logs: /tmp/kv-node*.log"
fi

# Show cluster status
echo ""
echo "=========================================="
echo "  Cluster Status"
echo "=========================================="
curl -s http://localhost:8080/cluster | jq '.cluster.nodes[] | {id, ip}' 2>/dev/null || echo "Cluster info not available"

echo ""
echo "=========================================="
echo "  Testing SYNC Mode (Quorum Writes)"
echo "=========================================="
echo ""

# Test writes
echo "📝 Writing 1000 keys with quorum..."
start_time=$(date +%s)
write_success=0
write_fail=0

for i in $(seq 1 1000); do
    if curl -s -X POST http://localhost:9080/kv/set \
        -H "Content-Type: application/json" \
        -d "{\"key\":\"user:$i\",\"value\":\"User $i\"}" > /dev/null 2>&1; then
        write_success=$((write_success + 1))
    else
        write_fail=$((write_fail + 1))
    fi
    
    if [ $((i % 100)) -eq 0 ]; then
        echo "  ✓ Wrote $i keys..."
    fi
done

write_duration=$(($(date +%s) - start_time))
write_ops_per_sec=$((write_success / write_duration))

echo ""
echo "✅ Write Results:"
echo "  Success: $write_success"
echo "  Failed: $write_fail"
echo "  Duration: ${write_duration}s"
echo "  Throughput: ${write_ops_per_sec} ops/sec"

echo ""
echo "📖 Reading 1000 keys..."
start_time=$(date +%s)
read_success=0
read_fail=0

for i in $(seq 1 1000); do
    if curl -s http://localhost:9080/kv/get?key=user:$i > /dev/null 2>&1; then
        read_success=$((read_success + 1))
    else
        read_fail=$((read_fail + 1))
    fi
    
    if [ $((i % 100)) -eq 0 ]; then
        echo "  ✓ Read $i keys..."
    fi
done

read_duration=$(($(date +%s) - start_time))
read_ops_per_sec=$((read_success / read_duration))

echo ""
echo "✅ Read Results:"
echo "  Success: $read_success"
echo "  Failed: $read_fail"
echo "  Duration: ${read_duration}s"
echo "  Throughput: ${read_ops_per_sec} ops/sec"

echo ""
echo "📊 Node Statistics:"
for i in $(seq 1 $NUM_NODES); do
    PORT=$((9080 + i - 1))
    stats=$(curl -s http://localhost:$PORT/kv/stats 2>/dev/null || echo "N/A")
    echo "  Node $i (port $PORT): $stats"
done

echo ""
echo "=========================================="
echo "  Cluster Running!"
echo "=========================================="
echo ""
echo "ClusterKit API: http://localhost:8080"
echo "KV Store Nodes: http://localhost:9080, 9081, 9082..."
echo ""
echo "Try it yourself:"
echo "  # Write"
echo "  curl -X POST http://localhost:9080/kv/set -d '{\"key\":\"test\",\"value\":\"hello\"}'"
echo ""
echo "  # Read"
echo "  curl http://localhost:9080/kv/get?key=test"
echo ""
echo "Logs: /tmp/kv-node*.log"
echo ""

# Keep running
read -p "Press Enter to stop all nodes..."
