package clusterkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// startHTTPServer starts the HTTP server for inter-node communication
func (ck *ClusterKit) startHTTPServer() error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", ck.handleHealth)
	mux.HandleFunc("/ready", ck.handleReady)

	// Node registration endpoint
	mux.HandleFunc("/join", ck.handleJoin)

	// Partition endpoints
	mux.HandleFunc("/partitions", ck.handleGetPartitions)
	mux.HandleFunc("/partitions/stats", ck.handleGetPartitionStats)

	// Consensus endpoints
	mux.HandleFunc("/consensus/leader", ck.handleGetLeader)
	mux.HandleFunc("/consensus/stats", ck.handleGetConsensusStats)

	// Metrics and monitoring endpoints
	mux.HandleFunc("/metrics", ck.handleGetMetrics)
	mux.HandleFunc("/health/detailed", ck.handleHealthCheck)
	mux.HandleFunc("/cluster", ck.handleGetCluster)
	mux.HandleFunc("/nodes", ck.handleListNodes) // Paginated node list

	ck.httpServer = &http.Server{
		Addr:    ck.httpAddr,
		Handler: mux,
	}

	go func() {
		if err := ck.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	return nil
}

// handleHealth responds to health check requests
func (ck *ClusterKit) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Result{
		Success: true,
		Message: "healthy",
	})
}

// handleReady checks if the cluster is ready for operations
func (ck *ClusterKit) handleReady(w http.ResponseWriter, r *http.Request) {
	ck.mu.RLock()
	partitionCount := 0
	if ck.cluster.PartitionMap != nil {
		partitionCount = len(ck.cluster.PartitionMap.Partitions)
	}
	nodeCount := len(ck.cluster.Nodes)

	// Check if all partitions have valid primary nodes AND replicas in NodeMap
	validPartitions := 0
	invalidPartitions := []string{}
	if ck.cluster.PartitionMap != nil {
		for partID, partition := range ck.cluster.PartitionMap.Partitions {
			// Check primary exists
			if _, exists := ck.cluster.NodeMap[partition.PrimaryNode]; !exists {
				invalidPartitions = append(invalidPartitions, partID+":primary")
				continue
			}

			// Check all replicas exist
			allReplicasValid := true
			for _, replicaID := range partition.ReplicaNodes {
				if _, exists := ck.cluster.NodeMap[replicaID]; !exists {
					allReplicasValid = false
					invalidPartitions = append(invalidPartitions, partID+":replica:"+replicaID)
					break
				}
			}

			if allReplicasValid {
				validPartitions++
			}
		}
	}
	ck.mu.RUnlock()

	// Cluster is ready if:
	// 1. Has at least one node
	// 2. Has partitions created
	// 3. All partitions have valid primary nodes AND all replicas
	ready := nodeCount > 0 && partitionCount > 0 && validPartitions == partitionCount

	if ready {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ready":            true,
			"nodes":            nodeCount,
			"partitions":       partitionCount,
			"valid_partitions": validPartitions,
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ready":            false,
			"nodes":            nodeCount,
			"partitions":       partitionCount,
			"valid_partitions": validPartitions,
			"message":          "cluster not ready",
		})
	}
}

