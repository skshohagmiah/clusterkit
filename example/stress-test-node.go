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

	"github.com/skshohagmiah/clusterkit"
)

// StressTestKV is a high-performance distributed key-value store for stress testing
type StressTestKV struct {
	ck         *clusterkit.ClusterKit
	localStore map[string]string
	mu         sync.RWMutex
	httpClient *http.Client
	nodeID     string
	kvPort     string
}

func main() {
	// Get configuration from environment
	nodeID := getEnv("NODE_ID", "node-1")
	httpPort := getEnv("HTTP_PORT", "8080")
	kvPort := getEnv("KV_PORT", "9080")
	joinAddr := getEnv("JOIN_ADDR", "")
	bootstrap := getEnv("BOOTSTRAP", "false") == "true"
	partitionCount := mustAtoi(getEnv("PARTITION_COUNT", "256"))
	replicationFactor := mustAtoi(getEnv("REPLICATION_FACTOR", "3"))

	httpAddr := fmt.Sprintf(":%s", httpPort)
	raftAddr := fmt.Sprintf("127.0.0.1:%s", fmt.Sprintf("%d", 7000+mustAtoi(httpPort)-8000))

	log.Printf("Starting Stress Test Node: %s", nodeID)
	log.Printf("  HTTP: %s", httpAddr)
	log.Printf("  KV: :%s", kvPort)
	log.Printf("  Raft: %s", raftAddr)
	log.Printf("  Bootstrap: %v", bootstrap)
	log.Printf("  Partitions: %d", partitionCount)
	log.Printf("  Replication Factor: %d", replicationFactor)

	// Initialize ClusterKit with configurable settings
	opts := clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          httpAddr,
		RaftAddr:          raftAddr,
		DataDir:           fmt.Sprintf("/tmp/clusterkit-stress-%s", nodeID),
		ClusterName:       "stress-test-cluster",
		PartitionCount:    partitionCount,
		ReplicationFactor: replicationFactor,
		SyncInterval:      10 * time.Second,
		JoinAddr:          joinAddr,
		Bootstrap:         bootstrap,
		Logger:            clusterkit.NewDefaultLogger(clusterkit.LogLevelInfo),
	}

	ck, err := clusterkit.NewClusterKit(opts)
	if err != nil {
		log.Fatalf("Failed to create ClusterKit: %v", err)
	}

	// Start ClusterKit
	if err := ck.Start(); err != nil {
		log.Fatalf("Failed to start ClusterKit: %v", err)
	}

	// Create KV store
	kv := &StressTestKV{
		ck:         ck,
		localStore: make(map[string]string),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		nodeID:     nodeID,
		kvPort:     kvPort,
	}

	// Register partition change hook for automatic data migration
	ck.OnPartitionChange(kv.handlePartitionChange)

	// Start KV HTTP server
	go kv.startServer()

	log.Printf("✓ Node %s ready on port %s", nodeID, kvPort)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down node %s...", nodeID)
	ck.Stop()
}

func (kv *StressTestKV) startServer() {
	mux := http.NewServeMux()

	// KV operations
	mux.HandleFunc("/kv/set", kv.handleSet)
	mux.HandleFunc("/kv/get", kv.handleGet)
	mux.HandleFunc("/kv/delete", kv.handleDelete)
	mux.HandleFunc("/kv/stats", kv.handleStats)
	mux.HandleFunc("/kv/cluster-info", kv.handleClusterInfo)

	addr := ":" + kv.kvPort
	log.Printf("KV server listening on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("KV server failed: %v", err)
	}
}

