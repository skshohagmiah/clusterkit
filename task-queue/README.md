# Distributed Task Queue Example

A production-ready distributed task queue built with ClusterKit that demonstrates:

- **Distributed task processing** across multiple nodes
- **Automatic load balancing** using consistent hashing
- **Task replication** for fault tolerance (RF=3)
- **Automatic failover** when nodes go down
- **Background task processing** with status tracking

## Features

- ✅ **Submit tasks** - Tasks are automatically routed to the correct node
- ✅ **Track status** - Query task status from any node
- ✅ **Multiple task types** - email, image, report, or custom
- ✅ **Fault tolerant** - Tasks survive node failures (replicated 3x)
- ✅ **Auto-processing** - Background workers process pending tasks
- ✅ **Statistics** - View queue stats per node

## Quick Start

### Start a 3-Node Cluster

**Terminal 1 - Node 1 (Bootstrap):**
```bash
NODE_ID=node-1 HTTP_ADDR=:8080 go run main.go
```

**Terminal 2 - Node 2:**
```bash
NODE_ID=node-2 HTTP_ADDR=:8081 JOIN_ADDR=localhost:8080 go run main.go
```

**Terminal 3 - Node 3:**
```bash
NODE_ID=node-3 HTTP_ADDR=:8082 JOIN_ADDR=localhost:8080 go run main.go
```

### Submit Tasks

```bash
# Submit an email task
curl -X POST http://localhost:10080/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":"user@example.com"}'

# Submit an image processing task
curl -X POST http://localhost:10081/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"image","payload":"photo.jpg"}'

# Submit a report generation task
curl -X POST http://localhost:10082/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"report","payload":"monthly-sales"}'
```

### Check Task Status

```bash
# Get task status (replace with actual task ID)
curl http://localhost:10080/tasks/status?id=task-1699123456789
```

### View Queue Statistics

```bash
# Node 1 stats
curl http://localhost:10080/tasks/stats

# Node 2 stats
curl http://localhost:10081/tasks/stats

# Node 3 stats
curl http://localhost:10082/tasks/stats
```

### List All Tasks on a Node

```bash
curl http://localhost:10080/tasks/list
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/tasks/submit` | Submit a new task |
| GET | `/tasks/status?id=<task_id>` | Get task status |
| GET | `/tasks/list` | List all tasks on this node |
| GET | `/tasks/stats` | Get queue statistics |

## Task Types & Processing Times

| Type | Processing Time | Example |
|------|----------------|---------|
| `email` | 1 second | Send email notification |
| `image` | 3 seconds | Process/resize image |
| `report` | 5 seconds | Generate PDF report |
| `default` | 2 seconds | Generic task |

## How It Works

### 1. Task Submission
```
Client → Any Node → ClusterKit determines partition → Primary Node
                                                     ↓
                                              Replicate to 2 replicas
```

### 2. Task Distribution
- Tasks are distributed using **consistent hashing** on task ID
- Each task is stored on 1 primary + 2 replica nodes (RF=3)
- ClusterKit automatically routes requests to the correct node

### 3. Task Processing
- Background workers check for pending tasks every 2 seconds
- Tasks are processed in parallel across all nodes
- Status updates: `pending` → `processing` → `completed`

### 4. Fault Tolerance
- If a node fails, tasks are still available on replicas
- New tasks are automatically routed to healthy nodes
- When node recovers, it rejoins and resumes processing

## Architecture

```
┌─────────────────────────────────────────────────┐
│              Client Applications                │
└────────────┬────────────┬────────────┬──────────┘
             │            │            │
    ┌────────▼───┐  ┌────▼─────┐  ┌──▼──────┐
    │  Node 1    │  │  Node 2  │  │  Node 3 │
    │  :10080    │  │  :10081  │  │  :10082 │
    ├────────────┤  ├──────────┤  ├─────────┤
    │ Task Queue │  │Task Queue│  │Task Queue│
    │ ClusterKit │  │ClusterKit│  │ClusterKit│
    └────────────┘  └──────────┘  └─────────┘
         │               │              │
         └───────────────┴──────────────┘
              Raft Consensus Layer
```

## Example Workflow

```bash
# 1. Submit 10 tasks
for i in {1..10}; do
  curl -X POST http://localhost:10080/tasks/submit \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"email\",\"payload\":\"user$i@example.com\"}"
done

# 2. Check distribution
curl http://localhost:10080/tasks/stats
curl http://localhost:10081/tasks/stats
curl http://localhost:10082/tasks/stats

# 3. Wait for processing (tasks complete in 1-5 seconds)
sleep 10

# 4. Verify all completed
curl http://localhost:10080/tasks/stats
```

## Production Considerations

### Scaling
- Add more nodes to increase processing capacity
- Tasks automatically rebalance across nodes
- Each node processes tasks independently

### Persistence
- Add database backend (PostgreSQL, MongoDB, etc.)
- Store tasks in persistent storage instead of memory
- Implement task retry logic for failed tasks

### Monitoring
- Add metrics (Prometheus, Grafana)
- Track task completion rates
- Monitor queue depth per node

### Advanced Features
- **Priority queues** - Process high-priority tasks first
- **Scheduled tasks** - Delay task execution
- **Task dependencies** - Chain tasks together
- **Dead letter queue** - Handle failed tasks
- **Rate limiting** - Limit tasks per second

## Comparison with Other Solutions

| Feature | This Example | Redis Queue | RabbitMQ | AWS SQS |
|---------|-------------|-------------|----------|---------|
| Setup | Simple | Medium | Complex | Easy |
| Distributed | ✅ | ✅ | ✅ | ✅ |
| Self-hosted | ✅ | ✅ | ✅ | ❌ |
| Replication | ✅ (RF=3) | ✅ | ✅ | ✅ |
| Dependencies | ClusterKit only | Redis | Erlang/OTP | AWS |
| Cost | Free | Free | Free | Paid |

## Use Cases

Perfect for:
- **Background job processing** (emails, notifications)
- **Image/video processing** pipelines
- **Report generation** systems
- **Data import/export** tasks
- **Scheduled maintenance** jobs
- **Webhook processing**
- **ETL pipelines**

## Next Steps

1. **Add persistence** - Store tasks in database
2. **Add authentication** - Secure API endpoints
3. **Add monitoring** - Prometheus metrics
4. **Add retries** - Retry failed tasks
5. **Add webhooks** - Notify on task completion
6. **Add UI** - Web dashboard for monitoring

## License

MIT License - Same as ClusterKit
