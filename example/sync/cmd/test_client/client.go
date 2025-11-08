package main

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client for SYNC (quorum-based) KV store
type Client struct {
	httpClient *http.Client
	topology   *Topology
	mu         sync.RWMutex
	nodes      []string
	hashConfig HashConfig
}

type Topology struct {
	Partitions map[string]*PartitionInfo
	Nodes      map[string]string
}

type HashConfig struct {
	Algorithm string `json:"algorithm"`
	Encoding  string `json:"encoding"`
	Modulo    int    `json:"modulo"`
	Format    string `json:"format"`
}

type PartitionInfo struct {
	ID           string
	PrimaryNode  string
	ReplicaNodes []string
}

func NewClient(nodes []string) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		nodes:      nodes,
		topology: &Topology{
			Partitions: make(map[string]*PartitionInfo),
			Nodes:      make(map[string]string),
		},
	}

	if err := c.refreshTopology(); err != nil {
		return nil, err
	}

	go c.refreshLoop()
	return c, nil
}

// Set with quorum (waits for 2/3 nodes)
func (c *Client) Set(key, value string) error {
	return c.SetWithQuorum(key, value, 2)
}

// SetWithQuorum writes to all nodes and waits for quorum
func (c *Client) SetWithQuorum(key, value string, quorum int) error {
	partitionID := c.getPartitionID(key)

	c.mu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.mu.RUnlock()

	if !exists {
		// Topology might be stale, refresh and retry
		if err := c.refreshTopology(); err == nil {
			c.mu.RLock()
			partition, exists = c.topology.Partitions[partitionID]
			c.mu.RUnlock()
		}
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

	// If quorum failed, refresh topology for next request
	go c.refreshTopology()

	return fmt.Errorf("quorum not reached: %d/%d", success, quorum)
}

// Get reads from any available node
func (c *Client) Get(key string) (string, error) {
	partitionID := c.getPartitionID(key)

	c.mu.RLock()
	partition, exists := c.topology.Partitions[partitionID]
	c.mu.RUnlock()

	if !exists {
		// Topology might be stale, refresh and retry
		if err := c.refreshTopology(); err == nil {
			c.mu.RLock()
			partition, exists = c.topology.Partitions[partitionID]
			c.mu.RUnlock()
		}
		if !exists {
			return "", fmt.Errorf("partition not found")
		}
	}

	// Try all nodes (primary + replicas)
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

	// All nodes failed, refresh topology for next request
	go c.refreshTopology()

	return "", fmt.Errorf("key not found")
}

func (c *Client) getPartitionID(key string) string {
	// Use hash config from ClusterKit
	if c.hashConfig.Algorithm != "md5" {
		// Fallback or error
		return ""
	}

	hash := md5.Sum([]byte(key))
	hashValue := binary.BigEndian.Uint32(hash[:4])
	partitionNum := int(hashValue) % c.hashConfig.Modulo
	return fmt.Sprintf(c.hashConfig.Format, partitionNum)
}

func (c *Client) getNodeAddr(nodeID string) string {
	c.mu.RLock()
	httpAddr := c.topology.Nodes[nodeID]
	c.mu.RUnlock()

	// Convert HTTP port to KV port
	// Example: ":8080" -> "localhost:9080"
	var httpPort int
	if _, err := fmt.Sscanf(httpAddr, ":%d", &httpPort); err == nil {
		kvPort := httpPort + 1000 // 8080 -> 9080, 8081 -> 9081, etc.
		return fmt.Sprintf("localhost:%d", kvPort)
	}

	return httpAddr
}

func (c *Client) refreshTopology() error {
	for _, node := range c.nodes {
		// Get cluster info from ClusterKit's /cluster endpoint
		resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/cluster", node))
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		var apiResponse struct {
			Cluster struct {
				Nodes []struct {
					ID string `json:"id"`
					IP string `json:"ip"`
				} `json:"nodes"`
				PartitionMap struct {
					Partitions map[string]struct {
						ID           string   `json:"id"`
						PrimaryNode  string   `json:"primary_node"`
						ReplicaNodes []string `json:"replica_nodes"`
					} `json:"partitions"`
				} `json:"partition_map"`
			} `json:"cluster"`
			HashConfig HashConfig `json:"hash_config"`
		}

		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &apiResponse); err != nil {
			continue
		}

		c.mu.Lock()
		// Update hash config from API
		c.hashConfig = apiResponse.HashConfig

		// Update nodes
		for _, n := range apiResponse.Cluster.Nodes {
			c.topology.Nodes[n.ID] = n.IP
		}
		// Update partitions
		for _, p := range apiResponse.Cluster.PartitionMap.Partitions {
			c.topology.Partitions[p.ID] = &PartitionInfo{
				ID:           p.ID,
				PrimaryNode:  p.PrimaryNode,
				ReplicaNodes: p.ReplicaNodes,
			}
		}
		c.mu.Unlock()

		fmt.Printf("[Client] Topology updated: %d nodes, %d partitions (hash: %s)\n",
			len(c.topology.Nodes), len(c.topology.Partitions), c.hashConfig.Algorithm)
		return nil
	}

	return fmt.Errorf("failed to refresh topology")
}

func (c *Client) refreshLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.refreshTopology()
	}
}
