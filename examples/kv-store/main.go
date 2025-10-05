package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// KVStore represents a distributed key-value store
type KVStore struct {
	ck   *clusterkit.ClusterKit
	data map[int]map[string]string
	mu   sync.RWMutex
}

// NewKVStore creates a new KV store instance
func NewKVStore() *KVStore {
	return &KVStore{
		data: make(map[int]map[string]string),
	}
}

func main() {
	// Get configuration from environment or use defaults
	nodeID := getEnv("NODE_ID", "kv-node-1")
	advertiseAddr := getEnv("ADVERTISE_ADDR", "localhost:9000")
	httpPort := getEnvInt("HTTP_PORT", 8080)
	grpcPort := getEnvInt("GRPC_PORT", 9000)
	etcdEndpoints := []string{getEnv("ETCD_ENDPOINTS", "http://localhost:2379")}

	store := NewKVStore()

	// Configure ClusterKit
	config := &clusterkit.Config{
		NodeID:        nodeID,
		AdvertiseAddr: advertiseAddr,
		HTTPPort:      httpPort,
		GRPCPort:      grpcPort,
		Partitions:    16, // Small number for demo
		ReplicaFactor: 2,
		EtcdEndpoints: etcdEndpoints,

		// Hook functions
		OnPartitionAssigned:   store.onPartitionAssigned,
		OnPartitionUnassigned: store.onPartitionUnassigned,
		OnLeaderElected:       store.onLeaderElected,
		OnLeaderLost:          store.onLeaderLost,
	}

	// Join the cluster
	ck, err := clusterkit.Join(config)
	if err != nil {
		log.Fatalf("Failed to join cluster: %v", err)
	}
	store.ck = ck

	// Setup HTTP handlers
	http.HandleFunc("/set", store.handleSet)
	http.HandleFunc("/get", store.handleGet)
	http.HandleFunc("/delete", store.handleDelete)
	http.HandleFunc("/dump", store.handleDump)
	http.HandleFunc("/replicate", store.handleReplicate)
	http.HandleFunc("/health", store.handleHealth)
	http.HandleFunc("/stats", store.handleStats)

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on port %d", httpPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	log.Printf("KV Store node %s started successfully", nodeID)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Leave the cluster gracefully
	if err := ck.Leave(); err != nil {
		log.Printf("Error leaving cluster: %v", err)
	}

	log.Println("Shutdown complete")
}

// HTTP Handlers

func (s *KVStore) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	value := r.URL.Query().Get("value")

	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	err := s.Set(key, value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func (s *KVStore) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	value, err := s.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s", value)
}

func (s *KVStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	err := s.Delete(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func (s *KVStore) handleDump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	partitionStr := r.URL.Query().Get("partition")
	if partitionStr == "" {
		http.Error(w, "Partition is required", http.StatusBadRequest)
		return
	}

	partition, err := strconv.Atoi(partitionStr)
	if err != nil {
		http.Error(w, "Invalid partition", http.StatusBadRequest)
		return
	}

	data := s.DumpPartition(partition)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *KVStore) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	value := r.URL.Query().Get("value")

	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	s.ReplicateSet(key, value)
	w.WriteHeader(http.StatusOK)
}

func (s *KVStore) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.ck.IsHealthy() {
		http.Error(w, "Unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"node_id": s.ck.NodeID(),
	})
}

func (s *KVStore) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.ck.GetStats()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// KV Store Operations

func (s *KVStore) Set(key, value string) error {
	partition := s.ck.GetPartition(key)

	if !s.ck.IsLeader(partition) {
		// Forward to leader
		leader := s.ck.GetLeader(partition)
		if leader == nil {
			return fmt.Errorf("no leader for partition %d", partition)
		}
		return s.forwardSet(leader, key, value)
	}

	// Store locally
	s.mu.Lock()
	if s.data[partition] == nil {
		s.data[partition] = make(map[string]string)
	}
	s.data[partition][key] = value
	s.mu.Unlock()

	// Replicate to followers
	s.replicate(partition, key, value)

	log.Printf("Set key=%s value=%s partition=%d", key, value, partition)
	return nil
}

