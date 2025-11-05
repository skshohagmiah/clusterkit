package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	clusterkit "github.com/skshohagmiah/clusterkit"
)

func main() {
	// Get node configuration from environment or use defaults
	nodeID := getEnv("NODE_ID", "node-1")
	nodeName := getEnv("NODE_NAME", "Server-1")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	raftAddr := getEnv("RAFT_ADDR", "127.0.0.1:9001")
	dataDir := getEnv("DATA_DIR", "./data")

	// Join address - address of any existing node (empty for first node)
	joinAddr := getEnv("JOIN_ADDR", "")

	// Bootstrap flag - set to true for the first node
	bootstrap := getEnv("BOOTSTRAP", "false") == "true"

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:       nodeID,
		NodeName:     nodeName,
		HTTPAddr:     httpAddr,
		RaftAddr:     raftAddr,
		JoinAddr:     joinAddr,
		Bootstrap:    bootstrap,
		DataDir:      dataDir,
		SyncInterval: 5 * time.Second,
		Config: &clusterkit.Config{
			ClusterName:       "my-app-cluster",
			PartitionCount:    16,
			ReplicationFactor: 3,
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize ClusterKit: %v", err)
	}

	// Start ClusterKit
	if err := ck.Start(); err != nil {
		log.Fatalf("Failed to start ClusterKit: %v", err)
	}

	fmt.Printf("\n=== Distributed KV Store Started ===\n")
	fmt.Printf("Node Name: %s\n", nodeName)
	fmt.Printf("HTTP Address: %s\n", httpAddr)
	fmt.Printf("Data Directory: %s\n", dataDir)
	fmt.Printf("====================================\n\n")

	// Start KV store
	kv := NewDistributedKV(ck, httpAddr)
	kv.Start()

	// Register partition change hook for data migration
	ck.OnPartitionChange(func(partitionID string, copyFrom *clusterkit.Node, copyTo *clusterkit.Node) {
		kv.handlePartitionChange(partitionID, copyFrom, copyTo)
	})

	fmt.Printf("\n✓ Distributed KV Store is running\n")

	// Run periodic status updates
	go runStatusUpdates(ck, kv)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	kv.Stop()
	if err := ck.Stop(); err != nil {
		log.Printf("Error stopping ClusterKit: %v", err)
	}
}

// DistributedKV is a distributed key-value store built on ClusterKit
type DistributedKV struct {
	ck         *clusterkit.ClusterKit
	localStore map[string]string
	mu         sync.RWMutex
	httpAddr   string
	server     *http.Server
}

// NewDistributedKV creates a new distributed KV store
func NewDistributedKV(ck *clusterkit.ClusterKit, httpAddr string) *DistributedKV {
	return &DistributedKV{
		ck:         ck,
		localStore: make(map[string]string),
		httpAddr:   httpAddr,
	}
}

// Start starts the KV store HTTP server
func (kv *DistributedKV) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/kv/get", kv.handleGet)
	mux.HandleFunc("/kv/set", kv.handleSet)
	mux.HandleFunc("/kv/delete", kv.handleDelete)
	mux.HandleFunc("/kv/list", kv.handleList)
	mux.HandleFunc("/kv/stats", kv.handleStats)
	mux.HandleFunc("/kv/replicate", kv.handleReplicate) // Internal replication endpoint

	// Use a different port for KV API (add 1000 to cluster port)
	// Extract port number from httpAddr (e.g., ":8080" -> 8080)
	kvPort := ":9080"
	if len(kv.httpAddr) > 1 && kv.httpAddr[0] == ':' {
		var clusterPort int
		fmt.Sscanf(kv.httpAddr, ":%d", &clusterPort)
		kvPort = fmt.Sprintf(":%d", clusterPort+1000)
	}

	kv.server = &http.Server{
		Addr:    kvPort,
		Handler: mux,
	}

	go func() {
		fmt.Printf("KV Store API listening on %s\n", kvPort)
		fmt.Printf("\nAPI Endpoints:\n")
		fmt.Printf("  GET  %s/kv/get?key=<key>\n", kvPort)
		fmt.Printf("  POST %s/kv/set (JSON: {\"key\": \"...\", \"value\": \"...\"})\n", kvPort)
		fmt.Printf("  POST %s/kv/delete?key=<key>\n", kvPort)
		fmt.Printf("  GET  %s/kv/list\n", kvPort)
		fmt.Printf("  GET  %s/kv/stats\n\n", kvPort)

		if err := kv.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("KV Store server error: %v", err)
		}
	}()
}

