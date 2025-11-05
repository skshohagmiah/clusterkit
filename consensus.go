package clusterkit

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// ConsensusManager handles leader election and consensus using Raft
type ConsensusManager struct {
	ck          *ClusterKit
	raft        *raft.Raft
	fsm         *clusterFSM
	mu          sync.RWMutex
	raftDir     string
	raftBind    string
	isBootstrap bool
}

// LeaderInfo contains information about the current leader
type LeaderInfo struct {
	LeaderID   string    `json:"leader_id"`
	LeaderName string    `json:"leader_name"`
	LeaderIP   string    `json:"leader_ip"`
	Term       uint64    `json:"term"`
	ElectedAt  time.Time `json:"elected_at"`
}

// clusterFSM implements the Raft FSM (Finite State Machine)
type clusterFSM struct {
	ck *ClusterKit
	mu sync.RWMutex
}

// Apply applies a Raft log entry to the FSM
func (f *clusterFSM) Apply(log *raft.Log) interface{} {
	var cmd map[string]interface{}
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return err
	}

	operation, ok := cmd["operation"].(string)
	if !ok {
		return fmt.Errorf("invalid operation")
	}

	fmt.Printf("Applying Raft command: %s\n", operation)

	f.mu.Lock()
	defer f.mu.Unlock()

	// Handle different operations
	switch operation {
	case "add_node":
		return f.applyAddNode(cmd["data"])
	case "remove_node":
		return f.applyRemoveNode(cmd["data"])
	case "create_partitions":
		return f.applyCreatePartitions(cmd["data"])
	case "rebalance_partitions":
		return f.applyRebalancePartitions(cmd["data"])
	case "update_partition":
		return f.applyUpdatePartition(cmd["data"])
	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}
}

// applyAddNode adds a node to the cluster
func (f *clusterFSM) applyAddNode(data interface{}) error {
	nodeData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid node data")
	}

	node := Node{
		ID:     nodeData["id"].(string),
		Name:   nodeData["name"].(string),
		IP:     nodeData["ip"].(string),
		Status: nodeData["status"].(string),
	}

	// Check if node already exists
	for i, existing := range f.ck.cluster.Nodes {
		if existing.ID == node.ID {
			// Update existing node
			f.ck.cluster.Nodes[i] = node
			fmt.Printf("✓ Updated node: %s\n", node.ID)
			return nil
		}
	}

	// Add new node
	f.ck.cluster.Nodes = append(f.ck.cluster.Nodes, node)
	fmt.Printf("✓ Added node: %s\n", node.ID)
	return nil
}

// applyRemoveNode removes a node from the cluster
func (f *clusterFSM) applyRemoveNode(data interface{}) error {
	nodeID, ok := data.(string)
	if !ok {
		return fmt.Errorf("invalid node ID")
	}

	for i, node := range f.ck.cluster.Nodes {
		if node.ID == nodeID {
			f.ck.cluster.Nodes = append(f.ck.cluster.Nodes[:i], f.ck.cluster.Nodes[i+1:]...)
			fmt.Printf("✓ Removed node: %s\n", nodeID)
			return nil
		}
	}

	return fmt.Errorf("node not found: %s", nodeID)
}

// applyCreatePartitions creates partitions in the cluster
func (f *clusterFSM) applyCreatePartitions(data interface{}) error {
	partitionData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid partition data")
	}

	if f.ck.cluster.PartitionMap == nil {
		f.ck.cluster.PartitionMap = &PartitionMap{
			Partitions: make(map[string]*Partition),
		}
	}

	// Extract partitions from data
	partitionsRaw, ok := partitionData["partitions"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid partitions map")
	}

	for partID, partRaw := range partitionsRaw {
		partMap := partRaw.(map[string]interface{})
		
		partition := &Partition{
			ID:          partID,
			PrimaryNode: partMap["primary_node"].(string),
		}

		// Extract replica nodes
		if replicas, ok := partMap["replica_nodes"].([]interface{}); ok {
			partition.ReplicaNodes = make([]string, len(replicas))
			for i, r := range replicas {
				partition.ReplicaNodes[i] = r.(string)
			}
		}

		f.ck.cluster.PartitionMap.Partitions[partID] = partition
	}

	fmt.Printf("✓ Created %d partitions\n", len(f.ck.cluster.PartitionMap.Partitions))
	return nil
}

