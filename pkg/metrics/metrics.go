package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics provides Prometheus metrics for ClusterKit
type Metrics struct {
	mu       sync.RWMutex
	registry *prometheus.Registry
	server   *http.Server
	config   *Config

	// Node metrics
	nodesTotal           prometheus.Gauge
	partitionsAssigned   *prometheus.GaugeVec
	partitionsLeader     *prometheus.GaugeVec
	heartbeatsTotal      *prometheus.CounterVec
	heartbeatDuration    *prometheus.HistogramVec

	// Rebalancing metrics
	rebalanceTotal       prometheus.Counter
	rebalanceDuration    prometheus.Histogram
	partitionMigrations  prometheus.Counter

	// etcd metrics
	etcdOperationsTotal    *prometheus.CounterVec
	etcdOperationDuration  *prometheus.HistogramVec
	etcdWatchEvents        *prometheus.CounterVec

	// Transport metrics
	httpRequestsTotal      *prometheus.CounterVec
	httpRequestDuration    *prometheus.HistogramVec
	grpcRequestsTotal      *prometheus.CounterVec
	grpcRequestDuration    *prometheus.HistogramVec

	// Application metrics
	customMetrics map[string]prometheus.Collector
}

// Config represents metrics configuration
type Config struct {
	Port      int    `json:"port"`
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Subsystem string `json:"subsystem"`
}

// Logger interface for metrics
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NewMetrics creates a new metrics instance
func NewMetrics(config *Config, logger Logger) *Metrics {
	if config.Port == 0 {
		config.Port = 2112
	}
	if config.Path == "" {
		config.Path = "/metrics"
	}
	if config.Namespace == "" {
		config.Namespace = "clusterkit"
	}

	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry:      registry,
		config:        config,
		customMetrics: make(map[string]prometheus.Collector),
	}

	m.initMetrics()
	m.registerMetrics()

	return m
}

// initMetrics initializes all Prometheus metrics
func (m *Metrics) initMetrics() {
	// Node metrics
	m.nodesTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: m.config.Namespace,
		Subsystem: "cluster",
		Name:      "nodes_total",
		Help:      "Total number of nodes in the cluster",
	})

	m.partitionsAssigned = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: m.config.Namespace,
		Subsystem: "node",
		Name:      "partitions_assigned",
		Help:      "Number of partitions assigned to this node",
	}, []string{"node_id"})

	m.partitionsLeader = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: m.config.Namespace,
		Subsystem: "node",
		Name:      "partitions_leader",
		Help:      "Number of partitions this node is leader for",
	}, []string{"node_id"})

	m.heartbeatsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "node",
		Name:      "heartbeats_total",
		Help:      "Total number of heartbeats sent",
	}, []string{"node_id", "status"})

	m.heartbeatDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: m.config.Namespace,
		Subsystem: "node",
		Name:      "heartbeat_duration_seconds",
		Help:      "Duration of heartbeat operations",
		Buckets:   prometheus.DefBuckets,
	}, []string{"node_id"})

	// Rebalancing metrics
	m.rebalanceTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "rebalance",
		Name:      "total",
		Help:      "Total number of rebalance operations",
	})

	m.rebalanceDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: m.config.Namespace,
		Subsystem: "rebalance",
		Name:      "duration_seconds",
		Help:      "Duration of rebalance operations",
		Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
	})

	m.partitionMigrations = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "partition",
		Name:      "migrations_total",
		Help:      "Total number of partition migrations",
	})

	// etcd metrics
	m.etcdOperationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "etcd",
		Name:      "operations_total",
		Help:      "Total number of etcd operations",
	}, []string{"operation", "status"})

	m.etcdOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: m.config.Namespace,
		Subsystem: "etcd",
		Name:      "operation_duration_seconds",
		Help:      "Duration of etcd operations",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation"})

	m.etcdWatchEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "etcd",
		Name:      "watch_events_total",
		Help:      "Total number of etcd watch events",
	}, []string{"event_type", "key_prefix"})

	// Transport metrics
	m.httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	m.httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: m.config.Namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "Duration of HTTP requests",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	m.grpcRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.config.Namespace,
		Subsystem: "grpc",
		Name:      "requests_total",
		Help:      "Total number of gRPC requests",
	}, []string{"method", "status"})

	m.grpcRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: m.config.Namespace,
		Subsystem: "grpc",
		Name:      "request_duration_seconds",
		Help:      "Duration of gRPC requests",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})
}

