package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"sync"
	"time"
)

// SmartKVClient is an intelligent client that knows the cluster topology
// and sends requests directly to the correct primary/replica nodes
type SmartKVClient struct {
	httpClient       *http.Client
	topology         *ClusterTopology
	topologyMu       sync.RWMutex
	partitionCount   int
	refreshInterval  time.Duration
	discoveryNodes   []string
	topologyVersion  string // ETag for efficient updates
	lastFetchTime    time.Time
	stopChan         chan struct{}
	hashFunction     string // Hash function from server (e.g., "fnv1a")
}

// ClusterTopology represents the cluster structure
type ClusterTopology struct {
	Partitions map[string]*PartitionInfo // partitionID -> info
	Nodes      map[string]string         // nodeID -> address
	HashConfig HashConfig                // Hash configuration from server
}

// HashConfig contains hash function configuration from server
type HashConfig struct {
	Algorithm string // e.g., "fnv1a", "murmur3", "xxhash"
	Seed      int    // Hash seed
	Modulo    int    // Partition count
	Format    string // Partition ID format (e.g., "partition-%d")
}

// PartitionInfo contains partition assignment
type PartitionInfo struct {
	ID           string
	PrimaryNode  string   // Node ID
	ReplicaNodes []string // Node IDs
}

// SmartClientOptions for configuring the smart client
type SmartClientOptions struct {
	// DiscoveryNodes are the initial nodes to discover topology from
	DiscoveryNodes []string

	// PartitionCount must match your cluster's partition count
	PartitionCount int

	// RefreshInterval for topology updates (default: 30s)
	RefreshInterval time.Duration

	// Timeout for HTTP requests (default: 5s)
	Timeout time.Duration
}

// NewSmartKVClient creates a smart client that routes directly to correct nodes
func NewSmartKVClient(opts SmartClientOptions) (*SmartKVClient, error) {
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = 30 * time.Second
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.PartitionCount == 0 {
		opts.PartitionCount = 16 // Default
	}

	client := &SmartKVClient{
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		partitionCount:  opts.PartitionCount,
		refreshInterval: opts.RefreshInterval,
		discoveryNodes:  opts.DiscoveryNodes,
		stopChan:        make(chan struct{}),
		topology: &ClusterTopology{
			Partitions: make(map[string]*PartitionInfo),
			Nodes:      make(map[string]string),
		},
	}

	// Fetch initial topology
	if err := client.fetchTopology(); err != nil {
		return nil, fmt.Errorf("failed to fetch topology: %v", err)
	}

	// Start background topology refresh with smart polling
	go client.smartRefreshLoop()

	return client, nil
}

// Close stops the background refresh loop
func (c *SmartKVClient) Close() {
	close(c.stopChan)
}

// Set stores a key-value pair directly on primary and replicas
func (c *SmartKVClient) Set(key, value string) error {
	// 1. Calculate partition locally (same logic as server)
	partitionID := c.getPartitionID(key)

	// 2. Get partition info
	c.topologyMu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.topologyMu.RUnlock()

	if !exists {
		return fmt.Errorf("partition %s not found in topology", partitionID)
	}

	// 3. Send to primary first
	primaryAddr := c.getNodeAddress(partition.PrimaryNode)
	if primaryAddr == "" {
		return fmt.Errorf("primary node %s not found", partition.PrimaryNode)
	}

	payload := map[string]string{
		"key":   key,
		"value": value,
	}
	jsonData, _ := json.Marshal(payload)

	// Send to primary
	url := fmt.Sprintf("http://%s/kv/set", primaryAddr)
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		// Refresh topology on error (node might be down)
		c.refreshOnError()
		return fmt.Errorf("failed to send to primary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Refresh topology on error
		c.refreshOnError()
		return fmt.Errorf("primary returned status %d", resp.StatusCode)
	}

	return nil
}

// SetWithReplication stores on primary and waits for replica acknowledgments
func (c *SmartKVClient) SetWithReplication(key, value string, quorum int) error {
	// 1. Calculate partition
	partitionID := c.getPartitionID(key)

	// 2. Get partition info
	c.topologyMu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.topologyMu.RUnlock()

	if !exists {
		return fmt.Errorf("partition %s not found", partitionID)
	}

	payload := map[string]string{
		"key":   key,
		"value": value,
	}
	jsonData, _ := json.Marshal(payload)

	// 3. Send to primary + replicas in parallel
	var wg sync.WaitGroup
	successChan := make(chan bool, 1+len(partition.ReplicaNodes))

	// Send to primary
	wg.Add(1)
	go func() {
		defer wg.Done()
		addr := c.getNodeAddress(partition.PrimaryNode)
		if addr != "" {
			url := fmt.Sprintf("http://%s/kv/replicate", addr)
			resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
			if err == nil && resp.StatusCode == http.StatusOK {
				successChan <- true
				resp.Body.Close()
			}
		}
	}()

	// Send to replicas
	for _, replicaID := range partition.ReplicaNodes {
		wg.Add(1)
		go func(nodeID string) {
			defer wg.Done()
			addr := c.getNodeAddress(nodeID)
			if addr != "" {
				url := fmt.Sprintf("http://%s/kv/replicate", addr)
				resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
				if err == nil && resp.StatusCode == http.StatusOK {
					successChan <- true
					resp.Body.Close()
				}
			}
		}(replicaID)
	}

	// Wait for responses
	go func() {
		wg.Wait()
		close(successChan)
	}()

	// Count successes
	successCount := 0
	for range successChan {
		successCount++
	}

	if successCount < quorum {
		return fmt.Errorf("quorum not reached: got %d, need %d", successCount, quorum)
	}

	return nil
}

