package clusterkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ClusterKit is the main entry point for the library
type ClusterKit struct {
	cluster          *Cluster
	nodeID           string // Current node's ID
	stateFile        string
	httpAddr         string
	httpServer       *http.Server
	httpClient       *http.Client
	knownNodes       []string
	mu               sync.RWMutex
	stopChan         chan struct{}
	syncInterval     time.Duration
	consensusManager *ConsensusManager
	hookManager      *HookManager // Partition change hooks
	healthChecker    *HealthChecker // Health monitoring
	logger           Logger       // Structured logger
	// Metrics tracking
	startTime    time.Time
	lastSync     time.Time
	requestCount int64
	errorCount   int64
}

// Options for initializing ClusterKit
type Options struct {
	// Required
	NodeID   string // Unique ID for this node (e.g., "node-1")
	HTTPAddr string // Address to listen on (e.g., ":8080")
	
	// Optional - Auto-generated if not provided
	NodeName          string        // Human-readable name (default: auto-generated from NodeID)
	RaftAddr          string        // Raft bind address (default: auto-calculated from HTTPAddr)
	DataDir           string        // Directory to store state (default: "./clusterkit-data")
	SyncInterval      time.Duration // Sync interval (default: 5s)
	
	// Cluster Configuration - Flattened (no nested Config struct)
	ClusterName       string // Name of the cluster (default: "clusterkit-cluster")
	PartitionCount    int    // Number of partitions (default: 16)
	ReplicationFactor int    // Replication factor (default: 3)
	
	// Cluster Formation
	JoinAddr  string // Address of existing node to join (empty for first node)
	Bootstrap bool   // Set to true for first node (default: auto-detect)
	
	// Health Checking
	HealthCheck HealthCheckConfig // Health check configuration
	
	// Optional Logger
	Logger Logger // Custom logger (default: DefaultLogger with Info level)
}

