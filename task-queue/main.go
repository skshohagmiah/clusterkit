package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/skshohagmiah/clusterkit"
)

// Task represents a job to be processed
type Task struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Payload     string    `json:"payload"`
	Status      string    `json:"status"` // pending, processing, completed, failed
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// DistributedTaskQueue manages distributed task processing
type DistributedTaskQueue struct {
	ck         *clusterkit.ClusterKit
	tasks      map[string]*Task // taskID -> Task
	mu         sync.RWMutex
	httpAddr   string
	server     *http.Server
	processing map[string]bool // Track which tasks are being processed
}

// NewDistributedTaskQueue creates a new distributed task queue
func NewDistributedTaskQueue(ck *clusterkit.ClusterKit, httpAddr string) *DistributedTaskQueue {
	return &DistributedTaskQueue{
		ck:         ck,
		tasks:      make(map[string]*Task),
		httpAddr:   httpAddr,
		processing: make(map[string]bool),
	}
}

// Start starts the task queue HTTP server
func (tq *DistributedTaskQueue) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/submit", tq.handleSubmit)
	mux.HandleFunc("/tasks/status", tq.handleStatus)
	mux.HandleFunc("/tasks/list", tq.handleList)
	mux.HandleFunc("/tasks/stats", tq.handleStats)
	mux.HandleFunc("/tasks/process", tq.handleProcess) // Internal processing endpoint

	// Calculate port (ClusterKit port + 2000)
	taskPort := ":10080"
	if len(tq.httpAddr) > 1 && tq.httpAddr[0] == ':' {
		var clusterPort int
		fmt.Sscanf(tq.httpAddr, ":%d", &clusterPort)
		taskPort = fmt.Sprintf(":%d", clusterPort+2000)
	}

	tq.server = &http.Server{
		Addr:    taskPort,
		Handler: mux,
	}

	go func() {
		fmt.Printf("Task Queue API listening on %s\n", taskPort)
		fmt.Printf("\nAPI Endpoints:\n")
		fmt.Printf("  POST %s/tasks/submit (JSON: {\"type\": \"...\", \"payload\": \"...\"}\n", taskPort)
		fmt.Printf("  GET  %s/tasks/status?id=<task_id>\n", taskPort)
		fmt.Printf("  GET  %s/tasks/list\n", taskPort)
		fmt.Printf("  GET  %s/tasks/stats\n\n", taskPort)

		if err := tq.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Task Queue server error: %v", err)
		}
	}()

	// Start background task processor
	go tq.processTasksLoop()

	fmt.Println("✓ Distributed Task Queue is running")
}

// handleSubmit handles task submission
func (tq *DistributedTaskQueue) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate task ID
	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())

	// Determine which node should handle this task
	partition, err := tq.ck.GetPartition(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	task := &Task{
		ID:        taskID,
		Type:      req.Type,
		Payload:   req.Payload,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	// Check if I'm the primary for this partition
	if tq.ck.IsPrimary(partition) {
		// Store locally
		tq.mu.Lock()
		tq.tasks[taskID] = task
		tq.mu.Unlock()

		// Replicate to replicas
		replicas := tq.ck.GetReplicas(partition)
		for _, replica := range replicas {
			if replica.ID != tq.ck.GetMyNodeID() {
				go tq.replicateTask(replica.IP, task)
			}
		}
	} else {
		// Forward to primary
		primary := tq.ck.GetPrimary(partition)
		tq.forwardToNode(w, primary.IP, "/tasks/submit", req)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"task_id": taskID,
		"status":  "pending",
		"node":    tq.ck.GetMyNodeID(),
	})
}

// handleStatus returns task status
func (tq *DistributedTaskQueue) handleStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}

	// Determine which node has this task
	partition, err := tq.ck.GetPartition(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if I have this task
	if tq.ck.IsPrimary(partition) || tq.ck.IsReplica(partition) {
		tq.mu.RLock()
		task, exists := tq.tasks[taskID]
		tq.mu.RUnlock()

		if !exists {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(task)
		return
	}

	// Forward to primary
	primary := tq.ck.GetPrimary(partition)
	http.Redirect(w, r, fmt.Sprintf("http://%s/tasks/status?id=%s", primary.IP, taskID), http.StatusTemporaryRedirect)
}

// handleList lists all tasks on this node
func (tq *DistributedTaskQueue) handleList(w http.ResponseWriter, r *http.Request) {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	tasks := make([]*Task, 0, len(tq.tasks))
	for _, task := range tq.tasks {
		tasks = append(tasks, task)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
		"node":  tq.ck.GetMyNodeID(),
	})
}

