package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/skshohagmiah/clusterkit"
)

// KVStore with SERVER-SIDE partition handling
// Server checks if it's primary/replica and handles replication
type KVStore struct {
	ck     *clusterkit.ClusterKit
	data   map[string]string
	mu     sync.RWMutex
	nodeID string
	kvPort string
}

func NewKVStore(ck *clusterkit.ClusterKit, nodeID, kvPort string) *KVStore {
	kv := &KVStore{
		ck:     ck,
		data:   make(map[string]string),
		nodeID: nodeID,
		kvPort: kvPort,
	}

	// Register partition change hook for data migration
	ck.OnPartitionChange(func(partitionID string, copyFrom, copyTo *clusterkit.Node) {
		kv.handlePartitionChange(partitionID, copyFrom, copyTo)
	})

	return kv
}

func (kv *KVStore) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/kv/set", kv.handleSet)
	mux.HandleFunc("/kv/get", kv.handleGet)
	mux.HandleFunc("/kv/delete", kv.handleDelete)
	mux.HandleFunc("/kv/stats", kv.handleStats)
	mux.HandleFunc("/kv/partitions", kv.handlePartitions)
	mux.HandleFunc("/kv/migrate", kv.handleMigrate) // Data migration endpoint

	fmt.Printf("[KV-%s] Starting on port %s (SERVER-SIDE mode)\n", kv.nodeID, kv.kvPort)
	return http.ListenAndServe(":"+kv.kvPort, mux)
}

// SERVER-SIDE: Check if I'm primary/replica and handle accordingly
func (kv *KVStore) handleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Replicate bool  `json:"replicate,omitempty"` // Internal flag
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		// I'm PRIMARY - store locally and replicate
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		// Replicate to replicas if not already a replication request
		if !req.Replicate {
			go kv.replicateToReplicas(partition, req.Key, req.Value)
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"role":   "primary",
			"node":   kv.nodeID,
		})
		return
	}

	// Check if I'm a replica
	if kv.ck.IsReplica(partition) {
		// I'm REPLICA - just store locally
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"role":   "replica",
			"node":   kv.nodeID,
		})
		return
	}

	// I'm neither primary nor replica - forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary == nil {
		http.Error(w, "primary not found", http.StatusServiceUnavailable)
		return
	}

	// Forward to primary
	kv.forwardToPrimary(w, primary.IP, req.Key, req.Value)
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

	// If I'm primary or replica, serve from local storage
	if kv.ck.IsPrimary(partition) || kv.ck.IsReplica(partition) {
		kv.mu.RLock()
		value, exists := kv.data[key]
		role := "replica"
		if kv.ck.IsPrimary(partition) {
			role = "primary"
		}
		kv.mu.RUnlock()

		if exists {
			json.NewEncoder(w).Encode(map[string]string{
				"key":   key,
				"value": value,
				"role":  role,
				"node":  kv.nodeID,
			})
			return
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

	resp, err := http.Get(fmt.Sprintf("http://%s/kv/get?key=%s", primary.IP, key))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (kv *KVStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	
	partition, err := kv.ck.GetPartition(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if kv.ck.IsPrimary(partition) {
		kv.mu.Lock()
		delete(kv.data, key)
		kv.mu.Unlock()

		// Replicate deletion to replicas
		go kv.replicateDeletion(partition, key)

		json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "role": "primary"})
		return
	}

	if kv.ck.IsReplica(partition) {
		kv.mu.Lock()
		delete(kv.data, key)
		kv.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "role": "replica"})
		return
	}

	// Forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary != nil {
		resp, _ := http.Get(fmt.Sprintf("http://%s/kv/delete?key=%s", primary.IP, key))
		if resp != nil {
			defer resp.Body.Close()
			w.WriteHeader(resp.StatusCode)
		}
	}
}

func (kv *KVStore) handleStats(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	totalKeys := len(kv.data)
	kv.mu.RUnlock()

	// Get my partitions
	partitions := kv.ck.GetPartitionsForNode(kv.nodeID)
	primaryCount := 0
	replicaCount := 0

	for _, p := range partitions {
		if kv.ck.IsPrimary(p) {
			primaryCount++
		} else if kv.ck.IsReplica(p) {
			replicaCount++
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id":         kv.nodeID,
		"total_keys":      totalKeys,
		"primary_partitions": primaryCount,
		"replica_partitions": replicaCount,
		"mode":            "server-side",
	})
}

