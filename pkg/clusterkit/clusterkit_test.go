package clusterkit

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	config := &Config{
		NodeID:        "test-node",
		AdvertiseAddr: "localhost:9000",
	}

	config.SetDefaults()

	if config.HTTPPort != 8080 {
		t.Errorf("Expected HTTPPort to be 8080, got %d", config.HTTPPort)
	}

	if config.GRPCPort != 9000 {
		t.Errorf("Expected GRPCPort to be 9000, got %d", config.GRPCPort)
	}

	if config.Partitions != 256 {
		t.Errorf("Expected Partitions to be 256, got %d", config.Partitions)
	}

	if config.ReplicaFactor != 3 {
		t.Errorf("Expected ReplicaFactor to be 3, got %d", config.ReplicaFactor)
	}

	if config.EtcdPrefix != "/clusterkit" {
		t.Errorf("Expected EtcdPrefix to be '/clusterkit', got %s", config.EtcdPrefix)
	}

	if config.HeartbeatInterval != 5*time.Second {
		t.Errorf("Expected HeartbeatInterval to be 5s, got %v", config.HeartbeatInterval)
	}

	if config.SessionTTL != 10*time.Second {
		t.Errorf("Expected SessionTTL to be 10s, got %v", config.SessionTTL)
	}

	if config.RebalanceDelay != 5*time.Second {
		t.Errorf("Expected RebalanceDelay to be 5s, got %v", config.RebalanceDelay)
	}

	if config.MetricsPort != 2112 {
		t.Errorf("Expected MetricsPort to be 2112, got %d", config.MetricsPort)
	}

	if len(config.EtcdEndpoints) != 1 || config.EtcdEndpoints[0] != "http://localhost:2379" {
		t.Errorf("Expected EtcdEndpoints to be ['http://localhost:2379'], got %v", config.EtcdEndpoints)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &Config{
				NodeID:        "test-node",
				AdvertiseAddr: "localhost:9000",
				Partitions:    16,
				ReplicaFactor: 2,
			},
			expectError: false,
		},
		{
			name: "missing NodeID",
			config: &Config{
				AdvertiseAddr: "localhost:9000",
				Partitions:    16,
				ReplicaFactor: 2,
			},
			expectError: true,
		},
		{
			name: "missing AdvertiseAddr",
			config: &Config{
				NodeID:        "test-node",
				Partitions:    16,
				ReplicaFactor: 2,
			},
			expectError: true,
		},
		{
			name: "zero partitions",
			config: &Config{
				NodeID:        "test-node",
				AdvertiseAddr: "localhost:9000",
				Partitions:    0,
				ReplicaFactor: 2,
			},
			expectError: true,
		},
		{
			name: "zero replica factor",
			config: &Config{
				NodeID:        "test-node",
				AdvertiseAddr: "localhost:9000",
				Partitions:    16,
				ReplicaFactor: 0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestClientConfigDefaults(t *testing.T) {
	config := &ClientConfig{}
	config.SetDefaults()

	if config.EtcdPrefix != "/clusterkit" {
		t.Errorf("Expected EtcdPrefix to be '/clusterkit', got %s", config.EtcdPrefix)
	}

	if config.CacheTTL != 30*time.Second {
		t.Errorf("Expected CacheTTL to be 30s, got %v", config.CacheTTL)
	}

	if config.DialTimeout != 5*time.Second {
		t.Errorf("Expected DialTimeout to be 5s, got %v", config.DialTimeout)
	}

	if config.RequestTimeout != 3*time.Second {
		t.Errorf("Expected RequestTimeout to be 3s, got %v", config.RequestTimeout)
	}

	if len(config.EtcdEndpoints) != 1 || config.EtcdEndpoints[0] != "http://localhost:2379" {
		t.Errorf("Expected EtcdEndpoints to be ['http://localhost:2379'], got %v", config.EtcdEndpoints)
	}
}

func TestNodeHTTPAddr(t *testing.T) {
	node := &Node{
		HTTPEndpoint: "localhost:8080",
	}

	expected := "http://localhost:8080"
	if node.HTTPAddr() != expected {
		t.Errorf("Expected HTTPAddr to be %s, got %s", expected, node.HTTPAddr())
	}
}

func TestNodeGRPCAddr(t *testing.T) {
	node := &Node{
		GRPCEndpoint: "localhost:9000",
	}

	expected := "localhost:9000"
	if node.GRPCAddr() != expected {
		t.Errorf("Expected GRPCAddr to be %s, got %s", expected, node.GRPCAddr())
	}
}
