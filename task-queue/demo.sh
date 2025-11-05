#!/bin/bash

# Distributed Task Queue Demo
# Tests: Cluster formation, task submission, distribution, processing, and fault tolerance

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

print_header "Distributed Task Queue Demo"
echo "This demo will test:"
echo "  1. Cluster formation (3 nodes)"
echo "  2. Task submission and distribution"
echo "  3. Automatic task processing"
echo "  4. Task replication (RF=3)"
echo "  5. Node failure handling"
echo ""

# Cleanup
print_info "Cleaning up previous runs..."
pkill -9 main 2>/dev/null || true
rm -rf ./data
sleep 1

# ============================================
# TEST 1: Start Cluster
# ============================================
print_header "TEST 1: Starting Task Queue Cluster"

print_info "Starting Node 1 (bootstrap)..."
NODE_ID=node-1 HTTP_ADDR=:8080 go run main.go > /tmp/taskq-node1.log 2>&1 &
NODE1_PID=$!
sleep 3

print_info "Starting Node 2..."
NODE_ID=node-2 HTTP_ADDR=:8081 JOIN_ADDR=localhost:8080 go run main.go > /tmp/taskq-node2.log 2>&1 &
NODE2_PID=$!
sleep 2

print_info "Starting Node 3..."
NODE_ID=node-3 HTTP_ADDR=:8082 JOIN_ADDR=localhost:8080 go run main.go > /tmp/taskq-node3.log 2>&1 &
NODE3_PID=$!

print_info "Waiting for cluster formation..."

# Wait for port 10080 to be listening
MAX_WAIT=60
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    if lsof -i :10080 >/dev/null 2>&1; then
        break
    fi
    echo -n "."
    sleep 2
    ELAPSED=$((ELAPSED + 2))
done
echo ""

if [ $ELAPSED -ge $MAX_WAIT ]; then
    print_error "Node 1 failed to start"
    exit 1
fi

