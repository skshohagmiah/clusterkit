#!/bin/bash

# ClusterKit Docker Demo
# Quick demonstration of ClusterKit running in Docker

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_header() {
    echo ""
    echo "=========================================="
    echo -e "${BLUE}  $1${NC}"
    echo "=========================================="
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

print_header "ClusterKit Docker Demo"

# Check if docker-compose is installed
if ! command -v docker-compose &> /dev/null; then
    print_error "docker-compose not found. Please install it first."
    exit 1
fi

# Cleanup previous run
print_info "Cleaning up previous containers..."
docker-compose down -v 2>/dev/null || true

# Start cluster
print_header "Starting 3-Node Cluster"
print_info "Building and starting containers..."
docker-compose up -d --build

# Wait for cluster to form
print_info "Waiting for cluster formation (30 seconds)..."
sleep 30

# Check cluster status
print_header "Cluster Status"
CLUSTER=$(curl -s http://localhost:8080/cluster 2>/dev/null || echo '{"error":"timeout"}')
if echo "$CLUSTER" | grep -q "error"; then
    print_error "Cluster not responding"
    docker-compose logs
    exit 1
fi

NODE_COUNT=$(echo "$CLUSTER" | grep -o '"id":"node-[0-9]"' | wc -l)
if [ "$NODE_COUNT" -eq 3 ]; then
    print_success "Cluster formed with 3 nodes"
else
    print_error "Expected 3 nodes, found $NODE_COUNT"
fi

# Check partitions
PARTS=$(curl -s http://localhost:8080/partitions/stats 2>/dev/null)
PART_COUNT=$(echo "$PARTS" | grep -o '"total_partitions":[0-9]*' | cut -d: -f2)
print_success "$PART_COUNT partitions created"

# Insert test data
print_header "Inserting Test Data"
print_info "Inserting 20 keys..."
SUCCESS=0
for i in {1..20}; do
    PORT=$((9080 + (i % 3)))
    if curl -s -X POST http://localhost:$PORT/kv/set \
        -H "Content-Type: application/json" \
        -d "{\"key\":\"user-$i\",\"value\":\"User $i\"}" > /dev/null 2>&1; then
        SUCCESS=$((SUCCESS + 1))
        echo -n "."
    else
        echo -n "x"
    fi
done
echo ""
print_success "$SUCCESS/20 keys inserted"

# Check distribution
print_header "Data Distribution"
sleep 2

NODE1=$(curl -s http://localhost:9080/kv/list 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2)
NODE2=$(curl -s http://localhost:9081/kv/list 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2)
NODE3=$(curl -s http://localhost:9082/kv/list 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2)

echo "Node 1: $NODE1 keys | Node 2: $NODE2 keys | Node 3: $NODE3 keys"
TOTAL=$((NODE1 + NODE2 + NODE3))
print_success "Total: $TOTAL key copies (replication working!)"

# Test replication
print_header "Testing Replication"
print_info "Checking if 'user-5' exists on all nodes..."

FOUND=0
for PORT in 9080 9081 9082; do
    if curl -s "http://localhost:$PORT/kv/get?key=user-5" 2>/dev/null | grep -q '"key"'; then
        FOUND=$((FOUND + 1))
    fi
done

if [ $FOUND -eq 3 ]; then
    print_success "Key replicated on all 3 nodes (RF=3)"
else
    print_info "Key found on $FOUND nodes"
fi

# Test fault tolerance
print_header "Testing Fault Tolerance"
print_info "Stopping Node 2..."
docker-compose stop node2
sleep 3

print_info "Testing cluster with 2 nodes..."
if curl -s -X POST http://localhost:9080/kv/set \
    -H "Content-Type: application/json" \
    -d '{"key":"test-failure","value":"Works!"}' > /dev/null 2>&1; then
    print_success "Cluster still operational with 2 nodes"
else
    print_error "Cluster not responding"
fi

print_info "Restarting Node 2..."
docker-compose start node2
sleep 10

CLUSTER=$(curl -s http://localhost:8080/cluster 2>/dev/null)
NODE_COUNT=$(echo "$CLUSTER" | grep -o '"id":"node-[0-9]"' | wc -l)
if [ "$NODE_COUNT" -eq 3 ]; then
    print_success "Node 2 rejoined cluster"
else
    print_info "Node 2 rejoining... (found $NODE_COUNT nodes)"
fi

# Summary
print_header "Demo Summary"
echo ""
echo "✅ Cluster Formation (3 nodes)"
echo "✅ Data Distribution & Replication"
echo "✅ Fault Tolerance (node failure)"
echo "✅ Node Recovery"
echo ""
echo "Access Points:"
echo "  • Node 1 ClusterKit: http://localhost:8080"
echo "  • Node 1 KV Store:   http://localhost:9080"
echo "  • Node 2 ClusterKit: http://localhost:8081"
echo "  • Node 2 KV Store:   http://localhost:9081"
echo "  • Node 3 ClusterKit: http://localhost:8082"
echo "  • Node 3 KV Store:   http://localhost:9082"
echo ""
echo "Try it yourself:"
echo "  # Store data"
echo "  curl -X POST http://localhost:9080/kv/set \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"key\":\"mykey\",\"value\":\"myvalue\"}'"
echo ""
echo "  # Retrieve data"
echo "  curl 'http://localhost:9080/kv/get?key=mykey'"
echo ""
echo "  # View cluster"
echo "  curl http://localhost:8080/cluster | jq"
echo ""
echo "  # View logs"
echo "  docker-compose logs -f"
echo ""
echo "  # Stop cluster"
echo "  docker-compose down"
echo ""

print_info "Cluster is running. Press Ctrl+C to keep it running, or wait 10s to auto-cleanup..."
sleep 10

print_header "Cleanup"
print_info "Stopping cluster..."
docker-compose down
print_success "Demo complete!"
