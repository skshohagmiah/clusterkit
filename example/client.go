package main

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

// Client is a smart KV client that routes directly to correct nodes
type Client struct {
	httpClient *http.Client
	topology   *Topology
	mu         sync.RWMutex
	nodes      []string
	partitions int
}

type Topology struct {
	Partitions map[string]*PartitionInfo
	Nodes      map[string]string
}

type PartitionInfo struct {
	ID           string
	PrimaryNode  string
	ReplicaNodes []string
}

// NewClient creates a new KV client
func NewClient(nodes []string, partitions int) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		nodes:      nodes,
		partitions: partitions,
		topology: &Topology{
			Partitions: make(map[string]*PartitionInfo),
			Nodes:      make(map[string]string),
		},
	}

	if err := c.refreshTopology(); err != nil {
		return nil, err
	}

	// Refresh topology every 30 seconds
	go c.refreshLoop()

	return c, nil
}

// Set stores a key-value pair with quorum
func (c *Client) Set(key, value string) error {
	return c.SetWithQuorum(key, value, 2)
}

// SetWithQuorum writes to multiple nodes and waits for quorum
func (c *Client) SetWithQuorum(key, value string, quorum int) error {
	partitionID := c.getPartitionID(key)

	c.mu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.mu.RUnlock()

	if !exists {
		c.refreshTopology()
		c.mu.RLock()
		partition, exists = c.topology.Partitions[partitionID]
		c.mu.RUnlock()

		if !exists {
			return fmt.Errorf("partition not found")
		}
	}

	// Write to all nodes (primary + replicas)
	allNodes := []string{partition.PrimaryNode}
	allNodes = append(allNodes, partition.ReplicaNodes...)

	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	successChan := make(chan bool, len(allNodes))
	for _, nodeID := range allNodes {
		go func(nid string) {
			addr := c.getNodeAddr(nid)
			if addr == "" {
				successChan <- false
				return
			}

			url := fmt.Sprintf("http://%s/kv/set", addr)
			resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
			if err != nil || resp.StatusCode != http.StatusOK {
				successChan <- false
				if resp != nil {
					resp.Body.Close()
				}
				return
			}
			resp.Body.Close()
			successChan <- true
		}(nodeID)
	}

	// Wait for quorum
	success := 0
	for i := 0; i < len(allNodes); i++ {
		if <-successChan {
			success++
			if success >= quorum {
				return nil
			}
		}
	}

	return fmt.Errorf("quorum not reached: %d/%d", success, quorum)
}

// Get retrieves a value
func (c *Client) Get(key string) (string, error) {
	partitionID := c.getPartitionID(key)

	c.mu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("partition not found")
	}

	// Try all nodes
	allNodes := []string{partition.PrimaryNode}
	allNodes = append(allNodes, partition.ReplicaNodes...)

	for _, nodeID := range allNodes {
		addr := c.getNodeAddr(nodeID)
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

	return "", fmt.Errorf("key not found")
}

// Delete removes a key
func (c *Client) Delete(key string) error {
	partitionID := c.getPartitionID(key)

	c.mu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("partition not found")
	}

	addr := c.getNodeAddr(partition.PrimaryNode)
	if addr == "" {
		return fmt.Errorf("primary not found")
	}

	url := fmt.Sprintf("http://%s/kv/delete?key=%s", addr, key)
	req, _ := http.NewRequest("POST", url, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *Client) getPartitionID(key string) string {
	h := fnv.New32a()
	h.Write([]byte(key))
	partitionNum := int(h.Sum32()) % c.partitions
	return fmt.Sprintf("partition-%d", partitionNum)
}

func (c *Client) getNodeAddr(nodeID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.topology.Nodes[nodeID]
}

func (c *Client) refreshTopology() error {
	for _, node := range c.nodes {
		// Fetch partitions
		resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/partitions", node))
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

		// Fetch cluster info
		clusterResp, err := c.httpClient.Get(fmt.Sprintf("http://%s/cluster", node))
		if err != nil {
			continue
		}
		defer clusterResp.Body.Close()

		var clusterData struct {
			Cluster struct {
				Nodes []struct {
					ID string `json:"id"`
					IP string `json:"ip"`
				} `json:"nodes"`
			} `json:"cluster"`
		}

		clusterBody, _ := io.ReadAll(clusterResp.Body)
		if err := json.Unmarshal(clusterBody, &clusterData); err != nil {
			continue
		}

		// Update topology
		c.mu.Lock()

		// Update nodes (convert cluster port to KV port)
		for _, node := range clusterData.Cluster.Nodes {
			var port int
			fmt.Sscanf(node.IP, ":%d", &port)
			kvAddr := fmt.Sprintf("localhost:%d", port+1000)
			c.topology.Nodes[node.ID] = kvAddr
		}

		// Update partitions
		for _, p := range partitionsResp.Partitions {
			c.topology.Partitions[p.ID] = &PartitionInfo{
				ID:           p.ID,
				PrimaryNode:  p.PrimaryNode,
				ReplicaNodes: p.ReplicaNodes,
			}
		}

		c.mu.Unlock()

		fmt.Printf("[Client] Topology updated: %d nodes, %d partitions\n",
			len(c.topology.Nodes), len(c.topology.Partitions))

		return nil
	}

	return fmt.Errorf("failed to fetch topology")
}

func (c *Client) refreshLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.refreshTopology()
	}
}

