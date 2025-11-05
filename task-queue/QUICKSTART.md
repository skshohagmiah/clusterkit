# Quick Start Guide - Distributed Task Queue

## Run the Demo

```bash
cd task-queue
./demo.sh
```

The demo will:
1. ✅ Start 3-node cluster
2. ✅ Submit 15 tasks (email, image, report types)
3. ✅ Show task distribution across nodes
4. ✅ Process tasks automatically in background
5. ✅ Simulate node failure and test fault tolerance

**Expected output:**
```
✅ Task Queue cluster formed with 3 nodes
✅ Submitted 15 tasks
✅ Tasks distributed across all 3 nodes
✅ Total task copies: 45 (with RF=3)
✅ Background task processing is working!
✅ Cluster still accepting tasks with 2 nodes
```

## Manual Testing

### 1. Start Cluster (3 terminals)

**Terminal 1:**
```bash
NODE_ID=node-1 HTTP_ADDR=:8080 go run main.go
```

**Terminal 2:**
```bash
NODE_ID=node-2 HTTP_ADDR=:8081 JOIN_ADDR=localhost:8080 go run main.go
```

**Terminal 3:**
```bash
NODE_ID=node-3 HTTP_ADDR=:8082 JOIN_ADDR=localhost:8080 go run main.go
```

### 2. Submit Tasks

```bash
# Email task (1 second processing)
curl -X POST http://localhost:10080/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":"user@example.com"}'

# Image task (3 seconds processing)
curl -X POST http://localhost:10081/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"image","payload":"photo.jpg"}'

# Report task (5 seconds processing)
curl -X POST http://localhost:10082/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"report","payload":"monthly-sales"}'
```

### 3. Check Status

```bash
# Get task status (use task_id from submit response)
curl http://localhost:10080/tasks/status?id=task-1699123456789

# View queue stats
curl http://localhost:10080/tasks/stats
curl http://localhost:10081/tasks/stats
curl http://localhost:10082/tasks/stats

# List all tasks on a node
curl http://localhost:10080/tasks/list
```

## API Ports

| Node | ClusterKit | Task Queue API |
|------|-----------|----------------|
| Node 1 | :8080 | :10080 |
| Node 2 | :8081 | :10081 |
| Node 3 | :8082 | :10082 |

## Task Types

| Type | Processing Time | Use Case |
|------|----------------|----------|
| `email` | 1 second | Send notifications |
| `image` | 3 seconds | Process images |
| `report` | 5 seconds | Generate reports |
| Custom | 2 seconds | Any other task |

## Testing Fault Tolerance

```bash
# 1. Submit some tasks
for i in {1..5}; do
  curl -X POST http://localhost:10080/tasks/submit \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"email\",\"payload\":\"user$i@example.com\"}"
done

# 2. Kill node 2
pkill -f "NODE_ID=node-2"

# 3. Submit more tasks (should still work)
curl -X POST http://localhost:10080/tasks/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":"test-after-failure"}'

# 4. Check tasks are still accessible via replicas
curl http://localhost:10080/tasks/stats
```

## Next Steps

See [README.md](README.md) for:
- Architecture details
- Production considerations
- Advanced features
- Comparison with other solutions
