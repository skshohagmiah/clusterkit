package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// CacheEntry represents a cache entry
type CacheEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// IsExpired checks if the cache entry has expired
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// DistributedCache represents a distributed cache
type DistributedCache struct {
	ck    *clusterkit.ClusterKit
	cache map[int]map[string]*CacheEntry // partition -> key -> entry
	mu    sync.RWMutex
}

// NewDistributedCache creates a new distributed cache
func NewDistributedCache() *DistributedCache {
	return &DistributedCache{
		cache: make(map[int]map[string]*CacheEntry),
	}
}

func main() {
	// Get configuration from environment
	nodeID := getEnv("NODE_ID", "cache-node-1")
	advertiseAddr := getEnv("ADVERTISE_ADDR", "localhost:9000")
	httpPort := getEnvInt("HTTP_PORT", 8080)
	grpcPort := getEnvInt("GRPC_PORT", 9000)
	etcdEndpoints := []string{getEnv("ETCD_ENDPOINTS", "http://localhost:2379")}

	cache := NewDistributedCache()

	// Configure ClusterKit
	config := &clusterkit.Config{
		NodeID:        nodeID,
		AdvertiseAddr: advertiseAddr,
		HTTPPort:      httpPort,
		GRPCPort:      grpcPort,
		Partitions:    16, // Small number for demo
		ReplicaFactor: 2,
		EtcdEndpoints: etcdEndpoints,

		// Hook functions
		OnPartitionAssigned:   cache.onPartitionAssigned,
		OnPartitionUnassigned: cache.onPartitionUnassigned,
		OnLeaderElected:       cache.onLeaderElected,
		OnLeaderLost:          cache.onLeaderLost,
	}

	// Join the cluster
	ck, err := clusterkit.Join(config)
	if err != nil {
		log.Fatalf("Failed to join cluster: %v", err)
	}
	cache.ck = ck

	// Setup HTTP handlers
	http.HandleFunc("/set", cache.handleSet)
	http.HandleFunc("/get", cache.handleGet)
	http.HandleFunc("/delete", cache.handleDelete)
	http.HandleFunc("/keys", cache.handleKeys)
	http.HandleFunc("/stats", cache.handleStats)
	http.HandleFunc("/health", cache.handleHealth)
	http.HandleFunc("/flush", cache.handleFlush)

	// Start cleanup goroutine
	go cache.cleanupExpired()

	// Start HTTP server
	go func() {
		log.Printf("Starting Cache HTTP server on port %d", httpPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	log.Printf("Distributed Cache node %s started successfully", nodeID)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Leave the cluster gracefully
	if err := ck.Leave(); err != nil {
		log.Printf("Error leaving cluster: %v", err)
	}

	log.Println("Shutdown complete")
}

// HTTP Handlers

func (c *DistributedCache) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	value := r.URL.Query().Get("value")
	ttlStr := r.URL.Query().Get("ttl")

	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	var ttl time.Duration = 3600 * time.Second // Default 1 hour
	if ttlStr != "" {
		if ttlSeconds, err := strconv.Atoi(ttlStr); err == nil {
			ttl = time.Duration(ttlSeconds) * time.Second
		}
	}

	err := c.Set(key, value, ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":    key,
		"value":  value,
		"ttl":    int(ttl.Seconds()),
		"status": "set",
	})
}

func (c *DistributedCache) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	entry, err := c.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":        key,
		"value":      entry.Value,
		"expires_at": entry.ExpiresAt.Unix(),
		"created_at": entry.CreatedAt.Unix(),
		"status":     "found",
	})
}

func (c *DistributedCache) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	err := c.Delete(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":    key,
		"status": "deleted",
	})
}

func (c *DistributedCache) handleKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	keys := c.GetKeys()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

func (c *DistributedCache) handleStats(w http.ResponseWriter, r *http.Request) {
	clusterStats := c.ck.GetStats()
	cacheStats := c.GetCacheStats()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cluster": clusterStats,
		"cache":   cacheStats,
	})
}

func (c *DistributedCache) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !c.ck.IsHealthy() {
		http.Error(w, "Unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"node_id": c.ck.NodeID(),
	})
}

func (c *DistributedCache) handleFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	count := c.Flush()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "flushed",
		"keys_removed": count,
	})
}

// Cache Operations

func (c *DistributedCache) Set(key, value string, ttl time.Duration) error {
	partition := c.ck.GetPartition(key)

	if !c.ck.IsLeader(partition) {
		// Forward to leader
		leader := c.ck.GetLeader(partition)
		if leader == nil {
			return fmt.Errorf("no leader for partition %d", partition)
		}
		return c.forwardSet(leader, key, value, ttl)
	}

	// Create cache entry
	entry := &CacheEntry{
		Key:       key,
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}

	// Store locally
	c.mu.Lock()
	if c.cache[partition] == nil {
		c.cache[partition] = make(map[string]*CacheEntry)
	}
	c.cache[partition][key] = entry
	c.mu.Unlock()

	// Replicate to followers
	c.replicate(partition, entry)

	log.Printf("Set key=%s value=%s ttl=%v partition=%d", key, value, ttl, partition)
	return nil
}

