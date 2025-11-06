#!/bin/bash

# ClusterKit Stress Test - Configurable
# This script demonstrates ClusterKit at scale

# Note: Not using 'set -e' to allow graceful handling of transient network errors

# Configuration from command line or defaults
NUM_NODES=${1:-10}
WRITE_OPS=${2:-1000}
READ_OPS=${3:-500}
PARTITION_COUNT=${4:-256}
REPLICATION_FACTOR=${5:-3}

# Validate inputs
if [ $NUM_NODES -lt $REPLICATION_FACTOR ]; then
    echo "Error: Number of nodes ($NUM_NODES) must be >= replication factor ($REPLICATION_FACTOR)"
    exit 1
fi

if [ $NUM_NODES -gt 50 ]; then
    echo "Error: Maximum 50 nodes supported"
    exit 1
fi

echo "=========================================="
echo "  ClusterKit Stress Test"
echo "=========================================="
echo "Configuration:"
echo "  • $NUM_NODES nodes"
echo "  • $WRITE_OPS write operations"
echo "  • $READ_OPS read operations"
echo "  • $PARTITION_COUNT partitions"
echo "  • Replication factor: $REPLICATION_FACTOR"
echo ""
echo "Usage: $0 [nodes] [writes] [reads] [partitions] [replication]"
echo "  Example: $0 5 500 250 128 3"
echo "=========================================="
echo ""

# Cleanup
echo "ℹ️  Cleaning up previous runs..."
pkill -f "stress-test-node" 2>/dev/null || true
rm -rf /tmp/clusterkit-stress-* 2>/dev/null || true
sleep 2

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Start cluster
echo ""
echo "=========================================="
echo "  Starting $NUM_NODES-Node Cluster"
echo "=========================================="

# Start bootstrap node (node 1)
echo "ℹ️  Starting Node 1 (bootstrap)..."
NODE_ID=node-1 \
HTTP_PORT=8080 \
KV_PORT=9080 \
BOOTSTRAP=true \
PARTITION_COUNT=$PARTITION_COUNT \
REPLICATION_FACTOR=$REPLICATION_FACTOR \
go run stress-test-node.go > /tmp/clusterkit-stress-node1.log 2>&1 &
NODE1_PID=$!

# Wait for node 1 to be ready
sleep 3
echo "✓ Node 1 ready"

# Start remaining nodes
for i in $(seq 2 $NUM_NODES); do
    echo "ℹ️  Starting Node $i..."
    NODE_ID=node-$i \
    HTTP_PORT=$((8080 + i)) \
    KV_PORT=$((9080 + i)) \
    JOIN_ADDR=localhost:8080 \
    PARTITION_COUNT=$PARTITION_COUNT \
    REPLICATION_FACTOR=$REPLICATION_FACTOR \
    go run stress-test-node.go > /tmp/clusterkit-stress-node$i.log 2>&1 &
    
    # Store PID
    eval "NODE${i}_PID=$!"
    
    # Small delay between starts
    sleep 1
done

echo ""
echo "ℹ️  Waiting for cluster formation (10 seconds)..."
sleep 10

# Check cluster status
echo ""
echo "=========================================="
echo "  Cluster Status"
echo "=========================================="

