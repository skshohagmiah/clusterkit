# ClusterKit Docker Setup

Run a 3-node ClusterKit cluster with Docker Compose.

## Quick Start

```bash
# Start the cluster
docker-compose up -d

# Check cluster status
curl http://localhost:8080/cluster | jq

# View logs
docker-compose logs -f

# Stop the cluster
docker-compose down
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Docker Network                        │
│                   (clusterkit-net)                       │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │   Node 1     │  │   Node 2     │  │   Node 3     │  │
│  │  (Bootstrap) │  │    (Join)    │  │    (Join)    │  │
│  │              │  │              │  │              │  │
│  │ ClusterKit   │  │ ClusterKit   │  │ ClusterKit   │  │
│  │   :8080      │  │   :8080      │  │   :8080      │  │
│  │              │  │              │  │              │  │
│  │ KV Store     │  │ KV Store     │  │ KV Store     │  │
│  │   :9080      │  │   :9080      │  │   :9080      │  │
│  │              │  │              │  │              │  │
│  │ Raft         │  │ Raft         │  │ Raft         │  │
│  │   :9001      │  │   :9001      │  │   :9001      │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│         ↓                 ↓                 ↓           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Volume 1    │  │  Volume 2    │  │  Volume 3    │  │
│  │  (Persist)   │  │  (Persist)   │  │  (Persist)   │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
         ↓                 ↓                 ↓
    localhost:8080    localhost:8081    localhost:8082  (ClusterKit)
    localhost:9080    localhost:9081    localhost:9082  (KV Store)
```

## Port Mapping

| Service | Internal Port | External Port | Purpose |
|---------|--------------|---------------|---------|
| Node 1 | 8080 | 8080 | ClusterKit HTTP API |
| Node 1 | 9080 | 9080 | KV Store HTTP API |
| Node 1 | 9001 | 9001 | Raft Consensus |
| Node 2 | 8080 | 8081 | ClusterKit HTTP API |
| Node 2 | 9080 | 9081 | KV Store HTTP API |
| Node 2 | 9001 | 9002 | Raft Consensus |
| Node 3 | 8080 | 8082 | ClusterKit HTTP API |
| Node 3 | 9080 | 9082 | KV Store HTTP API |
| Node 3 | 9001 | 9003 | Raft Consensus |

## Usage Examples

### 1. Start the Cluster

```bash
# Build and start all nodes
docker-compose up -d

# Wait for cluster to form (about 10 seconds)
sleep 10

# Verify cluster status
curl http://localhost:8080/cluster | jq '.nodes | length'
# Expected: 3
```

### 2. Check Cluster Health

```bash
# Check individual nodes
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8082/health

# Check detailed health
curl http://localhost:8080/health/detailed | jq
```

### 3. Store and Retrieve Data

```bash
# Store a key (will be distributed automatically)
curl -X POST http://localhost:9080/kv/set \
  -H "Content-Type: application/json" \
  -d '{"key":"user:123","value":"John Doe"}'

# Retrieve the key (from any node)
curl "http://localhost:9080/kv/get?key=user:123"
curl "http://localhost:9081/kv/get?key=user:123"
curl "http://localhost:9082/kv/get?key=user:123"

# List all keys on a node
curl http://localhost:9080/kv/list | jq
```

### 4. Test Replication

```bash
# Insert multiple keys
for i in {1..10}; do
  curl -s -X POST http://localhost:9080/kv/set \
    -H "Content-Type: application/json" \
    -d "{\"key\":\"user-$i\",\"value\":\"User $i\"}"
done

# Check distribution across nodes
echo "Node 1:" && curl -s http://localhost:9080/kv/list | jq '.count'
echo "Node 2:" && curl -s http://localhost:9081/kv/list | jq '.count'
echo "Node 3:" && curl -s http://localhost:9082/kv/list | jq '.count'
```

### 5. Test Fault Tolerance

```bash
# Stop Node 2
docker-compose stop node2

# Verify cluster still works (2 nodes)
curl -X POST http://localhost:9080/kv/set \
  -H "Content-Type: application/json" \
  -d '{"key":"test-after-failure","value":"Still works!"}'

# Retrieve data (from replicas)
curl "http://localhost:9080/kv/get?key=test-after-failure"

# Restart Node 2
docker-compose start node2

# Wait for rejoin
sleep 5

# Verify 3 nodes again
curl http://localhost:8080/cluster | jq '.nodes | length'
```