// NewClusterKit initializes a new ClusterKit instance
func NewClusterKit(opts Options) (*ClusterKit, error) {
	// Validate required fields
	if opts.NodeID == "" {
		return nil, fmt.Errorf("NodeID is required")
	}
	if opts.HTTPAddr == "" {
		return nil, fmt.Errorf("HTTPAddr is required")
	}
	
	// Auto-generate NodeName from NodeID if not provided
	if opts.NodeName == "" {
		opts.NodeName = generateNodeName(opts.NodeID)
	}
	
	// Auto-calculate RaftAddr from HTTPAddr if not provided
	if opts.RaftAddr == "" {
		opts.RaftAddr = calculateRaftAddr(opts.HTTPAddr)
	}
	
	// Set defaults for cluster configuration
	if opts.ClusterName == "" {
		opts.ClusterName = "clusterkit-cluster"
	}
	if opts.PartitionCount <= 0 {
		opts.PartitionCount = 16
	}
	if opts.ReplicationFactor <= 0 {
		opts.ReplicationFactor = 3
	}
	
	// Set defaults for optional fields
	if opts.DataDir == "" {
		opts.DataDir = "./clusterkit-data"
	}
	if opts.SyncInterval == 0 {
		opts.SyncInterval = 5 * time.Second
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(opts.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	stateFile := filepath.Join(opts.DataDir, "cluster-state.json")

	// Initialize cluster with flattened config
	cluster := &Cluster{
		ID:           opts.ClusterName,
		Name:         opts.ClusterName,
		Nodes:        []Node{},
		PartitionMap: &PartitionMap{Partitions: make(map[string]*Partition)},
		Config: &Config{
			ClusterName:       opts.ClusterName,
			PartitionCount:    opts.PartitionCount,
			ReplicationFactor: opts.ReplicationFactor,
		},
	}

	// Add self as a node
	selfNode := Node{
		ID:     opts.NodeID,
		Name:   opts.NodeName,
		IP:     opts.HTTPAddr,
		Status: "active",
	}
	cluster.Nodes = append(cluster.Nodes, selfNode)
	cluster.rebuildNodeMap() // Initialize NodeMap for O(1) lookups

	// Set default logger if not provided
	logger := opts.Logger
	if logger == nil {
		logger = NewDefaultLogger(LogLevelInfo)
	}
	
	// Initialize ClusterKit
	ck := &ClusterKit{
		cluster:      cluster,
		nodeID:       opts.NodeID,
		stateFile:    stateFile,
		httpAddr:     opts.HTTPAddr,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		knownNodes:   []string{},
		stopChan:     make(chan struct{}),
		syncInterval: opts.SyncInterval,
		logger:       logger,
		startTime:    time.Now(),
	}
	
	// Set join address if provided
	if opts.JoinAddr != "" {
		ck.knownNodes = []string{opts.JoinAddr}
	}

	// Initialize consensus manager
	ck.consensusManager = NewConsensusManager(ck, opts.Bootstrap, opts.RaftAddr)

	// Initialize hook manager
	ck.hookManager = &HookManager{
		hooks:              []PartitionChangeHook{},
		lastPartitionState: make(map[string]*Partition),
		workerPool:         make(chan struct{}, 50),
	}

	// Initialize health checker
	ck.healthChecker = NewHealthChecker(ck, opts.HealthCheck)

	// Load existing state if available
	if err := ck.loadState(); err != nil {
		fmt.Printf("No existing state found, starting fresh: %v\n", err)
	}

	return ck, nil
}

// Start begins the ClusterKit operations
func (ck *ClusterKit) Start() error {
	// Start HTTP server for inter-node communication
	if err := ck.startHTTPServer(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %v", err)
	}

	// Start consensus manager
	if err := ck.consensusManager.Start(); err != nil {
		return fmt.Errorf("failed to start consensus: %v", err)
	}

	// Start health checker
	ck.healthChecker.Start()

	// Discover and join known nodes
	if len(ck.knownNodes) > 0 {
		fmt.Printf("[JOIN] Attempting to join cluster via %v\n", ck.knownNodes)
		go ck.discoverNodes()
	} else {
		fmt.Printf("[JOIN] No known nodes to join (bootstrap=%v)\n", ck.consensusManager.isBootstrap)
	}

	// Auto-create partitions if bootstrap node and no partitions exist
	if ck.consensusManager.isBootstrap {
		go func() {
			// Wait for leader election
			time.Sleep(3 * time.Second)
			
			// Bootstrap node: Propose self addition through Raft to ensure all nodes have it
			if ck.consensusManager.IsLeader() {
				ck.mu.RLock()
				selfNode := ck.cluster.Nodes[0] // Bootstrap node is always first
				ck.mu.RUnlock()
				
				fmt.Printf("Bootstrap node proposing self through Raft: %s\n", selfNode.ID)
				if err := ck.consensusManager.ProposeAction("add_node", map[string]interface{}{
					"id":     selfNode.ID,
					"name":   selfNode.Name,
					"ip":     selfNode.IP,
					"status": selfNode.Status,
				}); err != nil {
					fmt.Printf("Warning: Failed to propose bootstrap node: %v\n", err)
				}
				
				// Wait for Raft replication
				time.Sleep(1 * time.Second)
			}
			
			ck.mu.RLock()
			replicationFactor := ck.cluster.Config.ReplicationFactor
			ck.mu.RUnlock()
			
			// Wait for enough nodes to join (up to 30 seconds)
			maxWait := 30
			if replicationFactor == 1 {
				maxWait = 3 // Single-node clusters don't need to wait long
			}
			
			for i := 0; i < maxWait; i++ {
				if !ck.consensusManager.IsLeader() {
					return // Not leader anymore
				}
				
				ck.mu.RLock()
				nodeCount := len(ck.cluster.Nodes)
				partitionCount := len(ck.cluster.PartitionMap.Partitions)
				ck.mu.RUnlock()
				
				// If partitions already exist, we're done
				if partitionCount > 0 {
					return
				}
				
				// Create partitions if we have enough nodes
				if nodeCount >= replicationFactor {
					// Deduplicate nodes before creating partitions
					ck.deduplicateNodes()
					
					ck.mu.RLock()
					finalNodeCount := len(ck.cluster.Nodes)
					ck.mu.RUnlock()
					
					fmt.Printf("Auto-creating partitions with %d nodes (replication factor: %d)...\n", finalNodeCount, replicationFactor)
					if err := ck.CreatePartitions(); err != nil {
						fmt.Printf("Failed to auto-create partitions: %v\n", err)
						// Continue loop to retry
					} else {
						fmt.Printf("✓ Created %d partitions automatically\n", ck.cluster.Config.PartitionCount)
						return // Success!
					}
				}
				
				// Wait before checking again
				time.Sleep(1 * time.Second)
			}
			
			// Timeout reached - log warning
			ck.mu.RLock()
			nodeCount := len(ck.cluster.Nodes)
			ck.mu.RUnlock()
			fmt.Printf("Warning: Partition auto-creation timed out after %d seconds (have %d nodes, need %d)\n", 
				maxWait, nodeCount, replicationFactor)
		}()
	}

	fmt.Printf("ClusterKit started on %s\n", ck.httpAddr)
	return nil
}

// Stop gracefully shuts down ClusterKit
func (ck *ClusterKit) Stop() error {
	close(ck.stopChan)

	// Gracefully shutdown HTTP server
	if ck.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ck.httpServer.Shutdown(ctx); err != nil {
			fmt.Printf("Failed to shutdown HTTP server gracefully: %v\n", err)
		}
	}

	// Stop health checker
	ck.healthChecker.Stop()

	// Stop consensus manager
	ck.consensusManager.Stop()

	// Save final state
	if err := ck.saveState(); err != nil {
		return fmt.Errorf("failed to save state: %v", err)
	}

	fmt.Println("ClusterKit stopped")
	return nil
}

// GetCluster returns the current cluster state
func (ck *ClusterKit) GetCluster() *Cluster {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	return ck.cluster
}

// GetConsensusManager returns the consensus manager
func (ck *ClusterKit) GetConsensusManager() *ConsensusManager {
	return ck.consensusManager
}

// saveState persists the cluster state to disk atomically
func (ck *ClusterKit) saveState() error {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	data, err := json.MarshalIndent(ck.cluster, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %v", err)
	}

	// Atomic write: write to temp file, then rename
	tempFile := ck.stateFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, ck.stateFile); err != nil {
		os.Remove(tempFile) // Clean up temp file on error
		return fmt.Errorf("failed to rename state file: %v", err)
	}

	return nil
}

