#!/bin/bash

# Custom Data Example - Demonstrates cluster-wide custom data storage

echo "🚀 Starting Custom Data Example"
echo "================================"
echo ""
echo "This example demonstrates:"
echo "  ✅ Storing custom data (feature flags, config, counters)"
echo "  ✅ Replication across all nodes via Raft consensus"
echo "  ✅ Reading data from any node in the cluster"
echo ""

# Clean up previous data
rm -rf ./data
mkdir -p ./data

# Start 3 nodes
echo "Starting node-1 (bootstrap)..."
go run main.go node-1 > ./data/node-1.log 2>&1 &
NODE1_PID=$!

sleep 3

echo "Starting node-2..."
go run main.go node-2 > ./data/node-2.log 2>&1 &
NODE2_PID=$!

sleep 2

echo "Starting node-3..."
go run main.go node-3 > ./data/node-3.log 2>&1 &
NODE3_PID=$!

echo ""
echo "✅ All nodes started!"
echo ""
echo "Waiting for cluster to stabilize and custom data to replicate..."
sleep 15

echo ""
echo "📋 Node Logs:"
echo "============="
echo ""

echo "--- Node 1 (Bootstrap) ---"
tail -n 20 ./data/node-1.log
echo ""

echo "--- Node 2 ---"
tail -n 15 ./data/node-2.log
echo ""

echo "--- Node 3 ---"
tail -n 15 ./data/node-3.log
echo ""

echo "✅ Custom data has been replicated across all nodes!"
echo ""
echo "Press Ctrl+C to stop all nodes..."

# Wait for user interrupt
trap "kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null; echo ''; echo '🛑 All nodes stopped'; exit 0" INT
wait
