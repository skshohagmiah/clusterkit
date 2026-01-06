#!/bin/bash

# Integration Test Runner - Verifies Data Partitioning and Replication

echo "🧪 ClusterKit Integration Test"
echo "================================"
echo ""
echo "This test verifies:"
echo "  ✅ Cluster formation (3 nodes)"
echo "  ✅ Partition distribution (16 partitions)"
echo "  ✅ Replication factor (RF=3)"
echo "  ✅ Custom data replication"
echo "  ✅ Partition balancing"
echo ""

# Change to project root
cd "$(dirname "$0")/.." || exit 1

# Run integration tests
echo "Running integration tests..."
echo ""

go test -v -run "TestMultiNode" -timeout 60s

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ All integration tests passed!"
    echo ""
    echo "What was verified:"
    echo "  ✓ 3 nodes formed a cluster"
    echo "  ✓ 16 partitions created"
    echo "  ✓ Each partition has 1 primary + 2 replicas"
    echo "  ✓ Keys consistently map to same partition on all nodes"
    echo "  ✓ Custom data replicated to all nodes via Raft"
    echo "  ✓ Partitions evenly distributed across nodes"
else
    echo ""
    echo "❌ Integration tests failed"
    exit 1
fi

echo ""
echo "Run partition balancing test..."
go test -v -run "TestPartitionBalancing" -timeout 10s

echo ""
echo "✅ Testing complete!"