// loadState loads the cluster state from disk
func (ck *ClusterKit) loadState() error {
	data, err := os.ReadFile(ck.stateFile)
	if err != nil {
		return err
	}

	ck.mu.Lock()
	defer ck.mu.Unlock()

	if err := json.Unmarshal(data, ck.cluster); err != nil {
		return fmt.Errorf("failed to unmarshal state: %v", err)
	}

	return nil
}

// discoverNodes attempts to connect to known nodes
func (ck *ClusterKit) discoverNodes() {
	fmt.Printf("[JOIN] discoverNodes called with %d known nodes\n", len(ck.knownNodes))
	for _, nodeAddr := range ck.knownNodes {
		fmt.Printf("[JOIN] Attempting to join %s...\n", nodeAddr)
		if err := ck.joinNodeWithRetry(nodeAddr, 3); err != nil {
			fmt.Printf("[JOIN] Failed to join node %s after retries: %v\n", nodeAddr, err)
		} else {
			fmt.Printf("[JOIN] Successfully joined cluster via %s\n", nodeAddr)
		}
	}
}

// deduplicateNodes removes duplicate nodes from the cluster
func (ck *ClusterKit) deduplicateNodes() {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	seen := make(map[string]bool)
	uniqueNodes := []Node{}

	for _, node := range ck.cluster.Nodes {
		if !seen[node.ID] {
			seen[node.ID] = true
			uniqueNodes = append(uniqueNodes, node)
		} else {
			fmt.Printf("Removing duplicate node: %s (%s)\n", node.Name, node.ID)
		}
	}

	ck.cluster.Nodes = uniqueNodes
	fmt.Printf("Deduplicated nodes: %d -> %d\n", len(ck.cluster.Nodes)+len(seen)-len(uniqueNodes), len(uniqueNodes))
}