### 6. Test Partition Migration

```bash
# Add a 4th node
docker-compose up -d --scale node3=2

# Wait for rebalancing
sleep 30

# Check if data migrated
curl http://localhost:9083/kv/list | jq '.count'
```

### 7. View Partition Distribution

```bash
# Get partition statistics
curl http://localhost:8080/partitions/stats | jq

# Get all partitions
curl http://localhost:8080/partitions | jq
```

### 8. Monitor Metrics

```bash
# Get cluster metrics
curl http://localhost:8080/metrics | jq

# Watch metrics in real-time
watch -n 2 'curl -s http://localhost:8080/metrics | jq'
```

## Logs

```bash
# View all logs
docker-compose logs -f

# View specific node
docker-compose logs -f node1
docker-compose logs -f node2
docker-compose logs -f node3

# View last 100 lines
docker-compose logs --tail=100 node1
```

## Scaling

### Add a 4th Node

```bash
# Create node4 service in docker-compose.yml or use scale
docker-compose up -d node4

# Or manually run
docker run -d \
  --name clusterkit-node4 \
  --network clusterkit_clusterkit-net \
  -e NODE_ID=node-4 \
  -e NODE_NAME=Server-4 \
  -e HTTP_ADDR=:8080 \
  -e RAFT_ADDR=node4:9001 \
  -e JOIN_ADDR=node1:8080 \
  -e DATA_DIR=/data/node4 \
  -p 8083:8080 \
  -p 9083:9080 \
  -p 9004:9001 \
  clusterkit-example
```

### Remove a Node

```bash
# Stop and remove node3
docker-compose stop node3
docker-compose rm -f node3

# Cluster will continue with 2 nodes
# Partitions will NOT automatically rebalance on node removal
```

## Troubleshooting

### Cluster Not Forming

```bash
# Check if nodes can reach each other
docker-compose exec node1 ping node2
docker-compose exec node1 ping node3

# Check Raft logs
docker-compose logs node1 | grep -i raft
docker-compose logs node2 | grep -i raft
```

### Data Not Replicating

```bash
# Check partition assignments
curl http://localhost:8080/partitions | jq

# Check if nodes are healthy
curl http://localhost:8080/health/detailed | jq
curl http://localhost:8081/health/detailed | jq
curl http://localhost:8082/health/detailed | jq
```

### Port Conflicts

```bash
# If ports are already in use, modify docker-compose.yml
# Change external ports:
ports:
  - "18080:8080"  # Use 18080 instead of 8080
  - "19080:9080"  # Use 19080 instead of 9080
```

## Cleanup

```bash
# Stop and remove containers
docker-compose down

# Remove volumes (deletes all data!)
docker-compose down -v

# Remove images
docker-compose down --rmi all

# Complete cleanup
docker-compose down -v --rmi all
docker system prune -af
```

## Production Considerations

1. **Persistent Volumes**
   - Data is stored in Docker volumes
   - Survives container restarts
   - Backup volumes regularly

2. **Resource Limits**
   ```yaml
   deploy:
     resources:
       limits:
         cpus: '0.5'
         memory: 512M
   ```

3. **Health Checks**
   - Already configured
   - Monitors node health
   - Auto-restarts on failure

4. **Networking**
   - Use overlay network for multi-host
   - Configure firewall rules
   - Enable TLS for production

5. **Monitoring**
   - Export metrics to Prometheus
   - Set up Grafana dashboards
   - Configure alerts

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| NODE_ID | Yes | - | Unique node identifier |
| NODE_NAME | Yes | - | Human-readable node name |
| HTTP_ADDR | Yes | - | HTTP server address |
| RAFT_ADDR | Yes | - | Raft consensus address |
| BOOTSTRAP | No | false | Bootstrap first node |
| JOIN_ADDR | No | - | Address of node to join |
| DATA_DIR | Yes | - | Data directory path |

## Next Steps

- Read the [main README](../README.md) for ClusterKit concepts
- Check [client SDK](../client/README.md) for building applications
- See [example code](main.go) for implementation details
- Run [demo script](demo.sh) for local testing

## Support

- 📖 [Documentation](https://github.com/skshohagmiah/clusterkit)
- 🐛 [Issue Tracker](https://github.com/skshohagmiah/clusterkit/issues)
- 💬 [Discussions](https://github.com/skshohagmiah/clusterkit/discussions)