// handleJoin handles node join requests
func (ck *ClusterKit) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var joinReq struct {
		Node     Node   `json:"node"`
		RaftAddr string `json:"raft_addr"`
	}

	if err := json.NewDecoder(r.Body).Decode(&joinReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	node := joinReq.Node
	fmt.Printf("Received join request from %s (%s)\n", node.Name, node.ID)

	// Only leader can add nodes to Raft
	cm := ck.consensusManager
	if !cm.IsLeader() {
		// Get the current leader
		leader, err := cm.GetLeader()
		if err != nil {
			http.Error(w, "no leader available", http.StatusServiceUnavailable)
			return
		}

		// Return leader information so client can retry
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTemporaryRedirect)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":     "not the leader",
			"leader_id": leader.LeaderID,
			"leader_ip": leader.LeaderIP,
			"message":   fmt.Sprintf("Please retry join request to leader at %s", leader.LeaderIP),
		})
		return
	}

	// Check if this is a rejoin (node already exists)
	ck.mu.RLock()
	isRejoin := false
	for _, existing := range ck.cluster.Nodes {
		if existing.ID == node.ID {
			isRejoin = true
			fmt.Printf("[REJOIN] Node %s is rejoining (was: %s, now: %s)\n", 
				node.ID, existing.IP, node.IP)
			break
		}
	}
	ck.mu.RUnlock()

	if isRejoin {
		// For rejoin, just update the node info through Raft
		// Don't add to Raft cluster again (it's already there)
		fmt.Printf("[REJOIN] Updating node %s info\n", node.ID)
		
		// Trigger rejoin hook BEFORE updating state
		// This allows application to prepare for data sync
		ck.hookManager.notifyNodeRejoin(&node)
	} else {
		// New node - add to Raft cluster
		if err := cm.AddVoter(node.ID, joinReq.RaftAddr); err != nil {
			fmt.Printf("Failed to add voter to Raft: %v\n", err)
			http.Error(w, fmt.Sprintf("failed to add to raft: %v", err), http.StatusInternalServerError)
			return
		}
		
		// Trigger join hook for new nodes
		ck.hookManager.notifyNodeJoin(&node)
	}

	// Propose node addition through Raft consensus
	if err := cm.ProposeAction("add_node", map[string]interface{}{
		"id":     node.ID,
		"name":   node.Name,
		"ip":     node.IP,
		"status": node.Status,
	}); err != nil {
		http.Error(w, fmt.Sprintf("failed to propose: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Printf("✓ Node %s added to cluster\n", node.Name)

	// Trigger rebalancing after node addition (only on leader, after Raft consensus)
	// Wait longer to ensure:
	// 1. Raft has replicated the node addition to all followers
	// 2. Partitions have been created if this is an early node
	// 3. All nodes have updated their NodeMap
	go func() {
		// Wait for Raft replication and partition creation
		time.Sleep(2 * time.Second)

		// Only rebalance if partitions exist
		ck.mu.RLock()
		hasPartitions := ck.cluster.PartitionMap != nil && len(ck.cluster.PartitionMap.Partitions) > 0
		ck.mu.RUnlock()

		if !hasPartitions {
			fmt.Printf("[REBALANCE] Skipping rebalance - partitions not yet created\n")
			return
		}

		if err := ck.RebalancePartitions(); err != nil {
			fmt.Printf("[REBALANCE] Failed to rebalance after node join: %v\n", err)
		}
	}()

	// Return minimal response (don't send entire cluster state for scalability)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Result{
		Success: true,
		Message: "Node joined successfully",
		Data: map[string]interface{}{
			"node_id":    node.ID,
			"cluster_id": ck.cluster.ID,
			"node_count": len(ck.cluster.Nodes),
		},
	})
}