// applyRebalancePartitions rebalances partitions across nodes
func (f *clusterFSM) applyRebalancePartitions(data interface{}) error {
	partitionData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid partition data")
	}

	if f.ck.cluster.PartitionMap == nil {
		return fmt.Errorf("no partition map exists")
	}

	// Update partition assignments
	partitionsRaw, ok := partitionData["partitions"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid partitions map")
	}

	for partID, partRaw := range partitionsRaw {
		partMap := partRaw.(map[string]interface{})
		
		if partition, exists := f.ck.cluster.PartitionMap.Partitions[partID]; exists {
			partition.PrimaryNode = partMap["primary_node"].(string)
			
			if replicas, ok := partMap["replica_nodes"].([]interface{}); ok {
				partition.ReplicaNodes = make([]string, len(replicas))
				for i, r := range replicas {
					partition.ReplicaNodes[i] = r.(string)
				}
			}
		}
	}

	fmt.Printf("✓ Rebalanced partitions\n")
	return nil
}

// applyUpdatePartition updates a specific partition
func (f *clusterFSM) applyUpdatePartition(data interface{}) error {
	partitionData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid partition data")
	}

	partID := partitionData["id"].(string)
	
	if f.ck.cluster.PartitionMap == nil {
		return fmt.Errorf("no partition map exists")
	}

	partition, exists := f.ck.cluster.PartitionMap.Partitions[partID]
	if !exists {
		return fmt.Errorf("partition not found: %s", partID)
	}

	if primary, ok := partitionData["primary_node"].(string); ok {
		partition.PrimaryNode = primary
	}

	if replicas, ok := partitionData["replica_nodes"].([]interface{}); ok {
		partition.ReplicaNodes = make([]string, len(replicas))
		for i, r := range replicas {
			partition.ReplicaNodes[i] = r.(string)
		}
	}

	fmt.Printf("✓ Updated partition: %s\n", partID)
	return nil
}

// Snapshot returns a snapshot of the FSM state
func (f *clusterFSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Create snapshot of cluster state
	return &clusterSnapshot{
		cluster: f.ck.GetCluster(),
	}, nil
}

// Restore restores the FSM state from a snapshot
func (f *clusterFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var cluster Cluster
	if err := json.NewDecoder(rc).Decode(&cluster); err != nil {
		return err
	}

	f.mu.Lock()
	f.ck.cluster = &cluster
	f.mu.Unlock()

	return nil
}

// clusterSnapshot implements raft.FSMSnapshot
type clusterSnapshot struct {
	cluster *Cluster
}

// Persist writes the snapshot to the given sink
func (s *clusterSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		b, err := json.Marshal(s.cluster)
		if err != nil {
			return err
		}

		if _, err := sink.Write(b); err != nil {
			return err
		}

		return sink.Close()
	}()

	if err != nil {
		sink.Cancel()
	}

	return err
}

// Release is called when we're finished with the snapshot
func (s *clusterSnapshot) Release() {}

// NewConsensusManager creates a new consensus manager with Raft
func NewConsensusManager(ck *ClusterKit, bootstrap bool, raftAddr string) *ConsensusManager {
	// Raft directory for logs and snapshots
	raftDir := filepath.Join(ck.stateFile, "..", "raft")
	
	// Use provided Raft address or default
	if raftAddr == "" {
		raftAddr = "127.0.0.1:9001"
	}
	
	return &ConsensusManager{
		ck:          ck,
		raftDir:     raftDir,
		raftBind:    raftAddr,
		isBootstrap: bootstrap,
		fsm:         &clusterFSM{ck: ck},
	}
}

// Start begins the consensus manager with Raft
func (cm *ConsensusManager) Start() error {
	// Create Raft directory
	if err := os.MkdirAll(cm.raftDir, 0755); err != nil {
		return fmt.Errorf("failed to create raft dir: %v", err)
	}

	// Setup Raft configuration
	config := raft.DefaultConfig()
	// Use the node ID, not the cluster ID
	var nodeID string
	if len(cm.ck.cluster.Nodes) > 0 {
		nodeID = cm.ck.cluster.Nodes[0].ID
	} else {
		nodeID = cm.ck.cluster.ID // Fallback
	}
	config.LocalID = raft.ServerID(nodeID)

	// Setup Raft communication
	addr, err := net.ResolveTCPAddr("tcp", cm.raftBind)
	if err != nil {
		return fmt.Errorf("failed to resolve raft bind address: %v", err)
	}

	transport, err := raft.NewTCPTransport(cm.raftBind, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create raft transport: %v", err)
	}

	// Create the snapshot store
	snapshots, err := raft.NewFileSnapshotStore(cm.raftDir, 2, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create snapshot store: %v", err)
	}

	// Create the log store and stable store
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(cm.raftDir, "raft.db"))
	if err != nil {
		return fmt.Errorf("failed to create bolt store: %v", err)
	}

	// Instantiate the Raft system
	ra, err := raft.NewRaft(config, cm.fsm, logStore, logStore, snapshots, transport)
	if err != nil {
		return fmt.Errorf("failed to create raft: %v", err)
	}

	cm.raft = ra

	// Bootstrap cluster if this is the first node
	if cm.isBootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		cm.raft.BootstrapCluster(configuration)
		fmt.Println("✓ Bootstrapped Raft cluster")
	}

	fmt.Printf("✓ Raft consensus started on %s\n", cm.raftBind)
	return nil
}