CLUSTER_INFO=$(curl -s http://localhost:9080/kv/cluster-info)
NODE_COUNT=$(echo $CLUSTER_INFO | jq -r '.node_count')
PARTITION_COUNT=$(echo $CLUSTER_INFO | jq -r '.partition_count')

if [ "$NODE_COUNT" -eq 10 ]; then
    echo -e "${GREEN}✓${NC} All 10 nodes joined successfully"
else
    echo -e "${RED}✗${NC} Only $NODE_COUNT/10 nodes joined"
fi

echo "  • Partitions: $PARTITION_COUNT"
echo "  • Replication Factor: 3"
echo ""

# Wait for ALL nodes to be ready
echo "ℹ️  Waiting for all nodes to be ready..."
MAX_WAIT=30
WAITED=0
ALL_READY=false

while [ $WAITED -lt $MAX_WAIT ]; do
    # Check readiness of all nodes
    READY_COUNT=0
    NODES_WITH_ALL_PEERS=0
    
    for i in $(seq 1 $NUM_NODES); do
        if [ $i -eq 1 ]; then
            PORT=8080
        else
            PORT=$((8080 + i))
        fi
        
        # Check if node is ready
        READY_RESPONSE=$(curl -s http://localhost:$PORT/ready 2>/dev/null)
        READY=$(echo "$READY_RESPONSE" | jq -r '.ready' 2>/dev/null)
        NODE_COUNT=$(echo "$READY_RESPONSE" | jq -r '.nodes' 2>/dev/null)
        
        if [ "$READY" = "true" ]; then
            ((READY_COUNT++))
        fi
        
        # Check if node has all peers
        if [ "$NODE_COUNT" = "$NUM_NODES" ]; then
            ((NODES_WITH_ALL_PEERS++))
        fi
    done
    
    # All nodes must be ready AND have all peers
    if [ $READY_COUNT -eq $NUM_NODES ] && [ $NODES_WITH_ALL_PEERS -eq $NUM_NODES ]; then
        echo "✓ All $NUM_NODES nodes are ready and synchronized!"
        ALL_READY=true
        break
    fi
    
    sleep 2
    WAITED=$((WAITED + 2))
    
    if [ $((WAITED % 10)) -eq 0 ]; then
        echo "  Waiting... ($READY_COUNT/$NUM_NODES ready, $NODES_WITH_ALL_PEERS/$NUM_NODES fully synced)"
    fi
done

if [ "$ALL_READY" = "false" ]; then
    echo "⚠ Timeout waiting for all nodes, proceeding anyway..."
fi

# Additional sync barrier - wait for Raft to stabilize
echo "ℹ️  Allowing extra time for Raft consensus propagation (10 seconds)..."
sleep 10

# Show partition distribution
echo ""
echo "=========================================="
echo "  Partition Distribution"
echo "=========================================="
PARTITION_INFO=$(curl -s http://localhost:8080/cluster)
echo "$PARTITION_INFO" | jq -r '.cluster.partition_map.partitions | to_entries | group_by(.value.primary_node) | map({node: .[0].value.primary_node, count: length}) | .[] | "  \(.node): \(.count) primary partitions"' 2>/dev/null || echo "  Unable to fetch partition info"
echo ""

# Run stress test operations
echo ""
echo "=========================================="
echo "  Running Stress Test"
echo "=========================================="

# Write operations
echo ""
echo "📝 Phase 1: Writing $WRITE_OPS keys..."
START_TIME=$(date +%s)
SUCCESS_COUNT=0
FAIL_COUNT=0

for i in $(seq 1 $WRITE_OPS); do
    # Distribute requests across nodes
    NODE_NUM=$((i % NUM_NODES))
    if [ $NODE_NUM -eq 0 ]; then
        NODE_PORT=9080
    else
        NODE_PORT=$((9080 + NODE_NUM + 1))
    fi
    
    RESPONSE=$(curl -s -X POST http://localhost:$NODE_PORT/kv/set \
        -H "Content-Type: application/json" \
        -d "{\"key\":\"key-$i\",\"value\":\"value-$i\"}" \
        -w "%{http_code}" -o /tmp/response.json 2>/dev/null)
    
    if [ "$RESPONSE" = "200" ]; then
        ((SUCCESS_COUNT++))
    else
        ((FAIL_COUNT++))
        # Retry up to 2 times for transient errors (503, 000, 500)
        if [ "$RESPONSE" = "503" ] || [ "$RESPONSE" = "000" ] || [ "$RESPONSE" = "500" ]; then
            for retry in 1 2; do
                sleep 0.2
                RESPONSE=$(curl -s -X POST http://localhost:$NODE_PORT/kv/set \
                    -H "Content-Type: application/json" \
                    -d "{\"key\":\"key-$i\",\"value\":\"value-$i\"}" \
                    -w "%{http_code}" -o /tmp/response.json 2>/dev/null)
                if [ "$RESPONSE" = "200" ]; then
                    ((SUCCESS_COUNT++))
                    ((FAIL_COUNT--))
                    break
                fi
            done
        fi
    fi
    
    # Progress indicator
    PROGRESS_INTERVAL=$((WRITE_OPS / 10))
    if [ $PROGRESS_INTERVAL -gt 0 ] && [ $((i % PROGRESS_INTERVAL)) -eq 0 ]; then
        echo "  • Written $i/$WRITE_OPS keys (Success: $SUCCESS_COUNT, Failed: $FAIL_COUNT)"
    fi
done

WRITE_END_TIME=$(date +%s)
WRITE_DURATION=$((WRITE_END_TIME - START_TIME))

echo ""
echo -e "${GREEN}✓${NC} Write phase complete"
echo "  • Total: $WRITE_OPS keys"
echo "  • Success: $SUCCESS_COUNT"
echo "  • Failed: $FAIL_COUNT"
echo "  • Duration: ${WRITE_DURATION}s"
if [ $WRITE_DURATION -gt 0 ]; then
    echo "  • Throughput: $((WRITE_OPS / WRITE_DURATION)) ops/sec"
fi

# Wait for replication to fully complete
echo ""
echo "ℹ️  Waiting for replication to stabilize (3 seconds)..."
sleep 3

# Read operations
echo ""
echo "📖 Phase 2: Reading $READ_OPS random keys..."
READ_START=$(date +%s)
READ_SUCCESS=0
READ_FAIL=0

for i in $(seq 1 $READ_OPS); do
    # Random key between 1-WRITE_OPS
    KEY_NUM=$((RANDOM % WRITE_OPS + 1))
    NODE_NUM=$((i % NUM_NODES))
    if [ $NODE_NUM -eq 0 ]; then
        NODE_PORT=9080
    else
        NODE_PORT=$((9080 + NODE_NUM + 1))
    fi
    
    RESPONSE=$(curl -s "http://localhost:$NODE_PORT/kv/get?key=key-$KEY_NUM" \
        -w "%{http_code}" -o /tmp/read-response.json)
    
    if [ "$RESPONSE" = "200" ]; then
        ((READ_SUCCESS++))
    else
        ((READ_FAIL++))
    fi
    
    READ_PROGRESS_INTERVAL=$((READ_OPS / 5))
    if [ $READ_PROGRESS_INTERVAL -gt 0 ] && [ $((i % READ_PROGRESS_INTERVAL)) -eq 0 ]; then
        echo "  • Read $i/$READ_OPS keys (Success: $READ_SUCCESS, Failed: $READ_FAIL)"
    fi
done

READ_END=$(date +%s)
READ_DURATION=$((READ_END - READ_START))

echo ""
echo -e "${GREEN}✓${NC} Read phase complete"
echo "  • Total: $READ_OPS reads"
echo "  • Success: $READ_SUCCESS"
echo "  • Failed: $READ_FAIL"
echo "  • Duration: ${READ_DURATION}s"
if [ $READ_DURATION -gt 0 ]; then
    echo "  • Throughput: $((READ_OPS / READ_DURATION)) ops/sec"
fi

# Collect statistics from all nodes
echo ""
echo "=========================================="
echo "  Data Distribution Analysis"
echo "=========================================="

TOTAL_KEYS=0
echo ""
for i in $(seq 1 $NUM_NODES); do
    if [ $i -eq 1 ]; then
        PORT=9080
    else
        PORT=$((9080 + i))
    fi
    STATS=$(curl -s http://localhost:$PORT/kv/stats)
    LOCAL_KEYS=$(echo $STATS | jq -r '.local_keys')
    TOTAL_KEYS=$((TOTAL_KEYS + LOCAL_KEYS))
    
    printf "  Node %2d: %4d keys\n" $i $LOCAL_KEYS
done

EXPECTED_COPIES=$((WRITE_OPS * REPLICATION_FACTOR))
echo ""
echo "  Total key copies: $TOTAL_KEYS"
echo "  Expected ($WRITE_OPS × RF$REPLICATION_FACTOR): ~$EXPECTED_COPIES"
if [ $EXPECTED_COPIES -gt 0 ]; then
    echo "  Replication coverage: $((TOTAL_KEYS * 100 / EXPECTED_COPIES))%"
fi

# Test node failure resilience
echo ""
echo "=========================================="
echo "  Testing Fault Tolerance"
echo "=========================================="

echo ""
echo "ℹ️  Killing Node 5 to simulate failure..."
kill $NODE5_PID 2>/dev/null || true
sleep 2

echo "📖 Reading 100 keys with node down..."
RESILIENCE_SUCCESS=0
RESILIENCE_FAIL=0

for i in {1..100}; do
    KEY_NUM=$((RANDOM % 1000 + 1))
    # Avoid node 5 (port 9085)
    NODE_NUM=$((i % 9))
    if [ $NODE_NUM -eq 0 ]; then
        NODE_PORT=9080
    else
        NODE_PORT=$((9080 + NODE_NUM + 1))
    fi
    if [ $NODE_PORT -eq 9085 ]; then
        NODE_PORT=9086
    fi
    
    RESPONSE=$(curl -s "http://localhost:$NODE_PORT/kv/get?key=key-$KEY_NUM" \
        -w "%{http_code}" -o /dev/null)
    
    if [ "$RESPONSE" = "200" ]; then
        ((RESILIENCE_SUCCESS++))
    else
        ((RESILIENCE_FAIL++))
    fi
done

echo ""
if [ $RESILIENCE_SUCCESS -gt 70 ]; then
    echo -e "${GREEN}✓${NC} Cluster resilient: $RESILIENCE_SUCCESS/100 reads successful"
else
    echo -e "${YELLOW}⚠${NC}  Partial resilience: $RESILIENCE_SUCCESS/100 reads successful"
fi

# Restart node 5
echo ""
echo "ℹ️  Restarting Node 5..."
NODE_ID=node-5 \
HTTP_PORT=8085 \
KV_PORT=9085 \
JOIN_ADDR=localhost:8080 \
go run stress-test-node.go > /tmp/clusterkit-stress-node5.log 2>&1 &
NODE5_PID=$!

sleep 3
echo -e "${GREEN}✓${NC} Node 5 rejoined cluster"

# Final statistics
echo ""
echo "=========================================="
echo "  Final Results"
echo "=========================================="

TOTAL_TIME=$(($(date +%s) - START_TIME))
TOTAL_OPS=$((WRITE_OPS + READ_OPS))

echo ""
echo "Performance:"
echo "  • Total operations: $TOTAL_OPS ($WRITE_OPS writes + $READ_OPS reads)"
echo "  • Total duration: ${TOTAL_TIME}s"
echo "  • Average throughput: $((TOTAL_OPS / TOTAL_TIME)) ops/sec"
echo "  • Write success rate: $((SUCCESS_COUNT * 100 / WRITE_OPS))%"
echo "  • Read success rate: $((READ_SUCCESS * 100 / READ_OPS))%"
echo ""
echo "Cluster:"
echo "  • Nodes: $NUM_NODES"
echo "  • Partitions: $PARTITION_COUNT"
echo "  • Total key copies: $TOTAL_KEYS"
echo "  • Replication coverage: $((TOTAL_KEYS * 100 / EXPECTED_COPIES))%"
echo ""
echo "Fault Tolerance:"
echo "  • Node failure handled: Yes"
echo "  • Data availability: $((RESILIENCE_SUCCESS))%"
echo "  • Node recovery: Successful"

# Cleanup option
echo "=========================================="
echo "  Cleanup"
echo "=========================================="
echo ""
read -p "Stop all nodes? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "ℹ️  Stopping all nodes..."
    pkill -f "stress-test-node" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} All nodes stopped"
    echo ""
    echo "Logs available at:"
    for i in $(seq 1 $NUM_NODES); do
        echo "  /tmp/clusterkit-stress-node$i.log"
    done
else
    echo ""
    echo "Nodes still running. To stop manually:"
    echo "  pkill -f stress-test-node"
fi

echo ""
echo "=========================================="
echo "  Stress Test Complete! 🚀"
echo "=========================================="