// handleGetCluster returns the current cluster state
func (ck *ClusterKit) handleGetCluster(w http.ResponseWriter, r *http.Request) {
	ck.mu.RLock()
	// Create a deep copy to avoid race conditions
	clusterCopy := Cluster{
		ID:     ck.cluster.ID,
		Name:   ck.cluster.Name,
		Nodes:  make([]Node, len(ck.cluster.Nodes)),
		Config: ck.cluster.Config,
	}
	copy(clusterCopy.Nodes, ck.cluster.Nodes)

	// Copy partition map if it exists
	if ck.cluster.PartitionMap != nil {
		clusterCopy.PartitionMap = &PartitionMap{
			Partitions: make(map[string]*Partition),
		}
		for k, v := range ck.cluster.PartitionMap.Partitions {
			partCopy := &Partition{
				ID:           v.ID,
				PrimaryNode:  v.PrimaryNode,
				ReplicaNodes: make([]string, len(v.ReplicaNodes)),
			}
			copy(partCopy.ReplicaNodes, v.ReplicaNodes)
			clusterCopy.PartitionMap.Partitions[k] = partCopy
		}
	}
	ck.mu.RUnlock()

	// Generate ETag based on cluster state (node count + partition count)
	etag := fmt.Sprintf("\"%d-%d\"", len(clusterCopy.Nodes), len(clusterCopy.PartitionMap.Partitions))

	// Check If-None-Match header
	if match := r.Header.Get("If-None-Match"); match != "" {
		if match == etag {
			// Topology hasn't changed
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Add hash function metadata to response
	response := map[string]interface{}{
		"cluster": clusterCopy,
		"hash_config": map[string]interface{}{
			"algorithm": "md5",
			"encoding":  "big_endian_uint32",
			"modulo":    clusterCopy.Config.PartitionCount,
			"format":    "partition-%d",
		},
	}

	// Set ETag header
	w.Header().Set("ETAG", etag)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=30") // Cache for 30 seconds

	data, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// handleGetPartitions returns all partitions
func (ck *ClusterKit) handleGetPartitions(w http.ResponseWriter, r *http.Request) {
	partitions := ck.ListPartitions()

	// Generate ETag based on partition count
	etag := fmt.Sprintf("\"%d\"", len(partitions))

	// Check If-None-Match header
	if match := r.Header.Get("If-None-Match"); match != "" {
		if match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Set ETag header
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=30")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"partitions": partitions,
		"count":      len(partitions),
	})
}

// handleGetPartitionStats returns partition statistics
func (ck *ClusterKit) handleGetPartitionStats(w http.ResponseWriter, r *http.Request) {
	stats := ck.GetPartitionStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleGetLeader returns the current leader information
func (ck *ClusterKit) handleGetLeader(w http.ResponseWriter, r *http.Request) {
	leader, err := ck.consensusManager.GetLeader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(leader)
}

// handleGetConsensusStats returns consensus statistics
func (ck *ClusterKit) handleGetConsensusStats(w http.ResponseWriter, r *http.Request) {
	stats := ck.consensusManager.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// joinNode attempts to join a node at the given address
func (ck *ClusterKit) joinNode(nodeAddr string) error {
	// Get self node info (first node in cluster is always self)
	var nodeID, nodeName string
	if len(ck.cluster.Nodes) > 0 {
		nodeID = ck.cluster.Nodes[0].ID
		nodeName = ck.cluster.Nodes[0].Name
	} else {
		nodeID = "unknown"
		nodeName = "unknown"
	}

	selfNode := Node{
		ID:     nodeID,
		Name:   nodeName,
		IP:     ck.httpAddr,
		Status: "active",
	}

	joinReq := map[string]interface{}{
		"node":      selfNode,
		"raft_addr": ck.consensusManager.raftBind,
	}

	data, err := json.Marshal(joinReq)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/join", nodeAddr)
	resp, err := ck.httpClient.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Handle redirect to leader
	if resp.StatusCode == http.StatusTemporaryRedirect {
		var redirectResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&redirectResp); err != nil {
			return fmt.Errorf("failed to decode redirect response: %v", err)
		}

		leaderIP, ok := redirectResp["leader_ip"].(string)
		if !ok || leaderIP == "" {
			return fmt.Errorf("no leader available in cluster")
		}

		fmt.Printf("Redirected to leader at %s\n", leaderIP)

		// Retry with leader
		return ck.joinNode(leaderIP)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("join failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Node information is now managed by Raft consensus
	// Local state will be updated through Raft log replication
	fmt.Printf("✓ Successfully joined cluster via %s\n", nodeAddr)

	return nil
}

// joinNodeWithRetry attempts to join a node with retry logic
func (ck *ClusterKit) joinNodeWithRetry(nodeAddr string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := ck.joinNode(nodeAddr); err == nil {
			fmt.Printf("✓ Successfully joined node %s on attempt %d\n", nodeAddr, i+1)
			return nil
		} else {
			fmt.Printf("Failed to join node %s (attempt %d/%d): %v\n", nodeAddr, i+1, maxRetries, err)
		}

		if i < maxRetries-1 {
			// Exponential backoff: wait 1s, 2s, 4s, etc.
			waitTime := time.Second * time.Duration(1<<uint(i))
			fmt.Printf("Retrying in %v...\n", waitTime)
			time.Sleep(waitTime)
		}
	}
	return fmt.Errorf("failed to join node %s after %d retries", nodeAddr, maxRetries)
}

// handleGetMetrics returns cluster metrics
func (ck *ClusterKit) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := ck.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// handleHealthCheck returns detailed health status
func (ck *ClusterKit) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	health := ck.HealthCheck()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// handleListNodes returns a paginated list of nodes (scalable for large clusters)
func (ck *ClusterKit) handleListNodes(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	limit := 50 // Default page size
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err == nil && parsedLimit == 1 {
			if limit > 1000 {
				limit = 1000 // Max 1000 nodes per page
			}
			if limit < 1 {
				limit = 1
			}
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}

	ck.mu.RLock()
	totalNodes := len(ck.cluster.Nodes)

	// Calculate pagination
	start := offset
	if start > totalNodes {
		start = totalNodes
	}

	end := start + limit
	if end > totalNodes {
		end = totalNodes
	}

	// Get paginated slice
	nodes := make([]Node, end-start)
	copy(nodes, ck.cluster.Nodes[start:end])
	ck.mu.RUnlock()

	// Build response
	response := map[string]interface{}{
		"nodes":       nodes,
		"total":       totalNodes,
		"offset":      offset,
		"limit":       limit,
		"has_more":    end < totalNodes,
		"next_offset": end,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
