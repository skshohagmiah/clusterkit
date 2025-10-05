package etcd

import (
	"context"
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// LeaseManager manages etcd leases for the cluster
type LeaseManager struct {
	mu      sync.RWMutex
	client  *clientv3.Client
	logger  Logger
	leases  map[clientv3.LeaseID]*LeaseInfo
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// LeaseInfo contains information about a lease
type LeaseInfo struct {
	ID       clientv3.LeaseID
	TTL      time.Duration
	Keys     []string
	KeepCh   <-chan *clientv3.LeaseKeepAliveResponse
	CancelFn context.CancelFunc
}

// NewLeaseManager creates a new lease manager
func NewLeaseManager(client *clientv3.Client, logger Logger) *LeaseManager {
	return &LeaseManager{
		client: client,
		logger: logger,
		leases: make(map[clientv3.LeaseID]*LeaseInfo),
		stopCh: make(chan struct{}),
	}
}

// Start starts the lease manager
func (lm *LeaseManager) Start() error {
	lm.wg.Add(1)
	go lm.monitorLeases()
	
	lm.logger.Infof("Lease manager started")
	return nil
}

// Stop stops the lease manager and revokes all leases
func (lm *LeaseManager) Stop() error {
	close(lm.stopCh)
	lm.wg.Wait()

	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Revoke all leases
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for leaseID, info := range lm.leases {
		if info.CancelFn != nil {
			info.CancelFn()
		}
		
		_, err := lm.client.Revoke(ctx, leaseID)
		if err != nil {
			lm.logger.Errorf("Failed to revoke lease %d: %v", leaseID, err)
		}
	}

	lm.logger.Infof("Lease manager stopped")
	return nil
}

// CreateLease creates a new lease with the specified TTL
func (lm *LeaseManager) CreateLease(ctx context.Context, ttl time.Duration) (clientv3.LeaseID, error) {
	resp, err := lm.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("failed to create lease: %w", err)
	}

	leaseID := resp.ID
	lm.logger.Debugf("Created lease %d with TTL %v", leaseID, ttl)

	// Start keep-alive
	keepCtx, cancel := context.WithCancel(context.Background())
	keepCh, err := lm.client.KeepAlive(keepCtx, leaseID)
	if err != nil {
		cancel()
		return 0, fmt.Errorf("failed to start keep-alive for lease %d: %w", leaseID, err)
	}

	lm.mu.Lock()
	lm.leases[leaseID] = &LeaseInfo{
		ID:       leaseID,
		TTL:      ttl,
		Keys:     make([]string, 0),
		KeepCh:   keepCh,
		CancelFn: cancel,
	}
	lm.mu.Unlock()

	return leaseID, nil
}

// RevokeLease revokes a lease
func (lm *LeaseManager) RevokeLease(ctx context.Context, leaseID clientv3.LeaseID) error {
	lm.mu.Lock()
	info, exists := lm.leases[leaseID]
	if exists {
		delete(lm.leases, leaseID)
		if info.CancelFn != nil {
			info.CancelFn()
		}
	}
	lm.mu.Unlock()

	if !exists {
		return fmt.Errorf("lease %d not found", leaseID)
	}

	_, err := lm.client.Revoke(ctx, leaseID)
	if err != nil {
		return fmt.Errorf("failed to revoke lease %d: %w", leaseID, err)
	}

	lm.logger.Debugf("Revoked lease %d", leaseID)
	return nil
}

// AttachKey attaches a key to a lease
func (lm *LeaseManager) AttachKey(leaseID clientv3.LeaseID, key string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	info, exists := lm.leases[leaseID]
	if !exists {
		return fmt.Errorf("lease %d not found", leaseID)
	}

	info.Keys = append(info.Keys, key)
	lm.logger.Debugf("Attached key %s to lease %d", key, leaseID)
	return nil
}

// GetLeaseInfo returns information about a lease
func (lm *LeaseManager) GetLeaseInfo(leaseID clientv3.LeaseID) (*LeaseInfo, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	info, exists := lm.leases[leaseID]
	if !exists {
		return nil, fmt.Errorf("lease %d not found", leaseID)
	}

	// Return a copy to avoid race conditions
	infoCopy := *info
	infoCopy.Keys = make([]string, len(info.Keys))
	copy(infoCopy.Keys, info.Keys)

	return &infoCopy, nil
}

// ListLeases returns all active leases
func (lm *LeaseManager) ListLeases() []clientv3.LeaseID {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	leases := make([]clientv3.LeaseID, 0, len(lm.leases))
	for leaseID := range lm.leases {
		leases = append(leases, leaseID)
	}

	return leases
}

// RefreshLease refreshes a lease by sending a keep-alive request
func (lm *LeaseManager) RefreshLease(ctx context.Context, leaseID clientv3.LeaseID) error {
	lm.mu.RLock()
	_, exists := lm.leases[leaseID]
	lm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("lease %d not found", leaseID)
	}

	// Send a manual keep-alive
	_, err := lm.client.KeepAliveOnce(ctx, leaseID)
	if err != nil {
		return fmt.Errorf("failed to refresh lease %d: %w", leaseID, err)
	}

	lm.logger.Debugf("Refreshed lease %d", leaseID)
	return nil
}

