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

// EventType represents the type of cluster event
type EventType int

const (
	EventNodeJoined EventType = iota
	EventNodeLeft
	EventNodeUpdated
	EventPartitionAssigned
	EventPartitionUnassigned
	EventLeaderElected
	EventLeaderLost
)

// ClusterEvent represents a cluster topology change event
type ClusterEvent struct {
	Type      EventType
	NodeID    string
	Node      *Node
	Partition int
	Timestamp time.Time
}

// EventHandler is a function that handles cluster events
type EventHandler func(event ClusterEvent)

// Watcher manages etcd watches and dispatches cluster events
type Watcher struct {
	mu       sync.RWMutex
	store    *etcd.Store
	config   *Config
	logger   Logger
	handlers []EventHandler
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewWatcher creates a new cluster event watcher
func NewWatcher(store *etcd.Store, config *Config) *Watcher {
	return &Watcher{
		store:    store,
		config:   config,
		logger:   config.Logger,
		handlers: make([]EventHandler, 0),
		stopCh:   make(chan struct{}),
	}
}

// AddHandler adds an event handler
func (w *Watcher) AddHandler(handler EventHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers = append(w.handlers, handler)
}

// Start starts all watchers
func (w *Watcher) Start(ctx context.Context) error {
	w.wg.Add(3)
	go w.watchNodes(ctx)
	go w.watchPartitions(ctx)
	go w.watchLeadership(ctx)

	w.logger.Infof("Cluster watcher started")
	return nil
}

// Stop stops all watchers
func (w *Watcher) Stop() error {
	close(w.stopCh)
	w.wg.Wait()

	w.logger.Infof("Cluster watcher stopped")
	return nil
}

// watchNodes watches for node membership changes
func (w *Watcher) watchNodes(ctx context.Context) {
	defer w.wg.Done()

	watchCh := w.store.Watch(ctx, "nodes/")
	w.logger.Debugf("Started watching node membership")

	for {
		select {
		case <-w.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				w.logger.Warnf("Node watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				w.handleNodeEvent(event)
			}
		}
	}
}

// watchPartitions watches for partition assignment changes
func (w *Watcher) watchPartitions(ctx context.Context) {
	defer w.wg.Done()

	watchCh := w.store.Watch(ctx, "partitions/")
	w.logger.Debugf("Started watching partition assignments")

	for {
		select {
		case <-w.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				w.logger.Warnf("Partition watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				w.handlePartitionEvent(event)
			}
		}
	}
}

// watchLeadership watches for leadership changes
func (w *Watcher) watchLeadership(ctx context.Context) {
	defer w.wg.Done()

	watchCh := w.store.Watch(ctx, "elections/")
	w.logger.Debugf("Started watching leadership elections")

	for {
		select {
		case <-w.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				w.logger.Warnf("Leadership watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				w.handleLeadershipEvent(event)
			}
		}
	}
}

// handleNodeEvent handles node membership change events
func (w *Watcher) handleNodeEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	nodeID := strings.TrimPrefix(strings.TrimPrefix(key, w.store.Prefix), "/nodes/")

	switch event.Type {
	case clientv3.EventTypePut:
		var node Node
		err := json.Unmarshal(event.Kv.Value, &node)
		if err != nil {
			w.logger.Errorf("Failed to unmarshal node %s: %v", nodeID, err)
			return
		}

		// Determine if this is a new node or update
		eventType := EventNodeUpdated
		if event.Kv.CreateRevision == event.Kv.ModRevision {
			eventType = EventNodeJoined
		}

		clusterEvent := ClusterEvent{
			Type:      eventType,
			NodeID:    nodeID,
			Node:      &node,
			Timestamp: time.Now(),
		}

		w.dispatchEvent(clusterEvent)

		if eventType == EventNodeJoined {
			w.logger.Infof("Node joined: %s", nodeID)
		} else {
			w.logger.Debugf("Node updated: %s", nodeID)
		}

	case clientv3.EventTypeDelete:
		clusterEvent := ClusterEvent{
			Type:      EventNodeLeft,
			NodeID:    nodeID,
			Timestamp: time.Now(),
		}

		w.dispatchEvent(clusterEvent)
		w.logger.Infof("Node left: %s", nodeID)
	}
}

