package clusterkit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSetCustomData verifies basic custom data storage
func TestSetCustomData(t *testing.T) {
	// Create minimal ClusterKit for testing
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: make(map[string][]byte),
		},
		mu: sync.RWMutex{},
	}

	// Test data
	key := "test-key"
	value := []byte("test-value")

	// Manually set data (bypassing Raft for unit test)
	ck.cluster.CustomData[key] = value

	// Retrieve data
	retrieved, err := ck.GetCustomData(key)
	if err != nil {
		t.Fatalf("GetCustomData failed: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected %s, got %s", value, retrieved)
	}
}

// TestGetCustomDataNotFound verifies error handling for missing keys
func TestGetCustomDataNotFound(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: make(map[string][]byte),
		},
		mu: sync.RWMutex{},
	}

	_, err := ck.GetCustomData("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent key, got nil")
	}
}

// TestCustomDataSizeLimit verifies 1MB size limit
func TestCustomDataSizeLimit(t *testing.T) {
	ck := &ClusterKit{
		consensusManager: &ConsensusManager{},
	}

	// Create data larger than 1MB
	largeData := make([]byte, 1024*1024+1)

	err := ck.SetCustomData("large-key", largeData)
	if err == nil {
		t.Error("Expected error for data exceeding 1MB limit")
	}
}

// TestCustomDataConcurrency verifies thread safety
func TestCustomDataConcurrency(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: make(map[string][]byte),
		},
		mu: sync.RWMutex{},
	}

	// Concurrent writes and reads
	var wg sync.WaitGroup
	numGoroutines := 10

	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "key-" + string(rune('0'+id))
			ck.cluster.CustomData[key] = []byte("value")
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ck.ListCustomDataKeys()
		}()
	}

	wg.Wait()
	// Test passes if no race conditions detected
}

// TestListCustomDataKeys verifies key listing
func TestListCustomDataKeys(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: map[string][]byte{
				"key1": []byte("value1"),
				"key2": []byte("value2"),
				"key3": []byte("value3"),
			},
		},
		mu: sync.RWMutex{},
	}

	keys := ck.ListCustomDataKeys()

	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Verify all keys present
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	for _, expectedKey := range []string{"key1", "key2", "key3"} {
		if !keyMap[expectedKey] {
			t.Errorf("Expected key %s not found", expectedKey)
		}
	}
}

// TestCustomDataContext verifies context cancellation
func TestCustomDataContext(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: make(map[string][]byte),
		},
		mu: sync.RWMutex{},
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should return context.Canceled error
	_, err := ck.GetCustomDataContext(ctx, "test-key")
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

// TestCustomDataContextTimeout verifies timeout handling
func TestCustomDataContextTimeout(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: make(map[string][]byte),
		},
		mu: sync.RWMutex{},
	}

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure timeout

	_, err := ck.GetCustomDataContext(ctx, "test-key")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

// TestCustomDataCopy verifies returned data is a copy
func TestCustomDataCopy(t *testing.T) {
	ck := &ClusterKit{
		cluster: &Cluster{
			CustomData: map[string][]byte{
				"test": []byte("original"),
			},
		},
		mu: sync.RWMutex{},
	}

	// Get data
	retrieved, err := ck.GetCustomData("test")
	if err != nil {
		t.Fatalf("GetCustomData failed: %v", err)
	}

	// Modify retrieved data
	retrieved[0] = 'X'

	// Verify original data unchanged
	original := ck.cluster.CustomData["test"]
	if original[0] != 'o' {
		t.Error("Original data was modified (copy protection failed)")
	}
}
