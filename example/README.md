# Distributed Key-Value Store Example

This example demonstrates how to build a **distributed key-value store** using ClusterKit. It shows how ClusterKit handles cluster coordination, partition management, and data distribution while you focus on your application logic.

## Features

- ✅ **Distributed Storage**: Data is automatically distributed across cluster nodes
- ✅ **Partition-Aware**: Uses consistent hashing to determine which nodes store which keys
- ✅ **RESTful API**: Simple HTTP API for GET, SET, DELETE operations
- ✅ **Cluster Monitoring**: Real-time cluster status and metrics
- ✅ **Automatic Coordination**: ClusterKit handles node discovery and partition assignment

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Distributed KV Store                      │
├─────────────────────────────────────────────────────────────┤
│  Node 1 (Port 9080)   │  Node 2 (Port 9081)  │  Node 3 (Port 9082) │
│  ┌──────────────┐     │  ┌──────────────┐    │  ┌──────────────┐   │
│  │ Local Store  │     │  │ Local Store  │    │  │ Local Store  │   │
│  │ key1: val1   │     │  │ key2: val2   │    │  │ key3: val3   │   │
│  └──────────────┘     │  └──────────────┘    │  └──────────────┘   │
└─────────────────────────────────────────────────────────────┘
                           ▲
                           │
                    ┌──────┴──────┐
                    │  ClusterKit  │
                    │  Coordination │
                    └──────────────┘
```

## Quick Start

### Option 1: Run Locally (3 Nodes)

```bash
# Terminal 1 - Start Node 1 (Bootstrap)
cd example
NODE_ID=node-1 NODE_NAME=Server-1 HTTP_ADDR=:8080 RAFT_ADDR=127.0.0.1:9001 BOOTSTRAP=true DATA_DIR=./data/node1 go run main.go

# Terminal 2 - Start Node 2
NODE_ID=node-2 NODE_NAME=Server-2 HTTP_ADDR=:8081 RAFT_ADDR=127.0.0.1:9002 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node2 go run main.go

# Terminal 3 - Start Node 3
NODE_ID=node-3 NODE_NAME=Server-3 HTTP_ADDR=:8082 RAFT_ADDR=127.0.0.1:9003 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node3 go run main.go
```

### Option 2: Run with Docker Compose

```bash
cd example/docker
docker-compose up
```

## API Usage

The KV store API runs on port **9080** (for node 1), **9081** (for node 2), etc.

### Set a Key-Value Pair

```bash
curl -X POST http://localhost:9080/kv/set \
  -H "Content-Type: application/json" \
  -d '{"key": "user:123", "value": "John Doe"}'
```

**Response:**
```json
{
  "status": "success",
  "key": "user:123"
}
```

### Get a Value

```bash
curl http://localhost:9080/kv/get?key=user:123
```

**Response:**
```json
{
  "key": "user:123",
  "value": "John Doe"
}
```

### Delete a Key

```bash
curl -X POST http://localhost:9080/kv/delete?key=user:123
```

**Response:**
```json
{
  "status": "deleted",
  "key": "user:123"
}
```

### List All Keys (on current node)

```bash
curl http://localhost:9080/kv/list
```

**Response:**
```json
{
  "keys": ["user:123", "user:456", "product:789"],
  "count": 3
}
```

### Get Statistics

```bash
curl http://localhost:9080/kv/stats
```

**Response:**
```json
{
  "local_keys": 5,
  "cluster_nodes": 3,
  "partitions": 16,
  "is_leader": true,
  "raft_state": "Leader",
  "uptime_seconds": 120
}
```

## How It Works

### 1. **Data Distribution**

When you set a key, ClusterKit determines which nodes should store it:

```go
// Get all nodes that should store this key
nodes, err := ck.GetNodesForKey("user:123")
// Returns: [{node-1, :8080}, {node-2, :8081}, {node-3, :8082}]
```

### 2. **Partition-Aware Storage**

Each node checks if it's responsible for a key:

```go
shouldHandle, role, _, err := ck.AmIResponsibleFor("user:123")
if shouldHandle {
    // Store locally as "primary" or "replica"
    localStore[key] = value
}
```

### 3. **Consistent Hashing**

ClusterKit uses consistent hashing to ensure:
- Keys are evenly distributed across nodes
- Minimal data movement when nodes join/leave
- Predictable key-to-node mapping

## Testing the Distribution

Try setting multiple keys and see how they're distributed:

```bash
# Set multiple keys
curl -X POST http://localhost:9080/kv/set -H "Content-Type: application/json" -d '{"key": "user:1", "value": "Alice"}'
curl -X POST http://localhost:9080/kv/set -H "Content-Type: application/json" -d '{"key": "user:2", "value": "Bob"}'
curl -X POST http://localhost:9080/kv/set -H "Content-Type: application/json" -d '{"key": "user:3", "value": "Charlie"}'