// Get retrieves a value by key from the primary node
func (c *SmartKVClient) Get(key string) (string, error) {
	// 1. Calculate partition
	partitionID := c.getPartitionID(key)

	// 2. Get partition info
	c.topologyMu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.topologyMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("partition %s not found", partitionID)
	}

	// 3. Read from primary
	primaryAddr := c.getNodeAddress(partition.PrimaryNode)
	if primaryAddr == "" {
		return "", fmt.Errorf("primary node not found")
	}

	url := fmt.Sprintf("http://%s/kv/get?key=%s", primaryAddr, key)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("key not found")
	}

	var result struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Value, nil
}

// GetFromReplica reads from any replica (eventual consistency)
func (c *SmartKVClient) GetFromReplica(key string) (string, error) {
	// 1. Calculate partition
	partitionID := c.getPartitionID(key)

	// 2. Get partition info
	c.topologyMu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.topologyMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("partition %s not found", partitionID)
	}

	// 3. Try primary first, then replicas
	allNodes := append([]string{partition.PrimaryNode}, partition.ReplicaNodes...)

	for _, nodeID := range allNodes {
		addr := c.getNodeAddress(nodeID)
		if addr == "" {
			continue
		}

		url := fmt.Sprintf("http://%s/kv/get?key=%s", addr, key)
		resp, err := c.httpClient.Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var result struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}

			body, _ := io.ReadAll(resp.Body)
			if err := json.Unmarshal(body, &result); err == nil {
				return result.Value, nil
			}
		}
	}

	return "", fmt.Errorf("key not found on any node")
}

// Delete removes a key
func (c *SmartKVClient) Delete(key string) error {
	// 1. Calculate partition
	partitionID := c.getPartitionID(key)

	// 2. Get partition info
	c.topologyMu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.topologyMu.RUnlock()

	if !exists {
		return fmt.Errorf("partition %s not found", partitionID)
	}

	// 3. Send to primary
	primaryAddr := c.getNodeAddress(partition.PrimaryNode)
	if primaryAddr == "" {
		return fmt.Errorf("primary node not found")
	}

	url := fmt.Sprintf("http://%s/kv/delete?key=%s", primaryAddr, key)
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete failed with status %d", resp.StatusCode)
	}

	return nil
}

// getPartitionID calculates which partition a key belongs to
// Uses the hash configuration from the server
func (c *SmartKVClient) getPartitionID(key string) string {
	c.topologyMu.RLock()
	hashConfig := c.topology.HashConfig
	c.topologyMu.RUnlock()
	
	// Use the hash algorithm from server
	var hashValue uint32
	switch hashConfig.Algorithm {
	case "fnv1a", "": // Default
		h := fnv.New32a()
		h.Write([]byte(key))
		hashValue = h.Sum32()
	default:
		// Fallback to fnv1a if unknown
		h := fnv.New32a()
		h.Write([]byte(key))
		hashValue = h.Sum32()
	}
	
	// Use modulo from server
	modulo := hashConfig.Modulo
	if modulo == 0 {
		modulo = c.partitionCount
	}
	partitionNum := int(hashValue) % modulo
	
	// Use format from server
	format := hashConfig.Format
	if format == "" {
		format = "partition-%d"
	}
	
	return fmt.Sprintf(format, partitionNum)
}

// getNodeAddress gets the address for a node ID
func (c *SmartKVClient) getNodeAddress(nodeID string) string {
	c.topologyMu.RLock()
	defer c.topologyMu.RUnlock()
	return c.topology.Nodes[nodeID]
}

