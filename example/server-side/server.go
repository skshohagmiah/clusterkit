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
		data:   make(map[string]string),
		nodeID: nodeID,
		kvPort: kvPort,
	}

	// Register partition change hook
	ck.OnPartitionChange(func(event *clusterkit.PartitionChangeEvent) {
		kv.handlePartitionChange(event.PartitionID, event.CopyFromNodes, event.CopyToNode)
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
		// I'm REPLICA - accept write (primary may be down - FAILOVER)
		kv.mu.Lock()
		kv.data[req.Key] = req.Value
		kv.mu.Unlock()

		// Log warning if this is a direct client write (not replication)
		if !req.Replicate {
			fmt.Printf("[KV-%s] ⚠️  Replica accepting write for %s (primary may be down)\n", 
				kv.nodeID, partition.ID)
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"role":    "replica",
			"node":    kv.nodeID,
			"warning": "written_to_replica",
		})
		return
	}

	// I'm neither primary nor replica - forward to primary
	primary := kv.ck.GetPrimary(partition)
	if primary != nil {
		// Try forwarding to primary
		if kv.forwardToPrimary(w, primary.IP, req.Key, req.Value) {
			return // Success
		}
		fmt.Printf("[KV-%s] ⚠️  Primary forward failed, trying replicas...\n", kv.nodeID)
	}

	// Primary failed or not found - try replicas (FAILOVER)
	replicas := kv.ck.GetReplicas(partition)
	for _, replica := range replicas {
		if kv.forwardToPrimary(w, replica.IP, req.Key, req.Value) {
			fmt.Printf("[KV-%s] ✅ Failover: forwarded to replica %s\n", kv.nodeID, replica.ID)
			return
		}
	}

	http.Error(w, "all nodes unavailable", http.StatusServiceUnavailable)
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

func (kv *KVStore) forwardToPrimary(w http.ResponseWriter, primaryAddr, key, value string) bool {
	payload := map[string]string{"key": key, "value": value}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("http://%s/kv/set", primaryAddr)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(map[string]string{"status": "forwarded"})
	return true
}

// Handle partition change (data migration)
func (kv *KVStore) handlePartitionChange(partitionID string, copyFromNodes []*clusterkit.Node, copyTo *clusterkit.Node) {
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return // Not for me
	}

	if len(copyFromNodes) == 0 {
		fmt.Printf("[KV-%s] Partition %s: New partition, no data to copy\n",
			kv.nodeID, partitionID)
		return
	}

	fmt.Printf("[KV-%s] 🔄 Partition %s: Migrating data from %d nodes\n",
		kv.nodeID, partitionID, len(copyFromNodes))

	// Merge data from ALL source nodes
	mergedData := make(map[string]string)
	
	for _, sourceNode := range copyFromNodes {
		// Request all keys for this partition from the source node
		url := fmt.Sprintf("http://%s/kv/migrate?partition=%s", sourceNode.IP, partitionID)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("[KV-%s] ⚠️  Failed to fetch from %s: %v\n", kv.nodeID, sourceNode.ID, err)
			continue
		}

		var migrationData struct {
			PartitionID string            `json:"partition_id"`
			Keys        map[string]string `json:"keys"`
			Count       int               `json:"count"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&migrationData); err != nil {
			fmt.Printf("[KV-%s] ⚠️  Failed to decode from %s: %v\n", kv.nodeID, sourceNode.ID, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		// Merge keys (last write wins - developer can add versioning here)
		for key, value := range migrationData.Keys {
			mergedData[key] = value
		}
		
		fmt.Printf("[KV-%s] 📦 Fetched %d keys from %s\n", kv.nodeID, migrationData.Count, sourceNode.ID)
	}

	// Store all merged keys locally
	kv.mu.Lock()
	for key, value := range mergedData {
		kv.data[key] = value
	}
	kv.mu.Unlock()

	fmt.Printf("[KV-%s] ✅ Migration completed: %d keys merged from %d sources for partition %s\n",
		kv.nodeID, len(mergedData), len(copyFromNodes), partitionID)
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
