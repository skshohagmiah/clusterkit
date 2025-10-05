package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/shohag/clusterkit/pkg/clusterkit"
	"github.com/shohag/clusterkit/pkg/etcd"
	"github.com/shohag/clusterkit/pkg/hash"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Client provides routing capabilities for ClusterKit clusters
type Client struct {
	mu           sync.RWMutex
	config       *clusterkit.ClientConfig
	store        *etcd.Store
	ring         *hash.PartitionRing
	nodes        map[string]*clusterkit.Node
	assignments  map[int]*clusterkit.PartitionInfo
	cache        *Cache
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewClient creates a new ClusterKit client
func NewClient(config *clusterkit.ClientConfig) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	config.SetDefaults()

	// Create etcd store
	store, err := etcd.NewStore(config.EtcdEndpoints, config.EtcdPrefix, &defaultLogger{})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd store: %w", err)
	}

	client := &Client{
		config:      config,
		store:       store,
		ring:        hash.NewPartitionRing(256, 3), // Default values, will be updated
		nodes:       make(map[string]*clusterkit.Node),
		assignments: make(map[int]*clusterkit.PartitionInfo),
		stopCh:      make(chan struct{}),
	}

	// Initialize cache if enabled
	if config.CacheEnabled {
		client.cache = NewCache(config.CacheTTL)
	}

	// Start watching for changes
	err = client.start()
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	return client, nil
}

// start starts the client and begins watching for cluster changes
func (c *Client) start() error {
	ctx := context.Background()

	// Load initial cluster state
	err := c.loadClusterState(ctx)
	if err != nil {
		return fmt.Errorf("failed to load cluster state: %w", err)
	}

	// Start watchers
	c.wg.Add(2)
	go c.watchNodes(ctx)
	go c.watchPartitions(ctx)

	return nil
}

// Close closes the client and stops all watchers
func (c *Client) Close() error {
	close(c.stopCh)
	c.wg.Wait()

	if c.cache != nil {
		c.cache.Close()
	}

	return c.store.Close()
}

// GetPartition returns the partition ID for a given key
func (c *Client) GetPartition(key string) int {
	if c.cache != nil {
		if partition, found := c.cache.GetPartition(key); found {
			return partition
		}
	}

	partition := c.ring.GetPartition(key)

	if c.cache != nil {
		c.cache.SetPartition(key, partition)
	}

	return partition
}

// GetLeader returns the leader node for a partition
func (c *Client) GetLeader(partition int) *clusterkit.Node {
	if c.cache != nil {
		if leader, found := c.cache.GetLeader(partition); found {
			return leader
		}
	}

	c.mu.RLock()
	info, exists := c.assignments[partition]
	c.mu.RUnlock()

	if !exists || info.Leader == nil {
		return nil
	}

	leader := info.Leader

	if c.cache != nil {
		c.cache.SetLeader(partition, leader)
	}

	return leader
}

// GetReplicas returns all replica nodes for a partition
func (c *Client) GetReplicas(partition int) []*clusterkit.Node {
	if c.cache != nil {
		if replicas, found := c.cache.GetReplicas(partition); found {
			return replicas
		}
	}

	c.mu.RLock()
	info, exists := c.assignments[partition]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	replicas := make([]*clusterkit.Node, len(info.Replicas))
	copy(replicas, info.Replicas)

	if c.cache != nil {
		c.cache.SetReplicas(partition, replicas)
	}

	return replicas
}

// GetNodes returns all alive nodes in the cluster
func (c *Client) GetNodes() []*clusterkit.Node {
	if c.cache != nil {
		if nodes, found := c.cache.GetNodes(); found {
			return nodes
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]*clusterkit.Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		// Create a copy to avoid race conditions
		nodeCopy := *node
		nodes = append(nodes, &nodeCopy)
	}

	if c.cache != nil {
		c.cache.SetNodes(nodes)
	}

	return nodes
}