func (c *DistributedCache) Get(key string) (*CacheEntry, error) {
	partition := c.ck.GetPartition(key)

	c.mu.RLock()
	partitionCache, exists := c.cache[partition]
	if !exists {
		c.mu.RUnlock()
		return nil, fmt.Errorf("key not found: %s", key)
	}

	entry, exists := partitionCache[key]
	if !exists {
		c.mu.RUnlock()
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// Check if expired
	if entry.IsExpired() {
		c.mu.RUnlock()
		c.mu.Lock()
		delete(partitionCache, key)
		c.mu.Unlock()
		return nil, fmt.Errorf("key expired: %s", key)
	}

	// Return a copy to avoid race conditions
	entryCopy := *entry
	c.mu.RUnlock()

	return &entryCopy, nil
}

func (c *DistributedCache) Delete(key string) error {
	partition := c.ck.GetPartition(key)

	if !c.ck.IsLeader(partition) {
		// Forward to leader
		leader := c.ck.GetLeader(partition)
		if leader == nil {
			return fmt.Errorf("no leader for partition %d", partition)
		}
		return c.forwardDelete(leader, key)
	}

	// Delete locally
	c.mu.Lock()
	if partitionCache, exists := c.cache[partition]; exists {
		delete(partitionCache, key)
	}
	c.mu.Unlock()

	log.Printf("Deleted key=%s partition=%d", key, partition)
	return nil
}

func (c *DistributedCache) GetKeys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var keys []string
	for _, partitionCache := range c.cache {
		for key, entry := range partitionCache {
			if !entry.IsExpired() {
				keys = append(keys, key)
			}
		}
	}

	return keys
}

func (c *DistributedCache) Flush() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for partition := range c.cache {
		count += len(c.cache[partition])
		c.cache[partition] = make(map[string]*CacheEntry)
	}

	log.Printf("Flushed %d keys from cache", count)
	return count
}

func (c *DistributedCache) GetCacheStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalKeys := 0
	expiredKeys := 0
	partitionCounts := make(map[int]int)

	for partition, partitionCache := range c.cache {
		for _, entry := range partitionCache {
			totalKeys++
			if entry.IsExpired() {
				expiredKeys++
			}
		}
		partitionCounts[partition] = len(partitionCache)
	}

	return map[string]interface{}{
		"total_keys":       totalKeys,
		"expired_keys":     expiredKeys,
		"active_keys":      totalKeys - expiredKeys,
		"partition_counts": partitionCounts,
	}
}

// Helper functions

func (c *DistributedCache) forwardSet(leader *clusterkit.Node, key, value string, ttl time.Duration) error {
	url := fmt.Sprintf("%s/set?key=%s&value=%s&ttl=%d", leader.HTTPAddr(), key, value, int(ttl.Seconds()))
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to forward set to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *DistributedCache) forwardDelete(leader *clusterkit.Node, key string) error {
	url := fmt.Sprintf("%s/delete?key=%s", leader.HTTPAddr(), key)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to forward delete to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *DistributedCache) replicate(partition int, entry *CacheEntry) {
	replicas := c.ck.GetReplicas(partition)
	for _, replica := range replicas {
		if replica.ID == c.ck.NodeID() {
			continue // Skip self
		}

		go func(node *clusterkit.Node) {
			// In a real implementation, you'd use a proper replication protocol
			log.Printf("Would replicate key %s to %s", entry.Key, node.ID)
		}(replica)
	}
}

func (c *DistributedCache) cleanupExpired() {
	ticker := time.NewTicker(60 * time.Second) // Cleanup every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.performCleanup()
		}
	}
}

func (c *DistributedCache) performCleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cleaned := 0
	for partition, partitionCache := range c.cache {
		for key, entry := range partitionCache {
			if entry.IsExpired() {
				delete(partitionCache, key)
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		log.Printf("Cleaned up %d expired cache entries", cleaned)
	}
}

// Hook implementations

func (c *DistributedCache) onPartitionAssigned(partition int, previousOwner *clusterkit.Node) {
	log.Printf("Partition %d assigned to this node", partition)

	c.mu.Lock()
	c.cache[partition] = make(map[string]*CacheEntry)
	c.mu.Unlock()

	if previousOwner != nil {
		// In a real implementation, you'd migrate cache data
		log.Printf("Would migrate cache data for partition %d from %s", partition, previousOwner.ID)
	}
}

func (c *DistributedCache) onPartitionUnassigned(partition int, newOwner *clusterkit.Node) {
	log.Printf("Partition %d moving to node %s", partition, newOwner.ID)

	c.mu.Lock()
	delete(c.cache, partition)
	c.mu.Unlock()
}

func (c *DistributedCache) onLeaderElected(partition int) {
	log.Printf("Became leader for partition %d", partition)
}

func (c *DistributedCache) onLeaderLost(partition int) {
	log.Printf("Lost leadership for partition %d", partition)
}

// Utility functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
