#!/bin/bash

# Script to run a 3-node cluster for testing

echo "Starting ClusterKit 3-Node Cluster..."
echo "======================================="

# Clean up old data
rm -rf ./data

# Start Node 1 (Bootstrap node - first in cluster)
echo "Starting Node 1 on :8080 (Bootstrap)..."
NODE_ID=node-1 NODE_NAME=Server-1 HTTP_ADDR=:8080 RAFT_ADDR=127.0.0.1:9001 BOOTSTRAP=true DATA_DIR=./data/node1 go run main.go &
NODE1_PID=$!
sleep 3

# Start Node 2 (Joins Node 1)
echo "Starting Node 2 on :8081 (Joining cluster)..."
NODE_ID=node-2 NODE_NAME=Server-2 HTTP_ADDR=:8081 RAFT_ADDR=127.0.0.1:9002 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node2 go run main.go &
NODE2_PID=$!
sleep 2

# Start Node 3 (Joins the cluster)
echo "Starting Node 3 on :8082 (Joining cluster)..."
NODE_ID=node-3 NODE_NAME=Server-3 HTTP_ADDR=:8082 RAFT_ADDR=127.0.0.1:9003 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node3 go run main.go &
NODE3_PID=$!

echo ""
echo "Cluster started successfully!"
echo "Node 1: http://localhost:8080"
echo "Node 2: http://localhost:8081"
echo "Node 3: http://localhost:8082"
echo ""
echo "Press Ctrl+C to stop all nodes..."

# Trap Ctrl+C and kill all nodes
trap "echo 'Stopping cluster...'; kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null; exit" INT

# Wait for all background processes
wait
