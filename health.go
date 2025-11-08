package clusterkit

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HealthChecker monitors node health and triggers removal on failures
type HealthChecker struct {
	ck                *ClusterKit
	interval          time.Duration
	timeout           time.Duration
	failureThreshold  int
	nodeFailures      map[string]int
	nodeLastSeen      map[string]time.Time
	mu                sync.RWMutex
	stopChan          chan struct{}
	enabled           bool
}

// HealthCheckConfig configures the health checker
type HealthCheckConfig struct {
	Enabled          bool          // Enable automatic health checking
	Interval         time.Duration // How often to check (default: 5s)
	Timeout          time.Duration // Request timeout (default: 2s)
	FailureThreshold int           // Failures before removal (default: 3)
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(ck *ClusterKit, config HealthCheckConfig) *HealthChecker {
	// Set defaults
	if config.Interval == 0 {
		config.Interval = 5 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Second
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 3
	}

	return &HealthChecker{
		ck:               ck,
		interval:         config.Interval,
		timeout:          config.Timeout,
		failureThreshold: config.FailureThreshold,
		nodeFailures:     make(map[string]int),
		nodeLastSeen:     make(map[string]time.Time),
		stopChan:         make(chan struct{}),
		enabled:          config.Enabled,
	}
}

// Start begins health checking
func (hc *HealthChecker) Start() {
	if !hc.enabled {
		fmt.Println("[HEALTH] Health checking disabled")
		return
	}

	fmt.Printf("[HEALTH] Starting health checker (interval: %v, threshold: %d)\n", 
		hc.interval, hc.failureThreshold)

	go hc.monitorLoop()
}

// Stop stops health checking
func (hc *HealthChecker) Stop() {
	if !hc.enabled {
		return
	}
	close(hc.stopChan)
}

// monitorLoop continuously monitors node health
func (hc *HealthChecker) monitorLoop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	fmt.Println("[HEALTH] Monitor loop started")

	for {
		select {
		case <-ticker.C:
			fmt.Println("[HEALTH] Running health check cycle...")
			hc.checkAllNodes()
		case <-hc.stopChan:
			fmt.Println("[HEALTH] Monitor loop stopped")
			return
		}
	}
}

// checkAllNodes checks health of all nodes except self
func (hc *HealthChecker) checkAllNodes() {
	hc.ck.mu.RLock()
	nodes := make([]Node, len(hc.ck.cluster.Nodes))
	copy(nodes, hc.ck.cluster.Nodes)
	selfID := hc.ck.nodeID
	hc.ck.mu.RUnlock()

	for _, node := range nodes {
		// Don't check self
		if node.ID == selfID {
			continue
		}

		// Check node health
		healthy := hc.checkNode(node)
		
		if healthy {
			hc.markHealthy(node.ID)
		} else {
			hc.markUnhealthy(node.ID)
		}
	}
}

// checkNode checks if a single node is healthy
func (hc *HealthChecker) checkNode(node Node) bool {
	// Build health check URL
	url := fmt.Sprintf("http://%s/ready", node.IP)
	
	// Create client with timeout
	client := &http.Client{
		Timeout: hc.timeout,
	}

	// Send health check request
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("[HEALTH] Node %s check failed: %v\n", node.ID, err)
		return false
	}
	defer resp.Body.Close()

	// Check status code
	healthy := resp.StatusCode == http.StatusOK
	if !healthy {
		fmt.Printf("[HEALTH] Node %s returned status %d\n", node.ID, resp.StatusCode)
	}
	return healthy
}

// markHealthy marks a node as healthy
func (hc *HealthChecker) markHealthy(nodeID string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Reset failure count
	if hc.nodeFailures[nodeID] > 0 {
		fmt.Printf("[HEALTH] Node %s recovered\n", nodeID)
		hc.nodeFailures[nodeID] = 0
	}
	
	hc.nodeLastSeen[nodeID] = time.Now()
}

