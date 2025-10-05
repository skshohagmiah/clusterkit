# ClusterKit Setup Guide

This guide will help you get ClusterKit up and running quickly.

## Prerequisites

- **Go 1.22+** - [Download Go](https://golang.org/dl/)
- **etcd 3.5+** - For cluster coordination
- **Docker** (optional) - For containerized deployment

## Quick Start

### 1. Clone and Build

```bash
# Clone the repository
git clone https://github.com/shohag/clusterkit.git
cd clusterkit

# Install dependencies and build
make deps
make build
```

### 2. Start etcd

#### Option A: Using Docker
```bash
make start-etcd
```

#### Option B: Using Docker Compose
```bash
docker-compose up etcd -d
```

#### Option C: Manual Installation
```bash
# Download etcd
wget https://github.com/etcd-io/etcd/releases/download/v3.5.10/etcd-v3.5.10-linux-amd64.tar.gz
tar xzf etcd-v3.5.10-linux-amd64.tar.gz
cd etcd-v3.5.10-linux-amd64

# Start etcd
./etcd --data-dir=/tmp/etcd-data \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-peer-urls http://0.0.0.0:2380 \
  --initial-advertise-peer-urls http://0.0.0.0:2380 \
  --initial-cluster default=http://0.0.0.0:2380 \
  --initial-cluster-token tkn \
  --initial-cluster-state new
```

### 3. Verify etcd is Running

```bash
make check-etcd
# or
curl http://localhost:2379/health
```

## Running Examples

### Simple Example

```bash
# Build and run the simple example
make run-simple
```

This will:
- Join a cluster as a single node
- Show partition assignments
- Demonstrate key routing
- Run for 10 seconds and exit

### KV Store Example

#### Single Node
```bash
# Terminal 1: Build and run KV store
make run-kv
```

#### Multi-Node Cluster
```bash
# Terminal 1: Node 1
NODE_ID=kv-node-1 ADVERTISE_ADDR=localhost:9001 HTTP_PORT=8081 GRPC_PORT=9001 go run examples/kv-store/main.go

# Terminal 2: Node 2  
NODE_ID=kv-node-2 ADVERTISE_ADDR=localhost:9002 HTTP_PORT=8082 GRPC_PORT=9002 go run examples/kv-store/main.go

# Terminal 3: Node 3
NODE_ID=kv-node-3 ADVERTISE_ADDR=localhost:9003 HTTP_PORT=8083 GRPC_PORT=9003 go run examples/kv-store/main.go
```

#### Using Docker Compose
```bash
# Start the entire cluster
docker-compose up -d

# Check logs
docker-compose logs -f kv-node-1
```

## Testing the KV Store

### Basic Operations

```bash
# Set a key-value pair
curl -X POST "http://localhost:8081/set?key=user:1&value=john"

# Get a value
curl "http://localhost:8081/get?key=user:1"

# Delete a key
curl -X DELETE "http://localhost:8081/delete?key=user:1"
```

### Health Check

```bash
curl "http://localhost:8081/health"
```

### Cluster Statistics

```bash
curl "http://localhost:8081/stats"
```

### Dump Partition Data

```bash
curl "http://localhost:8081/dump?partition=0"
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NODE_ID` | Unique node identifier | `kv-node-1` |
| `ADVERTISE_ADDR` | Address other nodes connect to | `localhost:9000` |
| `HTTP_PORT` | HTTP API port | `8080` |
| `GRPC_PORT` | gRPC port | `9000` |
| `ETCD_ENDPOINTS` | etcd endpoints (comma-separated) | `http://localhost:2379` |

## Development

### Building

```bash
# Build everything
make build

# Build examples only
make examples

# Format and vet code
make fmt vet
```

### Testing

```bash
# Run tests
make test

# Run with coverage
go test -cover ./...
```

### Linting

```bash
# Install golangci-lint first
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
make lint
```

## Architecture Overview

```
┌─────────────────────────────────┐
│         etcd Cluster            │
│  (Metadata, Membership, Leaders) │
└─────────────────────────────────┘
                 ▲
  Control Plane  │  <100 ops/sec
                 │
     ┌───────────┴───────────┐
     │                       │
     ▼                       ▼
┌─────────────┐         ┌─────────────┐
│   Node 1    │◄───────►│   Node 2    │
│ + ClusterKit│         │ + ClusterKit│
│             │         │             │
│ Partitions: │         │ Partitions: │
│  0 (Leader) │         │  1 (Leader) │
│  1 (Replica)│         │  0 (Replica)│
└─────────────┘         └─────────────┘
```

## Troubleshooting

### etcd Connection Issues

```bash
# Check if etcd is running
make check-etcd

# Check etcd logs
docker logs clusterkit-etcd

# Restart etcd
make stop-etcd
make start-etcd
```

### Node Discovery Issues

```bash
# Check etcd for registered nodes
etcdctl --endpoints=http://localhost:2379 get /clusterkit/nodes --prefix

# Watch for node registrations
etcdctl --endpoints=http://localhost:2379 watch /clusterkit/nodes --prefix
```

### Partition Assignment Issues

```bash
# Check partition assignments
etcdctl --endpoints=http://localhost:2379 get /clusterkit/partitions --prefix

# Check node logs for rebalancing messages
```

## Cleanup

```bash
# Stop all services
docker-compose down -v

# Or stop etcd only
make stop-etcd

# Clean build artifacts
make clean
```

## Next Steps

1. **Read the [API Documentation](README.md#-api-reference)**
2. **Check out [Best Practices](README.md#-best-practices)**
3. **Explore [Configuration Options](README.md#%EF%B8%8F-configuration)**
4. **Build your own distributed application!**

## Support

- **Documentation**: See [README.md](README.md)
- **Issues**: Report bugs and feature requests
- **Examples**: Check the `examples/` directory

Happy clustering! 🚀