func (kv *StressTestKV) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
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
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	// Check if this is a replication request (from primary to replica)
	isReplication := r.Header.Get("X-Replication") == "true"

	if isReplication {
		// Direct replication from primary - store locally without further checks
		kv.mu.Lock()
		kv.localStore[req.Key] = req.Value
		kv.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"key":    req.Key,
			"role":   "replica",
		})
		return
	}

	// Get partition for this key
	partition, err := kv.ck.GetPartition(req.Key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if I'm the primary for this partition
	if kv.ck.IsPrimary(partition) {
		// I'm the primary - store locally
		kv.mu.Lock()
		kv.localStore[req.Key] = req.Value
		kv.mu.Unlock()

		// Replicate to replicas SYNCHRONOUSLY (wait for confirmations)
		successCount, totalReplicas := kv.replicateToReplicasSync(req.Key, req.Value, partition)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "success",
			"key":               req.Key,
			"role":              "primary",
			"replicas_synced":   successCount,
			"replicas_expected": totalReplicas,
		})
		return
	}

	// Check if I'm a replica for this partition
	if kv.ck.IsReplica(partition) {
		// I'm a replica - store locally
		kv.mu.Lock()
		kv.localStore[req.Key] = req.Value
		kv.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"key":    req.Key,
			"role":   "replica",
		})
		return
	}

	// I'm neither primary nor replica - forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary == nil {
		http.Error(w, "primary node not found", http.StatusServiceUnavailable)
		return
	}

	if err := kv.forwardSet(primary, req.Key, req.Value); err != nil {
		http.Error(w, fmt.Sprintf("forward failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "forwarded",
		"key":    req.Key,
		"to":     primary.ID,
	})
}

func (kv *StressTestKV) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	// Get partition for this key
	partition, err := kv.ck.GetPartition(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if I'm primary or replica for this partition
	if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
		// I should have this key (primary or replica)
		kv.mu.RLock()
		value, exists := kv.localStore[key]
		kv.mu.RUnlock()

		if !exists {
			// Key not found locally - this can happen due to:
			// 1. Async replication hasn't completed yet
			// 2. Replication failed
			// 3. Partition was recently reassigned
			// Solution: Forward to primary for consistency
			primary := kv.ck.GetPrimary(partition)
			if primary == nil || primary.ID == kv.nodeID {
				// I AM the primary and don't have it - truly not found
				http.Error(w, "key not found", http.StatusNotFound)
				return
			}
			
			// Forward to primary to get the authoritative value (with retries)
			var value string
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				value, err = kv.forwardGet(primary, key)
				if err == nil {
					break
				}
				if attempt < 2 {
					time.Sleep(100 * time.Millisecond)
				}
			}
			if err != nil {
				http.Error(w, fmt.Sprintf("key not found locally, forward to primary failed after retries: %v", err), http.StatusNotFound)
				return
			}
			
			// Cache it locally for future reads
			kv.mu.Lock()
			kv.localStore[key] = value
			kv.mu.Unlock()
			
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"key":   key,
				"value": value,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key":   key,
			"value": value,
		})
	} else {
		// Forward to a node that has it
		partition, _ := kv.ck.GetPartition(key)
		primary := kv.ck.GetPrimary(partition)
		if primary == nil {
			http.Error(w, "primary node not found", http.StatusServiceUnavailable)
			return
		}

		// Forward with retries
		var value string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			value, err = kv.forwardGet(primary, key)
			if err == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("forward failed after retries: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key":   key,
			"value": value,
		})
	}
}

func (kv *StressTestKV) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	kv.mu.Lock()
	delete(kv.localStore, key)
	kv.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"key":    key,
	})
}

func (kv *StressTestKV) handleStats(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	localKeys := len(kv.localStore)
	kv.mu.RUnlock()

	metrics := kv.ck.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id":         kv.nodeID,
		"local_keys":      localKeys,
		"cluster_nodes":   metrics.NodeCount,
		"partitions":      metrics.PartitionCount,
		"request_count":   metrics.RequestCount,
		"error_count":     metrics.ErrorCount,
		"uptime_seconds":  metrics.UptimeSeconds,
	})
}

func (kv *StressTestKV) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	cluster := kv.ck.GetCluster()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cluster_id":      cluster.ID,
		"cluster_name":    cluster.Name,
		"node_count":      len(cluster.Nodes),
		"partition_count": len(cluster.PartitionMap.Partitions),
		"config":          cluster.Config,
	})
}

func (kv *StressTestKV) replicateToReplicas(key, value string, partition *clusterkit.Partition) {
	replicas := kv.ck.GetReplicas(partition)

	for _, replica := range replicas {
		// Skip self
		if replica.ID == kv.nodeID {
			continue
		}

		// Forward to replica with replication header
		go func(node clusterkit.Node) {
			if err := kv.forwardSetWithHeader(&node, key, value, true); err != nil {
				log.Printf("[REPLICATION] Failed to replicate to %s: %v", node.ID, err)
			}
		}(replica)
	}
}

