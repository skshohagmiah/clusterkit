package clusterkit

import (
	"sync"
	"time"
)

// Node represents a single node in the cluster
type Node struct {
	ID       string            `json:"id"`
	IP       string            `json:"ip"`
	Port     int               `json:"port"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Services map[string]string `json:"services,omitempty"` // Service name -> address mapping
}

// Partition represents a data partition in the cluster
type Partition struct {
	ID           string   `json:"id"`
	PrimaryNode  string   `json:"primary_node"`
	ReplicaNodes []string `json:"replica_nodes"`
}

// PartitionMap manages all partitions in the cluster
type PartitionMap struct {
	Partitions map[string]*Partition `json:"partitions"`
	mu         sync.RWMutex
}

// Cluster represents the entire distributed cluster
type Cluster struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Nodes        []Node           `json:"nodes"`
	NodeMap      map[string]*Node `json:"-"` // O(1) node lookup by ID
	PartitionMap *PartitionMap    `json:"partition_map"`
	Config       *Config          `json:"config"`
}

// Config holds cluster configuration
type Config struct {
	ClusterName       string `json:"cluster_name"`
	PartitionCount    int    `json:"partition_count"`
	ReplicationFactor int    `json:"replication_factor"`
}

// Result represents the outcome of an operation
type Result struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// WALEntry represents a write-ahead log entry
type WALEntry struct {
	Operation string      `json:"operation"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// Metrics represents cluster metrics and monitoring data
type Metrics struct {
	NodeCount      int       `json:"node_count"`
	PartitionCount int       `json:"partition_count"`
	RequestCount   int64     `json:"request_count"`
	ErrorCount     int64     `json:"error_count"`
	LastSync       time.Time `json:"last_sync"`
	IsLeader       bool      `json:"is_leader"`
	RaftState      string    `json:"raft_state"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
}

// HealthStatus represents detailed health information
type HealthStatus struct {
	Healthy        bool      `json:"healthy"`
	NodeID         string    `json:"node_id"`
	NodeName       string    `json:"node_name"`
	IsLeader       bool      `json:"is_leader"`
	NodeCount      int       `json:"node_count"`
	PartitionCount int       `json:"partition_count"`
	RaftState      string    `json:"raft_state"`
	LastSync       time.Time `json:"last_sync"`
	Uptime         string    `json:"uptime"`
}