// GetMetrics returns current cluster metrics
func (ck *ClusterKit) GetMetrics() *Metrics {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	var raftState string
	var isLeader bool
	if ck.consensusManager != nil {
		isLeader = ck.consensusManager.IsLeader()
		if stats := ck.consensusManager.GetStats(); stats != nil {
			raftState = stats.State
		}
	}

	return &Metrics{
		NodeCount:       len(ck.cluster.Nodes),
		PartitionCount:  len(ck.cluster.PartitionMap.Partitions),
		RequestCount:    ck.requestCount,
		ErrorCount:      ck.errorCount,
		LastSync:        ck.lastSync,
		IsLeader:        isLeader,
		RaftState:       raftState,
		UptimeSeconds:   int64(time.Since(ck.startTime).Seconds()),
	}
}

// HealthCheck returns detailed health status
func (ck *ClusterKit) HealthCheck() *HealthStatus {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	var raftState string
	var isLeader bool
	if ck.consensusManager != nil {
		isLeader = ck.consensusManager.IsLeader()
		if stats := ck.consensusManager.GetStats(); stats != nil {
			raftState = stats.State
		}
	}

	// Get self node info
	var nodeID, nodeName string
	if len(ck.cluster.Nodes) > 0 {
		nodeID = ck.cluster.Nodes[0].ID
		nodeName = ck.cluster.Nodes[0].Name
	}

	uptime := time.Since(ck.startTime)
	
	return &HealthStatus{
		Healthy:        true,
		NodeID:         nodeID,
		NodeName:       nodeName,
		IsLeader:       isLeader,
		NodeCount:      len(ck.cluster.Nodes),
		PartitionCount: len(ck.cluster.PartitionMap.Partitions),
		RaftState:      raftState,
		LastSync:       ck.lastSync,
		Uptime:         uptime.String(),
	}
}

// generateNodeName creates a human-readable name from NodeID
// Examples: "node-1" -> "Server-1", "server-2" -> "Server-2"
func generateNodeName(nodeID string) string {
	var num int
	
	// Try "node-N" pattern
	if _, err := fmt.Sscanf(nodeID, "node-%d", &num); err == nil {
		return fmt.Sprintf("Server-%d", num)
	}
	
	// Try "server-N" pattern
	if _, err := fmt.Sscanf(nodeID, "server-%d", &num); err == nil {
		return fmt.Sprintf("Server-%d", num)
	}
	
	// Try just "N" pattern
	if _, err := fmt.Sscanf(nodeID, "%d", &num); err == nil {
		return fmt.Sprintf("Server-%d", num)
	}
	
	// Default: capitalize first letter
	if len(nodeID) > 0 {
		return string(nodeID[0]-32) + nodeID[1:]
	}
	
	return nodeID
}

// calculateRaftAddr auto-calculates Raft address from HTTP address
// Examples: ":8080" -> "127.0.0.1:10001", ":8081" -> "127.0.0.1:10002"
// Uses port range 10001+ to avoid conflicts with common application ports (9000-9999)
func calculateRaftAddr(httpAddr string) string {
	var port int
	
	// Try to extract port from HTTP address
	if _, err := fmt.Sscanf(httpAddr, ":%d", &port); err == nil {
		// Calculate Raft port: 10001 + (httpPort - 8080)
		// This ensures Raft ports are in the 10000+ range, avoiding conflicts
		raftPort := 10001 + (port - 8080)
		return fmt.Sprintf("127.0.0.1:%d", raftPort)
	}
	
	// Default to 10001
	return "127.0.0.1:10001"
}
