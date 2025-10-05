package clusterkit

import (
	"fmt"
	"time"
)

// Node represents a cluster node
type Node struct {
	ID            string            `json:"id"`
	Addr          string            `json:"addr"`
	HTTPEndpoint  string            `json:"http_endpoint"`
	GRPCEndpoint  string            `json:"grpc_endpoint"`
	Metadata      map[string]string `json:"metadata"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
}

// HTTPAddr returns the HTTP address for this node
func (n *Node) HTTPAddr() string {
	return fmt.Sprintf("http://%s", n.HTTPEndpoint)
}

// GRPCAddr returns the gRPC address for this node
func (n *Node) GRPCAddr() string {
	return n.GRPCEndpoint
}

// Config represents the configuration for ClusterKit
type Config struct {
	// Identity
	NodeID        string `json:"node_id"`
	AdvertiseAddr string `json:"advertise_addr"`

	// Ports
	HTTPPort int `json:"http_port"`
	GRPCPort int `json:"grpc_port"`

	// Cluster
	Partitions    int `json:"partitions"`
	ReplicaFactor int `json:"replica_factor"`

	// etcd
	EtcdEndpoints []string `json:"etcd_endpoints"`
	EtcdPrefix    string   `json:"etcd_prefix"`

	// Timeouts
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	SessionTTL        time.Duration `json:"session_ttl"`
	RebalanceDelay    time.Duration `json:"rebalance_delay"`

	// Hooks
	OnPartitionAssigned   func(partition int, previousOwner *Node)
	OnPartitionUnassigned func(partition int, newOwner *Node)
	OnLeaderElected       func(partition int)
	OnLeaderLost          func(partition int)
	OnReplicaAdded        func(partition int, node *Node)
	OnReplicaRemoved      func(partition int, node *Node)

	// Optional
	Metadata    map[string]string `json:"metadata"`
	Logger      Logger            `json:"-"`
	MetricsPort int               `json:"metrics_port"`
}

// SetDefaults sets default values for the configuration
func (c *Config) SetDefaults() {
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.GRPCPort == 0 {
		c.GRPCPort = 9000
	}
	if c.Partitions == 0 {
		c.Partitions = 256
	}
	if c.ReplicaFactor == 0 {
		c.ReplicaFactor = 3
	}
	if c.EtcdPrefix == "" {
		c.EtcdPrefix = "/clusterkit"
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = 10 * time.Second
	}
	if c.RebalanceDelay == 0 {
		c.RebalanceDelay = 5 * time.Second
	}
	if c.MetricsPort == 0 {
		c.MetricsPort = 2112
	}
	if c.Metadata == nil {
		c.Metadata = make(map[string]string)
	}
	if len(c.EtcdEndpoints) == 0 {
		c.EtcdEndpoints = []string{"http://localhost:2379"}
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return fmt.Errorf("NodeID is required")
	}
	if c.AdvertiseAddr == "" {
		return fmt.Errorf("AdvertiseAddr is required")
	}
	if c.Partitions <= 0 {
		return fmt.Errorf("Partitions must be greater than 0")
	}
	if c.ReplicaFactor <= 0 {
		return fmt.Errorf("ReplicaFactor must be greater than 0")
	}
	return nil
}

// ClientConfig represents the configuration for ClusterKit client
type ClientConfig struct {
	EtcdEndpoints  []string      `json:"etcd_endpoints"`
	EtcdPrefix     string        `json:"etcd_prefix"`
	CacheEnabled   bool          `json:"cache_enabled"`
	CacheTTL       time.Duration `json:"cache_ttl"`
	DialTimeout    time.Duration `json:"dial_timeout"`
	RequestTimeout time.Duration `json:"request_timeout"`
}

// SetDefaults sets default values for the client configuration
func (c *ClientConfig) SetDefaults() {
	if c.EtcdPrefix == "" {
		c.EtcdPrefix = "/clusterkit"
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = 30 * time.Second
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 3 * time.Second
	}
	if len(c.EtcdEndpoints) == 0 {
		c.EtcdEndpoints = []string{"http://localhost:2379"}
	}
}

// PartitionInfo represents partition assignment information
type PartitionInfo struct {
	ID       int     `json:"id"`
	Leader   *Node   `json:"leader"`
	Replicas []*Node `json:"replicas"`
}

// Logger interface for pluggable logging
type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// DefaultLogger is a simple logger implementation
type DefaultLogger struct{}

func (l *DefaultLogger) Debug(args ...interface{})                 {}
func (l *DefaultLogger) Info(args ...interface{})                  {}
func (l *DefaultLogger) Warn(args ...interface{})                  {}
func (l *DefaultLogger) Error(args ...interface{})                 {}
func (l *DefaultLogger) Debugf(format string, args ...interface{}) {}
func (l *DefaultLogger) Infof(format string, args ...interface{})  {}
func (l *DefaultLogger) Warnf(format string, args ...interface{})  {}
func (l *DefaultLogger) Errorf(format string, args ...interface{}) {}