// loadClusterState loads the initial cluster state from etcd
func (c *Client) loadClusterState(ctx context.Context) error {
	// Load nodes
	err := c.loadNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	// Load partition assignments
	err = c.loadPartitions(ctx)
	if err != nil {
		return fmt.Errorf("failed to load partitions: %w", err)
	}

	return nil
}

// loadNodes loads all nodes from etcd
func (c *Client) loadNodes(ctx context.Context) error {
	nodes, err := c.store.List(ctx, "nodes/")
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, data := range nodes {
		nodeID := strings.TrimPrefix(key, "nodes/")

		var node clusterkit.Node
		err = json.Unmarshal(data, &node)
		if err != nil {
			continue
		}

		c.nodes[nodeID] = &node
		c.ring.AddNode(nodeID)
	}

	return nil
}

// loadPartitions loads all partition assignments from etcd
func (c *Client) loadPartitions(ctx context.Context) error {
	partitions, err := c.store.List(ctx, "partitions/")
	if err != nil {
		return fmt.Errorf("failed to list partitions: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, data := range partitions {
		var partition int
		_, err = fmt.Sscanf(key, "partitions/%d", &partition)
		if err != nil {
			continue
		}

		var info clusterkit.PartitionInfo
		err = json.Unmarshal(data, &info)
		if err != nil {
			continue
		}

		c.assignments[partition] = &info
	}

	return nil
}

// watchNodes watches for node membership changes
func (c *Client) watchNodes(ctx context.Context) {
	defer c.wg.Done()

	watchCh := c.store.Watch(ctx, "nodes/")

	for {
		select {
		case <-c.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				return
			}

			for _, event := range watchResp.Events {
				c.handleNodeEvent(event)
			}
		}
	}
}

// watchPartitions watches for partition assignment changes
func (c *Client) watchPartitions(ctx context.Context) {
	defer c.wg.Done()

	watchCh := c.store.Watch(ctx, "partitions/")

	for {
		select {
		case <-c.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				return
			}

			for _, event := range watchResp.Events {
				c.handlePartitionEvent(event)
			}
		}
	}
}

// handleNodeEvent handles node membership change events
func (c *Client) handleNodeEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	nodeID := strings.TrimPrefix(strings.TrimPrefix(key, c.config.EtcdPrefix), "/nodes/")

	c.mu.Lock()
	defer c.mu.Unlock()

	switch event.Type {
	case clientv3.EventTypePut:
		var node clusterkit.Node
		err := json.Unmarshal(event.Kv.Value, &node)
		if err != nil {
			return
		}

		isNew := c.nodes[nodeID] == nil
		c.nodes[nodeID] = &node

		if isNew {
			c.ring.AddNode(nodeID)
		}

	case clientv3.EventTypeDelete:
		delete(c.nodes, nodeID)
		c.ring.RemoveNode(nodeID)
	}

	// Invalidate cache
	if c.cache != nil {
		c.cache.InvalidateNodes()
	}
}

// handlePartitionEvent handles partition assignment change events
func (c *Client) handlePartitionEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	var partition int
	_, err := fmt.Sscanf(key, c.config.EtcdPrefix+"/partitions/%d", &partition)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch event.Type {
	case clientv3.EventTypePut:
		var info clusterkit.PartitionInfo
		err := json.Unmarshal(event.Kv.Value, &info)
		if err != nil {
			return
		}

		c.assignments[partition] = &info

	case clientv3.EventTypeDelete:
		delete(c.assignments, partition)
	}

	// Invalidate cache for this partition
	if c.cache != nil {
		c.cache.InvalidatePartition(partition)
	}
}

// defaultLogger is a simple logger implementation for the client
type defaultLogger struct{}

func (l *defaultLogger) Debugf(format string, args ...interface{}) {}
func (l *defaultLogger) Infof(format string, args ...interface{})  {}
func (l *defaultLogger) Warnf(format string, args ...interface{})  {}
func (l *defaultLogger) Errorf(format string, args ...interface{}) {}
