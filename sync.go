package clusterkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// StateUpdate represents a state update message
type StateUpdate struct {
	Node      Node      `json:"node"`
	Cluster   *Cluster  `json:"cluster"`
	Timestamp time.Time `json:"timestamp"`
}

// startHTTPServer starts the HTTP server for inter-node communication
func (ck *ClusterKit) startHTTPServer() error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", ck.handleHealth)

	// Node registration endpoint
	mux.HandleFunc("/join", ck.handleJoin)

	// Note: State synchronization is handled by Raft consensus, no separate /sync endpoint needed

	// Partition endpoints
	mux.HandleFunc("/partitions", ck.handleGetPartitions)
	mux.HandleFunc("/partitions/stats", ck.handleGetPartitionStats)
	mux.HandleFunc("/partitions/key", ck.handleGetPartitionForKey)

	// Consensus endpoints
	mux.HandleFunc("/consensus/leader", ck.handleGetLeader)
	mux.HandleFunc("/consensus/stats", ck.handleGetConsensusStats)

	// Metrics and monitoring endpoints
	mux.HandleFunc("/metrics", ck.handleGetMetrics)
	mux.HandleFunc("/health/detailed", ck.handleHealthCheck)
	mux.HandleFunc("/cluster", ck.handleGetCluster)

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
		http.Error(w, "not the leader", http.StatusServiceUnavailable)
		return
	}

	// Add node to Raft cluster
	if err := cm.AddVoter(node.ID, joinReq.RaftAddr); err != nil {
		fmt.Printf("Failed to add voter to Raft: %v\n", err)
		http.Error(w, fmt.Sprintf("failed to add to raft: %v", err), http.StatusInternalServerError)
		return
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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Result{
		Success: true,
		Message: "Node joined successfully",
		Data:    ck.cluster,
	})
}

// handleSync handles state synchronization requests
func (ck *ClusterKit) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var update StateUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ck.mu.Lock()
	// Merge nodes from incoming state
	for _, incomingNode := range update.Cluster.Nodes {
		found := false
		for i, existingNode := range ck.cluster.Nodes {
			if existingNode.ID == incomingNode.ID {
				ck.cluster.Nodes[i] = incomingNode
				found = true
				break
			}
		}
		if !found {
			ck.cluster.Nodes = append(ck.cluster.Nodes, incomingNode)
			ck.cluster.rebuildNodeMap() // Rebuild map for O(1) lookups
		}
	}
	ck.mu.Unlock()

	// Save updated state
	ck.saveState()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Result{
		Success: true,
		Message: "State synced successfully",
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
			"algorithm":  "fnv1a",
			"seed":       0,
			"modulo":     clusterCopy.Config.PartitionCount,
			"format":     "partition-%d",
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

// handleGetPartitionForKey returns the partition for a given key
func (ck *ClusterKit) handleGetPartitionForKey(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter is required", http.StatusBadRequest)
		return
	}

	partition, err := ck.GetPartition(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(partition)
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

// syncWithNode synchronizes state with a specific node
func (ck *ClusterKit) syncWithNode(node Node) error {
	ck.mu.RLock()
	// Get self node info
	var selfNodeID, selfNodeName string
	if len(ck.cluster.Nodes) > 0 {
		selfNodeID = ck.cluster.Nodes[0].ID
		selfNodeName = ck.cluster.Nodes[0].Name
	}

	// Create a deep copy of the cluster to avoid race conditions
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

	update := StateUpdate{
		Node: Node{
			ID:     selfNodeID,
			Name:   selfNodeName,
			IP:     ck.httpAddr,
			Status: "active",
		},
		Cluster:   &clusterCopy,
		Timestamp: time.Now(),
	}
	ck.mu.RUnlock()

	data, err := json.Marshal(update)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/sync", node.IP)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sync failed with status %d", resp.StatusCode)
	}

	return nil
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
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("join failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Update local state with cluster info from the joined node
	if clusterData, ok := result.Data.(map[string]interface{}); ok {
		ck.mu.Lock()
		if nodes, ok := clusterData["nodes"].([]interface{}); ok {
			for _, n := range nodes {
				if nodeMap, ok := n.(map[string]interface{}); ok {
					node := Node{
						ID:     nodeMap["id"].(string),
						Name:   nodeMap["name"].(string),
						IP:     nodeMap["ip"].(string),
						Status: nodeMap["status"].(string),
					}

					// Add if not exists
					exists := false
					for _, existing := range ck.cluster.Nodes {
						if existing.ID == node.ID {
							exists = true
							break
						}
					}
					if !exists {
						ck.cluster.Nodes = append(ck.cluster.Nodes, node)
						ck.cluster.rebuildNodeMap() // Rebuild map for O(1) lookups
					}
				}
			}
		}
		ck.mu.Unlock()
		ck.saveState()
	}

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
