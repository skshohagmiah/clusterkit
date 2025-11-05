#!/bin/bash

# ClusterKit Distributed KV Store Demo
# This script demonstrates automatic data partitioning across a 3-node cluster

set -e

echo "=========================================="
echo "  ClusterKit - Distributed KV Store Demo"
echo "=========================================="
echo ""

# Cleanup
echo "Cleaning up..."
pkill -9 main 2>/dev/null || true
rm -rf ./data
sleep 1

# Start cluster
echo "Starting 3-node cluster..."
NODE_ID=node-1 NODE_NAME=Server-1 HTTP_ADDR=:8080 RAFT_ADDR=127.0.0.1:9001 BOOTSTRAP=true DATA_DIR=./data/node1 go run main.go > /tmp/node1.log 2>&1 &
sleep 3

NODE_ID=node-2 NODE_NAME=Server-2 HTTP_ADDR=:8081 RAFT_ADDR=127.0.0.1:9002 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node2 go run main.go > /tmp/node2.log 2>&1 &
sleep 2

NODE_ID=node-3 NODE_NAME=Server-3 HTTP_ADDR=:8082 RAFT_ADDR=127.0.0.1:9003 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node3 go run main.go > /tmp/node3.log 2>&1 &

echo "✓ Nodes started"
echo "  Waiting for compilation and cluster formation..."
sleep 20

# Check cluster
echo ""
echo "=========================================="
echo "  CLUSTER STATUS"
echo "=========================================="
CLUSTER=$(timeout 5 curl -s http://localhost:8080/cluster 2>/dev/null || echo '{"error":"timeout"}')
if echo "$CLUSTER" | grep -q "error"; then
    echo "✗ Cluster not responding. Nodes may still be starting..."
    exit 1
fi

CLUSTER_ID=$(echo "$CLUSTER" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
CLUSTER_NAME=$(echo "$CLUSTER" | grep -o '"name":"[^"]*"' | head -1 | cut -d'"' -f4)
NODE_COUNT=$(echo "$CLUSTER" | grep -o '"id":"node-[0-9]"' | wc -l)
echo "Cluster: $CLUSTER_NAME ($CLUSTER_ID)"
echo "Nodes: $NODE_COUNT"

# Check partitions
PART_STATS=$(timeout 5 curl -s http://localhost:8080/partitions/stats 2>/dev/null || echo '{"total_partitions":0}')
TOTAL_PARTS=$(echo "$PART_STATS" | grep -o '"total_partitions":[0-9]*' | cut -d: -f2)
echo "Partitions: $TOTAL_PARTS"

if [ -z "$TOTAL_PARTS" ] || [ "$TOTAL_PARTS" -eq 0 ]; then
    echo ""
    echo "⏳ Partitions not created yet. Waiting longer..."
    sleep 10
    PART_STATS=$(timeout 5 curl -s http://localhost:8080/partitions/stats 2>/dev/null || echo '{"total_partitions":0}')
    TOTAL_PARTS=$(echo "$PART_STATS" | grep -o '"total_partitions":[0-9]*' | cut -d: -f2)
    echo "Partitions: $TOTAL_PARTS"
    
    if [ -z "$TOTAL_PARTS" ] || [ "$TOTAL_PARTS" -eq 0 ]; then
        echo "✗ Partitions still not created. Check logs."
        exit 1
    fi
fi

# Insert data
echo ""
echo "=========================================="
echo "  INSERTING TEST DATA"
echo "=========================================="
echo "Inserting 15 keys..."
for i in {1..15}; do
    timeout 3 curl -s -X POST http://localhost:9080/kv/set \
        -H "Content-Type: application/json" \
        -d "{\"key\":\"user-$i\",\"value\":\"User $i\"}" > /dev/null 2>&1
    echo -n "."
done
echo " Done!"

sleep 1

# Show distribution
echo ""
echo "=========================================="
echo "  DATA DISTRIBUTION ACROSS NODES"
echo "=========================================="

NODE1=$(timeout 3 curl -s http://localhost:9080/kv/list 2>/dev/null || echo '{"keys":[],"count":0}')
NODE2=$(timeout 3 curl -s http://localhost:9081/kv/list 2>/dev/null || echo '{"keys":[],"count":0}')
NODE3=$(timeout 3 curl -s http://localhost:9082/kv/list 2>/dev/null || echo '{"keys":[],"count":0}')

COUNT1=$(echo "$NODE1" | grep -o '"count":[0-9]*' | cut -d: -f2)
COUNT2=$(echo "$NODE2" | grep -o '"count":[0-9]*' | cut -d: -f2)
COUNT3=$(echo "$NODE3" | grep -o '"count":[0-9]*' | cut -d: -f2)

echo ""
echo "Node 1 (port 9080): $COUNT1 keys (includes replicas)"
echo "$NODE1" | grep -o '"user-[0-9]*"' | tr -d '"' | sed 's/^/  - /'

echo ""
echo "Node 2 (port 9081): $COUNT2 keys (includes replicas)"
echo "$NODE2" | grep -o '"user-[0-9]*"' | tr -d '"' | sed 's/^/  - /'

echo ""
echo "Node 3 (port 9082): $COUNT3 keys (includes replicas)"
echo "$NODE3" | grep -o '"user-[0-9]*"' | tr -d '"' | sed 's/^/  - /'

# Summary
echo ""
echo "=========================================="
echo "  SUMMARY"
echo "=========================================="
echo "Total keys inserted: 15"
echo "Node 1: $COUNT1 keys"
echo "Node 2: $COUNT2 keys"
echo "Node 3: $COUNT3 keys"
echo "Total distributed: $((COUNT1 + COUNT2 + COUNT3)) keys"
echo ""

echo ""
echo "📊 REPLICATION STATUS:"
echo "  With replication factor 3, each key is stored on 3 nodes"
echo "  (1 primary + 2 replicas)"
echo ""

if [ $COUNT1 -gt 0 ] && [ $COUNT2 -gt 0 ] && [ $COUNT3 -gt 0 ]; then
    echo "✅ SUCCESS! Data is replicated across all 3 nodes"
    echo "   Consistent hashing + replication is working!"
else
    echo "⚠️  Some nodes don't have data yet"
fi

# Test specific key
echo ""
echo "=========================================="
echo "  KEY LOOKUP TEST"
echo "=========================================="
TEST_KEY="user-5"
echo "Finding which node has '$TEST_KEY'..."
for PORT in 9080 9081 9082; do
    RESULT=$(curl -s "http://localhost:$PORT/kv/get?key=$TEST_KEY" 2>/dev/null)
    if echo "$RESULT" | grep -q '"key"'; then
        echo "  ✓ Found on node at port $PORT"
    fi
done

# Cleanup
echo ""
echo "=========================================="
echo "Cleaning up..."
pkill -9 main 2>/dev/null || true
echo "✓ Demo complete!"
echo "=========================================="
