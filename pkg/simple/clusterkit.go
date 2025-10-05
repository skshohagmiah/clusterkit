package simple

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// SimpleClusterKit is a minimal working implementation for demonstration
type SimpleClusterKit struct {
	mu            sync.RWMutex
	nodeID        string
	nodes         map[string]*Node
	partitions    int
	replicaFactor int
	ring          *ConsistentHashRing
}

// Node represents a cluster node
type Node struct {
	ID       string            `json:"id"`
	Addr     string            `json:"addr"`
	Metadata map[string]string `json:"metadata"`
}

// Config represents ClusterKit configuration
type Config struct {
	NodeID        string
	AdvertiseAddr string
	Partitions    int
	ReplicaFactor int
}

// ConsistentHashRing implements consistent hashing
type ConsistentHashRing struct {
	nodes    map[uint32]string
	sortedKeys []uint32
	replicas int
}

// NewSimpleClusterKit creates a new simple ClusterKit instance
func NewSimpleClusterKit(config *Config) *SimpleClusterKit {
	if config.Partitions == 0 {
		config.Partitions = 16
	}
	if config.ReplicaFactor == 0 {
		config.ReplicaFactor = 2
	}

	ck := &SimpleClusterKit{
		nodeID:        config.NodeID,
		nodes:         make(map[string]*Node),
		partitions:    config.Partitions,
		replicaFactor: config.ReplicaFactor,
		ring:          NewConsistentHashRing(100), // 100 virtual nodes per physical node
	}

	// Add self to cluster
	ck.nodes[config.NodeID] = &Node{
		ID:       config.NodeID,
		Addr:     config.AdvertiseAddr,
		Metadata: make(map[string]string),
	}
	ck.ring.AddNode(config.NodeID)

	return ck
}

// NewConsistentHashRing creates a new consistent hash ring
func NewConsistentHashRing(replicas int) *ConsistentHashRing {
	return &ConsistentHashRing{
		nodes:    make(map[uint32]string),
		replicas: replicas,
	}
}

// AddNode adds a node to the hash ring
func (r *ConsistentHashRing) AddNode(nodeID string) {
	for i := 0; i < r.replicas; i++ {
		key := r.hash(fmt.Sprintf("%s:%d", nodeID, i))
		r.nodes[key] = nodeID
		r.sortedKeys = append(r.sortedKeys, key)
	}
	sort.Slice(r.sortedKeys, func(i, j int) bool {
		return r.sortedKeys[i] < r.sortedKeys[j]
	})
}

// RemoveNode removes a node from the hash ring
func (r *ConsistentHashRing) RemoveNode(nodeID string) {
	for i := 0; i < r.replicas; i++ {
		key := r.hash(fmt.Sprintf("%s:%d", nodeID, i))
		delete(r.nodes, key)
	}
	
	// Rebuild sorted keys
	r.sortedKeys = r.sortedKeys[:0]
	for key := range r.nodes {
		r.sortedKeys = append(r.sortedKeys, key)
	}
	sort.Slice(r.sortedKeys, func(i, j int) bool {
		return r.sortedKeys[i] < r.sortedKeys[j]
	})
}

// GetNode returns the node responsible for a key
func (r *ConsistentHashRing) GetNode(key string) string {
	if len(r.sortedKeys) == 0 {
		return ""
	}

	hash := r.hash(key)
	
	// Binary search for the first node >= hash
	idx := sort.Search(len(r.sortedKeys), func(i int) bool {
		return r.sortedKeys[i] >= hash
	})
	
	// Wrap around if necessary
	if idx == len(r.sortedKeys) {
		idx = 0
	}
	
	return r.nodes[r.sortedKeys[idx]]
}

