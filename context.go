package clusterkit

import (
	"context"
	"encoding/base64"
	"fmt"
)

// SetCustomDataContext stores custom data with context support for cancellation
// The data is stored as raw bytes, allowing any serialization format (JSON, protobuf, msgpack, etc.)
// This operation goes through Raft consensus and will fail if this node is not the leader
// Maximum value size is 1MB to prevent excessive memory usage
func (ck *ClusterKit) SetCustomDataContext(ctx context.Context, key string, value []byte) error {
	// Check context before expensive operations
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if len(value) > 1024*1024 { // 1MB limit
		return fmt.Errorf("value too large (max 1MB, got %d bytes)", len(value))
	}

	// Encode value as base64 for JSON serialization
	valueEncoded := base64.StdEncoding.EncodeToString(value)

	return ck.consensusManager.ProposeAction("set_custom_data", map[string]interface{}{
		"key":   key,
		"value": valueEncoded,
	})
}

// GetCustomDataContext retrieves custom data with context support
// This is a local read operation and does not require consensus
// Returns an error if the key is not found
func (ck *ClusterKit) GetCustomDataContext(ctx context.Context, key string) ([]byte, error) {
	// Check context
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	ck.mu.RLock()
	defer ck.mu.RUnlock()

	if ck.cluster.CustomData == nil {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	value, exists := ck.cluster.CustomData[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// Return a copy to prevent external modification
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	return valueCopy, nil
}

// DeleteCustomDataContext removes custom data with context support
// This operation goes through Raft consensus and will fail if this node is not the leader
func (ck *ClusterKit) DeleteCustomDataContext(ctx context.Context, key string) error {
	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	return ck.consensusManager.ProposeAction("delete_custom_data", key)
}

// CreatePartitionsContext initializes partitions with context support
func (ck *ClusterKit) CreatePartitionsContext(ctx context.Context) error {
	// Check context before expensive operation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return ck.CreatePartitions()
}

// RebalancePartitionsContext redistributes partitions with context support
func (ck *ClusterKit) RebalancePartitionsContext(ctx context.Context) error {
	// Check context before expensive operation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return ck.RebalancePartitions()
}
