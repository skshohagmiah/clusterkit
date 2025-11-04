# ClusterKit

> A lightweight, production-ready distributed cluster coordination library for Go

ClusterKit provides **cluster coordination** (nodes, partitions, consensus) while letting you handle your own data storage and replication logic. Built on **HashiCorp Raft** for strong consistency.

[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🎯 **Cluster Coordination** | Automatic node discovery and membership management |
| 📦 **Partition Management** | Consistent hashing for data distribution across nodes |
| 🔄 **Raft Consensus** | Production-grade consensus using HashiCorp Raft |
| 🎭 **Leader Election** | Automatic leader election for coordinated operations |
| 🔍 **Replica Discovery** | Find primary and replica nodes for any key |
| 🌐 **HTTP API** | RESTful endpoints for cluster management |
| 💾 **State Persistence** | WAL and snapshots for crash recovery |

## 🎯 What ClusterKit Does vs What You Do

**ClusterKit Provides:**
- ✅ Node membership and discovery
- ✅ Partition assignments (which node handles which data)
- ✅ Replica node discovery
- ✅ Leader election and consensus
- ✅ Cluster state synchronization

**You Implement:**
- 🔧 Data storage (PostgreSQL, Redis, MongoDB, etc.)
- 🔧 Data replication (sync/async, quorum, etc.)
- 🔧 Business logic and data models

**Think of ClusterKit as a GPS for your distributed system** - it tells you where data should go, you decide how to store it.

## Installation

```bash
go get github.com/skshohagmiah/clusterkit
```

## 🐳 Try the Example with Docker (Demo Only)

**Note:** ClusterKit is a **library** that you embed in your application. The Docker setup is for running the **example application** to see ClusterKit in action.

```bash
# Clone the repository
git clone https://github.com/skshohagmiah/clusterkit
cd clusterkit/example/docker

# Start a 3-node example cluster
docker-compose up

# In another terminal, check cluster status
curl http://localhost:8080/cluster
curl http://localhost:8081/cluster
curl http://localhost:8082/cluster
```

**This demo shows** a 3-node cluster with:
- ✅ Automatic partition creation
- ✅ Raft consensus
- ✅ Leader election
- ✅ Data persistence

### Docker Commands

```bash
# Start cluster in background
docker-compose up -d

# View logs
docker-compose logs -f

# Check specific node
docker-compose logs node1

# Stop cluster
docker-compose down

# Stop and remove data
docker-compose down -v

# Rebuild after code changes
docker-compose up --build
```

### Test the Cluster

```bash
# Check cluster status
curl http://localhost:8080/cluster

# Get partitions
curl http://localhost:8080/partitions

# Get partition for a key
curl "http://localhost:8080/partitions/key?key=user:123"

# Check consensus leader
curl http://localhost:8080/consensus/leader

# Check Raft stats
curl http://localhost:8080/consensus/stats
```

## Quick Start

### Simple Example (The Easy Way)

```go
// 1. Initialize ClusterKit (partitions created automatically!)
ck, _ := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:    "node-1",
    NodeName:  "Server-1",
    HTTPAddr:  ":8080",
    RaftAddr:  "127.0.0.1:9001",
    Bootstrap: true,  // First node
    DataDir:   "./data",
    Config: &clusterkit.Config{
        ClusterName:       "my-app",
        PartitionCount:    16,
        ReplicationFactor: 3,
    },
})
ck.Start()

// 2. Get all nodes that should store this key
nodes, _ := ck.GetNodesForKey("user:123")
// Returns: [{node-1, :8080}, {node-2, :8081}, {node-3, :8082}]

// 3. Write to all nodes (your choice: sync, async, quorum, etc.)
for _, node := range nodes {
    myDB.WriteToNode(node, "user:123", userData)
}

// 4. Read from any node
data := myDB.ReadFromNode(nodes[0], "user:123")

// That's it! No need to check if you're primary or replica!
```

### Check If You Should Handle a Key

```go
// Optional: Check if current node should handle this key
shouldHandle, role, allNodes, _ := ck.AmIResponsibleFor("user:123")

if shouldHandle {
    // role is "primary" or "replica"
    fmt.Printf("I'm the %s for this key\n", role)
    
    // Store locally
    myDB.Write("user:123", userData)
    
    // Optionally replicate to other nodes
    for _, node := range allNodes {
        if node.ID != ck.GetMyNodeInfo().ID {
            sendToNode(node, "user:123", userData)
        }
    }
}
```

### Production-Ready Application (Using Environment Variables)

**Your Application Code (main.go):**
```go
package main

import (
    "log"
    "os"
    "strconv"
    "time"
    
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Read configuration from environment variables
    nodeID := getEnv("NODE_ID", "node-1")
    nodeName := getEnv("NODE_NAME", "Server-1")
    httpAddr := getEnv("HTTP_ADDR", ":8080")
    raftAddr := getEnv("RAFT_ADDR", "127.0.0.1:9001")
    joinAddr := getEnv("JOIN_ADDR", "")
    bootstrap := getEnv("BOOTSTRAP", "false") == "true"
    dataDir := getEnv("DATA_DIR", "./data")
    
    // Initialize ClusterKit
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:       nodeID,
        NodeName:     nodeName,
        HTTPAddr:     httpAddr,
        RaftAddr:     raftAddr,
        JoinAddr:     joinAddr,
        Bootstrap:    bootstrap,
        DataDir:      dataDir,
        SyncInterval: 5 * time.Second,
        Config: &clusterkit.Config{
            ClusterName:       getEnv("CLUSTER_NAME", "my-cluster"),
            PartitionCount:    getEnvInt("PARTITION_COUNT", 16),
            ReplicationFactor: getEnvInt("REPLICATION_FACTOR", 3),
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Application started with ClusterKit")
    log.Printf("Node ID: %s", nodeID)
    log.Printf("HTTP Address: %s", httpAddr)
    
    // Your application logic here
    runYourApplication(ck)
    
    select {}
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        if intVal, err := strconv.Atoi(value); err == nil {
            return intVal
        }
    }
    return defaultValue
}

func runYourApplication(ck *clusterkit.ClusterKit) {
    // Your business logic here
    // Use ck.GetNodesForKey(), ck.AmIResponsibleFor(), etc.
}
```

**Then deploy with Docker Compose:**

```yaml
version: '3.8'

services:
  app-node1:
    build: .
    environment:
      - NODE_ID=node-1
      - NODE_NAME=Server-1
      - HTTP_ADDR=:8080
      - RAFT_ADDR=app-node1:9001
      - BOOTSTRAP=true
      - DATA_DIR=/data
      - CLUSTER_NAME=my-app-cluster
      - PARTITION_COUNT=16
      - REPLICATION_FACTOR=3
    ports:
      - "8080:8080"
    volumes:
      - app1-data:/data

  app-node2:
    build: .
    environment:
      - NODE_ID=node-2
      - NODE_NAME=Server-2
      - HTTP_ADDR=:8080
      - RAFT_ADDR=app-node2:9001
      - JOIN_ADDR=app-node1:8080
      - DATA_DIR=/data
      - CLUSTER_NAME=my-app-cluster
      - PARTITION_COUNT=16
      - REPLICATION_FACTOR=3
    ports:
      - "8081:8080"
    volumes:
      - app2-data:/data
    depends_on:
      - app-node1

  app-node3:
    build: .
    environment:
      - NODE_ID=node-3
      - NODE_NAME=Server-3
      - HTTP_ADDR=:8080
      - RAFT_ADDR=app-node3:9001
      - JOIN_ADDR=app-node1:8080
      - DATA_DIR=/data
      - CLUSTER_NAME=my-app-cluster
      - PARTITION_COUNT=16
      - REPLICATION_FACTOR=3
    ports:
      - "8082:8080"
    volumes:
      - app3-data:/data
    depends_on:
      - app-node1

volumes:
  app1-data:
  app2-data:
  app3-data:
```

**Key Benefits:**
- ✅ **Same code** runs on all nodes
- ✅ **Environment-based** configuration
- ✅ **Docker-friendly** - no hardcoded values
- ✅ **Production-ready** - easy to deploy anywhere

## How It Works

1. **Initialization** - Each application instance initializes ClusterKit with a unique Node ID
2. **HTTP Server** - ClusterKit starts an HTTP server for inter-node communication
3. **Discovery** - New nodes connect to known nodes and exchange cluster state
4. **State Sync** - Nodes periodically sync state with each other (default: 5 seconds)
5. **Persistence** - Each node saves cluster state to disk in `cluster-state.json`
6. **WAL Logging** - All operations are logged to `wal.log` for durability

## API Reference

### Creating a ClusterKit Instance

```go
type Options struct {
    NodeID        string        // Unique ID for this node
    NodeName      string        // Human-readable name
    HTTPAddr      string        // Address to listen on (e.g., ":8080")
    KnownNodes    []string      // List of known node addresses
    DataDir       string        // Directory to store state and WAL
    SyncInterval  time.Duration // How often to sync with other nodes
    Config        *Config       // Cluster configuration
}

ck, err := clusterkit.NewClusterKit(options)
```

### Starting ClusterKit

```go
err := ck.Start()
```

### Getting Cluster State

```go
cluster := ck.GetCluster()
fmt.Printf("Cluster: %s\n", cluster.Name)
fmt.Printf("Total Nodes: %d\n", len(cluster.Nodes))
```

### Stopping ClusterKit

```go
err := ck.Stop() // Saves state before shutdown
```

## Environment Variables Example

You can use environment variables to configure nodes:

```bash
# Node 1 (Bootstrap)
NODE_ID=node-1 NODE_NAME=Server-1 HTTP_ADDR=:8080 BOOTSTRAP=true DATA_DIR=./data/node1 go run main.go

# Node 2 (Join)
NODE_ID=node-2 NODE_NAME=Server-2 HTTP_ADDR=:8081 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node2 go run main.go

# Node 3 (Join)
NODE_ID=node-3 NODE_NAME=Server-3 HTTP_ADDR=:8082 JOIN_ADDR=localhost:8080 DATA_DIR=./data/node3 go run main.go
```

## HTTP Endpoints

ClusterKit exposes these endpoints for inter-node communication:

### Cluster Management
- `GET /health` - Health check
- `POST /join` - Node registration
- `POST /sync` - State synchronization
- `GET /cluster` - Get cluster state

### Partition Management

```go
type DistributedKV struct {
    ck      *clusterkit.ClusterKit
    localDB map[string][]byte
}

func (kv *DistributedKV) Set(key string, value []byte) error {
    // Get all nodes that should store this key
    nodes, err := kv.ck.GetNodesForKey(key)
    if err != nil {
        return err
    }
    
    // Write to all nodes (you choose: sync, async, quorum, etc.)
    for _, node := range nodes {
        if node.ID == kv.ck.GetMyNodeInfo().ID {
            // Store locally
            kv.localDB[key] = value
        } else {
            // Replicate to other nodes
            kv.replicateToNode(node, key, value)
        }
    }
    
    return nil
}

func (kv *DistributedKV) Get(key string) ([]byte, error) {
    // Check if this node should have the data
    shouldHandle, _, _, err := kv.ck.AmIResponsibleFor(key)
    if err != nil {
        return nil, err
    }
    
    if shouldHandle {
        // Read from local storage
        return kv.localDB[key], nil
    }
    
    // Forward to a node that has it
    nodes, _ := kv.ck.GetNodesForKey(key)
    return kv.readFromNode(nodes[0], key)
}
```

**That's it!** ClusterKit handles:
- ✅ Which nodes store which keys
- ✅ Partition assignments
- ✅ Rebalancing when nodes join/leave
- ✅ Leader election
- ✅ Consensus

You handle:
- 🔧 Your storage (database, cache, etc.)
- 🔧 Your replication strategy
- 🔧 Your business logic

## 📚 API Reference

### Initialization

#### `NewClusterKit(options Options) (*ClusterKit, error)`

Create a new ClusterKit instance.

```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    NodeID:       "node-1",           // Unique node identifier
    NodeName:     "Server-1",         // Human-readable name
    HTTPAddr:     ":8080",            // HTTP listen address
    RaftAddr:     "127.0.0.1:9001",   // Raft bind address
    Bootstrap:    true,               // true for first node only
    JoinAddr:     "",                 // Address to join (empty for bootstrap)
    DataDir:      "./data/node1",     // Data directory
    SyncInterval: 5 * time.Second,    // State sync interval
    Config: &clusterkit.Config{
        ClusterName:       "my-cluster",
        PartitionCount:    16,
        ReplicationFactor: 3,
    },
})
```

#### `Start() error`

Start the ClusterKit instance (HTTP server, Raft, discovery).

```go
if err := ck.Start(); err != nil {
    log.Fatal(err)
}
```

#### `Stop() error`

Gracefully shutdown ClusterKit.

```go
ck.Stop()
```

---

### Partition Management (Simple API)

#### `GetNodesForKey(key string) ([]Node, error)` ⭐ **Most Used**

Get ALL nodes (primary + replicas) that should store this key. **This is the main method you'll use!**

```go
nodes, err := ck.GetNodesForKey("user:123")
// Returns: [{node-1, :8080}, {node-2, :8081}, {node-3, :8082}]

// Write to all nodes
for _, node := range nodes {
    myDB.WriteToNode(node, "user:123", userData)
}
```

#### `AmIResponsibleFor(key string) (bool, string, []Node, error)` ⭐ **Check Responsibility**

Check if current node should handle this key and get all related nodes.

```go
shouldHandle, role, allNodes, err := ck.AmIResponsibleFor("user:123")
// role: "primary", "replica", or ""

if shouldHandle {
    // Store locally
    myDB.Write("user:123", userData)
    
    // Replicate to other nodes
    for _, node := range allNodes {
        if node.ID != myNodeID {
            sendToNode(node, "user:123", userData)
        }
    }
}
```

#### `GetPrimaryNodeForKey(key string) (*Node, error)`

Get only the primary node for a key.

```go
primary, err := ck.GetPrimaryNodeForKey("user:123")
// Returns: {ID: "node-2", Name: "Server-2", IP: ":8081", Status: "active"}
```

#### `GetReplicaNodesForKey(key string) ([]Node, error)`

Get only the replica nodes for a key.

```go
replicas, err := ck.GetReplicaNodesForKey("user:123")
// Returns: [{ID: "node-1", ...}, {ID: "node-3", ...}]
```

#### `GetMyNodeInfo() Node`

Get information about the current node.

```go
me := ck.GetMyNodeInfo()
// Returns: {ID: "node-1", Name: "Server-1", IP: ":8080", Status: "active"}
```

---

### Advanced Partition Methods

#### `GetPartitionForKey(key string) (*Partition, error)`

Get the partition object for a key (advanced usage).

```go
partition, err := ck.GetPartitionForKey("user:123")
// Returns: {ID: "partition-5", PrimaryNode: "node-2", ReplicaNodes: ["node-1", "node-3"]}
```

#### `CreatePartitions() error`

Manually create partitions (auto-created on bootstrap by default).

```go
if ck.GetConsensusManager().IsLeader() {
    err := ck.CreatePartitions()
}
```

#### `ListPartitions() []*Partition`

Get all partitions in the cluster.

```go
partitions := ck.ListPartitions()
for _, p := range partitions {
    fmt.Printf("Partition %s: Primary=%s\n", p.ID, p.PrimaryNode)
}
```

#### `GetPartitionStats() *PartitionStats`

Get partition distribution statistics.

```go
stats := ck.GetPartitionStats()
// Returns: {TotalPartitions: 16, PartitionsPerNode: {...}, ReplicasPerNode: {...}}
```

#### `RebalancePartitions() error`

Rebalance partitions across nodes (leader only).

```go
if ck.GetConsensusManager().IsLeader() {
    err := ck.RebalancePartitions()
}
```

---

### Cluster Information

#### `GetCluster() *Cluster`

Get current cluster state.

```go
cluster := ck.GetCluster()
fmt.Printf("Cluster: %s, Nodes: %d\n", cluster.Name, len(cluster.Nodes))
```

---

### Consensus & Leadership

#### `GetConsensusManager() *ConsensusManager`

Get the consensus manager for leader operations.

```go
cm := ck.GetConsensusManager()
```

#### `IsLeader() bool`

Check if current node is the Raft leader.

```go
if cm.IsLeader() {
    // Perform leader-only operations
    ck.CreatePartitions()
}
```

#### `GetLeader() (*LeaderInfo, error)`

Get current leader information.

```go
leader, err := cm.GetLeader()
// Returns: {LeaderID: "node-1", LeaderName: "Server-1", LeaderIP: ":8080", Term: 5}
```

#### `WaitForLeader(timeout time.Duration) error`

Wait for leader election to complete.

```go
err := cm.WaitForLeader(10 * time.Second)
```

#### `GetStats() *ConsensusStats`

Get Raft consensus statistics.

```go
stats := cm.GetStats()
// Returns: {State: "Leader", Term: 5, LastLogIndex: 42, CommitIndex: 42, ...}
```

---

### Complete Example

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/skshohagmiah/clusterkit"
)