// GetLeaseTTL gets the remaining TTL for a lease
func (lm *LeaseManager) GetLeaseTTL(ctx context.Context, leaseID clientv3.LeaseID) (time.Duration, error) {
	resp, err := lm.client.TimeToLive(ctx, leaseID)
	if err != nil {
		return 0, fmt.Errorf("failed to get TTL for lease %d: %w", leaseID, err)
	}

	if resp.TTL == -1 {
		return 0, fmt.Errorf("lease %d not found or expired", leaseID)
	}

	return time.Duration(resp.TTL) * time.Second, nil
}

// monitorLeases monitors lease keep-alive responses
func (lm *LeaseManager) monitorLeases() {
	defer lm.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lm.stopCh:
			return
		case <-ticker.C:
			lm.checkLeaseHealth()
		}
	}
}

// checkLeaseHealth checks the health of all leases
func (lm *LeaseManager) checkLeaseHealth() {
	lm.mu.RLock()
	leases := make([]clientv3.LeaseID, 0, len(lm.leases))
	for leaseID := range lm.leases {
		leases = append(leases, leaseID)
	}
	lm.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, leaseID := range leases {
		ttl, err := lm.GetLeaseTTL(ctx, leaseID)
		if err != nil {
			lm.logger.Warnf("Lease %d health check failed: %v", leaseID, err)
			lm.handleUnhealthyLease(leaseID)
		} else {
			lm.logger.Debugf("Lease %d is healthy (TTL: %v)", leaseID, ttl)
		}
	}
}

// handleUnhealthyLease handles an unhealthy lease
func (lm *LeaseManager) handleUnhealthyLease(leaseID clientv3.LeaseID) {
	lm.mu.Lock()
	info, exists := lm.leases[leaseID]
	if exists {
		delete(lm.leases, leaseID)
		if info.CancelFn != nil {
			info.CancelFn()
		}
	}
	lm.mu.Unlock()

	if exists {
		lm.logger.Warnf("Removed unhealthy lease %d (keys: %v)", leaseID, info.Keys)
	}
}

// GetStats returns lease manager statistics
func (lm *LeaseManager) GetStats() LeaseStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	totalKeys := 0
	for _, info := range lm.leases {
		totalKeys += len(info.Keys)
	}

	return LeaseStats{
		ActiveLeases: len(lm.leases),
		TotalKeys:    totalKeys,
	}
}

// LeaseStats represents lease manager statistics
type LeaseStats struct {
	ActiveLeases int `json:"active_leases"`
	TotalKeys    int `json:"total_keys"`
}