// registerMetrics registers all metrics with the registry
func (m *Metrics) registerMetrics() {
	collectors := []prometheus.Collector{
		m.nodesTotal,
		m.partitionsAssigned,
		m.partitionsLeader,
		m.heartbeatsTotal,
		m.heartbeatDuration,
		m.rebalanceTotal,
		m.rebalanceDuration,
		m.partitionMigrations,
		m.etcdOperationsTotal,
		m.etcdOperationDuration,
		m.etcdWatchEvents,
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.grpcRequestsTotal,
		m.grpcRequestDuration,
	}

	for _, collector := range collectors {
		m.registry.MustRegister(collector)
	}
}

// Start starts the metrics HTTP server
func (m *Metrics) Start() error {
	mux := http.NewServeMux()
	mux.Handle(m.config.Path, promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))

	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.config.Port),
		Handler: mux,
	}

	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't fail
		}
	}()

	return nil
}

// Stop stops the metrics server
func (m *Metrics) Stop() error {
	if m.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return m.server.Shutdown(ctx)
}

// Node Metrics Methods
func (m *Metrics) SetNodesTotal(count int) {
	m.nodesTotal.Set(float64(count))
}

func (m *Metrics) SetPartitionsAssigned(nodeID string, count int) {
	m.partitionsAssigned.WithLabelValues(nodeID).Set(float64(count))
}

func (m *Metrics) SetPartitionsLeader(nodeID string, count int) {
	m.partitionsLeader.WithLabelValues(nodeID).Set(float64(count))
}

func (m *Metrics) IncHeartbeat(nodeID, status string) {
	m.heartbeatsTotal.WithLabelValues(nodeID, status).Inc()
}

func (m *Metrics) ObserveHeartbeatDuration(nodeID string, duration time.Duration) {
	m.heartbeatDuration.WithLabelValues(nodeID).Observe(duration.Seconds())
}

// Rebalancing Metrics Methods
func (m *Metrics) IncRebalance() {
	m.rebalanceTotal.Inc()
}

func (m *Metrics) ObserveRebalanceDuration(duration time.Duration) {
	m.rebalanceDuration.Observe(duration.Seconds())
}

func (m *Metrics) IncPartitionMigration() {
	m.partitionMigrations.Inc()
}

// etcd Metrics Methods
func (m *Metrics) IncEtcdOperation(operation, status string) {
	m.etcdOperationsTotal.WithLabelValues(operation, status).Inc()
}

func (m *Metrics) ObserveEtcdOperationDuration(operation string, duration time.Duration) {
	m.etcdOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *Metrics) IncEtcdWatchEvent(eventType, keyPrefix string) {
	m.etcdWatchEvents.WithLabelValues(eventType, keyPrefix).Inc()
}

// HTTP Metrics Methods
func (m *Metrics) IncHTTPRequest(method, path, status string) {
	m.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
}

func (m *Metrics) ObserveHTTPRequestDuration(method, path string, duration time.Duration) {
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// gRPC Metrics Methods
func (m *Metrics) IncGRPCRequest(method, status string) {
	m.grpcRequestsTotal.WithLabelValues(method, status).Inc()
}

func (m *Metrics) ObserveGRPCRequestDuration(method string, duration time.Duration) {
	m.grpcRequestDuration.WithLabelValues(method).Observe(duration.Seconds())
}

// Custom Metrics Methods
func (m *Metrics) RegisterCustomMetric(name string, collector prometheus.Collector) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.customMetrics[name]; exists {
		return fmt.Errorf("metric %s already registered", name)
	}

	err := m.registry.Register(collector)
	if err != nil {
		return fmt.Errorf("failed to register metric %s: %w", name, err)
	}

	m.customMetrics[name] = collector
	return nil
}

func (m *Metrics) UnregisterCustomMetric(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	collector, exists := m.customMetrics[name]
	if !exists {
		return fmt.Errorf("metric %s not found", name)
	}

	if !m.registry.Unregister(collector) {
		return fmt.Errorf("failed to unregister metric %s", name)
	}

	delete(m.customMetrics, name)
	return nil
}

// GetRegistry returns the Prometheus registry
func (m *Metrics) GetRegistry() *prometheus.Registry {
	return m.registry
}

// Timer is a helper for timing operations
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// Duration returns the elapsed duration
func (t *Timer) Duration() time.Duration {
	return time.Since(t.start)
}

// ObserveDuration observes the duration and returns it
func (t *Timer) ObserveDuration() time.Duration {
	duration := time.Since(t.start)
	return duration
}
