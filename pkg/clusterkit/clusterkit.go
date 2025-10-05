package clusterkit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/etcd"
)

// ClusterKit is the main entry point for the clustering library
type ClusterKit struct {
	mu          sync.RWMutex
	config      *Config
	logger      Logger
	store       *etcd.Store
	membership  *MembershipManager
	partitions  *PartitionManager
	ctx         context.Context
	cancel      context.CancelFunc
	started     bool
}

// Join creates a new ClusterKit instance and joins the cluster
func Join(config *Config) (*ClusterKit, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Set defaults and validate
	config.SetDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if config.Logger == nil {
		config.Logger = &DefaultLogger{}
	}

	// Create etcd store
	store, err := etcd.NewStore(config.EtcdEndpoints, config.EtcdPrefix, config.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd store: %w", err)
	}

	// Create membership manager
	membership, err := NewMembershipManager(store, config)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("failed to create membership manager: %w", err)
	}

	// Create partition manager
	partitionManager := NewPartitionManager(store, config, membership)

	ctx, cancel := context.WithCancel(context.Background())

	ck := &ClusterKit{
		config:     config,
		logger:     config.Logger,
		store:      store,
		membership: membership,
		partitions: partitionManager,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start components
	err = ck.start()
	if err != nil {
		cancel()
		store.Close()
		return nil, fmt.Errorf("failed to start ClusterKit: %w", err)
	}

	config.Logger.Infof("ClusterKit node %s joined cluster successfully", config.NodeID)
	return ck, nil
}

// start starts all ClusterKit components
func (ck *ClusterKit) start() error {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	if ck.started {
		return fmt.Errorf("ClusterKit already started")
	}

	// Start membership manager
	err := ck.membership.Start(ck.ctx)
	if err != nil {
		return fmt.Errorf("failed to start membership manager: %w", err)
	}

	// Start partition manager
	err = ck.partitions.Start(ck.ctx)
	if err != nil {
		ck.membership.Stop()
		return fmt.Errorf("failed to start partition manager: %w", err)
	}

	ck.started = true
	return nil
}

// Leave gracefully leaves the cluster
func (ck *ClusterKit) Leave() error {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	if !ck.started {
		return fmt.Errorf("ClusterKit not started")
	}

	ck.logger.Infof("Node %s leaving cluster", ck.config.NodeID)

	// Cancel context to stop all operations
	ck.cancel()

	// Stop partition manager
	err := ck.partitions.Stop()
	if err != nil {
		ck.logger.Errorf("Failed to stop partition manager: %v", err)
	}

	// Stop membership manager
	err = ck.membership.Stop()
	if err != nil {
		ck.logger.Errorf("Failed to stop membership manager: %v", err)
	}

	// Close etcd store
	err = ck.store.Close()
	if err != nil {
		ck.logger.Errorf("Failed to close etcd store: %v", err)
	}

	ck.started = false
	ck.logger.Infof("Node %s left cluster successfully", ck.config.NodeID)
	return nil
}

// GetPartition returns the partition ID for a given key
func (ck *ClusterKit) GetPartition(key string) int {
	return ck.partitions.GetPartition(key)
}

// GetLeader returns the leader node for a partition
func (ck *ClusterKit) GetLeader(partition int) *Node {
	return ck.partitions.GetLeader(partition)
}

// GetReplicas returns all replica nodes for a partition
func (ck *ClusterKit) GetReplicas(partition int) []*Node {
	return ck.partitions.GetReplicas(partition)
}

// IsLeader checks if this node is the leader for a partition
func (ck *ClusterKit) IsLeader(partition int) bool {
	return ck.partitions.IsLeader(partition)
}

// IsReplica checks if this node is a replica for a partition
func (ck *ClusterKit) IsReplica(partition int) bool {
	return ck.partitions.IsReplica(partition)
}

// GetNodes returns all alive nodes in the cluster
func (ck *ClusterKit) GetNodes() []*Node {
	return ck.membership.GetNodes()
}

// NodeID returns this node's ID
func (ck *ClusterKit) NodeID() string {
	return ck.config.NodeID
}

// GetPartitionMap returns the complete partition assignment map for debugging
func (ck *ClusterKit) GetPartitionMap() map[int][]*Node {
	return ck.partitions.GetPartitionMap()
}

// IsHealthy returns true if the ClusterKit instance is healthy
func (ck *ClusterKit) IsHealthy() bool {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	if !ck.started {
		return false
	}

	// Check if we can still reach etcd
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Try to get our own node info as a health check
	localNode := ck.membership.GetLocalNode()
	if localNode == nil {
		return false
	}

	var node Node
	key := fmt.Sprintf("nodes/%s", localNode.ID)
	err := ck.store.Get(ctx, key, &node)
	return err == nil
}

// GetConfig returns a copy of the configuration
func (ck *ClusterKit) GetConfig() Config {
	return *ck.config
}

// GetStats returns cluster statistics
func (ck *ClusterKit) GetStats() ClusterStats {
	nodes := ck.GetNodes()
	partitionMap := ck.GetPartitionMap()
	
	localPartitions := 0
	leaderPartitions := 0
	
	for partition := range partitionMap {
		if ck.IsReplica(partition) {
			localPartitions++
		}
		if ck.IsLeader(partition) {
			leaderPartitions++
		}
	}

	return ClusterStats{
		NodeCount:        len(nodes),
		PartitionCount:   ck.config.Partitions,
		LocalPartitions:  localPartitions,
		LeaderPartitions: leaderPartitions,
		ReplicaFactor:    ck.config.ReplicaFactor,
	}
}

// ClusterStats represents cluster statistics
type ClusterStats struct {
	NodeCount        int `json:"node_count"`
	PartitionCount   int `json:"partition_count"`
	LocalPartitions  int `json:"local_partitions"`
	LeaderPartitions int `json:"leader_partitions"`
	ReplicaFactor    int `json:"replica_factor"`
}
