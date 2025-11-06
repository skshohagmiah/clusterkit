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

// KVStore is a distributed key-value store
type KVStore struct {
	ck         *clusterkit.ClusterKit
	data       map[string]string
	mu         sync.RWMutex
	httpClient *http.Client
	nodeID     string
	kvPort     string
}

func main() {
	// Configuration from environment
	nodeID := getEnv("NODE_ID", "node-1")
	httpPort := getEnv("HTTP_PORT", "8080")
	kvPort := getEnv("KV_PORT", "9080")
	joinAddr := getEnv("JOIN_ADDR", "")
	bootstrap := getEnv("BOOTSTRAP", "false") == "true"
	partitions := mustAtoi(getEnv("PARTITION_COUNT", "64"))
	replication := mustAtoi(getEnv("REPLICATION_FACTOR", "3"))
	dataDir := getEnv("DATA_DIR", "./clusterkit-data")

	log.Printf("Starting node %s (HTTP: %s, KV: %s)", nodeID, httpPort, kvPort)

	// Create ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          ":" + httpPort,
		JoinAddr:          joinAddr,
		Bootstrap:         bootstrap,
		ClusterName:       "kv-cluster",
		PartitionCount:    partitions,
		ReplicationFactor: replication,
		DataDir:           dataDir,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := ck.Start(); err != nil {
		log.Fatal(err)
	}

	// Create KV store
	kv := &KVStore{
		ck:         ck,
		data:       make(map[string]string),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		nodeID:     nodeID,
		kvPort:     kvPort,
	}

	// Register partition change hook for data migration
	ck.OnPartitionChange(kv.handlePartitionChange)

	// Start KV server
	go kv.startServer()

	log.Printf("✓ Node %s ready", nodeID)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down %s...", nodeID)
	ck.Stop()
}

func (kv *KVStore) startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/kv/set", kv.handleSet)
	mux.HandleFunc("/kv/get", kv.handleGet)
	mux.HandleFunc("/kv/delete", kv.handleDelete)
	mux.HandleFunc("/kv/list", kv.handleList)
	mux.HandleFunc("/kv/stats", kv.handleStats)
	mux.HandleFunc("/health", kv.handleHealth)

	log.Fatal(http.ListenAndServe(":"+kv.kvPort, mux))
}

func (kv *KVStore) handleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if this is replication from primary
	isReplication := r.Header.Get("X-Replication") == "true"

	if isReplication {
		// Direct replication - just store
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "role": "replica"})
		return
	}

	// Get partition
	partition, err := kv.ck.GetPartition(req.Key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If I'm primary, store and replicate
	if kv.ck.IsPrimary(partition) {
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		// Replicate to all replicas
		go kv.replicateToReplicas(req.Key, req.Value, partition)

		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "role": "primary"})
		return
	}

	// If I'm replica, just store
	if kv.ck.IsReplica(partition) {
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "role": "replica"})
		return
	}

	// Forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary == nil {
		http.Error(w, "primary not found", http.StatusServiceUnavailable)
		return
	}

	if err := kv.forwardRequest(primary, req.Key, req.Value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "role": "forwarded"})
}

func (kv *KVStore) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	partition, err := kv.ck.GetPartition(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If I'm primary or replica, try to serve
	if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
		kv.mu.RLock()
		value, exists := kv.data[key]
		kv.mu.RUnlock()

		if exists {
			json.NewEncoder(w).Encode(map[string]string{"key": key, "value": value})
			return
		}

		// If I'm replica and don't have it, try primary
		if kv.ck.IsReplica(partition) {
			primary := kv.ck.GetPrimary(partition)
			if primary != nil && primary.ID != kv.nodeID {
				value, err := kv.forwardGet(primary, key)
				if err == nil {
					// Cache it
					kv.mu.Lock()
					kv.data[key] = value
					kv.mu.Unlock()

					json.NewEncoder(w).Encode(map[string]string{"key": key, "value": value})
					return
				}
			}
		}

		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	// Forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary == nil {
		http.Error(w, "primary not found", http.StatusServiceUnavailable)
		return
	}

	value, err := kv.forwardGet(primary, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"key": key, "value": value})
}

