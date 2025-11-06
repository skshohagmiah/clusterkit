#!/bin/bash

# Start 3 nodes for debugging
echo "Starting 3-node cluster..."
NODE_ID=node-1 HTTP_PORT=8080 KV_PORT=9080 BOOTSTRAP=true PARTITION_COUNT=64 REPLICATION_FACTOR=3 go run stress-test-node.go > /tmp/debug-node1.log 2>&1 &
sleep 3

NODE_ID=node-2 HTTP_PORT=8082 KV_PORT=9082 JOIN_ADDR=localhost:8080 PARTITION_COUNT=64 REPLICATION_FACTOR=3 go run stress-test-node.go > /tmp/debug-node2.log 2>&1 &
NODE_ID=node-3 HTTP_PORT=8083 KV_PORT=9083 JOIN_ADDR=localhost:8080 PARTITION_COUNT=64 REPLICATION_FACTOR=3 go run stress-test-node.go > /tmp/debug-node3.log 2>&1 &

sleep 10

echo "Checking readiness..."
for port in 8080 8082 8083; do
    echo -n "Port $port: "
    curl -s http://localhost:$port/ready | jq -r '.ready,.nodes,.valid_partitions' | tr '\n' ' '
    echo ""
done

echo ""
echo "Testing 20 writes to node 1 (port 9080)..."
SUCCESS=0
FAIL=0

for i in {1..20}; do
    RESP=$(curl -s -X POST http://localhost:9080/kv/set -H "Content-Type: application/json" -d "{\"key\":\"test-$i\",\"value\":\"v$i\"}" -w "%{http_code}" -o /tmp/debug-resp.json 2>/dev/null)
    
    if [ "$RESP" = "200" ]; then
        ((SUCCESS++))
        BODY=$(cat /tmp/debug-resp.json)
        ROLE=$(echo "$BODY" | jq -r '.role // "forwarded"')
        echo "  Write $i: SUCCESS ($ROLE)"
    else
        ((FAIL++))
        echo "  Write $i: FAILED (HTTP $RESP) - $(cat /tmp/debug-resp.json)"
    fi
done

echo ""
echo "Results: $SUCCESS success, $FAIL failed"

echo ""
echo "Checking data distribution..."
for port in 9080 9082 9083; do
    echo -n "Port $port: "
    curl -s http://localhost:$port/kv/stats | jq -r '.local_keys'
done

echo ""
echo "Checking Node 1 details..."
curl -s http://localhost:9080/kv/stats | jq .

echo ""
read -p "Stop nodes? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    pkill -f "stress-test-node"
    echo "Nodes stopped"
fi