// Stop stops the KV store server
func (kv *DistributedKV) Stop() {
	if kv.server != nil {
		kv.server.Close()
	}
}

// handlePartitionChange is called when a partition assignment changes
func (kv *DistributedKV) handlePartitionChange(partitionID string, copyFrom *clusterkit.Node, copyTo *clusterkit.Node) {
	myNodeID := kv.ck.GetMyNodeID()

	// Only care if I'm the target node
	if copyTo == nil || copyTo.ID != myNodeID {
		return
	}

	fmt.Printf("\n[PARTITION CHANGE] I need data for partition %s\n", partitionID)

	// Copy data from the source node
	if copyFrom != nil {
		fmt.Printf("[MIGRATION] Copying from %s (%s)\n", copyFrom.ID, copyFrom.IP)
		go kv.copyPartitionData(partitionID, copyFrom)
	} else {
		fmt.Printf("[MIGRATION] No source node (I already have the data)\n")
	}
}

// copyPartitionData fetches all keys for a partition from another node
func (kv *DistributedKV) copyPartitionData(partitionID string, fromNode *clusterkit.Node) {
	if fromNode == nil {
		fmt.Printf("[MIGRATION] ✗ No source node\n")
		return
	}

	// Convert cluster port to KV port
	kvAddr := fromNode.IP
	if len(fromNode.IP) > 1 && fromNode.IP[0] == ':' {
		var clusterPort int
		fmt.Sscanf(fromNode.IP, ":%d", &clusterPort)
		kvAddr = fmt.Sprintf(":%d", clusterPort+1000)
	}

	// Fetch all keys from the node
	url := fmt.Sprintf("http://localhost%s/kv/list", kvAddr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("[MIGRATION] ✗ Failed to fetch from %s: %v\n", fromNode.ID, err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Keys  []string `json:"keys"`
		Count int      `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("[MIGRATION] ✗ Failed to decode: %v\n", err)
		return
	}

	// Copy each key that belongs to this partition
	copiedCount := 0
	for _, key := range result.Keys {
		partition, err := kv.ck.GetPartition(key)
		if err != nil || partition.ID != partitionID {
			continue
		}

		// Fetch the value
		getURL := fmt.Sprintf("http://localhost%s/kv/get?key=%s", kvAddr, key)
		getResp, err := http.Get(getURL)
		if err != nil {
			continue
		}

		var keyValue struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}

		if err := json.NewDecoder(getResp.Body).Decode(&keyValue); err == nil {
			kv.mu.Lock()
			kv.localStore[keyValue.Key] = keyValue.Value
			kv.mu.Unlock()
			copiedCount++
		}
		getResp.Body.Close()
	}

	fmt.Printf("[MIGRATION] ✓ Copied %d keys for partition %s from %s\n",
		copiedCount, partitionID, fromNode.ID)
}

// Set stores a key-value pair in the distributed store
func (kv *DistributedKV) Set(key, value string) error {
	// Step 1: Get partition for this key
	partition, err := kv.ck.GetPartition(key)
	if err != nil {
		return fmt.Errorf("failed to get partition: %v", err)
	}

	// Step 2: Get primary and replicas
	primary := kv.ck.GetPrimary(partition)
	replicas := kv.ck.GetReplicas(partition)

	fmt.Printf("[SET] Key '%s' -> Partition: %s, Primary: %s, Replicas: %d\n",
		key, partition.ID, primary.ID, len(replicas))

	// Step 3: Handle primary
	if kv.ck.IsPrimary(partition) {
		// I'm the primary - store locally
		kv.mu.Lock()
		kv.localStore[key] = value
		kv.mu.Unlock()
		fmt.Printf("[SET] ✓ Stored on primary (me)\n")

		// Replicate to all replicas in parallel
		kv.replicateToNodes(key, value, replicas)
	} else {
		// I'm not the primary - forward to primary
		fmt.Printf("[SET] → Forwarding to primary: %s\n", primary.ID)
		if err := kv.forwardToNode(primary, key, value); err != nil {
			return fmt.Errorf("failed to forward to primary: %v", err)
		}
	}

	// Step 4: Handle if I'm a replica
	if kv.ck.IsReplica(partition) {
		kv.mu.Lock()
		kv.localStore[key] = value
		kv.mu.Unlock()
		fmt.Printf("[SET] ✓ Stored on replica (me)\n")
	}

	return nil
}

// forwardToNode sends a key-value pair to another node via HTTP
func (kv *DistributedKV) forwardToNode(node *clusterkit.Node, key, value string) error {
	// Convert cluster port to KV port (add 1000)
	// e.g., :8080 -> :9080, :8081 -> :9081
	kvAddr := node.IP
	if len(node.IP) > 1 && node.IP[0] == ':' {
		var clusterPort int
		fmt.Sscanf(node.IP, ":%d", &clusterPort)
		kvAddr = fmt.Sprintf(":%d", clusterPort+1000)
	}

	url := fmt.Sprintf("http://localhost%s/kv/replicate", kvAddr)

	payload := map[string]string{
		"key":   key,
		"value": value,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("[SET] ✗ Failed to send to %s: %v\n", node.ID, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("[SET] ✓ Replicated to %s\n", node.ID)
	} else {
		fmt.Printf("[SET] ✗ Failed to replicate to %s: status %d\n", node.ID, resp.StatusCode)
	}

	return nil
}

// replicateToNodes sends data to multiple nodes in parallel
func (kv *DistributedKV) replicateToNodes(key, value string, nodes []clusterkit.Node) {
	var wg sync.WaitGroup
	myNodeID := kv.ck.GetMyNodeID()

	for _, node := range nodes {
		// Skip self
		if node.ID == myNodeID {
			continue
		}

		wg.Add(1)
		go func(n clusterkit.Node) {
			defer wg.Done()
			kv.forwardToNode(&n, key, value)
		}(node)
	}

	wg.Wait()
}

// Get retrieves a value from the distributed store
func (kv *DistributedKV) Get(key string) (string, error) {
	// Just check local store
	kv.mu.RLock()
	value, exists := kv.localStore[key]
	kv.mu.RUnlock()

	if exists {
		fmt.Printf("[GET] Retrieved from local store: %s = %s\n", key, value)
		return value, nil
	}

	return "", fmt.Errorf("key not found")
}

// Delete removes a key from the distributed store
func (kv *DistributedKV) Delete(key string) error {
	kv.mu.Lock()
	delete(kv.localStore, key)
	kv.mu.Unlock()
	fmt.Printf("[DELETE] Deleted from local store: %s\n", key)
	return nil
}

func (kv *DistributedKV) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter required", http.StatusBadRequest)
		return
	}

	value, err := kv.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"key":   key,
		"value": value,
	})
}

func (kv *DistributedKV) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Store directly without triggering further replication
	kv.mu.Lock()
	kv.localStore[req.Key] = req.Value
	kv.mu.Unlock()

	fmt.Printf("[REPLICATE] Received replication: %s = %s\n", req.Key, req.Value)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (kv *DistributedKV) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	if err := kv.Set(req.Key, req.Value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"key":    req.Key,
	})
}

func (kv *DistributedKV) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter required", http.StatusBadRequest)
		return
	}

	if err := kv.Delete(key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"key":    key,
	})
}

func (kv *DistributedKV) handleList(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	keys := make([]string, 0, len(kv.localStore))
	for k := range kv.localStore {
		keys = append(keys, k)
	}
	kv.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

func (kv *DistributedKV) handleStats(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	localCount := len(kv.localStore)
	kv.mu.RUnlock()

	metrics := kv.ck.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"local_keys":     localCount,
		"cluster_nodes":  metrics.NodeCount,
		"partitions":     metrics.PartitionCount,
		"is_leader":      metrics.IsLeader,
		"raft_state":     metrics.RaftState,
		"uptime_seconds": metrics.UptimeSeconds,
	})
}

func runStatusUpdates(ck *clusterkit.ClusterKit, kvStore *DistributedKV) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cluster := ck.GetCluster()
		metrics := ck.GetMetrics()

		kvStore.mu.RLock()
		localKeys := len(kvStore.localStore)
		kvStore.mu.RUnlock()

		fmt.Printf("\n=== Cluster Status ===\n")
		fmt.Printf("Cluster: %s\n", cluster.Name)
		fmt.Printf("Total Nodes: %d\n", len(cluster.Nodes))
		fmt.Printf("Partitions: %d\n", metrics.PartitionCount)
		fmt.Printf("Local Keys: %d\n", localKeys)
		fmt.Printf("Leader: %v\n", metrics.IsLeader)
		fmt.Printf("Raft State: %s\n", metrics.RaftState)
		for _, node := range cluster.Nodes {
			fmt.Printf("  - %s (%s) at %s [%s]\n", node.Name, node.ID, node.IP, node.Status)
		}
		fmt.Printf("======================\n\n")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
