package clusterkit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/etcd"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// MembershipManager manages cluster membership
type MembershipManager struct {
	mu       sync.RWMutex
	store    *etcd.Store
	session  *etcd.Session
	config   *Config
	logger   Logger
	nodes    map[string]*Node
	localNode *Node
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewMembershipManager creates a new membership manager
func NewMembershipManager(store *etcd.Store, config *Config) (*MembershipManager, error) {
	if config.Logger == nil {
		config.Logger = &DefaultLogger{}
	}

	localNode := &Node{
		ID:            config.NodeID,
		Addr:          config.AdvertiseAddr,
		HTTPEndpoint:  fmt.Sprintf("%s:%d", strings.Split(config.AdvertiseAddr, ":")[0], config.HTTPPort),
		GRPCEndpoint:  fmt.Sprintf("%s:%d", strings.Split(config.AdvertiseAddr, ":")[0], config.GRPCPort),
		Metadata:      config.Metadata,
		LastHeartbeat: time.Now(),
	}

	mm := &MembershipManager{
		store:     store,
		config:    config,
		logger:    config.Logger,
		nodes:     make(map[string]*Node),
		localNode: localNode,
		stopCh:    make(chan struct{}),
	}

	return mm, nil
}

// Start starts the membership manager
func (mm *MembershipManager) Start(ctx context.Context) error {
	// Create session for this node
	session, err := mm.store.NewSession(mm.config.SessionTTL)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	mm.session = session

	// Register this node
	err = mm.registerNode(ctx)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	// Load existing nodes
	err = mm.loadNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to load existing nodes: %w", err)
	}

	// Start watching for membership changes
	mm.wg.Add(2)
	go mm.watchMembership(ctx)
	go mm.heartbeatLoop(ctx)

	mm.logger.Infof("Membership manager started for node %s", mm.config.NodeID)
	return nil
}

// Stop stops the membership manager
func (mm *MembershipManager) Stop() error {
	close(mm.stopCh)
	mm.wg.Wait()

	// Unregister this node
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mm.unregisterNode(ctx)
	if err != nil {
		mm.logger.Errorf("Failed to unregister node: %v", err)
	}

	// Close session
	if mm.session != nil {
		err = mm.session.Close()
		if err != nil {
			mm.logger.Errorf("Failed to close session: %v", err)
		}
	}

	mm.logger.Infof("Membership manager stopped for node %s", mm.config.NodeID)
	return nil
}

// GetNodes returns all active nodes
func (mm *MembershipManager) GetNodes() []*Node {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	nodes := make([]*Node, 0, len(mm.nodes))
	for _, node := range mm.nodes {
		// Create a copy to avoid race conditions
		nodeCopy := *node
		nodes = append(nodes, &nodeCopy)
	}

	return nodes
}

// GetNode returns a specific node by ID
func (mm *MembershipManager) GetNode(nodeID string) *Node {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	node, exists := mm.nodes[nodeID]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	nodeCopy := *node
	return &nodeCopy
}

// GetLocalNode returns the local node
func (mm *MembershipManager) GetLocalNode() *Node {
	// Return a copy to avoid race conditions
	nodeCopy := *mm.localNode
	return &nodeCopy
}

// IsNodeAlive checks if a node is considered alive
func (mm *MembershipManager) IsNodeAlive(nodeID string) bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	node, exists := mm.nodes[nodeID]
	if !exists {
		return false
	}

	// Consider node alive if heartbeat is within 2x session TTL
	threshold := time.Now().Add(-2 * mm.config.SessionTTL)
	return node.LastHeartbeat.After(threshold)
}

// registerNode registers this node in etcd
func (mm *MembershipManager) registerNode(ctx context.Context) error {
	key := fmt.Sprintf("nodes/%s", mm.config.NodeID)
	
	// Use session lease for automatic cleanup
	_, err := mm.store.PutWithLease(ctx, key, mm.localNode, mm.config.SessionTTL)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	mm.logger.Infof("Registered node %s", mm.config.NodeID)
	return nil
}

// unregisterNode removes this node from etcd
func (mm *MembershipManager) unregisterNode(ctx context.Context) error {
	key := fmt.Sprintf("nodes/%s", mm.config.NodeID)
	err := mm.store.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to unregister node: %w", err)
	}

	mm.logger.Infof("Unregistered node %s", mm.config.NodeID)
	return nil
}

// loadNodes loads existing nodes from etcd
func (mm *MembershipManager) loadNodes(ctx context.Context) error {
	nodes, err := mm.store.List(ctx, "nodes/")
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	for key, data := range nodes {
		nodeID := strings.TrimPrefix(key, "nodes/")
		
		var node Node
		err = json.Unmarshal(data, &node)
		if err != nil {
			mm.logger.Errorf("Failed to unmarshal node %s: %v", nodeID, err)
			continue
		}

		mm.nodes[nodeID] = &node
		mm.logger.Debugf("Loaded node %s", nodeID)
	}

	mm.logger.Infof("Loaded %d existing nodes", len(mm.nodes))
	return nil
}

// watchMembership watches for membership changes
func (mm *MembershipManager) watchMembership(ctx context.Context) {
	defer mm.wg.Done()

	watchCh := mm.store.Watch(ctx, "nodes/")
	mm.logger.Infof("Started watching membership changes")

	for {
		select {
		case <-mm.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				mm.logger.Warnf("Membership watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				mm.handleMembershipEvent(event)
			}
		}
	}
}

// handleMembershipEvent handles a membership change event
func (mm *MembershipManager) handleMembershipEvent(event *clientv3.Event) {
	key := string(event.Key)
	nodeID := strings.TrimPrefix(strings.TrimPrefix(key, mm.store.Prefix), "/nodes/")

	switch event.Type {
	case clientv3.EventTypePut:
		var node Node
		err := json.Unmarshal(event.Kv.Value, &node)
		if err != nil {
			mm.logger.Errorf("Failed to unmarshal node %s: %v", nodeID, err)
			return
		}

		mm.mu.Lock()
		isNew := mm.nodes[nodeID] == nil
		mm.nodes[nodeID] = &node
		mm.mu.Unlock()

		if isNew {
			mm.logger.Infof("Node joined: %s", nodeID)
		} else {
			mm.logger.Debugf("Node updated: %s", nodeID)
		}

	case clientv3.EventTypeDelete:
		mm.mu.Lock()
		delete(mm.nodes, nodeID)
		mm.mu.Unlock()

		mm.logger.Infof("Node left: %s", nodeID)
	}
}

// heartbeatLoop sends periodic heartbeats
func (mm *MembershipManager) heartbeatLoop(ctx context.Context) {
	defer mm.wg.Done()

	ticker := time.NewTicker(mm.config.HeartbeatInterval)
	defer ticker.Stop()

	mm.logger.Infof("Started heartbeat loop with interval %v", mm.config.HeartbeatInterval)

	for {
		select {
		case <-mm.stopCh:
			return
		case <-ticker.C:
			mm.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat sends a heartbeat for this node
func (mm *MembershipManager) sendHeartbeat(ctx context.Context) {
	mm.localNode.LastHeartbeat = time.Now()
	
	key := fmt.Sprintf("nodes/%s", mm.config.NodeID)
	err := mm.store.Put(ctx, key, mm.localNode)
	if err != nil {
		mm.logger.Errorf("Failed to send heartbeat: %v", err)
		return
	}

	mm.logger.Debugf("Sent heartbeat for node %s", mm.config.NodeID)
}
