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

// Client for ASYNC (primary-first) KV store
type Client struct {
	httpClient *http.Client
	topology   *Topology
	mu         sync.RWMutex
	nodes      []string
	hashConfig HashConfig
}

type Topology struct {
	Partitions map[string]*PartitionInfo
	Nodes      map[string]*NodeInfo
}

type NodeInfo struct {
	ID       string            `json:"id"`
	IP       string            `json:"ip"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Services map[string]string `json:"services,omitempty"`
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
			Nodes:      make(map[string]*NodeInfo),
		},
	}

	if err := c.refreshTopology(); err != nil {
		return nil, err
	}

	go c.refreshLoop()
	return c, nil
}

// Set writes to primary first, then replicates in background (ASYNC strategy)
// If primary fails, immediately falls back to replicas for zero-downtime
func (c *Client) Set(key, value string) error {
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

	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	// 1. Try PRIMARY first (blocking - fast response)
	primaryAddr := c.getNodeAddr(partition.PrimaryNode)
	if primaryAddr != "" {
		url := fmt.Sprintf("http://%s/kv/set", primaryAddr)
		resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			
			// Success! Replicate to replicas in BACKGROUND
			go func() {
				for _, replicaID := range partition.ReplicaNodes {
					replicaAddr := c.getNodeAddr(replicaID)
					if replicaAddr == "" {
						continue
					}

					url := fmt.Sprintf("http://%s/kv/set", replicaAddr)
					resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
					if err == nil && resp != nil {
						resp.Body.Close()
					}
				}
			}()
			
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		
		fmt.Printf("[Client] ⚠️  Primary %s failed, trying replicas...\n", partition.PrimaryNode)
	}

	// 2. PRIMARY FAILED! Try replicas immediately (FAILOVER)
	for _, replicaID := range partition.ReplicaNodes {
		replicaAddr := c.getNodeAddr(replicaID)
		if replicaAddr == "" {
			continue
		}

		url := fmt.Sprintf("http://%s/kv/set", replicaAddr)
		resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			
			fmt.Printf("[Client] ✅ Failover successful: wrote to replica %s\n", replicaID)
			
			// Trigger topology refresh in background
			go c.refreshTopology()
			
			// Replicate to other replicas in background
			go func() {
				for _, otherReplicaID := range partition.ReplicaNodes {
					if otherReplicaID == replicaID {
						continue
					}
					otherAddr := c.getNodeAddr(otherReplicaID)
					if otherAddr == "" {
						continue
					}

					url := fmt.Sprintf("http://%s/kv/set", otherAddr)
					resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
					if err == nil && resp != nil {
						resp.Body.Close()
					}
				}
			}()
			
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// All nodes failed
	go c.refreshTopology()
	return fmt.Errorf("all nodes unavailable for partition %s", partitionID)
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
	nodeInfo := c.topology.Nodes[nodeID]
	c.mu.RUnlock()
	
	if nodeInfo == nil {
		return ""
	}
	
	// Use service discovery to get KV service address
	if kvAddr, exists := nodeInfo.Services["kv"]; exists {
		// Convert relative address to full address
		// Example: ":9080" -> "localhost:9080"
		if kvAddr[0] == ':' {
			return "localhost" + kvAddr
		}
		return kvAddr
	}
	
	// Fallback to old method if no service registered
	var httpPort int
	if _, err := fmt.Sscanf(nodeInfo.IP, ":%d", &httpPort); err == nil {
		kvPort := httpPort + 1000 // 8080 -> 9080, 8081 -> 9081, etc.
		return fmt.Sprintf("localhost:%d", kvPort)
	}
	
	return nodeInfo.IP
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
				Nodes []NodeInfo `json:"nodes"`
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
			c.topology.Nodes[n.ID] = &NodeInfo{
				ID:       n.ID,
				IP:       n.IP,
				Name:     n.Name,
				Status:   n.Status,
				Services: n.Services,
			}
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
