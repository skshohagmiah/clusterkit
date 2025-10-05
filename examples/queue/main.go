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
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// Message represents a queue message
type Message struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
	Retries   int       `json:"retries"`
}

// DistributedQueue represents a distributed message queue
type DistributedQueue struct {
	ck     *clusterkit.ClusterKit
	queues map[int]map[string][]*Message // partition -> topic -> messages
	mu     sync.RWMutex
}

// NewDistributedQueue creates a new distributed queue
func NewDistributedQueue() *DistributedQueue {
	return &DistributedQueue{
		queues: make(map[int]map[string][]*Message),
	}
}

func main() {
	// Get configuration from environment
	nodeID := getEnv("NODE_ID", "queue-node-1")
	advertiseAddr := getEnv("ADVERTISE_ADDR", "localhost:9000")
	httpPort := getEnvInt("HTTP_PORT", 8080)
	grpcPort := getEnvInt("GRPC_PORT", 9000)
	etcdEndpoints := []string{getEnv("ETCD_ENDPOINTS", "http://localhost:2379")}

	queue := NewDistributedQueue()

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
		OnPartitionAssigned:   queue.onPartitionAssigned,
		OnPartitionUnassigned: queue.onPartitionUnassigned,
		OnLeaderElected:       queue.onLeaderElected,
		OnLeaderLost:          queue.onLeaderLost,
	}

	// Join the cluster
	ck, err := clusterkit.Join(config)
	if err != nil {
		log.Fatalf("Failed to join cluster: %v", err)
	}
	queue.ck = ck

	// Setup HTTP handlers
	http.HandleFunc("/publish", queue.handlePublish)
	http.HandleFunc("/consume", queue.handleConsume)
	http.HandleFunc("/topics", queue.handleTopics)
	http.HandleFunc("/health", queue.handleHealth)
	http.HandleFunc("/stats", queue.handleStats)

	// Start HTTP server
	go func() {
		log.Printf("Starting Queue HTTP server on port %d", httpPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	log.Printf("Distributed Queue node %s started successfully", nodeID)

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

func (q *DistributedQueue) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	topic := r.URL.Query().Get("topic")
	payload := r.URL.Query().Get("payload")

	if topic == "" || payload == "" {
		http.Error(w, "Topic and payload are required", http.StatusBadRequest)
		return
	}

	messageID, err := q.Publish(topic, payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message_id": messageID,
		"topic":      topic,
		"status":     "published",
	})
}

func (q *DistributedQueue) handleConsume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	topic := r.URL.Query().Get("topic")
	if topic == "" {
		http.Error(w, "Topic is required", http.StatusBadRequest)
		return
	}

	message, err := q.Consume(topic)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if message == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": nil,
			"status":  "no_messages",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
		"status":  "consumed",
	})
}

func (q *DistributedQueue) handleTopics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	topics := q.GetTopics()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"topics": topics,
		"count":  len(topics),
	})
}

func (q *DistributedQueue) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !q.ck.IsHealthy() {
		http.Error(w, "Unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"node_id": q.ck.NodeID(),
	})
}

func (q *DistributedQueue) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := q.ck.GetStats()
	queueStats := q.GetQueueStats()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cluster": stats,
		"queue":   queueStats,
	})
}

// Queue Operations

func (q *DistributedQueue) Publish(topic, payload string) (string, error) {
	// Generate message ID and determine partition
	messageID := fmt.Sprintf("%s-%d", topic, time.Now().UnixNano())
	partition := q.ck.GetPartition(topic)

	if !q.ck.IsLeader(partition) {
		// Forward to leader
		leader := q.ck.GetLeader(partition)
		if leader == nil {
			return "", fmt.Errorf("no leader for partition %d", partition)
		}
		return q.forwardPublish(leader, topic, payload)
	}

	// Create message
	message := &Message{
		ID:        messageID,
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
		Retries:   0,
	}

	// Store locally
	q.mu.Lock()
	if q.queues[partition] == nil {
		q.queues[partition] = make(map[string][]*Message)
	}
	if q.queues[partition][topic] == nil {
		q.queues[partition][topic] = make([]*Message, 0)
	}
	q.queues[partition][topic] = append(q.queues[partition][topic], message)
	q.mu.Unlock()

	// Replicate to followers
	q.replicate(partition, message)

	log.Printf("Published message %s to topic %s (partition %d)", messageID, topic, partition)
	return messageID, nil
}