// handleStats returns queue statistics
func (tq *DistributedTaskQueue) handleStats(w http.ResponseWriter, r *http.Request) {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	stats := map[string]int{
		"total":      0,
		"pending":    0,
		"processing": 0,
		"completed":  0,
		"failed":     0,
	}

	for _, task := range tq.tasks {
		stats["total"]++
		stats[task.Status]++
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"stats": stats,
		"node":  tq.ck.GetMyNodeID(),
	})
}

// handleProcess handles internal task replication
func (tq *DistributedTaskQueue) handleProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var task Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tq.mu.Lock()
	tq.tasks[task.ID] = &task
	tq.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// processTasksLoop continuously processes pending tasks
func (tq *DistributedTaskQueue) processTasksLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tq.processPendingTasks()
	}
}

// processPendingTasks finds and processes pending tasks
func (tq *DistributedTaskQueue) processPendingTasks() {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	for taskID, task := range tq.tasks {
		if task.Status == "pending" && !tq.processing[taskID] {
			// Mark as processing
			task.Status = "processing"
			tq.processing[taskID] = true

			// Process in background
			go tq.processTask(task)
		}
	}
}

// processTask simulates task processing
func (tq *DistributedTaskQueue) processTask(task *Task) {
	defer func() {
		tq.mu.Lock()
		delete(tq.processing, task.ID)
		tq.mu.Unlock()
	}()

	// Simulate work based on task type
	switch task.Type {
	case "email":
		time.Sleep(1 * time.Second)
		task.Result = fmt.Sprintf("Email sent: %s", task.Payload)
	case "image":
		time.Sleep(3 * time.Second)
		task.Result = fmt.Sprintf("Image processed: %s", task.Payload)
	case "report":
		time.Sleep(5 * time.Second)
		task.Result = fmt.Sprintf("Report generated: %s", task.Payload)
	default:
		time.Sleep(2 * time.Second)
		task.Result = fmt.Sprintf("Task completed: %s", task.Payload)
	}

	tq.mu.Lock()
	task.Status = "completed"
	task.CompletedAt = time.Now()
	tq.mu.Unlock()

	fmt.Printf("✓ Completed task %s (type: %s) on node %s\n", task.ID, task.Type, tq.ck.GetMyNodeID())
}

// replicateTask replicates task to another node
func (tq *DistributedTaskQueue) replicateTask(nodeAddr string, task *Task) {
	taskJSON, _ := json.Marshal(task)
	resp, err := http.Post(
		fmt.Sprintf("http://%s/tasks/process", nodeAddr),
		"application/json",
		bytes.NewBuffer(taskJSON),
	)
	if err != nil {
		log.Printf("Failed to replicate task to %s: %v", nodeAddr, err)
		return
	}
	defer resp.Body.Close()
}

// forwardToNode forwards request to another node
func (tq *DistributedTaskQueue) forwardToNode(w http.ResponseWriter, nodeAddr, path string, data interface{}) {
	dataJSON, _ := json.Marshal(data)
	resp, err := http.Post(
		fmt.Sprintf("http://%s%s", nodeAddr, path),
		"application/json",
		bytes.NewBuffer(dataJSON),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Forward response
	w.WriteHeader(resp.StatusCode)
	json.NewDecoder(resp.Body).Decode(w)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	// Configuration from environment
	nodeID := getEnv("NODE_ID", "node-1")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	joinAddr := getEnv("JOIN_ADDR", "")

	// Auto-detect bootstrap
	bootstrap := nodeID == "node-1" && joinAddr == ""
	if getEnv("BOOTSTRAP", "") != "" {
		bootstrap = getEnv("BOOTSTRAP", "false") == "true"
	}

	// Initialize ClusterKit
	ck, err := clusterkit.NewClusterKit(clusterkit.Options{
		NodeID:            nodeID,
		HTTPAddr:          httpAddr,
		JoinAddr:          joinAddr,
		Bootstrap:         bootstrap,
		DataDir:           fmt.Sprintf("./data/%s", nodeID),
		ClusterName:       "task-queue-cluster",
		PartitionCount:    16,
		ReplicationFactor: 3,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := ck.Start(); err != nil {
		log.Fatal(err)
	}
	defer ck.Stop()

	fmt.Println("\n=== Distributed Task Queue Started ===")
	fmt.Printf("Node ID: %s\n", nodeID)
	fmt.Printf("ClusterKit Port: %s\n", httpAddr)
	fmt.Printf("Bootstrap: %v\n", bootstrap)
	fmt.Println("=====================================")

	// Initialize Task Queue
	tq := NewDistributedTaskQueue(ck, httpAddr)
	tq.Start()

	// Wait forever
	select {}
}