// Stop stops the consensus manager
func (cm *ConsensusManager) Stop() error {
	if cm.raft != nil {
		return cm.raft.Shutdown().Error()
	}
	return nil
}

// GetLeader returns the current leader information
func (cm *ConsensusManager) GetLeader() (*LeaderInfo, error) {
	if cm.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	leaderAddr, leaderID := cm.raft.LeaderWithID()
	if leaderID == "" {
		return nil, fmt.Errorf("no leader elected")
	}

	// Try to find the node in cluster
	node, err := cm.ck.cluster.GetNodeByID(string(leaderID))
	if err != nil {
		// Return basic info if node not found in cluster
		return &LeaderInfo{
			LeaderID: string(leaderID),
			LeaderIP: string(leaderAddr),
			Term:     parseUint64(cm.raft.Stats()["term"]),
		}, nil
	}

	return &LeaderInfo{
		LeaderID:   node.ID,
		LeaderName: node.Name,
		LeaderIP:   node.IP,
		Term:       parseUint64(cm.raft.Stats()["term"]),
	}, nil
}

// IsLeader returns whether this node is the current leader
func (cm *ConsensusManager) IsLeader() bool {
	if cm.raft == nil {
		return false
	}
	return cm.raft.State() == raft.Leader
}

// GetCurrentTerm returns the current election term
func (cm *ConsensusManager) GetCurrentTerm() uint64 {
	if cm.raft == nil {
		return 0
	}
	return parseUint64(cm.raft.Stats()["term"])
}

// WaitForLeader waits until a leader is elected or timeout
func (cm *ConsensusManager) WaitForLeader(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		_, leaderID := cm.raft.LeaderWithID()
		if leaderID != "" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for leader election")
}

// ProposeAction proposes an action through Raft consensus
func (cm *ConsensusManager) ProposeAction(action string, data interface{}) error {
	if !cm.IsLeader() {
		return fmt.Errorf("only leader can propose actions")
	}

	cmd := map[string]interface{}{
		"operation": action,
		"data":      data,
		"timestamp": time.Now(),
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	// Apply through Raft
	future := cm.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return err
	}

	fmt.Printf("✓ Raft consensus achieved for action: %s\n", action)
	return nil
}

// AddVoter adds a new voting member to the Raft cluster
func (cm *ConsensusManager) AddVoter(nodeID, address string) error {
	if !cm.IsLeader() {
		return fmt.Errorf("only leader can add voters")
	}

	future := cm.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(address), 0, 0)
	return future.Error()
}

// RemoveServer removes a server from the Raft cluster
func (cm *ConsensusManager) RemoveServer(nodeID string) error {
	if !cm.IsLeader() {
		return fmt.Errorf("only leader can remove servers")
	}

	future := cm.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	return future.Error()
}

// ConsensusStats returns consensus statistics
type ConsensusStats struct {
	State         string `json:"state"`
	Term          uint64 `json:"term"`
	LastLogIndex  uint64 `json:"last_log_index"`
	LastLogTerm   uint64 `json:"last_log_term"`
	CommitIndex   uint64 `json:"commit_index"`
	AppliedIndex  uint64 `json:"applied_index"`
	NumPeers      int    `json:"num_peers"`
	Leader        string `json:"leader"`
}

// GetStats returns consensus statistics from Raft
func (cm *ConsensusManager) GetStats() *ConsensusStats {
	if cm.raft == nil {
		return &ConsensusStats{State: "not_initialized"}
	}

	stats := cm.raft.Stats()
	_, leaderID := cm.raft.LeaderWithID()

	return &ConsensusStats{
		State:         cm.raft.State().String(),
		Term:          parseUint64(stats["term"]),
		LastLogIndex:  parseUint64(stats["last_log_index"]),
		LastLogTerm:   parseUint64(stats["last_log_term"]),
		CommitIndex:   parseUint64(stats["commit_index"]),
		AppliedIndex:  parseUint64(stats["applied_index"]),
		NumPeers:      parseInt(stats["num_peers"]),
		Leader:        string(leaderID),
	}
}

// Helper functions to parse stats
func parseUint64(v interface{}) uint64 {
	if val, ok := v.(uint64); ok {
		return val
	}
	return 0
}

func parseInt(v interface{}) int {
	if val, ok := v.(int); ok {
		return val
	}
	return 0
}
