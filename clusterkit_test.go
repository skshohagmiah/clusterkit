package clusterkit

import (
	"testing"
)

// TestNewClusterKit tests the initialization of ClusterKit
func TestNewClusterKit(t *testing.T) {
	opts := Options{
		NodeID:            "test-node-1",
		NodeName:          "Test Node 1",
		HTTPAddr:          ":8080",
		RaftAddr:          "127.0.0.1:9001",
		DataDir:           "/tmp/clusterkit-test",
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	if ck == nil {
		t.Fatal("ClusterKit instance is nil")
	}

	if ck.cluster.ID != opts.ClusterName {
		t.Errorf("Expected cluster ID %s, got %s", opts.ClusterName, ck.cluster.ID)
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
				HTTPAddr: ":8080",
			},
			wantErr: true,
		},
		{
			name: "missing HTTPAddr",
			opts: Options{
				NodeID: "node-1",
			},
			wantErr: true,
		},
		{
			name: "valid configuration",
			opts: Options{
				NodeID:   "node-1",
				HTTPAddr: ":8080",
				DataDir:  "/tmp/test",
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
	opts := Options{
		NodeID:            "test-node-1",
		HTTPAddr:          ":8080",
		DataDir:           "/tmp/clusterkit-test-metrics",
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
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
	opts := Options{
		NodeID:            "test-node-1",
		HTTPAddr:          ":8080",
		DataDir:           "/tmp/clusterkit-test-health",
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
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

	// NodeName is auto-generated from NodeID if not provided
	expectedNodeName := "Test-node-1" // generateNodeName("test-node-1") -> "Test-node-1"
	if health.NodeName != expectedNodeName {
		t.Errorf("Expected NodeName %s, got %s", expectedNodeName, health.NodeName)
	}

	if health.NodeCount != 1 {
		t.Errorf("Expected NodeCount 1, got %d", health.NodeCount)
	}
}

// TestGetCluster tests getting cluster information
func TestGetCluster(t *testing.T) {
	opts := Options{
		NodeID:            "test-node-1",
		HTTPAddr:          ":8080",
		DataDir:           "/tmp/clusterkit-test-cluster",
		ClusterName:       "test-cluster",
		PartitionCount:    8,
		ReplicationFactor: 2,
	}

	ck, err := NewClusterKit(opts)
	if err != nil {
		t.Fatalf("Failed to create ClusterKit: %v", err)
	}

	cluster := ck.GetCluster()
	if cluster == nil {
		t.Fatal("Cluster is nil")
	}

	if cluster.ID != opts.ClusterName {
		t.Errorf("Expected cluster ID %s, got %s", opts.ClusterName, cluster.ID)
	}

	if cluster.Name != opts.ClusterName {
		t.Errorf("Expected cluster name %s, got %s", opts.ClusterName, cluster.Name)
	}

	if len(cluster.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(cluster.Nodes))
	}
}