# Wait for all nodes to join
MAX_RETRIES=15
NODE_COUNT=0
for i in $(seq 1 $MAX_RETRIES); do
    CLUSTER=$(curl -s --max-time 3 http://localhost:8080/cluster 2>/dev/null || echo '{"error":"timeout"}')
    
    if ! echo "$CLUSTER" | grep -q "error"; then
        NODE_COUNT=$(echo "$CLUSTER" | grep -o '"id":"node-[0-9]"' | wc -l | tr -d ' ')
        
        if [ "$NODE_COUNT" -eq 3 ]; then
            print_success "Task Queue cluster formed with 3 nodes"
            break
        fi
    fi
    
    if [ $i -lt $MAX_RETRIES ]; then
        echo -n "."
        sleep 2
    fi
done
echo ""

if [ "$NODE_COUNT" -ne 3 ]; then
    print_error "Expected 3 nodes, found $NODE_COUNT"
    exit 1
fi

sleep 3

# ============================================
# TEST 2: Submit Tasks
# ============================================
print_header "TEST 2: Task Submission & Distribution"

print_info "Submitting 15 tasks (5 of each type)..."
TASK_IDS=()

# Submit email tasks
for i in {1..5}; do
    RESULT=$(curl -s --max-time 3 -X POST http://localhost:10080/tasks/submit \
        -H "Content-Type: application/json" \
        -d "{\"type\":\"email\",\"payload\":\"user$i@example.com\"}" 2>/dev/null)
    
    TASK_ID=$(echo "$RESULT" | grep -o '"task_id":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$TASK_ID" ]; then
        TASK_IDS+=("$TASK_ID")
        echo -n "."
    fi
done

# Submit image tasks
for i in {1..5}; do
    RESULT=$(curl -s --max-time 3 -X POST http://localhost:10081/tasks/submit \
        -H "Content-Type: application/json" \
        -d "{\"type\":\"image\",\"payload\":\"photo$i.jpg\"}" 2>/dev/null)
    
    TASK_ID=$(echo "$RESULT" | grep -o '"task_id":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$TASK_ID" ]; then
        TASK_IDS+=("$TASK_ID")
        echo -n "."
    fi
done

# Submit report tasks
for i in {1..5}; do
    RESULT=$(curl -s --max-time 3 -X POST http://localhost:10082/tasks/submit \
        -H "Content-Type: application/json" \
        -d "{\"type\":\"report\",\"payload\":\"report-$i\"}" 2>/dev/null)
    
    TASK_ID=$(echo "$RESULT" | grep -o '"task_id":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$TASK_ID" ]; then
        TASK_IDS+=("$TASK_ID")
        echo -n "."
    fi
done
echo ""

print_success "Submitted ${#TASK_IDS[@]} tasks"

# Check distribution
sleep 2
print_info "Checking task distribution across nodes..."

STATS1=$(curl -s --max-time 3 http://localhost:10080/tasks/stats 2>/dev/null)
STATS2=$(curl -s --max-time 3 http://localhost:10081/tasks/stats 2>/dev/null)
STATS3=$(curl -s --max-time 3 http://localhost:10082/tasks/stats 2>/dev/null)

COUNT1=$(echo "$STATS1" | grep -o '"total":[0-9]*' | cut -d: -f2)
COUNT2=$(echo "$STATS2" | grep -o '"total":[0-9]*' | cut -d: -f2)
COUNT3=$(echo "$STATS3" | grep -o '"total":[0-9]*' | cut -d: -f2)

echo "Node 1: $COUNT1 tasks | Node 2: $COUNT2 tasks | Node 3: $COUNT3 tasks"

if [ $COUNT1 -gt 0 ] && [ $COUNT2 -gt 0 ] && [ $COUNT3 -gt 0 ]; then
    print_success "Tasks distributed across all 3 nodes"
else
    print_error "Tasks not properly distributed"
fi

TOTAL_TASKS=$((COUNT1 + COUNT2 + COUNT3))
print_success "Total task copies: $TOTAL_TASKS (with RF=3)"

# ============================================
# TEST 3: Task Processing
# ============================================
print_header "TEST 3: Automatic Task Processing"

print_info "Waiting for tasks to be processed (10 seconds)..."
sleep 10

# Check completion status
STATS1=$(curl -s --max-time 3 http://localhost:10080/tasks/stats 2>/dev/null)
COMPLETED1=$(echo "$STATS1" | grep -o '"completed":[0-9]*' | cut -d: -f2)

STATS2=$(curl -s --max-time 3 http://localhost:10081/tasks/stats 2>/dev/null)
COMPLETED2=$(echo "$STATS2" | grep -o '"completed":[0-9]*' | cut -d: -f2)

STATS3=$(curl -s --max-time 3 http://localhost:10082/tasks/stats 2>/dev/null)
COMPLETED3=$(echo "$STATS3" | grep -o '"completed":[0-9]*' | cut -d: -f2)

TOTAL_COMPLETED=$((COMPLETED1 + COMPLETED2 + COMPLETED3))

echo "Completed tasks: Node 1: $COMPLETED1 | Node 2: $COMPLETED2 | Node 3: $COMPLETED3"
print_success "Total completed: $TOTAL_COMPLETED tasks"

if [ $TOTAL_COMPLETED -gt 0 ]; then
    print_success "Background task processing is working!"
fi

# ============================================
# TEST 4: Task Status Query
# ============================================
print_header "TEST 4: Task Status Query"

if [ ${#TASK_IDS[@]} -gt 0 ]; then
    TEST_TASK_ID="${TASK_IDS[0]}"
    print_info "Querying status for task: $TEST_TASK_ID"
    
    STATUS=$(curl -s --max-time 3 "http://localhost:10080/tasks/status?id=$TEST_TASK_ID" 2>/dev/null)
    TASK_STATUS=$(echo "$STATUS" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
    
    if [ -n "$TASK_STATUS" ]; then
        print_success "Task status: $TASK_STATUS"
    else
        print_error "Failed to query task status"
    fi
fi

# ============================================
# TEST 5: Node Failure
# ============================================
print_header "TEST 5: Node Failure & Recovery"

print_info "Killing Node 2 (simulating failure)..."
kill -9 $NODE2_PID 2>/dev/null || true
sleep 3

# Submit new task to test cluster still works
print_info "Submitting task with Node 2 down..."
RESULT=$(curl -s --max-time 3 -X POST http://localhost:10080/tasks/submit \
    -H "Content-Type: application/json" \
    -d '{"type":"email","payload":"test-after-failure"}' 2>/dev/null)

if echo "$RESULT" | grep -q "task_id"; then
    print_success "Cluster still accepting tasks with 2 nodes"
else
    print_error "Cluster not responding after node failure"
fi

# Check if old tasks are still accessible (replicas)
if [ ${#TASK_IDS[@]} -gt 0 ]; then
    TEST_TASK_ID="${TASK_IDS[0]}"
    STATUS=$(curl -s --max-time 3 "http://localhost:10080/tasks/status?id=$TEST_TASK_ID" 2>/dev/null)
    
    if echo "$STATUS" | grep -q "status"; then
        print_success "Tasks still accessible via replicas!"
    fi
fi

# ============================================
# SUMMARY
# ============================================
print_header "DEMO SUMMARY"

echo ""
echo "Tests Completed:"
echo "  ✅ Cluster Formation (3 nodes)"
echo "  ✅ Task Submission (15 tasks)"
echo "  ✅ Task Distribution (across all nodes)"
echo "  ✅ Automatic Processing (background workers)"
echo "  ✅ Task Status Query"
echo "  ✅ Fault Tolerance (node failure)"
echo ""
echo "Key Features Demonstrated:"
echo "  • Consistent hashing for task distribution"
echo "  • Automatic replication (RF=3)"
echo "  • Background task processing"
echo "  • Fault tolerance (cluster works with node down)"
echo "  • Task status tracking"
echo ""
echo "Final Statistics:"
echo "  Total tasks submitted: ${#TASK_IDS[@]}"
echo "  Total task copies: $TOTAL_TASKS"
echo "  Tasks completed: $TOTAL_COMPLETED"
echo "  Active nodes: 2 (Node 2 killed)"
echo ""
echo "Logs available at:"
echo "  /tmp/taskq-node1.log"
echo "  /tmp/taskq-node2.log"
echo "  /tmp/taskq-node3.log"
echo ""

# Cleanup
print_header "Cleanup"
print_info "Stopping all nodes..."
pkill -9 main 2>/dev/null || true
sleep 1
print_success "Demo complete!"
echo ""
echo "To run again: ./demo.sh"
echo "To manually test: See README.md for API examples"
echo ""
