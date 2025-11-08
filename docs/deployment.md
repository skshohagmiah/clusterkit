# Production Deployment Guide

Best practices for deploying ClusterKit in production environments.

---

## Production Checklist

- [ ] Configure health checking
- [ ] Set up monitoring and metrics
- [ ] Configure persistent storage
- [ ] Set appropriate partition count
- [ ] Configure replication factor
- [ ] Set up logging
- [ ] Configure resource limits
- [ ] Test failure scenarios
- [ ] Document runbooks

---

## Configuration

### Production Configuration

```go
ck, err := clusterkit.NewClusterKit(clusterkit.Options{
    // Required
    NodeID:   os.Getenv("NODE_ID"),
    HTTPAddr: ":" + os.Getenv("HTTP_PORT"),
    
    // Cluster
    ClusterName:       "production-cluster",
    JoinAddr:          os.Getenv("JOIN_ADDR"),
    PartitionCount:    128,  // More partitions for better distribution
    ReplicationFactor: 3,    // Survive 2 node failures
    
    // Storage
    DataDir: "/var/lib/clusterkit",
    
    // Health Checking
    HealthCheck: clusterkit.HealthCheckConfig{
        Enabled:          true,
        Interval:         5 * time.Second,
        Timeout:          2 * time.Second,
        FailureThreshold: 3,
    },
})
```

### Environment Variables

```bash
# Required
export NODE_ID="node-1"
export HTTP_PORT="8080"

# Optional
export JOIN_ADDR="node-1:8080"
export DATA_DIR="/var/lib/clusterkit"
export PARTITION_COUNT="128"
export REPLICATION_FACTOR="3"
```

---

## Docker Deployment

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/
COPY --from=builder /app/server .

EXPOSE 8080
VOLUME ["/data"]

CMD ["./server"]
```

### docker-compose.yml

```yaml
version: '3.8'

services:
  node-1:
    build: .
    container_name: clusterkit-node-1
    environment:
      - NODE_ID=node-1
      - HTTP_PORT=8080
      - DATA_DIR=/data
    ports:
      - "8080:8080"
    volumes:
      - node1-data:/data
    networks:
      - clusterkit
    restart: unless-stopped

  node-2:
    build: .
    container_name: clusterkit-node-2
    environment:
      - NODE_ID=node-2
      - HTTP_PORT=8080
      - JOIN_ADDR=node-1:8080
      - DATA_DIR=/data
    ports:
      - "8081:8080"
    volumes:
      - node2-data:/data
    networks:
      - clusterkit
    depends_on:
      - node-1
    restart: unless-stopped

  node-3:
    build: .
    container_name: clusterkit-node-3
    environment:
      - NODE_ID=node-3
      - HTTP_PORT=8080
      - JOIN_ADDR=node-1:8080
      - DATA_DIR=/data
    ports:
      - "8082:8080"
    volumes:
      - node3-data:/data
    networks:
      - clusterkit
    depends_on:
      - node-1
    restart: unless-stopped

volumes:
  node1-data:
  node2-data:
  node3-data:

networks:
  clusterkit:
    driver: bridge
```

---

## Kubernetes Deployment

### StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: clusterkit
  namespace: production
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
        env:
        - name: NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: HTTP_PORT
          value: "8080"
        - name: JOIN_ADDR
          value: "clusterkit-0.clusterkit:8080"
        - name: DATA_DIR
          value: "/data"
        - name: PARTITION_COUNT
          value: "128"
        - name: REPLICATION_FACTOR
          value: "3"
        ports:
        - containerPort: 8080
          name: http
        volumeMounts:
        - name: data
          mountPath: /data
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: clusterkit
  namespace: production
spec:
  clusterIP: None
  selector:
    app: clusterkit
  ports:
  - port: 8080
    name: http
```

---

## Monitoring

### Prometheus Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    clusterNodes = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "clusterkit_nodes_total",
        Help: "Total number of nodes in cluster",
    })
    
    healthyNodes = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "clusterkit_healthy_nodes",
        Help: "Number of healthy nodes",
    })
    
    rebalanceCount = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "clusterkit_rebalances_total",
        Help: "Total number of rebalances",
    })
    
    rebalanceDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name: "clusterkit_rebalance_duration_seconds",
        Help: "Rebalance duration in seconds",
    })
)

func init() {
    prometheus.MustRegister(clusterNodes)
    prometheus.MustRegister(healthyNodes)
    prometheus.MustRegister(rebalanceCount)
    prometheus.MustRegister(rebalanceDuration)
}

// Update metrics
ck.OnRebalanceComplete(func(event *clusterkit.RebalanceEvent, duration time.Duration) {
    rebalanceCount.Inc()
    rebalanceDuration.Observe(duration.Seconds())
})

