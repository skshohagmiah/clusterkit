package hash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// Ring represents a consistent hash ring
type Ring struct {
	mu       sync.RWMutex
	nodes    map[string]bool
	ring     map[uint64]string
	sortedHashes []uint64
	replicas int // virtual nodes per physical node
}

// NewRing creates a new consistent hash ring
func NewRing(replicas int) *Ring {
	return &Ring{
		nodes:    make(map[string]bool),
		ring:     make(map[uint64]string),
		replicas: replicas,
	}
}

// Add adds a node to the ring
func (r *Ring) Add(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if r.nodes[node] {
		return // already exists
	}
	
	r.nodes[node] = true
	
	// Add virtual nodes
	for i := 0; i < r.replicas; i++ {
		hash := r.hash(fmt.Sprintf("%s:%d", node, i))
		r.ring[hash] = node
		r.sortedHashes = append(r.sortedHashes, hash)
	}
	
	sort.Slice(r.sortedHashes, func(i, j int) bool {
		return r.sortedHashes[i] < r.sortedHashes[j]
	})
}

// Remove removes a node from the ring
func (r *Ring) Remove(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if !r.nodes[node] {
		return // doesn't exist
	}
	
	delete(r.nodes, node)
	
	// Remove virtual nodes
	for i := 0; i < r.replicas; i++ {
		hash := r.hash(fmt.Sprintf("%s:%d", node, i))
		delete(r.ring, hash)
		
		// Remove from sorted hashes
		for j, h := range r.sortedHashes {
			if h == hash {
				r.sortedHashes = append(r.sortedHashes[:j], r.sortedHashes[j+1:]...)
				break
			}
		}
	}
}

// Get returns the node responsible for the given key
func (r *Ring) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if len(r.ring) == 0 {
		return ""
	}
	
	hash := r.hash(key)
	
	// Find the first node with hash >= key hash
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= hash
	})
	
	// Wrap around if necessary
	if idx == len(r.sortedHashes) {
		idx = 0
	}
	
	return r.ring[r.sortedHashes[idx]]
}

// GetN returns the N nodes responsible for the given key
func (r *Ring) GetN(key string, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if len(r.ring) == 0 || n <= 0 {
		return nil
	}
	
	hash := r.hash(key)
	
	// Find the first node with hash >= key hash
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= hash
	})
	
	// Wrap around if necessary
	if idx == len(r.sortedHashes) {
		idx = 0
	}
	
	seen := make(map[string]bool)
	result := make([]string, 0, n)
	
	for len(result) < n && len(seen) < len(r.nodes) {
		node := r.ring[r.sortedHashes[idx]]
		if !seen[node] {
			seen[node] = true
			result = append(result, node)
		}
		
		idx = (idx + 1) % len(r.sortedHashes)
	}
	
	return result
}

// Nodes returns all nodes in the ring
func (r *Ring) Nodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	nodes := make([]string, 0, len(r.nodes))
	for node := range r.nodes {
		nodes = append(nodes, node)
	}
	
	return nodes
}

// Size returns the number of nodes in the ring
func (r *Ring) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// hash computes SHA-256 hash and returns first 8 bytes as uint64
func (r *Ring) hash(key string) uint64 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(h[:8])
}

// PartitionRing manages partition assignments using consistent hashing
type PartitionRing struct {
	mu         sync.RWMutex
	partitions int
	replicas   int
	ring       *Ring
	assignments map[int][]string // partition -> nodes
}

// NewPartitionRing creates a new partition ring
func NewPartitionRing(partitions, replicas int) *PartitionRing {
	return &PartitionRing{
		partitions:  partitions,
		replicas:    replicas,
		ring:        NewRing(150), // 150 virtual nodes per physical node
		assignments: make(map[int][]string),
	}
}

// AddNode adds a node to the partition ring
func (pr *PartitionRing) AddNode(nodeID string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pr.ring.Add(nodeID)
	pr.rebalance()
}

// RemoveNode removes a node from the partition ring
func (pr *PartitionRing) RemoveNode(nodeID string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pr.ring.Remove(nodeID)
	pr.rebalance()
}

// GetPartition returns the partition ID for a given key
func (pr *PartitionRing) GetPartition(key string) int {
	hash := pr.ring.hash(key)
	return int(hash % uint64(pr.partitions))
}

// GetNodes returns the nodes assigned to a partition
func (pr *PartitionRing) GetNodes(partition int) []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	
	nodes, exists := pr.assignments[partition]
	if !exists {
		return nil
	}
	
	// Return a copy to avoid race conditions
	result := make([]string, len(nodes))
	copy(result, nodes)
	return result
}

// GetLeader returns the leader node for a partition (first node in assignment)
func (pr *PartitionRing) GetLeader(partition int) string {
	nodes := pr.GetNodes(partition)
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0]
}

// GetAssignments returns all partition assignments
func (pr *PartitionRing) GetAssignments() map[int][]string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	
	result := make(map[int][]string)
	for partition, nodes := range pr.assignments {
		result[partition] = make([]string, len(nodes))
		copy(result[partition], nodes)
	}
	
	return result
}

// rebalance recalculates partition assignments
func (pr *PartitionRing) rebalance() {
	nodes := pr.ring.Nodes()
	if len(nodes) == 0 {
		pr.assignments = make(map[int][]string)
		return
	}
	
	newAssignments := make(map[int][]string)
	
	for partition := 0; partition < pr.partitions; partition++ {
		partitionKey := fmt.Sprintf("partition:%d", partition)
		assignedNodes := pr.ring.GetN(partitionKey, pr.replicas)
		
		// Ensure we don't assign more replicas than available nodes
		if len(assignedNodes) > len(nodes) {
			assignedNodes = nodes
		}
		
		newAssignments[partition] = assignedNodes
	}
	
	pr.assignments = newAssignments
}