func (kv *KVStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	kv.mu.Lock()
	delete(kv.data, key)
	kv.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (kv *KVStore) handleList(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	keys := make([]string, 0, len(kv.data))
	for key := range kv.data {
		keys = append(keys, key)
	}
	kv.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

func (kv *KVStore) handleStats(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	count := len(kv.data)
	kv.mu.RUnlock()

	metrics := kv.ck.GetMetrics()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id":    kv.nodeID,
		"local_keys": count,
		"nodes":      metrics.NodeCount,
		"partitions": metrics.PartitionCount,
	})
}

func (kv *KVStore) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (kv *KVStore) replicateToReplicas(key, value string, partition *clusterkit.Partition) {
	replicas := kv.ck.GetReplicas(partition)

	for _, replica := range replicas {
		if replica.ID == kv.nodeID {
			continue
		}

		go func(node clusterkit.Node) {
			kvAddr := convertToKVAddr(node.IP)
			url := fmt.Sprintf("http://localhost%s/kv/set", kvAddr)

			payload := map[string]string{"key": key, "value": value}
			data, _ := json.Marshal(payload)

			req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Replication", "true")

			resp, err := kv.httpClient.Do(req)
			if err != nil {
				log.Printf("[REPLICATION] Failed to %s: %v", node.ID, err)
				return
			}
			resp.Body.Close()
		}(replica)
	}
}

func (kv *KVStore) forwardRequest(node *clusterkit.Node, key, value string) error {
	kvAddr := convertToKVAddr(node.IP)
	url := fmt.Sprintf("http://localhost%s/kv/set", kvAddr)

	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	resp, err := kv.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("forward failed: %d", resp.StatusCode)
	}

	return nil
}

func (kv *KVStore) forwardGet(node *clusterkit.Node, key string) (string, error) {
	kvAddr := convertToKVAddr(node.IP)
	url := fmt.Sprintf("http://localhost%s/kv/get?key=%s", kvAddr, key)

	resp, err := kv.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("not found")
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

func (kv *KVStore) handlePartitionChange(partitionID string, copyFrom, copyTo *clusterkit.Node) {
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return
	}

	if copyFrom == nil {
		log.Printf("[MIGRATION] Partition %s assigned (no source)", partitionID)
		return
	}

	log.Printf("[MIGRATION] Starting copy of partition %s from %s", partitionID, copyFrom.ID)
	
	// Actually copy the data
	go kv.copyPartitionData(partitionID, copyFrom)
}

func (kv *KVStore) copyPartitionData(partitionID string, fromNode *clusterkit.Node) {
	kvAddr := convertToKVAddr(fromNode.IP)
	
	// Get all keys from source node
	url := fmt.Sprintf("http://localhost%s/kv/list", kvAddr)
	resp, err := kv.httpClient.Get(url)
	if err != nil {
		log.Printf("[MIGRATION] Failed to connect to %s: %v", fromNode.ID, err)
		return
	}
	defer resp.Body.Close()

	var keysResp struct {
		Keys []string `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		log.Printf("[MIGRATION] Failed to decode keys: %v", err)
		return
	}

	// Copy each key that belongs to this partition
	copied := 0
	for _, key := range keysResp.Keys {
		// Check if key belongs to this partition
		partition, err := kv.ck.GetPartition(key)
		if err != nil || partition.ID != partitionID {
			continue
		}

		// Fetch value from source
		getURL := fmt.Sprintf("http://localhost%s/kv/get?key=%s", kvAddr, key)
		getResp, err := kv.httpClient.Get(getURL)
		if err != nil {
			continue
		}

		var valueResp struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}

		if err := json.NewDecoder(getResp.Body).Decode(&valueResp); err != nil {
			getResp.Body.Close()
			continue
		}
		getResp.Body.Close()

		// Store locally
		kv.mu.Lock()
		kv.data[key] = valueResp.Value
		kv.mu.Unlock()

		copied++
	}

	log.Printf("[MIGRATION] ✓ Copied %d keys for partition %s from %s", copied, partitionID, fromNode.ID)
}

func convertToKVAddr(clusterAddr string) string {
	var port int
	fmt.Sscanf(clusterAddr, ":%d", &port)
	return fmt.Sprintf(":%d", port+1000)
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
