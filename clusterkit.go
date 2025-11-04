package clusterkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ClusterKit is the main entry point for the library
type ClusterKit struct {
	cluster          *Cluster
	stateFile        string
	walFile          string
	httpAddr         string
	knownNodes       []string
	mu               sync.RWMutex
	stopChan         chan struct{}
	syncInterval     time.Duration
	consensusManager *ConsensusManager
}

// Options for initializing ClusterKit
type Options struct {
	NodeID       string        // Unique ID for this node
	NodeName     string        // Human-readable name
	HTTPAddr     string        // Address to listen on (e.g., ":8080")
	RaftAddr     string        // Raft bind address (e.g., "127.0.0.1:9001")
	JoinAddr     string        // Address of any existing node to join (empty for first node)
	DataDir      string        // Directory to store state and WAL
	SyncInterval time.Duration // How often to sync with other nodes
	Config       *Config       // Cluster configuration
	Bootstrap    bool          // Set to true for the first node in the cluster
}

// NewClusterKit initializes a new ClusterKit instance
func NewClusterKit(opts Options) (*ClusterKit, error) {
	if opts.NodeID == "" {
		return nil, fmt.Errorf("NodeID is required")
	}
	if opts.HTTPAddr == "" {
		return nil, fmt.Errorf("HTTPAddr is required")
	}
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
	walFile := filepath.Join(opts.DataDir, "wal.log")

	// Initialize cluster
	cluster := &Cluster{
		ID:           opts.NodeID,
		Name:         opts.Config.ClusterName,
		Nodes:        []Node{},
		PartitionMap: &PartitionMap{Partitions: make(map[string]*Partition)},
		Config:       opts.Config,
	}

	// Add self as a node
	selfNode := Node{
		ID:     opts.NodeID,
		Name:   opts.NodeName,
		IP:     opts.HTTPAddr,
		Status: "active",
	}
	cluster.Nodes = append(cluster.Nodes, selfNode)

	ck := &ClusterKit{
		cluster:      cluster,
		stateFile:    stateFile,
		walFile:      walFile,
		httpAddr:     opts.HTTPAddr,
		knownNodes:   []string{},
		stopChan:     make(chan struct{}),
		syncInterval: opts.SyncInterval,
	}
	
	// Set join address if provided
	if opts.JoinAddr != "" {
		ck.knownNodes = []string{opts.JoinAddr}
	}

	// Initialize consensus manager
	ck.consensusManager = NewConsensusManager(ck, opts.Bootstrap, opts.RaftAddr)

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

	// Discover and join known nodes
	go ck.discoverNodes()

	// Start periodic state sync
	go ck.syncLoop()

	// Auto-create partitions if bootstrap node and no partitions exist
	if ck.consensusManager.isBootstrap {
		go func() {
			// Wait for leader election
			time.Sleep(2 * time.Second)
			if ck.consensusManager.IsLeader() && len(ck.cluster.PartitionMap.Partitions) == 0 {
				fmt.Println("Auto-creating partitions...")
				if err := ck.CreatePartitions(); err != nil {
					fmt.Printf("Failed to auto-create partitions: %v\n", err)
				} else {
					fmt.Printf("✓ Created %d partitions automatically\n", ck.cluster.Config.PartitionCount)
				}
			}
		}()
	}

	fmt.Printf("ClusterKit started on %s\n", ck.httpAddr)
	return nil
}

// Stop gracefully shuts down ClusterKit
func (ck *ClusterKit) Stop() error {
	close(ck.stopChan)

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

// saveState persists the cluster state to disk
func (ck *ClusterKit) saveState() error {
	ck.mu.RLock()
	defer ck.mu.RUnlock()

	data, err := json.MarshalIndent(ck.cluster, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %v", err)
	}

	if err := os.WriteFile(ck.stateFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %v", err)
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

// syncLoop periodically syncs state with other nodes
func (ck *ClusterKit) syncLoop() {
	ticker := time.NewTicker(ck.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ck.syncWithNodes()
		case <-ck.stopChan:
			return
		}
	}
}

// syncWithNodes synchronizes state with all known nodes
func (ck *ClusterKit) syncWithNodes() {
	ck.mu.RLock()
	nodes := make([]Node, len(ck.cluster.Nodes))
	copy(nodes, ck.cluster.Nodes)
	ck.mu.RUnlock()

	for _, node := range nodes {
		if node.ID == ck.cluster.ID {
			continue // Skip self
		}

		if err := ck.syncWithNode(node); err != nil {
			fmt.Printf("Failed to sync with node %s: %v\n", node.Name, err)
		}
	}

	// Save state after sync
	if err := ck.saveState(); err != nil {
		fmt.Printf("Failed to save state: %v\n", err)
	}
}

// discoverNodes attempts to connect to known nodes
func (ck *ClusterKit) discoverNodes() {
	for _, nodeAddr := range ck.knownNodes {
		if err := ck.joinNode(nodeAddr); err != nil {
			fmt.Printf("Failed to join node %s: %v\n", nodeAddr, err)
		}
	}
}