// handlePartitionEvent handles partition assignment change events
func (w *Watcher) handlePartitionEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	var partition int
	_, err := fmt.Sscanf(key, w.store.Prefix+"/partitions/%d", &partition)
	if err != nil {
		w.logger.Errorf("Failed to parse partition key %s: %v", key, err)
		return
	}

	switch event.Type {
	case clientv3.EventTypePut:
		var info PartitionInfo
		err := json.Unmarshal(event.Kv.Value, &info)
		if err != nil {
			w.logger.Errorf("Failed to unmarshal partition %d: %v", partition, err)
			return
		}

		// Check if local node is involved
		localNodeID := w.config.NodeID
		isAssigned := false
		
		for _, replica := range info.Replicas {
			if replica.ID == localNodeID {
				isAssigned = true
				break
			}
		}

		if isAssigned {
			clusterEvent := ClusterEvent{
				Type:      EventPartitionAssigned,
				NodeID:    localNodeID,
				Partition: partition,
				Timestamp: time.Now(),
			}
			w.dispatchEvent(clusterEvent)
		}

	case clientv3.EventTypeDelete:
		// Partition unassigned
		clusterEvent := ClusterEvent{
			Type:      EventPartitionUnassigned,
			NodeID:    w.config.NodeID,
			Partition: partition,
			Timestamp: time.Now(),
		}
		w.dispatchEvent(clusterEvent)
	}
}

// handleLeadershipEvent handles leadership change events
func (w *Watcher) handleLeadershipEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	var partition int
	_, err := fmt.Sscanf(key, w.store.Prefix+"/elections/partition-%d", &partition)
	if err != nil {
		w.logger.Errorf("Failed to parse leadership key %s: %v", key, err)
		return
	}

	localNodeID := w.config.NodeID

	switch event.Type {
	case clientv3.EventTypePut:
		leaderID := string(event.Kv.Value)
		
		if leaderID == localNodeID {
			clusterEvent := ClusterEvent{
				Type:      EventLeaderElected,
				NodeID:    localNodeID,
				Partition: partition,
				Timestamp: time.Now(),
			}
			w.dispatchEvent(clusterEvent)
		}

	case clientv3.EventTypeDelete:
		// Leadership lost - we don't know who the previous leader was from delete event
		// This will be handled by the leadership manager
		clusterEvent := ClusterEvent{
			Type:      EventLeaderLost,
			NodeID:    localNodeID,
			Partition: partition,
			Timestamp: time.Now(),
		}
		w.dispatchEvent(clusterEvent)
	}
}

// dispatchEvent sends an event to all registered handlers
func (w *Watcher) dispatchEvent(event ClusterEvent) {
	w.mu.RLock()
	handlers := make([]EventHandler, len(w.handlers))
	copy(handlers, w.handlers)
	w.mu.RUnlock()

	// Dispatch to all handlers asynchronously
	for _, handler := range handlers {
		go func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Errorf("Event handler panicked: %v", r)
				}
			}()
			h(event)
		}(handler)
	}
}

// GetEventTypeName returns a human-readable name for an event type
func GetEventTypeName(eventType EventType) string {
	switch eventType {
	case EventNodeJoined:
		return "NodeJoined"
	case EventNodeLeft:
		return "NodeLeft"
	case EventNodeUpdated:
		return "NodeUpdated"
	case EventPartitionAssigned:
		return "PartitionAssigned"
	case EventPartitionUnassigned:
		return "PartitionUnassigned"
	case EventLeaderElected:
		return "LeaderElected"
	case EventLeaderLost:
		return "LeaderLost"
	default:
		return "Unknown"
	}
}

// String returns a string representation of a cluster event
func (e ClusterEvent) String() string {
	return fmt.Sprintf("ClusterEvent{Type: %s, NodeID: %s, Partition: %d, Time: %s}",
		GetEventTypeName(e.Type), e.NodeID, e.Partition, e.Timestamp.Format(time.RFC3339))
}

// WatcherStats represents watcher statistics
type WatcherStats struct {
	HandlersCount int       `json:"handlers_count"`
	StartTime     time.Time `json:"start_time"`
	IsRunning     bool      `json:"is_running"`
}

// GetStats returns watcher statistics
func (w *Watcher) GetStats() WatcherStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WatcherStats{
		HandlersCount: len(w.handlers),
		IsRunning:     true, // Simplified - could track actual state
	}
}
