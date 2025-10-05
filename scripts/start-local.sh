#!/bin/bash

# Simple script to start etcd and your app locally

echo "Starting local ClusterKit development environment..."

# Check if etcd is already running
if ! curl -s http://localhost:2379/health > /dev/null 2>&1; then
    echo "Starting etcd..."
    
    # Try Docker first
    if command -v docker &> /dev/null; then
        docker run -d --name clusterkit-etcd \
            -p 2379:2379 -p 2380:2380 \
            quay.io/coreos/etcd:v3.5.10 \
            /usr/local/bin/etcd \
            --listen-client-urls http://0.0.0.0:2379 \
            --advertise-client-urls http://0.0.0.0:2379 \
            --listen-peer-urls http://0.0.0.0:2380 \
            --initial-advertise-peer-urls http://0.0.0.0:2380 \
            --initial-cluster default=http://0.0.0.0:2380 \
            --initial-cluster-token tkn \
            --initial-cluster-state new
        
        echo "Waiting for etcd to start..."
        sleep 3
    else
        echo "Docker not found. Please install Docker or etcd binary."
        exit 1
    fi
else
    echo "etcd already running"
fi

# Verify etcd is healthy
if curl -s http://localhost:2379/health | grep -q "true"; then
    echo "✅ etcd is healthy"
    echo "🚀 You can now run your ClusterKit applications!"
    echo ""
    echo "Examples:"
    echo "  go run examples/simple/main.go"
    echo "  go run examples/kv-store/main.go"
else
    echo "❌ etcd health check failed"
    exit 1
fi