// Expose metrics
http.Handle("/metrics", promhttp.Handler())
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "ClusterKit Monitoring",
    "panels": [
      {
        "title": "Cluster Size",
        "targets": [
          {
            "expr": "clusterkit_nodes_total"
          }
        ]
      },
      {
        "title": "Healthy Nodes",
        "targets": [
          {
            "expr": "clusterkit_healthy_nodes"
          }
        ]
      },
      {
        "title": "Rebalance Rate",
        "targets": [
          {
            "expr": "rate(clusterkit_rebalances_total[5m])"
          }
        ]
      }
    ]
  }
}
```

---

## Logging

### Structured Logging

```go
import "github.com/sirupsen/logrus"

log := logrus.New()
log.SetFormatter(&logrus.JSONFormatter{})
log.SetLevel(logrus.InfoLevel)

ck.OnNodeJoin(func(event *clusterkit.NodeJoinEvent) {
    log.WithFields(logrus.Fields{
        "event":        "node_join",
        "node_id":      event.Node.ID,
        "cluster_size": event.ClusterSize,
    }).Info("Node joined cluster")
})

ck.OnNodeLeave(func(event *clusterkit.NodeLeaveEvent) {
    log.WithFields(logrus.Fields{
        "event":   "node_leave",
        "node_id": event.Node.ID,
        "reason":  event.Reason,
    }).Warn("Node left cluster")
})
```

---

## Backup and Recovery

### Backup Strategy

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/backups/clusterkit"
DATA_DIR="/var/lib/clusterkit"
DATE=$(date +%Y%m%d_%H%M%S)

# Stop node gracefully
systemctl stop clusterkit

# Backup data
tar -czf "$BACKUP_DIR/clusterkit_$DATE.tar.gz" "$DATA_DIR"

# Start node
systemctl start clusterkit

# Cleanup old backups (keep last 7 days)
find "$BACKUP_DIR" -name "clusterkit_*.tar.gz" -mtime +7 -delete
```

### Recovery

```bash
#!/bin/bash
# restore.sh

BACKUP_FILE=$1
DATA_DIR="/var/lib/clusterkit"

# Stop node
systemctl stop clusterkit

# Clear existing data
rm -rf "$DATA_DIR"/*

# Restore backup
tar -xzf "$BACKUP_FILE" -C /

# Start node
systemctl start clusterkit
```

---

## Security

### TLS Configuration

```go
import "crypto/tls"

// Configure TLS
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    CertFile:   "/etc/clusterkit/cert.pem",
    KeyFile:    "/etc/clusterkit/key.pem",
}

// Use with HTTP server
server := &http.Server{
    Addr:      ":8443",
    TLSConfig: tlsConfig,
}
```

### Authentication

```go
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        
        if !validateToken(token) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}

http.Handle("/cluster", authMiddleware(clusterHandler))
```

---

## Performance Tuning

### Partition Count

```go
// Small cluster (3-10 nodes)
PartitionCount: 64

// Medium cluster (10-50 nodes)
PartitionCount: 128

// Large cluster (50-100 nodes)
PartitionCount: 256
```

### Replication Factor

```go
// Development
ReplicationFactor: 2  // Tolerate 1 failure

// Production
ReplicationFactor: 3  // Tolerate 2 failures

// Critical systems
ReplicationFactor: 5  // Tolerate 4 failures
```

### Resource Limits

```yaml
# Kubernetes
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

---

## Troubleshooting

### Common Issues

**Issue:** Nodes not joining cluster

**Solutions:**
- Check network connectivity
- Verify JOIN_ADDR is correct
- Check firewall rules
- Review logs for errors

**Issue:** Frequent rebalancing

**Solutions:**
- Increase health check thresholds
- Check network stability
- Review node resource usage

**Issue:** High memory usage

**Solutions:**
- Reduce partition count
- Enable Raft snapshots more frequently
- Check for memory leaks in application

---

## Runbooks

### Node Failure

```
1. Detect failure (automatic via health checks)
2. Verify node is actually down
3. Check if data loss occurred
4. Monitor rebalancing progress
5. Verify cluster stability
6. Investigate root cause
7. Replace failed node if needed
```

### Cluster Upgrade

```
1. Backup all nodes
2. Upgrade one node at a time
3. Wait for node to rejoin
4. Verify health and rebalancing
5. Proceed to next node
6. Monitor for issues
```

### Emergency Shutdown

```
1. Stop accepting new requests
2. Drain existing requests
3. Stop nodes gracefully (ck.Stop())
4. Backup data
5. Document state
```

---

## Best Practices

1. **Use odd number of nodes** (3, 5, 7) for Raft quorum
2. **Monitor health metrics** continuously
3. **Test failure scenarios** regularly
4. **Backup data** daily
5. **Use structured logging** for debugging
6. **Set resource limits** to prevent resource exhaustion
7. **Enable TLS** for production
8. **Document procedures** for operations team

---

## Further Reading

- [Architecture](./architecture.md) - System design
- [Health Checking](./health-checking.md) - Failure detection
- [Monitoring Guide](https://prometheus.io/docs/introduction/overview/) - Prometheus
- [Kubernetes Best Practices](https://kubernetes.io/docs/concepts/configuration/overview/) - K8s docs