// main runs the test workload
func main() {
	fmt.Println("🚀 Starting KV Store Test Client...")
	fmt.Println()

	// Create client
	client, err := NewClient([]string{"localhost:8080"}, 64)
	if err != nil {
		fmt.Printf("❌ Failed to create client: %v\n", err)
		return
	}

	fmt.Println("✓ Client connected to cluster")
	fmt.Printf("✓ Topology: %d nodes, %d partitions\n", len(client.topology.Nodes), len(client.topology.Partitions))
	fmt.Println()

	// Test 1: Write 100 keys
	fmt.Println("📝 Writing 100 keys...")
	writeStart := time.Now()
	writeSuccess := 0
	writeFailed := 0
	partitionWrites := make(map[string]int)
	nodeWrites := make(map[string]int)

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := fmt.Sprintf("value-%d-%d", i, time.Now().Unix())

		// Show which partition and node this key goes to
		partitionID := client.getPartitionID(key)
		client.mu.RLock()
		partition, exists := client.topology.Partitions[partitionID]
		client.mu.RUnlock()

		if exists {
			partitionWrites[partitionID]++
			nodeWrites[partition.PrimaryNode]++
			if i < 10 {
				fmt.Printf("  %s -> %s (primary: %s)\n", key, partitionID, partition.PrimaryNode)
			}
		}

		if err := client.Set(key, value); err != nil {
			writeFailed++
			if i < 5 {
				fmt.Printf("  ⚠️  Failed to write %s: %v\n", key, err)
			}
		} else {
			writeSuccess++
		}

		if (i+1)%20 == 0 {
			fmt.Printf("  ✓ Wrote %d keys...\n", i+1)
		}
	}

	writeDuration := time.Since(writeStart)
	fmt.Printf("✓ Write complete: %d success, %d failed (%.2fs, %.0f ops/sec)\n",
		writeSuccess, writeFailed, writeDuration.Seconds(), float64(writeSuccess)/writeDuration.Seconds())
	fmt.Println("\nWrite distribution by node:")
	for node, count := range nodeWrites {
		fmt.Printf("  %s: %d keys\n", node, count)
	}
	fmt.Println()

	// Test 2: Read all keys
	fmt.Println("📖 Reading 100 keys...")
	readStart := time.Now()
	readSuccess := 0
	readFailed := 0
	nodeReads := make(map[string]int)

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)

		// Track which node we're reading from
		partitionID := client.getPartitionID(key)
		client.mu.RLock()
		partition, exists := client.topology.Partitions[partitionID]
		client.mu.RUnlock()

		if exists {
			nodeReads[partition.PrimaryNode]++
		}

		if _, err := client.Get(key); err != nil {
			readFailed++
		} else {
			readSuccess++
		}

		if (i+1)%20 == 0 {
			fmt.Printf("  ✓ Read %d keys...\n", i+1)
		}
	}

	readDuration := time.Since(readStart)
	fmt.Printf("✓ Read complete: %d success, %d failed (%.2fs, %.0f ops/sec)\n",
		readSuccess, readFailed, readDuration.Seconds(), float64(readSuccess)/readDuration.Seconds())
	fmt.Println("\nRead distribution by node:")
	for node, count := range nodeReads {
		fmt.Printf("  %s: %d keys\n", node, count)
	}
	fmt.Println()

	// Test 3: Continuous workload during failures
	fmt.Println("🔄 Starting continuous workload (60 seconds)...")
	fmt.Println("   (Node failures and additions will happen during this)")
	fmt.Println()

	stopChan := make(chan bool)
	stats := &WorkloadStats{
		mu: sync.Mutex{},
	}

	// Start continuous read/write workload
	for i := 0; i < 5; i++ {
		go func(workerID int) {
			for {
				select {
				case <-stopChan:
					return
				default:
					// Write
					key := fmt.Sprintf("worker-%d-key-%d", workerID, time.Now().UnixNano()%1000)
					value := fmt.Sprintf("value-%d", time.Now().Unix())

					if err := client.Set(key, value); err != nil {
						stats.recordWriteError()
					} else {
						stats.recordWrite()
					}

					time.Sleep(100 * time.Millisecond)

					// Read
					if _, err := client.Get(key); err != nil {
						stats.recordReadError()
					} else {
						stats.recordRead()
					}

					time.Sleep(100 * time.Millisecond)
				}
			}
		}(i)
	}

	// Print stats every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	timeout := time.After(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			stats.print()
		case <-timeout:
			close(stopChan)
			time.Sleep(500 * time.Millisecond) // Let workers finish
			fmt.Println()
			fmt.Println("========================================")
			fmt.Println("  Final Results")
			fmt.Println("========================================")
			stats.print()
			fmt.Println()
			fmt.Println("✅ Test complete!")
			return
		}
	}
}

// WorkloadStats tracks workload statistics
type WorkloadStats struct {
	writes      int
	reads       int
	writeErrors int
	readErrors  int
	mu          sync.Mutex
}

func (s *WorkloadStats) recordWrite() {
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
}

func (s *WorkloadStats) recordRead() {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
}

func (s *WorkloadStats) recordWriteError() {
	s.mu.Lock()
	s.writeErrors++
	s.mu.Unlock()
}

func (s *WorkloadStats) recordReadError() {
	s.mu.Lock()
	s.readErrors++
	s.mu.Unlock()
}

func (s *WorkloadStats) print() {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := s.writes + s.reads
	errors := s.writeErrors + s.readErrors
	successRate := 100.0
	if total+errors > 0 {
		successRate = float64(total) / float64(total+errors) * 100
	}

	fmt.Printf("📊 Stats: %d writes, %d reads | Errors: %d writes, %d reads | Success: %.1f%%\n",
		s.writes, s.reads, s.writeErrors, s.readErrors, successRate)
}