// markUnhealthy marks a node as unhealthy
func (hc *HealthChecker) markUnhealthy(nodeID string) {
	hc.mu.Lock()
	hc.nodeFailures[nodeID]++
	failures := hc.nodeFailures[nodeID]
	hc.mu.Unlock()

	fmt.Printf("[HEALTH] Node %s health check failed (%d/%d)\n", 
		nodeID, failures, hc.failureThreshold)

	// Check if we should remove the node
	if failures >= hc.failureThreshold {
		fmt.Printf("[HEALTH] ❌ Node %s exceeded failure threshold, removing from cluster\n", nodeID)
		hc.removeNode(nodeID)
	}
}

// removeNode removes a failed node from the cluster
func (hc *HealthChecker) removeNode(nodeID string) {
	// Only leader can remove nodes
	if !hc.ck.consensusManager.IsLeader() {
		fmt.Printf("[HEALTH] Not leader, skipping node removal for %s\n", nodeID)
		return
	}

	fmt.Printf("[HEALTH] Removing failed node %s from cluster\n", nodeID)

	// Get node info and partitions before removal
	hc.ck.mu.RLock()
	var node *Node
	var partitionsOwned, partitionsReplica []string
	
	for i := range hc.ck.cluster.Nodes {
		if hc.ck.cluster.Nodes[i].ID == nodeID {
			nodeCopy := hc.ck.cluster.Nodes[i]
			node = &nodeCopy
			break
		}
	}
	
	if hc.ck.cluster.PartitionMap != nil {
		for _, partition := range hc.ck.cluster.PartitionMap.Partitions {
			if partition.PrimaryNode == nodeID {
				partitionsOwned = append(partitionsOwned, partition.ID)
			}
			for _, replicaID := range partition.ReplicaNodes {
				if replicaID == nodeID {
					partitionsReplica = append(partitionsReplica, partition.ID)
					break
				}
			}
		}
	}
	hc.ck.mu.RUnlock()

	// Remove from Raft cluster
	if err := hc.ck.consensusManager.RemoveServer(nodeID); err != nil {
		fmt.Printf("[HEALTH] Failed to remove from Raft: %v\n", err)
		return
	}

	// Propose removal through Raft consensus
	if err := hc.ck.consensusManager.ProposeAction("remove_node", map[string]interface{}{
		"id": nodeID,
	}); err != nil {
		fmt.Printf("[HEALTH] Failed to propose node removal: %v\n", err)
		return
	}

	// Trigger node leave hook
	if node != nil {
		hc.ck.hookManager.notifyNodeLeave(node, "health_check_failure", partitionsOwned, partitionsReplica)
	}

	// Clear failure tracking
	hc.mu.Lock()
	delete(hc.nodeFailures, nodeID)
	delete(hc.nodeLastSeen, nodeID)
	hc.mu.Unlock()

	// Trigger rebalancing after removal
	go func() {
		time.Sleep(2 * time.Second)
		
		hc.ck.mu.RLock()
		hasPartitions := hc.ck.cluster.PartitionMap != nil && 
			len(hc.ck.cluster.PartitionMap.Partitions) > 0
		hc.ck.mu.RUnlock()

		if hasPartitions {
			fmt.Printf("[HEALTH] Triggering rebalance after node removal\n")
			if err := hc.ck.RebalancePartitions(); err != nil {
				fmt.Printf("[HEALTH] Failed to rebalance: %v\n", err)
			}
		}
	}()

	fmt.Printf("[HEALTH] ✓ Node %s removed successfully\n", nodeID)
}

// GetNodeHealth returns health status of all nodes
func (hc *HealthChecker) GetNodeHealth() map[string]NodeHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]NodeHealth)
	for nodeID, failures := range hc.nodeFailures {
		result[nodeID] = NodeHealth{
			NodeID:       nodeID,
			Failures:     failures,
			LastSeen:     hc.nodeLastSeen[nodeID],
			Healthy:      failures == 0,
		}
	}
	return result
}

// NodeHealth represents health status of a node
type NodeHealth struct {
	NodeID   string    `json:"node_id"`
	Failures int       `json:"failures"`
	LastSeen time.Time `json:"last_seen"`
	Healthy  bool      `json:"healthy"`
}