func main() {
    // Initialize ClusterKit
    ck, err := clusterkit.NewClusterKit(clusterkit.Options{
        NodeID:    "node-1",
        NodeName:  "Server-1",
        HTTPAddr:  ":8080",
        RaftAddr:  "127.0.0.1:9001",
        Bootstrap: true,
        DataDir:   "./data/node1",
        Config: &clusterkit.Config{
            ClusterName:       "my-app",
            PartitionCount:    16,
            ReplicationFactor: 3,
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // Start ClusterKit
    if err := ck.Start(); err != nil {
        log.Fatal(err)
    }
    defer ck.Stop()

    // Wait for leader election
    cm := ck.GetConsensusManager()
    cm.WaitForLeader(10 * time.Second)

    // Create partitions (leader only)
    if cm.IsLeader() {
        ck.CreatePartitions()
    }

    // Use ClusterKit to route data
    key := "user:123"
    
    // Get partition info
    partition, _ := ck.GetPartitionForKey(key)
    fmt.Printf("Key %s belongs to partition %s\n", key, partition.ID)
    
    // Check if this node should handle the key
    isPrimary, _ := ck.IsPrimaryForKey(key)
    if isPrimary {
        // Write to your database
        myDB.Write(key, data)
        
        // Replicate to replica nodes
        replicas, _ := ck.GetReplicaNodes(key)
        for _, replica := range replicas {
            sendToNode(replica, key, data)
        }
    } else {
        // Forward to primary
        primary, _ := ck.GetPrimaryNode(key)
        forwardTo(primary, key, data)
    }
}
```

## 📖 Examples

- **[Basic Example](./example)** - Simple multi-node cluster setup
- **[Partitions Demo](./example/partitions-demo)** - Complete partition management example

## 🐳 Dockerizing Your Application with ClusterKit

ClusterKit is a **library**, so you dockerize **your application** that uses ClusterKit.

### Example: Your Application Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app

# Copy your app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build your app (which uses ClusterKit)
RUN go build -o myapp .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/myapp .

# Expose your app's ports
EXPOSE 8080 9001

CMD ["./myapp"]
```

### Example: Your docker-compose.yml

```yaml
version: '3.8'

services:
  app-node1:
    build: .
    environment:
      - NODE_ID=node-1
      - NODE_NAME=Server-1
      - HTTP_ADDR=:8080
      - RAFT_ADDR=app-node1:9001
      - BOOTSTRAP=true
    ports:
      - "8080:8080"
    volumes:
      - app1-data:/data

  app-node2:
    build: .
    environment:
      - NODE_ID=node-2
      - NODE_NAME=Server-2
      - HTTP_ADDR=:8080
      - RAFT_ADDR=app-node2:9001
      - JOIN_ADDR=app-node1:8080
    ports:
      - "8081:8080"
    volumes:
      - app2-data:/data
    depends_on:
      - app-node1

volumes:
  app1-data:
  app2-data:
```

### Demo Example (Reference)

The included `example/docker/docker-compose.yml` shows a working example:

```yaml
version: '3.8'

services:
  node1:
    build: .
    environment:
      - NODE_ID=node-1
      - NODE_NAME=Server-1
      - HTTP_ADDR=:8080
      - RAFT_ADDR=node1:9001
      - BOOTSTRAP=true
    ports:
      - "8080:8080"
      - "9001:9001"
    volumes:
      - node1-data:/data
    networks:
      - clusterkit-network

  node2:
    build: .
    environment:
      - NODE_ID=node-2
      - NODE_NAME=Server-2
      - HTTP_ADDR=:8080
      - RAFT_ADDR=node2:9001
      - JOIN_ADDR=node1:8080
    ports:
      - "8081:8080"
      - "9002:9001"
    volumes:
      - node2-data:/data
    networks:
      - clusterkit-network
    depends_on:
      - node1

  # node3 similar...
```

### Custom Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o clusterkit-app ./example/main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/clusterkit-app .
EXPOSE 8080 9001
CMD ["./clusterkit-app"]
```

### Kubernetes Deployment

For Kubernetes, use StatefulSets with persistent volumes:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: clusterkit
spec:
  serviceName: clusterkit
  replicas: 3
  selector:
    matchLabels:
      app: clusterkit
  template:
    metadata:
      labels:
        app: clusterkit
    spec:
      containers:
      - name: clusterkit
        image: your-registry/clusterkit:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 9001
          name: raft
        env:
        - name: NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: HTTP_ADDR
          value: ":8080"
        - name: RAFT_ADDR
          value: "$(NODE_NAME).clusterkit:9001"
        - name: BOOTSTRAP
          value: "false"
        - name: JOIN_ADDR
          value: "clusterkit-0.clusterkit:8080"
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 10Gi
```

### Environment Variables

| Variable | Description | Example | Required |
|----------|-------------|---------|----------|
| `NODE_ID` | Unique node identifier | `node-1` | Yes |
| `NODE_NAME` | Human-readable name | `Server-1` | Yes |
| `HTTP_ADDR` | HTTP listen address | `:8080` | Yes |
| `RAFT_ADDR` | Raft bind address | `node1:9001` | Yes |
| `BOOTSTRAP` | Bootstrap first node | `true` | First node only |
| `JOIN_ADDR` | Address to join | `node1:8080` | Joining nodes |
| `DATA_DIR` | Data directory | `/data` | Yes |

## 📚 Documentation

- **[PARTITIONS.md](./PARTITIONS.md)** - Detailed partition management guide

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on:
- Reporting bugs
- Suggesting features
- Submitting pull requests
- Code style and testing

## 📝 License

MIT License - see [LICENSE](LICENSE) file for details.

Copyright (c) 2024 Shohag Miah

## 🌟 Support

If you find ClusterKit useful, please consider:
- ⭐ Starring the repository
- 🐛 Reporting bugs
- 💡 Suggesting features
- 🤝 Contributing code
- 📢 Sharing with others

## 📞 Contact

- **GitHub Issues**: For bugs and feature requests
- **GitHub Discussions**: For questions and ideas
- **Email**: [your-email@example.com]

---

**Built with ❤️ using Go and HashiCorp Raft**
