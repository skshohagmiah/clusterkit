package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/skshohagmiah/clusterkit"
)

// KVStore with SYNC replication (quorum-based)
type KVStore struct {
	ck       *clusterkit.ClusterKit
	data     map[string]string
	mu       sync.RWMutex
	nodeID   string
	kvPort   string
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
	mux.HandleFunc("/kv/migrate", kv.handleMigrate) // Data migration endpoint

	fmt.Printf("[KV-%s] Starting on port %s (SYNC mode)\n", kv.nodeID, kv.kvPort)
	return http.ListenAndServe(":"+kv.kvPort, mux)
}

// Simple handler - just store data (client handles routing)
func (kv *KVStore) handleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Just store the data - client already routed to correct node
	kv.mu.Lock()
	kv.data[req.Key] = req.Value
	kv.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"node_id": kv.nodeID,
	})
}

func (kv *KVStore) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	// Just read from local storage - client already routed to correct node
	kv.mu.RLock()
	value, exists := kv.data[key]
	kv.mu.RUnlock()

	if exists {
		json.NewEncoder(w).Encode(map[string]string{"key": key, "value": value})
		return
	}
	
	http.Error(w, "key not found", http.StatusNotFound)
}

func (kv *KVStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	kv.mu.Lock()
	delete(kv.data, key)
	kv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (kv *KVStore) handleStats(w http.ResponseWriter, r *http.Request) {
	kv.mu.RLock()
	count := len(kv.data)
	kv.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id": kv.nodeID,
		"keys":    count,
		"mode":    "sync-quorum",
	})
}

// Migration endpoint: Export all keys for a partition
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

// Handle partition changes (data migration)
func (kv *KVStore) handlePartitionChange(partitionID string, copyFrom, copyTo *clusterkit.Node) {
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return // Not for me
	}

	if copyFrom == nil {
		fmt.Printf("[KV-%s] Partition %s: New partition, no data to copy\n",
			kv.nodeID, partitionID)
		return
	}

	fmt.Printf("[KV-%s] 🔄 Migrating partition %s from %s\n",
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

	fmt.Printf("[KV-%s] ✅ Migrated %d keys from %s for partition %s\n",
		kv.nodeID, migrationData.Count, copyFrom.ID, partitionID)
}

func main() {
	nodeID := os.Getenv("NODE_ID")
	httpPort := os.Getenv("HTTP_PORT")
	kvPort := os.Getenv("KV_PORT")
	joinAddr := os.Getenv("JOIN_ADDR")

	if nodeID == "" || httpPort == "" || kvPort == "" {
		log.Fatal("NODE_ID, HTTP_PORT, and KV_PORT required")
	}

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          ":" + httpPort,
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
