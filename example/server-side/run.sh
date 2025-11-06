#!/bin/bash

# Server-Side Example - Server handles routing and replication
# Demonstrates partition management, primary/replica logic, and data migration

set -e

NUM_NODES=${1:-6}
PARTITIONS=${2:-64}
REPLICATION=${3:-3}

echo "=========================================="
echo "  Server-Side Example - Partition Handling"
echo "=========================================="
echo "Nodes: $NUM_NODES"
echo "Partitions: $PARTITIONS"
echo "Replication Factor: $REPLICATION"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    pkill -9 -f "server.go" || true
    pkill -9 -f "NODE_ID=node-" || true
    sleep 2
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
    KV_PORT=$((10080 + i - 1))  # Changed from 9080 to 10080 to avoid Raft port conflicts
    DATA_DIR="/tmp/ck-node$i"
    
    mkdir -p $DATA_DIR
    
    if [ $i -eq 1 ]; then
        # Bootstrap node
        echo "🚀 Starting $NODE_ID (bootstrap) on ports $HTTP_PORT/$KV_PORT"
        NODE_ID=$NODE_ID HTTP_PORT=$HTTP_PORT KV_PORT=$KV_PORT \
        PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
        DATA_DIR=$DATA_DIR \
        go run server.go > /tmp/kv-node$i.log 2>&1 &
        
        # Wait for bootstrap node to be ready
        echo "  Waiting for bootstrap node..."
        sleep 5
        
        # Verify bootstrap is up
        for attempt in {1..10}; do
            if curl -s http://localhost:$HTTP_PORT/health > /dev/null 2>&1; then
                echo "  ✓ Bootstrap node ready"
                break
            fi
            sleep 1
        done
    else
        # Join existing cluster
        echo "🔗 Starting $NODE_ID (joining) on ports $HTTP_PORT/$KV_PORT"
        NODE_ID=$NODE_ID HTTP_PORT=$HTTP_PORT KV_PORT=$KV_PORT \
        JOIN_ADDR=localhost:8080 \
        PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
        DATA_DIR=$DATA_DIR \
        go run server.go > /tmp/kv-node$i.log 2>&1 &
        
        # Wait a bit between joins
        sleep 3
    fi
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
echo "  Testing Server-Side Routing"
echo "=========================================="
echo ""

# Test writes (send to random nodes - server will route)
echo "📝 Writing 1000 keys (server-side routing)..."
start_time=$(date +%s)
write_success=0
write_fail=0

for i in $(seq 1 1000); do
    # Send to random node (not necessarily the primary)
    NODE_PORT=$((10080 + (i % NUM_NODES)))
    
    if curl -s -X POST http://localhost:$NODE_PORT/kv/set \
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
echo "📖 Reading 1000 keys (from random nodes)..."
start_time=$(date +%s)
read_success=0
read_fail=0

for i in $(seq 1 1000); do
    # Read from random node
    NODE_PORT=$((10080 + (i % NUM_NODES)))
    
    if curl -s http://localhost:$NODE_PORT/kv/get?key=user:$i > /dev/null 2>&1; then
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
    PORT=$((10080 + i - 1))
    stats=$(curl -s http://localhost:$PORT/kv/stats 2>/dev/null || echo "N/A")
    echo "  Node $i (port $PORT): $stats"
done

echo ""
echo "📦 Partition Distribution (sample):"
for i in $(seq 1 3); do
    PORT=$((10080 + i - 1))
    echo "  Node $i:"
    curl -s http://localhost:$PORT/kv/partitions 2>/dev/null | jq -r '.partitions[]? | "    \(.partition_id): \(.role)"' 2>/dev/null | head -5 || echo "    (not available)"
    echo "    ..."
done

echo ""
echo "=========================================="
echo "  Cluster Running!"
echo "=========================================="
echo ""
echo "ClusterKit API: http://localhost:8080"
echo "KV Store Nodes: http://localhost:10080, 10081, 10082..."
echo ""
echo "Try it yourself (send to ANY node):"
echo "  # Write to node-1"
echo "  curl -X POST http://localhost:10080/kv/set -d '{\"key\":\"test\",\"value\":\"hello\"}'"
echo ""
echo "  # Read from node-5 (server will route if needed)"
echo "  curl http://localhost:10084/kv/get?key=test"
echo ""
echo "Logs: /tmp/kv-node*.log"
echo ""

# Keep running
read -p "Press Enter to stop all nodes..."
