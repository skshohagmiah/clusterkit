#!/bin/bash

# Distributed KV Store Demo
# Shows automatic failover and partition rebalancing

NUM_NODES=${1:-5}
PARTITIONS=${2:-64}
REPLICATION=${3:-3}

echo "=========================================="
echo "  Distributed KV Store Demo"
echo "=========================================="
echo "  • Nodes: $NUM_NODES"
echo "  • Partitions: $PARTITIONS"
echo "  • Replication: $REPLICATION"
echo "=========================================="
echo ""

# Cleanup
echo "Cleaning up..."
pkill -9 -f "go run.*server.go" 2>/dev/null || true
pkill -9 -f "go run.*client.go" 2>/dev/null || true
rm -rf clusterkit-data /tmp/ck-node* 2>/dev/null || true
lsof -ti:9001,9002,9003,9004,9005,9006,9007 | xargs kill -9 2>/dev/null || true
sleep 3

# Start nodes
echo ""
echo "Starting $NUM_NODES nodes..."

# Bootstrap node
mkdir -p /tmp/ck-node1
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 BOOTSTRAP=true \
PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
DATA_DIR=/tmp/ck-node1 \
go run server.go > /tmp/kv-node1.log 2>&1 &

sleep 3
echo "  ✓ Node 1 (bootstrap)"

# Other nodes
for i in $(seq 2 $NUM_NODES); do
    mkdir -p /tmp/ck-node$i
    NODE_ID=node-$i HTTP_PORT=$((8080+i)) KV_PORT=$((9080+i)) \
    JOIN_ADDR=localhost:8080 PARTITION_COUNT=$PARTITIONS \
    REPLICATION_FACTOR=$REPLICATION \
    DATA_DIR=/tmp/ck-node$i \
    go run server.go > /tmp/kv-node$i.log 2>&1 &
    
    sleep 1
    echo "  ✓ Node $i"
done

echo ""
echo "Waiting for cluster to form (10s)..."
sleep 10

# Check cluster
NODES=$(curl -s http://localhost:8080/cluster 2>/dev/null | jq -r '.cluster.nodes | length' 2>/dev/null || echo "0")
echo "✓ Cluster ready: $NODES nodes"

# Show partition distribution
echo ""
echo "Initial Partition Distribution:"
curl -s http://localhost:8080/partitions/stats 2>/dev/null | \
    jq -r '.partitions_per_node | to_entries | sort_by(.key) | .[] | "  \(.key): \(.value) primary partitions"' 2>/dev/null
echo ""
echo "Detailed partition map (first 10 partitions):"
curl -s http://localhost:8080/partitions 2>/dev/null | \
    jq -r '.partitions[:10] | .[] | "  \(.id): primary=\(.primary_node), replicas=[\(.replica_nodes | join(", "))]"' 2>/dev/null

# Run test client
echo ""
echo "=========================================="
echo "  Running Test Client"
echo "=========================================="
echo ""

go run client.go > /tmp/kv-client.log 2>&1 &
CLIENT_PID=$!

sleep 15

# Kill node 3
echo ""
echo "🔴 Killing node-3 (simulating failure)..."
pkill -f "NODE_ID=node-3"
sleep 2
echo "✓ Node 3 killed"

echo ""
echo "Waiting for rebalancing (15s)..."
sleep 15

# Show updated cluster
NODES=$(curl -s http://localhost:8080/cluster 2>/dev/null | jq -r '.cluster.nodes | length' 2>/dev/null || echo "0")
echo "Cluster now has $NODES nodes"

echo ""
echo "Updated Partition Distribution (after node-3 failure):"
curl -s http://localhost:8080/partitions/stats 2>/dev/null | \
    jq -r '.partitions_per_node | to_entries | sort_by(.key) | .[] | "  \(.key): \(.value) primary partitions"' 2>/dev/null
echo ""
echo "Partitions that moved from node-3 (sample):"
curl -s http://localhost:8080/partitions 2>/dev/null | \
    jq -r '[.partitions[] | select(.primary_node != "node-3" and (.replica_nodes | contains(["node-3"])))] | .[:5] | .[] | "  \(.id): primary=\(.primary_node) (node-3 was replica)"' 2>/dev/null

# Add node 6
echo ""
echo "🟢 Adding node-6 (simulating scale-up)..."
mkdir -p /tmp/ck-node6
NODE_ID=node-6 HTTP_PORT=8086 KV_PORT=9086 JOIN_ADDR=localhost:8080 \
PARTITION_COUNT=$PARTITIONS REPLICATION_FACTOR=$REPLICATION \
DATA_DIR=/tmp/ck-node6 \
go run server.go > /tmp/kv-node6.log 2>&1 &

sleep 5
echo "✓ Node 6 added"

echo ""
echo "Waiting for rebalancing (15s)..."
sleep 15

# Show final cluster
NODES=$(curl -s http://localhost:8080/cluster 2>/dev/null | jq -r '.cluster.nodes | length' 2>/dev/null || echo "0")
echo "Cluster now has $NODES nodes"

echo ""
echo "Final Partition Distribution (after node-6 added):"
curl -s http://localhost:8080/partitions/stats 2>/dev/null | \
    jq -r '.partitions_per_node | to_entries | sort_by(.key) | .[] | "  \(.key): \(.value) primary partitions"' 2>/dev/null
echo ""
echo "Partitions assigned to new node-6 (sample):"
curl -s http://localhost:8080/partitions 2>/dev/null | \
    jq -r '[.partitions[] | select(.primary_node == "node-6" or (.replica_nodes | contains(["node-6"])))] | .[:5] | .[] | "  \(.id): primary=\(.primary_node), replicas=[\(.replica_nodes | join(", "))]"' 2>/dev/null

# Wait for client
echo ""
echo "Waiting for client to finish (20s)..."
sleep 20

wait $CLIENT_PID 2>/dev/null || true

# Show results
echo ""
echo "=========================================="
echo "  Test Results"
echo "=========================================="
tail -50 /tmp/kv-client.log

# Cleanup
echo ""
read -p "Stop all nodes? (y/n) " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    pkill -f "go run.*server.go"
    echo "✓ All nodes stopped"
fi

echo ""
echo "Logs: /tmp/kv-node*.log, /tmp/kv-client.log"
echo ""
echo "=========================================="
echo "  Demo Complete! 🚀"
echo "=========================================="
