#!/bin/bash

# ClusterKit Quick Start Demo
# Demonstrates a 3-node cluster with automatic rebalancing

set -e

echo "=========================================="
echo "  ClusterKit Quick Start Demo"
echo "=========================================="
echo ""

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    pkill -f "go run.*node-" 2>/dev/null || true
    rm -rf /tmp/clusterkit-demo-* 2>/dev/null || true
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

# Set trap to cleanup on exit
trap cleanup EXIT

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    exit 1
fi

echo -e "${BLUE}Step 1: Starting bootstrap node (node-1)${NC}"
echo "  Port: 8080"
echo "  Data: /tmp/clusterkit-demo-node-1"
echo ""

cd example/sync
NODE_ID=node-1 \
HTTP_PORT=8080 \
RAFT_PORT=9080 \
KV_PORT=10080 \
DATA_DIR=/tmp/clusterkit-demo-node-1 \
go run . > /tmp/clusterkit-demo-node-1.log 2>&1 &

sleep 3
echo -e "${GREEN}✓ Node-1 started (bootstrap)${NC}"
echo ""

echo -e "${BLUE}Step 2: Starting node-2${NC}"
echo "  Port: 8081"
echo "  Joining: localhost:8080"
echo ""

NODE_ID=node-2 \
HTTP_PORT=8081 \
RAFT_PORT=9081 \
KV_PORT=10081 \
JOIN_ADDR=localhost:8080 \
DATA_DIR=/tmp/clusterkit-demo-node-2 \
go run . > /tmp/clusterkit-demo-node-2.log 2>&1 &

sleep 3
echo -e "${GREEN}✓ Node-2 joined cluster${NC}"
echo ""

echo -e "${BLUE}Step 3: Starting node-3${NC}"
echo "  Port: 8082"
echo "  Joining: localhost:8080"
echo ""

NODE_ID=node-3 \
HTTP_PORT=8082 \
RAFT_PORT=9082 \
KV_PORT=10082 \
JOIN_ADDR=localhost:8080 \
DATA_DIR=/tmp/clusterkit-demo-node-3 \
go run . > /tmp/clusterkit-demo-node-3.log 2>&1 &

sleep 3
echo -e "${GREEN}✓ Node-3 joined cluster${NC}"
echo ""

echo "=========================================="
echo "  Cluster Status"
echo "=========================================="
echo ""

# Get cluster info
curl -s http://localhost:8080/cluster | jq -r '.cluster.nodes | to_entries[] | "  Node: \(.value.id) - IP: \(.value.ip)"'

echo ""
echo "=========================================="
echo "  Testing Operations"
echo "=========================================="
echo ""

echo -e "${BLUE}Writing test data...${NC}"
curl -s -X POST http://localhost:10080/kv/set -d "key=user:123&value=John Doe" > /dev/null
curl -s -X POST http://localhost:10080/kv/set -d "key=user:456&value=Jane Smith" > /dev/null
curl -s -X POST http://localhost:10080/kv/set -d "key=product:789&value=Laptop" > /dev/null
echo -e "${GREEN}✓ Wrote 3 keys${NC}"
echo ""

echo -e "${BLUE}Reading data from different nodes...${NC}"
VALUE1=$(curl -s "http://localhost:10080/kv/get?key=user:123" | jq -r '.value')
VALUE2=$(curl -s "http://localhost:10081/kv/get?key=user:456" | jq -r '.value')
VALUE3=$(curl -s "http://localhost:10082/kv/get?key=product:789" | jq -r '.value')

echo "  user:123 = $VALUE1"
echo "  user:456 = $VALUE2"
echo "  product:789 = $VALUE3"
echo -e "${GREEN}✓ Read successful from all nodes${NC}"
echo ""

echo "=========================================="
echo "  Node Statistics"
echo "=========================================="
echo ""

for port in 10080 10081 10082; do
    STATS=$(curl -s "http://localhost:$port/kv/stats")
    NODE=$(echo $STATS | jq -r '.node_id')
    KEYS=$(echo $STATS | jq -r '.keys')
    echo "  $NODE: $KEYS keys"
done

echo ""
echo "=========================================="
echo "  Demo Complete!"
echo "=========================================="
echo ""
echo "Cluster is running with 3 nodes:"
echo "  • ClusterKit API: http://localhost:8080"
echo "  • KV Store Node-1: http://localhost:10080"
echo "  • KV Store Node-2: http://localhost:10081"
echo "  • KV Store Node-3: http://localhost:10082"
echo ""
echo "Try these commands:"
echo "  # View cluster state"
echo "  curl http://localhost:8080/cluster | jq"
echo ""
echo "  # Write data"
echo "  curl -X POST http://localhost:10080/kv/set -d 'key=test&value=hello'"
echo ""
echo "  # Read data"
echo "  curl http://localhost:10080/kv/get?key=test"
echo ""
echo "  # View node stats"
echo "  curl http://localhost:10080/kv/stats | jq"
echo ""
echo "Logs are in:"
echo "  /tmp/clusterkit-demo-node-*.log"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop the cluster${NC}"
echo ""

# Keep running
wait
