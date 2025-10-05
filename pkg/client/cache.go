package client

import (
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// CacheEntry represents a cached entry with expiration
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// IsExpired checks if the cache entry has expired
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Cache provides caching for cluster topology information
type Cache struct {
	mu          sync.RWMutex
	ttl         time.Duration
	partitions  map[string]*CacheEntry // key -> partition
	leaders     map[int]*CacheEntry    // partition -> leader
	replicas    map[int]*CacheEntry    // partition -> replicas
	nodes       *CacheEntry            // all nodes
	stopCh      chan struct{}
	cleanupDone chan struct{}
}

// NewCache creates a new cache with the specified TTL
func NewCache(ttl time.Duration) *Cache {
	cache := &Cache{
		ttl:         ttl,
		partitions:  make(map[string]*CacheEntry),
		leaders:     make(map[int]*CacheEntry),
		replicas:    make(map[int]*CacheEntry),
		stopCh:      make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Close stops the cache and cleanup goroutine
func (c *Cache) Close() {
	close(c.stopCh)
	<-c.cleanupDone
}

// GetPartition retrieves a cached partition for a key
func (c *Cache) GetPartition(key string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.partitions[key]
	if !exists || entry.IsExpired() {
		return 0, false
	}

	return entry.Value.(int), true
}

// SetPartition caches a partition for a key
func (c *Cache) SetPartition(key string, partition int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.partitions[key] = &CacheEntry{
		Value:     partition,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// GetLeader retrieves a cached leader for a partition
func (c *Cache) GetLeader(partition int) (*clusterkit.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.leaders[partition]
	if !exists || entry.IsExpired() {
		return nil, false
	}

	return entry.Value.(*clusterkit.Node), true
}

// SetLeader caches a leader for a partition
func (c *Cache) SetLeader(partition int, leader *clusterkit.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create a copy to avoid race conditions
	leaderCopy := *leader
	c.leaders[partition] = &CacheEntry{
		Value:     &leaderCopy,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// GetReplicas retrieves cached replicas for a partition
func (c *Cache) GetReplicas(partition int) ([]*clusterkit.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.replicas[partition]
	if !exists || entry.IsExpired() {
		return nil, false
	}

	return entry.Value.([]*clusterkit.Node), true
}

// SetReplicas caches replicas for a partition
func (c *Cache) SetReplicas(partition int, replicas []*clusterkit.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create copies to avoid race conditions
	replicasCopy := make([]*clusterkit.Node, len(replicas))
	for i, replica := range replicas {
		nodeCopy := *replica
		replicasCopy[i] = &nodeCopy
	}

	c.replicas[partition] = &CacheEntry{
		Value:     replicasCopy,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// GetNodes retrieves cached nodes
func (c *Cache) GetNodes() ([]*clusterkit.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.nodes == nil || c.nodes.IsExpired() {
		return nil, false
	}

	return c.nodes.Value.([]*clusterkit.Node), true
}

// SetNodes caches all nodes
func (c *Cache) SetNodes(nodes []*clusterkit.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create copies to avoid race conditions
	nodesCopy := make([]*clusterkit.Node, len(nodes))
	for i, node := range nodes {
		nodeCopy := *node
		nodesCopy[i] = &nodeCopy
	}

	c.nodes = &CacheEntry{
		Value:     nodesCopy,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// InvalidatePartition invalidates cache entries for a specific partition
func (c *Cache) InvalidatePartition(partition int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.leaders, partition)
	delete(c.replicas, partition)

	// Also invalidate partition mappings that might be affected
	// This is a simple approach - in production you might want more granular invalidation
	for key := range c.partitions {
		delete(c.partitions, key)
	}
}

// InvalidateNodes invalidates all node-related cache entries
func (c *Cache) InvalidateNodes() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nodes = nil
	
	// Clear all caches since node changes can affect everything
	c.partitions = make(map[string]*CacheEntry)
	c.leaders = make(map[int]*CacheEntry)
	c.replicas = make(map[int]*CacheEntry)
}

// Clear clears all cache entries
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.partitions = make(map[string]*CacheEntry)
	c.leaders = make(map[int]*CacheEntry)
	c.replicas = make(map[int]*CacheEntry)
	c.nodes = nil
}

// cleanupLoop periodically removes expired entries
func (c *Cache) cleanupLoop() {
	defer close(c.cleanupDone)

	ticker := time.NewTicker(c.ttl / 2) // Cleanup twice per TTL period
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// cleanup removes expired entries
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Clean up partition cache
	for key, entry := range c.partitions {
		if now.After(entry.ExpiresAt) {
			delete(c.partitions, key)
		}
	}

	// Clean up leader cache
	for partition, entry := range c.leaders {
		if now.After(entry.ExpiresAt) {
			delete(c.leaders, partition)
		}
	}

	// Clean up replica cache
	for partition, entry := range c.replicas {
		if now.After(entry.ExpiresAt) {
			delete(c.replicas, partition)
		}
	}

	// Clean up nodes cache
	if c.nodes != nil && now.After(c.nodes.ExpiresAt) {
		c.nodes = nil
	}
}

// Stats returns cache statistics
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStats{
		PartitionEntries: len(c.partitions),
		LeaderEntries:    len(c.leaders),
		ReplicaEntries:   len(c.replicas),
		TTL:              c.ttl,
	}

	if c.nodes != nil {
		stats.NodesEntry = true
	}

	return stats
}

// CacheStats represents cache statistics
type CacheStats struct {
	PartitionEntries int           `json:"partition_entries"`
	LeaderEntries    int           `json:"leader_entries"`
	ReplicaEntries   int           `json:"replica_entries"`
	NodesEntry       bool          `json:"nodes_entry"`
	TTL              time.Duration `json:"ttl"`
}