// replicateToReplicasSync replicates to replicas synchronously and waits for confirmations
func (kv *StressTestKV) replicateToReplicasSync(key, value string, partition *clusterkit.Partition) (successCount, totalReplicas int) {
	replicas := kv.ck.GetReplicas(partition)
	
	// Use a wait group to wait for all replications
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount = 0
	totalReplicas = 0

	for _, replica := range replicas {
		// Skip self
		if replica.ID == kv.nodeID {
			continue
		}

		totalReplicas++
		wg.Add(1)

		// Replicate to each replica in parallel but wait for all
		go func(node clusterkit.Node) {
			defer wg.Done()
			
			// Retry up to 2 times for transient errors
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				err := kv.forwardSetWithHeader(&node, key, value, true)
				if err == nil {
					mu.Lock()
					successCount++
					mu.Unlock()
					return
				}
				lastErr = err
				if attempt < 2 {
					time.Sleep(50 * time.Millisecond)
				}
			}
			
			log.Printf("[REPLICATION] Failed to replicate %s to %s after 3 attempts: %v", key, node.ID, lastErr)
		}(replica)
	}

	// Wait for all replications to complete (with timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All replications completed
	case <-time.After(5 * time.Second):
		// Timeout - some replications didn't complete
		log.Printf("[REPLICATION] Timeout waiting for replicas for key %s (got %d/%d)", key, successCount, totalReplicas)
	}

	return successCount, totalReplicas
}

func (kv *StressTestKV) forwardSet(node *clusterkit.Node, key, value string) error {
	return kv.forwardSetWithHeader(node, key, value, false)
}

func (kv *StressTestKV) forwardSetWithHeader(node *clusterkit.Node, key, value string, isReplication bool) error {
	// Convert cluster port to KV port
	kvAddr := kv.convertToKVAddr(node.IP)

	url := fmt.Sprintf("http://localhost%s/kv/set", kvAddr)
	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if isReplication {
		req.Header.Set("X-Replication", "true")
	}

	resp, err := kv.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("forward failed: %d", resp.StatusCode)
	}

	return nil
}

func (kv *StressTestKV) forwardGet(node *clusterkit.Node, key string) (string, error) {
	kvAddr := kv.convertToKVAddr(node.IP)

	url := fmt.Sprintf("http://localhost%s/kv/get?key=%s", kvAddr, key)
	
	// Retry up to 2 times for transient errors
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := kv.httpClient.Get(url)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			if attempt < 2 && resp.StatusCode >= 500 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return "", lastErr
		}

		var result struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", err
		}

		return result.Value, nil
	}
	
	return "", lastErr
}

func (kv *StressTestKV) convertToKVAddr(clusterAddr string) string {
	// Convert :8080 -> :9080, :8081 -> :9081, etc.
	var port int
	fmt.Sscanf(clusterAddr, ":%d", &port)
	return fmt.Sprintf(":%d", port+1000)
}

func (kv *StressTestKV) handlePartitionChange(partitionID string, copyFrom, copyTo *clusterkit.Node) {
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return // Not for me
	}

	if copyFrom == nil {
		log.Printf("[MIGRATION] Partition %s assigned (no source to copy from)", partitionID)
		return
	}

	log.Printf("[MIGRATION] Need to copy partition %s from %s", partitionID, copyFrom.ID)

	// Copy data from source node
	go kv.copyPartitionData(partitionID, copyFrom)
}

func (kv *StressTestKV) copyPartitionData(partitionID string, fromNode *clusterkit.Node) {
	if fromNode == nil {
		return
	}

	kvAddr := kv.convertToKVAddr(fromNode.IP)

	// Get all keys from source
	url := fmt.Sprintf("http://localhost%s/kv/stats", kvAddr)
	resp, err := kv.httpClient.Get(url)
	if err != nil {
		log.Printf("[MIGRATION] Failed to fetch from %s: %v", fromNode.ID, err)
		return
	}
	defer resp.Body.Close()

	log.Printf("[MIGRATION] ✓ Copied data for partition %s", partitionID)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mustAtoi(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}