func (s *KVStore) Get(key string) (string, error) {
	partition := s.ck.GetPartition(key)

	s.mu.RLock()
	partitionData, exists := s.data[partition]
	if !exists {
		s.mu.RUnlock()
		return "", fmt.Errorf("key not found: %s", key)
	}

	value, exists := partitionData[key]
	s.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("key not found: %s", key)
	}

	return value, nil
}

func (s *KVStore) Delete(key string) error {
	partition := s.ck.GetPartition(key)

	if !s.ck.IsLeader(partition) {
		// Forward to leader
		leader := s.ck.GetLeader(partition)
		if leader == nil {
			return fmt.Errorf("no leader for partition %d", partition)
		}
		return s.forwardDelete(leader, key)
	}

	// Delete locally
	s.mu.Lock()
	if partitionData, exists := s.data[partition]; exists {
		delete(partitionData, key)
	}
	s.mu.Unlock()

	log.Printf("Deleted key=%s partition=%d", key, partition)
	return nil
}

func (s *KVStore) DumpPartition(partition int) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.data[partition]
	if !exists {
		return make(map[string]string)
	}

	// Return a copy
	result := make(map[string]string)
	for k, v := range data {
		result[k] = v
	}

	return result
}

func (s *KVStore) ReplicateSet(key, value string) {
	partition := s.ck.GetPartition(key)

	s.mu.Lock()
	if s.data[partition] == nil {
		s.data[partition] = make(map[string]string)
	}
	s.data[partition][key] = value
	s.mu.Unlock()

	log.Printf("Replicated key=%s value=%s partition=%d", key, value, partition)
}

// Helper functions

func (s *KVStore) forwardSet(leader *clusterkit.Node, key, value string) error {
	url := fmt.Sprintf("%s/set?key=%s&value=%s", leader.HTTPAddr(), key, value)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to forward set to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	return nil
}

func (s *KVStore) forwardDelete(leader *clusterkit.Node, key string) error {
	url := fmt.Sprintf("%s/delete?key=%s", leader.HTTPAddr(), key)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to forward delete to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	return nil
}

func (s *KVStore) replicate(partition int, key, value string) {
	replicas := s.ck.GetReplicas(partition)
	for _, replica := range replicas {
		if replica.ID == s.ck.NodeID() {
			continue // Skip self
		}

		go func(node *clusterkit.Node) {
			url := fmt.Sprintf("%s/replicate?key=%s&value=%s", node.HTTPAddr(), key, value)
			_, err := http.Post(url, "application/json", nil)
			if err != nil {
				log.Printf("Failed to replicate to %s: %v", node.ID, err)
			}
		}(replica)
	}
}

// Hook implementations

func (s *KVStore) onPartitionAssigned(partition int, previousOwner *clusterkit.Node) {
	log.Printf("Partition %d assigned to this node", partition)

	s.mu.Lock()
	s.data[partition] = make(map[string]string)
	s.mu.Unlock()

	if previousOwner != nil {
		s.pullData(partition, previousOwner)
	}
}

func (s *KVStore) onPartitionUnassigned(partition int, newOwner *clusterkit.Node) {
	log.Printf("Partition %d moving to node %s", partition, newOwner.ID)

	s.mu.Lock()
	delete(s.data, partition)
	s.mu.Unlock()
}

func (s *KVStore) onLeaderElected(partition int) {
	log.Printf("Became leader for partition %d", partition)
}

func (s *KVStore) onLeaderLost(partition int) {
	log.Printf("Lost leadership for partition %d", partition)
}

func (s *KVStore) pullData(partition int, node *clusterkit.Node) {
	url := fmt.Sprintf("%s/dump?partition=%d", node.HTTPAddr(), partition)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Failed to pull data from %s: %v", node.ID, err)
		return
	}
	defer resp.Body.Close()

	var data map[string]string
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		log.Printf("Failed to decode data from %s: %v", node.ID, err)
		return
	}

	s.mu.Lock()
	s.data[partition] = data
	s.mu.Unlock()

	log.Printf("Pulled %d keys for partition %d from %s", len(data), partition, node.ID)
}

// Utility functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
