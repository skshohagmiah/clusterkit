package clusterkit

import (
	"testing"
)

// TestNewClusterKit tests the initialization of ClusterKit
func TestNewClusterKit(t *testing.T) {
	config := &Config{
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	opts := Options{
		NodeID:   "test-node-1",
		NodeName: "Test Node 1",
		HTTPAddr: ":8080",
		RaftAddr: "127.0.0.1:9001",
		DataDir:  "/tmp/clusterkit-test",
		Config:   config,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	if ck == nil {
		t.Fatal("ClusterKit instance is nil")
	}

	if ck.cluster.ID != config.ClusterName {
		t.Errorf("Expected cluster ID %s, got %s", config.ClusterName, ck.cluster.ID)
	}

	if len(ck.cluster.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(ck.cluster.Nodes))
	}

	if ck.cluster.Nodes[0].ID != opts.NodeID {
		t.Errorf("Expected node ID %s, got %s", opts.NodeID, ck.cluster.Nodes[0].ID)
	}
}

// TestNewClusterKitValidation tests input validation
func TestNewClusterKitValidation(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name: "missing NodeID",
			opts: Options{
				NodeName: "Test Node",
				HTTPAddr: ":8080",
				RaftAddr: "127.0.0.1:9001",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    8,
					ReplicationFactor: 2,
				},
			},
			wantErr: true,
		},
		{
			name: "missing NodeName",
			opts: Options{
				NodeID:   "node-1",
				HTTPAddr: ":8080",
				RaftAddr: "127.0.0.1:9001",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    8,
					ReplicationFactor: 2,
				},
			},
			wantErr: true,
		},
		{
			name: "missing HTTPAddr",
			opts: Options{
				NodeID:   "node-1",
				NodeName: "Test Node",
				RaftAddr: "127.0.0.1:9001",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    8,
					ReplicationFactor: 2,
				},
			},
			wantErr: true,
		},
		{
			name: "missing RaftAddr",
			opts: Options{
				NodeID:   "node-1",
				NodeName: "Test Node",
				HTTPAddr: ":8080",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    8,
					ReplicationFactor: 2,
				},
			},
			wantErr: true,
		},
		{
			name: "nil Config",
			opts: Options{
				NodeID:   "node-1",
				NodeName: "Test Node",
				HTTPAddr: ":8080",
				RaftAddr: "127.0.0.1:9001",
			},
			wantErr: true,
		},
		{
			name: "invalid PartitionCount",
			opts: Options{
				NodeID:   "node-1",
				NodeName: "Test Node",
				HTTPAddr: ":8080",
				RaftAddr: "127.0.0.1:9001",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    0,
					ReplicationFactor: 2,
				},
			},
			wantErr: true,
		},
		{
			name: "valid configuration",
			opts: Options{
				NodeID:   "node-1",
				NodeName: "Test Node",
				HTTPAddr: ":8080",
				RaftAddr: "127.0.0.1:9001",
				DataDir:  "/tmp/test",
				Config: &Config{
					ClusterName:       "test",
					PartitionCount:    8,
					ReplicationFactor: 2,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClusterKit(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClusterKit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetMetrics tests the metrics functionality
func TestGetMetrics(t *testing.T) {
	config := &Config{
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	opts := Options{
		NodeID:   "test-node-1",
		NodeName: "Test Node 1",
		HTTPAddr: ":8080",
		RaftAddr: "127.0.0.1:9001",
		DataDir:  "/tmp/clusterkit-test-metrics",
		Config:   config,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	metrics := ck.GetMetrics()
	if metrics == nil {
		t.Fatal("Metrics is nil")
	}

	if metrics.NodeCount != 1 {
		t.Errorf("Expected NodeCount 1, got %d", metrics.NodeCount)
	}

	if metrics.UptimeSeconds < 0 {
		t.Errorf("Expected positive uptime, got %d", metrics.UptimeSeconds)
	}
}

// TestHealthCheck tests the health check functionality
func TestHealthCheck(t *testing.T) {
	config := &Config{
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	opts := Options{
		NodeID:   "test-node-1",
		NodeName: "Test Node 1",
		HTTPAddr: ":8080",
		RaftAddr: "127.0.0.1:9001",
		DataDir:  "/tmp/clusterkit-test-health",
		Config:   config,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	health := ck.HealthCheck()
	if health == nil {
		t.Fatal("Health status is nil")
	}

	if health.NodeID != opts.NodeID {
		t.Errorf("Expected NodeID %s, got %s", opts.NodeID, health.NodeID)
	}

	if health.NodeName != opts.NodeName {
		t.Errorf("Expected NodeName %s, got %s", opts.NodeName, health.NodeName)
	}

	if health.NodeCount != 1 {
		t.Errorf("Expected NodeCount 1, got %d", health.NodeCount)
	}
}

// TestGetCluster tests getting cluster information
func TestGetCluster(t *testing.T) {
	config := &Config{
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	opts := Options{
		NodeID:   "test-node-1",
		NodeName: "Test Node 1",
		HTTPAddr: ":8080",
		RaftAddr: "127.0.0.1:9001",
		DataDir:  "/tmp/clusterkit-test-cluster",
		Config:   config,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	cluster := ck.GetCluster()
	if cluster == nil {
		t.Fatal("Cluster is nil")
	}

	if cluster.ID != config.ClusterName {
		t.Errorf("Expected cluster ID %s, got %s", config.ClusterName, cluster.ID)
	}

	if cluster.Name != config.ClusterName {
		t.Errorf("Expected cluster name %s, got %s", config.ClusterName, cluster.Name)
	}

	if len(cluster.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(cluster.Nodes))
	}
}
