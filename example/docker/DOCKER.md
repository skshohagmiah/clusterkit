# Docker Deployment Guide

This guide shows you how to deploy ClusterKit using Docker.

## Quick Start

```bash
# Start a 3-node cluster
docker-compose up

# Start in background
docker-compose up -d

# View logs
docker-compose logs -f

# Stop cluster
docker-compose down
```

## Architecture

The Docker Compose setup creates:
- **3 nodes** (node1, node2, node3)
- **Persistent volumes** for each node
- **Isolated network** for inter-node communication
- **Health checks** for monitoring

## Ports

| Node | HTTP Port | Raft Port |
|------|-----------|-----------|
| node1 | 8080 | 9001 |
| node2 | 8081 | 9002 |
| node3 | 8082 | 9003 |

## Testing

```bash
# Check cluster status
curl http://localhost:8080/cluster

# Get partitions
curl http://localhost:8080/partitions

# Get partition for a key
curl "http://localhost:8080/partitions/key?key=user:123"

# Check leader
curl http://localhost:8080/consensus/leader

# Check Raft stats
curl http://localhost:8080/consensus/stats
```

## Scaling

To add more nodes, edit `docker-compose.yml`:

```yaml
  node4:
    build: .
    container_name: clusterkit-node4
    environment:
      - NODE_ID=node-4
      - NODE_NAME=Server-4
      - HTTP_ADDR=:8080
      - RAFT_ADDR=node4:9001
      - JOIN_ADDR=node1:8080
      - DATA_DIR=/data
    ports:
      - "8083:8080"
      - "9004:9001"
    volumes:
      - node4-data:/data
    networks:
      - clusterkit-network
    depends_on:
      - node1
```

Then add the volume:

```yaml
volumes:
  node1-data:
  node2-data:
  node3-data:
  node4-data:  # Add this
```

## Production Deployment

### Using Docker Swarm

```bash
# Initialize swarm
docker swarm init

# Deploy stack
docker stack deploy -c docker-compose.yml clusterkit

# List services
docker service ls

# Scale a service
docker service scale clusterkit_node2=2

# Remove stack
docker stack rm clusterkit
```

### Using Kubernetes

See the Kubernetes example in README.md for StatefulSet deployment.

## Troubleshooting

### Check logs
```bash
docker-compose logs node1
docker-compose logs node2
docker-compose logs node3
```

### Restart a node
```bash
docker-compose restart node2
```

### Clean restart
```bash
docker-compose down -v  # Remove volumes
docker-compose up --build
```

### Enter container
```bash
docker exec -it clusterkit-node1 sh
```

## Data Persistence

Data is stored in Docker volumes:
- `node1-data` - Node 1 data (Raft logs, snapshots, state)
- `node2-data` - Node 2 data
- `node3-data` - Node 3 data

To backup:
```bash
docker run --rm -v clusterkit_node1-data:/data -v $(pwd):/backup alpine tar czf /backup/node1-backup.tar.gz /data
```

To restore:
```bash
docker run --rm -v clusterkit_node1-data:/data -v $(pwd):/backup alpine tar xzf /backup/node1-backup.tar.gz -C /
```

## Environment Variables

All configuration is done via environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `NODE_ID` | Unique node ID | `node-1` |
| `NODE_NAME` | Human-readable name | `Server-1` |
| `HTTP_ADDR` | HTTP listen address | `:8080` |
| `RAFT_ADDR` | Raft bind address | `node1:9001` |
| `BOOTSTRAP` | Bootstrap first node | `true` |
| `JOIN_ADDR` | Address to join | `node1:8080` |
| `DATA_DIR` | Data directory | `/data` |

## Health Checks

Each container has a health check that pings the `/health` endpoint:

```yaml
healthcheck:
  test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/health"]
  interval: 10s
  timeout: 5s
  retries: 3
```

Check health status:
```bash
docker ps
# Look for "healthy" in STATUS column
```

## Monitoring

### View cluster status
```bash
watch -n 1 'curl -s http://localhost:8080/cluster | jq'
```

### Monitor Raft stats
```bash
watch -n 1 'curl -s http://localhost:8080/consensus/stats | jq'
```

### Monitor all nodes
```bash
for port in 8080 8081 8082; do
  echo "=== Node on port $port ==="
  curl -s http://localhost:$port/cluster | jq '.nodes | length'
done
```