// GetNodes returns multiple nodes for replication
func (r *ConsistentHashRing) GetNodes(key string, count int) []string {
	if len(r.sortedKeys) == 0 {
		return nil
	}

	if count > len(r.nodes)/r.replicas {
		count = len(r.nodes) / r.replicas
	}

	hash := r.hash(key)
	result := make([]string, 0, count)
	seen := make(map[string]bool)
	
	// Find starting position
	idx := sort.Search(len(r.sortedKeys), func(i int) bool {
		return r.sortedKeys[i] >= hash
	})
	
	// Collect unique nodes
	for len(result) < count && len(seen) < len(r.nodes)/r.replicas {
		if idx >= len(r.sortedKeys) {
			idx = 0
		}
		
		nodeID := r.nodes[r.sortedKeys[idx]]
		if !seen[nodeID] {
			result = append(result, nodeID)
			seen[nodeID] = true
		}
		idx++
	}
	
	return result
}

// hash creates a hash value for a string
func (r *ConsistentHashRing) hash(key string) uint32 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint32(h[:4])
}

// GetPartition returns the partition ID for a key
func (ck *SimpleClusterKit) GetPartition(key string) int {
	hash := sha256.Sum256([]byte(key))
	partition := int(binary.BigEndian.Uint32(hash[:4])) % ck.partitions
	if partition < 0 {
		partition = -partition
	}
	return partition
}

// GetLeader returns the leader node for a partition
func (ck *SimpleClusterKit) GetLeader(partition int) *Node {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	
	// Use partition as key to find leader
	partitionKey := fmt.Sprintf("partition:%d", partition)
	leaderID := ck.ring.GetNode(partitionKey)
	
	return ck.nodes[leaderID]
}

// GetReplicas returns all replica nodes for a partition
func (ck *SimpleClusterKit) GetReplicas(partition int) []*Node {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	
	partitionKey := fmt.Sprintf("partition:%d", partition)
	nodeIDs := ck.ring.GetNodes(partitionKey, ck.replicaFactor)
	
	var replicas []*Node
	for _, nodeID := range nodeIDs {
		if node, exists := ck.nodes[nodeID]; exists {
			replicas = append(replicas, node)
		}
	}
	
	return replicas
}

// IsLeader checks if this node is the leader for a partition
func (ck *SimpleClusterKit) IsLeader(partition int) bool {
	leader := ck.GetLeader(partition)
	return leader != nil && leader.ID == ck.nodeID
}

// GetNodes returns all nodes in the cluster
func (ck *SimpleClusterKit) GetNodes() []*Node {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	
	var nodes []*Node
	for _, node := range ck.nodes {
		nodes = append(nodes, node)
	}
	
	return nodes
}

// AddNode adds a node to the cluster (simulated)
func (ck *SimpleClusterKit) AddNode(nodeID, addr string) {
	ck.mu.Lock()
	defer ck.mu.Unlock()
	
	ck.nodes[nodeID] = &Node{
		ID:       nodeID,
		Addr:     addr,
		Metadata: make(map[string]string),
	}
	ck.ring.AddNode(nodeID)
}

// RemoveNode removes a node from the cluster (simulated)
func (ck *SimpleClusterKit) RemoveNode(nodeID string) {
	ck.mu.Lock()
	defer ck.mu.Unlock()
	
	delete(ck.nodes, nodeID)
	ck.ring.RemoveNode(nodeID)
}

// GetStats returns cluster statistics
func (ck *SimpleClusterKit) GetStats() map[string]interface{} {
	ck.mu.RLock()
	defer ck.mu.RUnlock()
	
	localPartitions := 0
	leaderPartitions := 0
	
	for i := 0; i < ck.partitions; i++ {
		replicas := ck.GetReplicas(i)
		
		// Check if this node is a replica
		for _, replica := range replicas {
			if replica.ID == ck.nodeID {
				localPartitions++
				break
			}
		}
		
		// Check if this node is the leader
		if ck.IsLeader(i) {
			leaderPartitions++
		}
	}
	
	return map[string]interface{}{
		"node_count":        len(ck.nodes),
		"partition_count":   ck.partitions,
		"replica_factor":    ck.replicaFactor,
		"local_partitions":  localPartitions,
		"leader_partitions": leaderPartitions,
	}
}

// NodeID returns this node's ID
func (ck *SimpleClusterKit) NodeID() string {
	return ck.nodeID
}
