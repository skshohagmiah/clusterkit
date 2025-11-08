package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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
		data:   make(map[string]string),
		nodeID: nodeID,
		kvPort: kvPort,
	}

	// Register partition change hook
	ck.OnPartitionChange(func(partitionID string, copyFromNodes []*clusterkit.Node, copyTo *clusterkit.Node) {
		kv.handlePartitionChange(partitionID, copyFromNodes, copyTo)
	})

	// Register node rejoin hook - CRITICAL for data sync!
	ck.OnNodeRejoin(func(node *clusterkit.Node) {
		if node.ID == kv.nodeID {
			// This node is rejoining - sync all data!
			log.Printf("[KV-%s]  REJOINING cluster - syncing all partitions...\n", kv.nodeID)
			kv.syncAllPartitionsOnRejoin()
		}
	})

	// Register node leave hook
	ck.OnNodeLeave(func(nodeID string) {
		log.Printf("[KV-%s] Node %s left the cluster\n", kv.nodeID, nodeID)
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

// handlePartitionChange is called when a partition is reassigned
// syncAllPartitionsOnRejoin syncs all partitions when this node rejoins
func (kv *KVStore) syncAllPartitionsOnRejoin() {
	log.Printf("[KV-%s] 🗑️  Clearing all local data (stale after offline period)\n", kv.nodeID)
	
	// STRATEGY 1: Clear all existing data (RECOMMENDED for production)
	// This ensures no stale data remains
	kv.mu.Lock()
	kv.data = make(map[string]string) // Clear everything!
	kv.mu.Unlock()
	
	// STRATEGY 2 (Alternative): Keep local data and merge
	// Use this if you want to preserve local writes during offline period
	// But you'll need conflict resolution (version vectors, last-write-wins, etc.)
	
	// Get all partitions this node should have
	cluster := kv.ck.GetCluster()
	if cluster.PartitionMap == nil {
		log.Printf("[KV-%s] No partitions to sync\n", kv.nodeID)
		return
	}

	synced := 0
	for _, partition := range cluster.PartitionMap.Partitions {
		// Check if this node is primary or replica for this partition
		isPrimary := partition.PrimaryNode == kv.nodeID
		isReplica := false
		for _, replicaID := range partition.ReplicaNodes {
			if replicaID == kv.nodeID {
				isReplica = true
				break
			}
		}

		if isPrimary || isReplica {
			// Sync this partition from other replicas
			copyFromNodes := []*clusterkit.Node{}
			
			// Add primary if not self
			if partition.PrimaryNode != kv.nodeID {
				for _, node := range cluster.Nodes {
					if node.ID == partition.PrimaryNode {
						copyFromNodes = append(copyFromNodes, &node)
						break
					}
				}
			}
			
			// Add replicas if not self
			for _, replicaID := range partition.ReplicaNodes {
				if replicaID != kv.nodeID {
					for _, node := range cluster.Nodes {
						if node.ID == replicaID {
							copyFromNodes = append(copyFromNodes, &node)
							break
						}
					}
				}
			}

			if len(copyFromNodes) > 0 {
				// Sync data for this partition
				kv.syncPartitionData(partition.ID, copyFromNodes)
				synced++
			}
		}
	}

	log.Printf("[KV-%s] ✅ Synced %d partitions on rejoin\n", kv.nodeID, synced)
}

// syncPartitionData fetches and merges data for a specific partition
func (kv *KVStore) syncPartitionData(partitionID string, sourceNodes []*clusterkit.Node) {
	for _, sourceNode := range sourceNodes {
		// Build migration URL
		kvPort := extractPort(sourceNode.IP)
		kvPort = fmt.Sprintf("9%s", kvPort[1:]) // Convert 8080 -> 9080
		url := fmt.Sprintf("http://localhost:%s/kv/migrate?partition=%s", kvPort, partitionID)

		// Fetch data
		resp, err := http.Get(url)
		if err != nil {
			continue // Try next source
		}
		defer resp.Body.Close()

		var data map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			continue
		}

		// Copy fresh data from replica
		// Since we cleared local data, this is a clean copy (no conflicts!)
		kv.mu.Lock()
		for key, value := range data {
			kv.data[key] = value
		}
		kv.mu.Unlock()

		log.Printf("[KV-%s] ✅ Synced partition %s from %s (%d keys)\n", 
			kv.nodeID, partitionID, sourceNode.ID, len(data))
		return // Successfully synced from this source
	}
}

func (kv *KVStore) handlePartitionChange(partitionID string, copyFromNodes []*clusterkit.Node, copyTo *clusterkit.Node) {
	// Only act if I'm the destination node
	if copyTo == nil || copyTo.ID != kv.nodeID {
		return
	}

	if len(copyFromNodes) == 0 {
		log.Printf("[KV-%s] New partition %s assigned (no data to copy)\n", kv.nodeID, partitionID)
		return
	}

	log.Printf("[KV-%s] 🔄 Migrating partition %s from %d nodes\n", kv.nodeID, partitionID, len(copyFromNodes))

	// Merge data from ALL source nodes
	mergedData := make(map[string]string)
	
	for _, sourceNode := range copyFromNodes {
		// Fetch data from this source node
		url := fmt.Sprintf("http://localhost:%s/kv/migrate?partition=%s", extractPort(sourceNode.IP), partitionID)
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("[KV-%s] ⚠️  Failed to fetch from %s: %v\n", kv.nodeID, sourceNode.ID, err)
			continue
		}

		// Decode migration data
		var migrationData struct {
			PartitionID string            `json:"partition_id"`
			Keys        map[string]string `json:"keys"`
			Count       int               `json:"count"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&migrationData); err != nil {
			log.Printf("[KV-%s] ⚠️  Failed to decode from %s: %v\n", kv.nodeID, sourceNode.ID, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		// Merge keys (last write wins - developer can add versioning here)
		for key, value := range migrationData.Keys {
			mergedData[key] = value
		}
		
		log.Printf("[KV-%s] 📦 Fetched %d keys from %s\n", kv.nodeID, migrationData.Count, sourceNode.ID)
	}

	// Import all merged keys
	kv.mu.Lock()
	for key, value := range mergedData {
		kv.data[key] = value
	}
	kv.mu.Unlock()

	log.Printf("[KV-%s] ✅ Migrated %d keys for partition %s from %d sources\n", 
		kv.nodeID, len(mergedData), partitionID, len(copyFromNodes))
}

func extractPort(ip string) string {
	return ip[strings.LastIndex(ip, ":")+1:]
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

	// Default data directory if not provided
	if dataDir == "" {
		dataDir = "./clusterkit-data"
	}

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          ":" + httpPort,
		JoinAddr:          joinAddr,
		DataDir:           dataDir,
		PartitionCount:    64,
		ReplicationFactor: 3,
		Bootstrap:         joinAddr == "",
		HealthCheck: clusterkit.HealthCheckConfig{
			Enabled:          true,
			Interval:         5 * time.Second,
			Timeout:          2 * time.Second,
			FailureThreshold: 3,
		},
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