func (q *DistributedQueue) Consume(topic string) (*Message, error) {
	partition := q.ck.GetPartition(topic)

	if !q.ck.IsLeader(partition) {
		// Forward to leader
		leader := q.ck.GetLeader(partition)
		if leader == nil {
			return nil, fmt.Errorf("no leader for partition %d", partition)
		}
		return q.forwardConsume(leader, topic)
	}

	// Consume locally
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.queues[partition] == nil || q.queues[partition][topic] == nil {
		return nil, nil // No messages
	}

	messages := q.queues[partition][topic]
	if len(messages) == 0 {
		return nil, nil // No messages
	}

	// Get first message (FIFO)
	message := messages[0]
	q.queues[partition][topic] = messages[1:]

	log.Printf("Consumed message %s from topic %s (partition %d)", message.ID, topic, partition)
	return message, nil
}

func (q *DistributedQueue) GetTopics() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()

	topicSet := make(map[string]bool)
	for _, partitionQueues := range q.queues {
		for topic := range partitionQueues {
			topicSet[topic] = true
		}
	}

	topics := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		topics = append(topics, topic)
	}

	return topics
}

func (q *DistributedQueue) GetQueueStats() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	totalMessages := 0
	topicCounts := make(map[string]int)

	for _, partitionQueues := range q.queues {
		for topic, messages := range partitionQueues {
			count := len(messages)
			totalMessages += count
			topicCounts[topic] += count
		}
	}

	return map[string]interface{}{
		"total_messages": totalMessages,
		"topic_counts":   topicCounts,
		"total_topics":   len(topicCounts),
	}
}

// Helper functions

func (q *DistributedQueue) forwardPublish(leader *clusterkit.Node, topic, payload string) (string, error) {
	url := fmt.Sprintf("%s/publish?topic=%s&payload=%s", leader.HTTPAddr(), topic, payload)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to forward publish to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if messageID, ok := result["message_id"].(string); ok {
		return messageID, nil
	}

	return "", fmt.Errorf("invalid response from leader")
}

func (q *DistributedQueue) forwardConsume(leader *clusterkit.Node, topic string) (*Message, error) {
	url := fmt.Sprintf("%s/consume?topic=%s", leader.HTTPAddr(), topic)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to forward consume to leader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leader returned status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result["message"] == nil {
		return nil, nil
	}

	messageData, err := json.Marshal(result["message"])
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	var message Message
	err = json.Unmarshal(messageData, &message)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &message, nil
}

func (q *DistributedQueue) replicate(partition int, message *Message) {
	replicas := q.ck.GetReplicas(partition)
	for _, replica := range replicas {
		if replica.ID == q.ck.NodeID() {
			continue // Skip self
		}

		go func(node *clusterkit.Node) {
			// In a real implementation, you'd use a proper replication protocol
			log.Printf("Would replicate message %s to %s", message.ID, node.ID)
		}(replica)
	}
}

// Hook implementations

func (q *DistributedQueue) onPartitionAssigned(partition int, previousOwner *clusterkit.Node) {
	log.Printf("Partition %d assigned to this node", partition)

	q.mu.Lock()
	q.queues[partition] = make(map[string][]*Message)
	q.mu.Unlock()

	if previousOwner != nil {
		// In a real implementation, you'd migrate queue data
		log.Printf("Would migrate queue data for partition %d from %s", partition, previousOwner.ID)
	}
}

func (q *DistributedQueue) onPartitionUnassigned(partition int, newOwner *clusterkit.Node) {
	log.Printf("Partition %d moving to node %s", partition, newOwner.ID)

	q.mu.Lock()
	delete(q.queues, partition)
	q.mu.Unlock()
}

func (q *DistributedQueue) onLeaderElected(partition int) {
	log.Printf("Became leader for partition %d", partition)
}

func (q *DistributedQueue) onLeaderLost(partition int) {
	log.Printf("Lost leadership for partition %d", partition)
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