func (kv *KVStore) handlePartitions(w http.ResponseWriter, r *http.Request) {
	partitions := kv.ck.GetPartitionsForNode(kv.nodeID)
	
	result := make([]map[string]interface{}, 0)
	for _, p := range partitions {
		role := "replica"
		if kv.ck.IsPrimary(p) {
			role = "primary"
		}
		
		result = append(result, map[string]interface{}{
			"partition_id": p.ID,
			"role":         role,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id":    kv.nodeID,
		"partitions": result,
	})
}

// New endpoint: Get all keys for a specific partition (for data migration)
func (kv *KVStore) handleMigrate(w http.ResponseWriter, r *http.Request) {
	partitionID := r.URL.Query().Get("partition")
	if partitionID == "" {
		http.Error(w, "partition required", http.StatusBadRequest)
		return
	}

	// Get all keys belonging to this partition
	kv.mu.RLock()
	partitionKeys := make(map[string]string)
	for key, value := range kv.data {
		partition, err := kv.ck.GetPartition(key)
		if err == nil && partition.ID == partitionID {
			partitionKeys[key] = value
		}
	}
	kv.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"partition_id": partitionID,
		"keys":         partitionKeys,
		"count":        len(partitionKeys),
	})
}

// Replication helpers
func (kv *KVStore) replicateToReplicas(partition *clusterkit.Partition, key, value string) {
	replicas := kv.ck.GetReplicas(partition)
	
	for _, replica := range replicas {
		payload := map[string]interface{}{
			"key":       key,
			"value":     value,
			"replicate": true, // Mark as replication request
		}
		data, _ := json.Marshal(payload)

		url := fmt.Sprintf("http://%s/kv/set", replica.IP)
		resp, err := http.Post(url, "application/json", bytes.NewReader(data))
		if err == nil && resp != nil {
			resp.Body.Close()
		}
	}
}

func (kv *KVStore) replicateDeletion(partition *clusterkit.Partition, key string) {
	replicas := kv.ck.GetReplicas(partition)
	
	for _, replica := range replicas {
		url := fmt.Sprintf("http://%s/kv/delete?key=%s", replica.IP, key)
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			resp.Body.Close()
		}
	}
}

func (kv *KVStore) forwardToPrimary(w http.ResponseWriter, primaryAddr, key, value string) {
	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("http://%s/kv/set", primaryAddr)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(map[string]string{"status": "forwarded"})
}

// Handle partition change (data migration)
func (kv *KVStore) handlePartitionChange(partitionID string, copyFrom, copyTo *clusterkit.Node) {
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return // Not for me
	}

	if copyFrom == nil {
		fmt.Printf("[KV-%s] Partition %s: New partition, no data to copy\n",
			kv.nodeID, partitionID)
		return
	}

	fmt.Printf("[KV-%s] 🔄 Partition %s: Migrating data from %s\n",
		kv.nodeID, partitionID, copyFrom.ID)

	// Request all keys for this partition from the source node
	url := fmt.Sprintf("http://%s/kv/migrate?partition=%s", copyFrom.IP, partitionID)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("[KV-%s] ❌ Migration failed: %v\n", kv.nodeID, err)
		return
	}
	defer resp.Body.Close()

	var migrationData struct {
		PartitionID string            `json:"partition_id"`
		Keys        map[string]string `json:"keys"`
		Count       int               `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&migrationData); err != nil {
		fmt.Printf("[KV-%s] ❌ Failed to decode migration data: %v\n", kv.nodeID, err)
		return
	}

	// Store all migrated keys locally
	kv.mu.Lock()
	for key, value := range migrationData.Keys {
		kv.data[key] = value
	}
	kv.mu.Unlock()

	fmt.Printf("[KV-%s] ✅ Migration completed: %d keys copied from %s for partition %s\n",
		kv.nodeID, migrationData.Count, copyFrom.ID, partitionID)
}

func main() {
	nodeID := os.Getenv("NODE_ID")
	httpPort := os.Getenv("HTTP_PORT")
	kvPort := os.Getenv("KV_PORT")
	joinAddr := os.Getenv("JOIN_ADDR")
	dataDir := os.Getenv("DATA_DIR")

	if nodeID == "" || httpPort == "" || kvPort == "" {
		log.Fatal("NODE_ID, HTTP_PORT, and KV_PORT required")
	}

	if dataDir == "" {
		dataDir = "./clusterkit-data"
	}

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          ":" + httpPort,
		DataDir:           dataDir,
		JoinAddr:          joinAddr,
		PartitionCount:    64,
		ReplicationFactor: 3,
		Bootstrap:         joinAddr == "",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := ck.Start(); err != nil {
		log.Fatal(err)
	}
	defer ck.Stop()

	// Start KV store
	kv := NewKVStore(ck, nodeID, kvPort)
	log.Fatal(kv.Start())
}