// hasTopologyChanged checks if topology version changed using ETag
func (c *SmartKVClient) hasTopologyChanged() bool {
	for _, node := range c.discoveryNodes {
		url := fmt.Sprintf("http://%s/cluster", node)
		req, err := http.NewRequest("HEAD", url, nil)
		if err != nil {
			continue
		}

		// Add If-None-Match header with current version
		if c.topologyVersion != "" {
			req.Header.Set("If-None-Match", c.topologyVersion)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		// 304 Not Modified = no change
		if resp.StatusCode == http.StatusNotModified {
			return false
		}

		// Check ETag
		newVersion := resp.Header.Get("ETag")
		if newVersion != "" && newVersion != c.topologyVersion {
			return true
		}
	}

	return false
}

// smartRefreshLoop polls topology with smart caching
func (c *SmartKVClient) smartRefreshLoop() {
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if topology changed (lightweight HEAD request)
			if c.hasTopologyChanged() {
				// Only fetch if changed!
				if err := c.fetchTopology(); err != nil {
					fmt.Printf("[SmartClient] Failed to refresh topology: %v\n", err)
				}
			}
		case <-c.stopChan:
			return
		}
	}
}

// fetchTopology retrieves cluster topology from discovery nodes
func (c *SmartKVClient) fetchTopology() error {
	for _, node := range c.discoveryNodes {
		// Fetch partitions
		url := fmt.Sprintf("http://%s/partitions", node)
		resp, err := c.httpClient.Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		var partitionsResp struct {
			Partitions []struct {
				ID           string   `json:"id"`
				PrimaryNode  string   `json:"primary_node"`
				ReplicaNodes []string `json:"replica_nodes"`
			} `json:"partitions"`
		}

		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &partitionsResp); err != nil {
			continue
		}

		// Fetch cluster info for node addresses and hash config
		clusterURL := fmt.Sprintf("http://%s/cluster", node)
		clusterResp, err := c.httpClient.Get(clusterURL)
		if err != nil {
			continue
		}
		defer clusterResp.Body.Close()

		var response struct {
			Cluster struct {
				Nodes []struct {
					ID   string `json:"id"`
					IP   string `json:"ip"`
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"cluster"`
			HashConfig struct {
				Algorithm string `json:"algorithm"`
				Seed      int    `json:"seed"`
				Modulo    int    `json:"modulo"`
				Format    string `json:"format"`
			} `json:"hash_config"`
		}

		clusterBody, _ := io.ReadAll(clusterResp.Body)
		if err := json.Unmarshal(clusterBody, &response); err != nil {
			continue
		}

		// Update topology
		c.topologyMu.Lock()

		// Update nodes
		for _, node := range response.Cluster.Nodes {
			// Convert cluster port to KV port (add 1000)
			kvAddr := node.IP
			if len(node.IP) > 1 && node.IP[0] == ':' {
				var clusterPort int
				fmt.Sscanf(node.IP, ":%d", &clusterPort)
				kvAddr = fmt.Sprintf("localhost:%d", clusterPort+1000)
			}
			c.topology.Nodes[node.ID] = kvAddr
		}

		// Update partitions
		for _, p := range partitionsResp.Partitions {
			c.topology.Partitions[p.ID] = &PartitionInfo{
				ID:           p.ID,
				ReplicaNodes: p.ReplicaNodes,
			}
		}

		// Store hash config from server
		c.topology.HashConfig = HashConfig{
			Algorithm: response.HashConfig.Algorithm,
			Seed:      response.HashConfig.Seed,
			Modulo:    response.HashConfig.Modulo,
			Format:    response.HashConfig.Format,
		}
		if c.topology.HashConfig.Algorithm == "" {
			c.topology.HashConfig.Algorithm = "fnv1a" // Default
		}
		c.hashFunction = c.topology.HashConfig.Algorithm
		
		// Store version and timestamp
		c.topologyVersion = clusterResp.Header.Get("ETag")
		c.lastFetchTime = time.Now()
		
		c.topologyMu.Unlock()
		
		return nil // Success!
	}

	return fmt.Errorf("failed to fetch topology from all discovery nodes")
}
// refreshOnError attempts to refresh topology when a request fails
func (c *SmartKVClient) refreshOnError() {
	// Rate limit: only refresh if last fetch was > 5s ago
	c.topologyMu.RLock()
	timeSinceLastFetch := time.Since(c.lastFetchTime)
	c.topologyMu.RUnlock()

	if timeSinceLastFetch > 5*time.Second {
		go c.fetchTopology()
	}
}

// GetTopology returns the current topology (for debugging)
func (c *SmartKVClient) GetTopology() *ClusterTopology {
	c.topologyMu.RLock()
	defer c.topologyMu.RUnlock()

	// Return a copy
	topology := &ClusterTopology{
		Partitions: make(map[string]*PartitionInfo),
		Nodes:      make(map[string]string),
	}

	for k, v := range c.topology.Partitions {
		topology.Partitions[k] = v
	}
	for k, v := range c.topology.Nodes {
		topology.Nodes[k] = v
	}

	return topology
}
