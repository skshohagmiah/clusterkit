package main

import (
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
	fmt.Printf("Node ID: %s\n", nodeID)
	fmt.Printf("Node Name: %s\n", nodeName)
	fmt.Printf("HTTP Address: %s\n", httpAddr)
	fmt.Printf("Data Directory: %s\n", dataDir)
	fmt.Printf("====================================\n\n")

	// Initialize distributed KV store
	kvStore := NewDistributedKV(ck, httpAddr)
	kvStore.Start()

	// Run periodic status updates
	go runStatusUpdates(ck, kvStore)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	kvStore.Stop()
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

	fmt.Printf("[SET] Key '%s' -> Primary: %s, Replicas: %d\n", key, primary.ID, len(replicas))

	// Step 3: Send to primary (in production: HTTP POST to primary node)
	if kv.ck.IsPrimary(partition) {
		kv.mu.Lock()
		kv.localStore[key] = value
		kv.mu.Unlock()
		fmt.Printf("[SET] ✓ Stored on primary (me)\n")
	} else {
		fmt.Printf("[SET] → Would send to primary: %s at %s\n", primary.ID, primary.IP)
	}

	// Step 4: Send to replicas (in production: parallel HTTP POST to replicas)
	if kv.ck.IsReplica(partition) {
		kv.mu.Lock()
		kv.localStore[key] = value
		kv.mu.Unlock()
		fmt.Printf("[SET] ✓ Stored on replica (me)\n")
	}
	
	// Forward to other replicas
	for _, replica := range replicas {
		if replica.ID != kv.ck.GetMyNodeID() {
			fmt.Printf("[SET] → Would send to replica: %s at %s\n", replica.ID, replica.IP)
		}
	}

	return nil
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
		"local_keys":      localCount,
		"cluster_nodes":   metrics.NodeCount,
		"partitions":      metrics.PartitionCount,
		"is_leader":       metrics.IsLeader,
		"raft_state":      metrics.RaftState,
		"uptime_seconds":  metrics.UptimeSeconds,
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