# Check which keys each node has
curl http://localhost:9080/kv/list  # Node 1
curl http://localhost:9081/kv/list  # Node 2
curl http://localhost:9082/kv/list  # Node 3
```

You'll see that different keys are stored on different nodes based on consistent hashing!

## Monitoring

Each node prints status updates every 15 seconds:

```
=== Cluster Status ===
Cluster: my-app-cluster
Total Nodes: 3
Partitions: 16
Local Keys: 5
Leader: true
Raft State: Leader
  - Server-1 (node-1) at :8080 [active]
  - Server-2 (node-2) at :8081 [active]
  - Server-3 (node-3) at :8082 [active]
======================
```

## ClusterKit Endpoints

ClusterKit also exposes its own management endpoints on ports 8080, 8081, 8082:

```bash
# Health check
curl http://localhost:8080/health

# Cluster information
curl http://localhost:8080/cluster

# Metrics
curl http://localhost:8080/metrics

# Detailed health status
curl http://localhost:8080/health/detailed

# Partition information
curl http://localhost:8080/partitions

# Consensus leader
curl http://localhost:8080/consensus/leader

# Raft statistics
curl http://localhost:8080/consensus/stats
```

## Key Concepts Demonstrated

### 1. **Separation of Concerns**
- **ClusterKit**: Handles cluster coordination, partitioning, and consensus
- **Your App**: Handles data storage and business logic

### 2. **Partition Management**
```go
// ClusterKit tells you which nodes should handle each key
nodes, _ := ck.GetNodesForKey(key)

// You decide how to store and replicate the data
for _, node := range nodes {
    storeOnNode(node, key, value)
}
```

### 3. **Role Awareness**
```go
shouldHandle, role, allNodes, _ := ck.AmIResponsibleFor(key)
// role: "primary", "replica", or ""
```

## Extending This Example

This example is intentionally simple. In production, you might add:

1. **Replication**: Forward writes to replica nodes
2. **Persistence**: Save data to disk (SQLite, LevelDB, etc.)
3. **Read Preferences**: Read from replicas for better performance
4. **Quorum Writes**: Wait for N nodes to acknowledge writes
5. **Conflict Resolution**: Handle concurrent writes
6. **TTL Support**: Automatic key expiration
7. **Batch Operations**: Bulk GET/SET operations

## Environment Variables

| Variable | Description | Example | Required |
|----------|-------------|---------|----------|
| `NODE_ID` | Unique node identifier | `node-1` | Yes |
| `NODE_NAME` | Human-readable name | `Server-1` | Yes |
| `HTTP_ADDR` | ClusterKit HTTP address | `:8080` | Yes |
| `RAFT_ADDR` | Raft bind address | `127.0.0.1:9001` | Yes |
| `BOOTSTRAP` | Bootstrap first node | `true` | First node only |
| `JOIN_ADDR` | Address to join | `localhost:8080` | Joining nodes |
| `DATA_DIR` | Data directory | `./data/node1` | Yes |

## Troubleshooting

### Keys not found on a node?
That's expected! Each node only stores keys it's responsible for based on consistent hashing.

### Want to find which node has a key?
```bash
curl "http://localhost:8080/partitions/key?key=user:123"
```

### Node won't join cluster?
- Ensure the bootstrap node is running first
- Check that `JOIN_ADDR` points to an active node
- Verify network connectivity between nodes

## Learn More

- [ClusterKit Documentation](../README.md)
- [Partition Management Guide](../PARTITIONS.md)
- [Contributing Guidelines](../CONTRIBUTING.md)

---

**Built with ClusterKit** - A lightweight distributed cluster coordination library for Go
