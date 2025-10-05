package clusterkit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/etcd"
)

// LeaderElection manages leader election for partitions
type LeaderElection struct {
	mu        sync.RWMutex
	store     *etcd.Store
	config    *Config
	logger    Logger
	elections map[int]*etcd.Election // partition -> election
	leaders   map[int]string         // partition -> leader node ID
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewLeaderElection creates a new leader election manager
func NewLeaderElection(store *etcd.Store, config *Config) *LeaderElection {
	return &LeaderElection{
		store:     store,
		config:    config,
		logger:    config.Logger,
		elections: make(map[int]*etcd.Election),
		leaders:   make(map[int]string),
		stopCh:    make(chan struct{}),
	}
}

// Start starts the leader election manager
func (le *LeaderElection) Start(ctx context.Context) error {
	le.wg.Add(1)
	go le.watchLeadershipChanges(ctx)
	
	le.logger.Infof("Leader election manager started")
	return nil
}

// Stop stops the leader election manager
func (le *LeaderElection) Stop() error {
	close(le.stopCh)
	le.wg.Wait()

	le.mu.Lock()
	defer le.mu.Unlock()

	// Close all elections
	for partition, election := range le.elections {
		if err := election.Close(); err != nil {
			le.logger.Errorf("Failed to close election for partition %d: %v", partition, err)
		}
	}

	le.logger.Infof("Leader election manager stopped")
	return nil
}

// CampaignForLeadership starts campaigning for leadership of a partition
func (le *LeaderElection) CampaignForLeadership(ctx context.Context, partition int) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	// Check if already campaigning
	if _, exists := le.elections[partition]; exists {
		return fmt.Errorf("already campaigning for partition %d", partition)
	}

	// Create election
	key := fmt.Sprintf("partition-%d", partition)
	value := le.config.NodeID
	
	election, err := le.store.NewElection(key, value, le.config.SessionTTL)
	if err != nil {
		return fmt.Errorf("failed to create election for partition %d: %w", partition, err)
	}

	le.elections[partition] = election

	// Start campaigning
	go func() {
		err := election.Campaign(ctx)
		if err != nil {
			le.logger.Errorf("Failed to campaign for partition %d: %v", partition, err)
			return
		}

		// Check if we became leader
		if election.IsLeader() {
			le.mu.Lock()
			le.leaders[partition] = le.config.NodeID
			le.mu.Unlock()
			
			le.logger.Infof("Became leader for partition %d", partition)
			
			// Notify via hook
			if le.config.OnLeaderElected != nil {
				le.config.OnLeaderElected(partition)
			}
		}
	}()

	return nil
}

// ResignLeadership resigns from leadership of a partition
func (le *LeaderElection) ResignLeadership(ctx context.Context, partition int) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	election, exists := le.elections[partition]
	if !exists {
		return fmt.Errorf("not campaigning for partition %d", partition)
	}

	// Resign from election
	err := election.Resign(ctx)
	if err != nil {
		return fmt.Errorf("failed to resign from partition %d: %w", partition, err)
	}

	// Clean up
	delete(le.elections, partition)
	delete(le.leaders, partition)

	le.logger.Infof("Resigned leadership for partition %d", partition)
	
	// Notify via hook
	if le.config.OnLeaderLost != nil {
		le.config.OnLeaderLost(partition)
	}

	return nil
}

// IsLeader checks if this node is the leader for a partition
func (le *LeaderElection) IsLeader(partition int) bool {
	le.mu.RLock()
	defer le.mu.RUnlock()

	leader, exists := le.leaders[partition]
	return exists && leader == le.config.NodeID
}

// GetLeader returns the current leader for a partition
func (le *LeaderElection) GetLeader(partition int) string {
	le.mu.RLock()
	defer le.mu.RUnlock()

	return le.leaders[partition]
}

// GetLeaderships returns all partitions this node is leading
func (le *LeaderElection) GetLeaderships() []int {
	le.mu.RLock()
	defer le.mu.RUnlock()

	var partitions []int
	for partition, leader := range le.leaders {
		if leader == le.config.NodeID {
			partitions = append(partitions, partition)
		}
	}

	return partitions
}

// watchLeadershipChanges watches for leadership changes across all partitions
func (le *LeaderElection) watchLeadershipChanges(ctx context.Context) {
	defer le.wg.Done()

	watchCh := le.store.Watch(ctx, "elections/")
	le.logger.Infof("Started watching leadership changes")

	for {
		select {
		case <-le.stopCh:
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				le.logger.Warnf("Leadership watch channel closed")
				return
			}

			for _, event := range watchResp.Events {
				le.handleLeadershipEvent(event)
			}
		}
	}
}

// handleLeadershipEvent handles a leadership change event
func (le *LeaderElection) handleLeadershipEvent(event *clientv3.Event) {
	key := string(event.Kv.Key)
	
	// Extract partition from key: /clusterkit/elections/partition-N
	var partition int
	_, err := fmt.Sscanf(key, le.store.Prefix+"/elections/partition-%d", &partition)
	if err != nil {
		le.logger.Errorf("Failed to parse leadership key %s: %v", key, err)
		return
	}

	le.mu.Lock()
	defer le.mu.Unlock()

	switch event.Type {
	case clientv3.EventTypePut:
		// New leader elected
		newLeader := string(event.Kv.Value)
		oldLeader := le.leaders[partition]
		
		le.leaders[partition] = newLeader
		
		if oldLeader != newLeader {
			le.logger.Infof("Leadership changed for partition %d: %s -> %s", partition, oldLeader, newLeader)
			
			// If we lost leadership
			if oldLeader == le.config.NodeID && newLeader != le.config.NodeID {
				if le.config.OnLeaderLost != nil {
					go le.config.OnLeaderLost(partition)
				}
			}
			
			// If we gained leadership
			if newLeader == le.config.NodeID && oldLeader != le.config.NodeID {
				if le.config.OnLeaderElected != nil {
					go le.config.OnLeaderElected(partition)
				}
			}
		}

	case clientv3.EventTypeDelete:
		// Leader stepped down
		oldLeader := le.leaders[partition]
		delete(le.leaders, partition)
		
		le.logger.Infof("Leadership lost for partition %d (was: %s)", partition, oldLeader)
		
		// If we lost leadership
		if oldLeader == le.config.NodeID {
			if le.config.OnLeaderLost != nil {
				go le.config.OnLeaderLost(partition)
			}
		}
	}
}

// ForceLeadershipCheck forces a check of current leadership status
func (le *LeaderElection) ForceLeadershipCheck(ctx context.Context) error {
	// Load current leadership state from etcd
	elections, err := le.store.List(ctx, "elections/")
	if err != nil {
		return fmt.Errorf("failed to list elections: %w", err)
	}

	le.mu.Lock()
	defer le.mu.Unlock()

	// Clear current state
	le.leaders = make(map[int]string)

	// Rebuild leadership map
	for key, value := range elections {
		var partition int
		_, err := fmt.Sscanf(key, "elections/partition-%d", &partition)
		if err != nil {
			continue
		}

		le.leaders[partition] = string(value)
	}

	le.logger.Infof("Refreshed leadership state: %d active leaders", len(le.leaders))
	return nil
}
