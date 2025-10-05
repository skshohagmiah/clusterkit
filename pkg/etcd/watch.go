package etcd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// WatchEventType represents the type of watch event
type WatchEventType int

const (
	WatchEventPut WatchEventType = iota
	WatchEventDelete
)

// WatchEvent represents a watch event
type WatchEvent struct {
	Type     WatchEventType
	Key      string
	Value    []byte
	PrevKv   *clientv3.KeyValue
	Revision int64
}

// WatchHandler is a function that handles watch events
type WatchHandler func(event WatchEvent)

// WatchRequest represents a watch request
type WatchRequest struct {
	Prefix   string
	Handler  WatchHandler
	Options  []clientv3.OpOption
	CancelFn context.CancelFunc
}

// WatchManager manages etcd watches
type WatchManager struct {
	mu       sync.RWMutex
	client   *clientv3.Client
	logger   Logger
	watches  map[string]*WatchRequest
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewWatchManager creates a new watch manager
func NewWatchManager(client *clientv3.Client, logger Logger) *WatchManager {
	return &WatchManager{
		client:  client,
		logger:  logger,
		watches: make(map[string]*WatchRequest),
		stopCh:  make(chan struct{}),
	}
}

// Start starts the watch manager
func (wm *WatchManager) Start() error {
	wm.logger.Infof("Watch manager started")
	return nil
}

// Stop stops the watch manager and cancels all watches
func (wm *WatchManager) Stop() error {
	close(wm.stopCh)
	wm.wg.Wait()

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Cancel all watches
	for watchID, watch := range wm.watches {
		if watch.CancelFn != nil {
			watch.CancelFn()
		}
		wm.logger.Debugf("Cancelled watch: %s", watchID)
	}

	wm.logger.Infof("Watch manager stopped")
	return nil
}

// WatchPrefix watches for changes to keys with the given prefix
func (wm *WatchManager) WatchPrefix(prefix string, handler WatchHandler, opts ...clientv3.OpOption) (string, error) {
	watchID := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Add prefix option
	options := append(opts, clientv3.WithPrefix())
	
	request := &WatchRequest{
		Prefix:   prefix,
		Handler:  handler,
		Options:  options,
		CancelFn: cancel,
	}

	wm.mu.Lock()
	wm.watches[watchID] = request
	wm.mu.Unlock()

	// Start watching
	wm.wg.Add(1)
	go wm.watchLoop(ctx, watchID, prefix, handler, options...)

	wm.logger.Debugf("Started watch for prefix: %s (ID: %s)", prefix, watchID)
	return watchID, nil
}

// WatchKey watches for changes to a specific key
func (wm *WatchManager) WatchKey(key string, handler WatchHandler, opts ...clientv3.OpOption) (string, error) {
	watchID := fmt.Sprintf("%s-%d", key, time.Now().UnixNano())
	
	ctx, cancel := context.WithCancel(context.Background())
	
	request := &WatchRequest{
		Prefix:   key,
		Handler:  handler,
		Options:  opts,
		CancelFn: cancel,
	}

	wm.mu.Lock()
	wm.watches[watchID] = request
	wm.mu.Unlock()

	// Start watching
	wm.wg.Add(1)
	go wm.watchLoop(ctx, watchID, key, handler, opts...)

	wm.logger.Debugf("Started watch for key: %s (ID: %s)", key, watchID)
	return watchID, nil
}

// WatchRange watches for changes to keys in a range
func (wm *WatchManager) WatchRange(start, end string, handler WatchHandler, opts ...clientv3.OpOption) (string, error) {
	watchID := fmt.Sprintf("%s-%s-%d", start, end, time.Now().UnixNano())
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Add range option
	options := append(opts, clientv3.WithRange(end))
	
	request := &WatchRequest{
		Prefix:   start,
		Handler:  handler,
		Options:  options,
		CancelFn: cancel,
	}

	wm.mu.Lock()
	wm.watches[watchID] = request
	wm.mu.Unlock()

	// Start watching
	wm.wg.Add(1)
	go wm.watchLoop(ctx, watchID, start, handler, options...)

	wm.logger.Debugf("Started watch for range: %s-%s (ID: %s)", start, end, watchID)
	return watchID, nil
}

// CancelWatch cancels a watch by ID
func (wm *WatchManager) CancelWatch(watchID string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	watch, exists := wm.watches[watchID]
	if !exists {
		return fmt.Errorf("watch %s not found", watchID)
	}

	if watch.CancelFn != nil {
		watch.CancelFn()
	}

	delete(wm.watches, watchID)
	wm.logger.Debugf("Cancelled watch: %s", watchID)
	return nil
}

// ListWatches returns all active watch IDs
func (wm *WatchManager) ListWatches() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	watches := make([]string, 0, len(wm.watches))
	for watchID := range wm.watches {
		watches = append(watches, watchID)
	}

	return watches
}

// GetWatchInfo returns information about a watch
func (wm *WatchManager) GetWatchInfo(watchID string) (*WatchRequest, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	watch, exists := wm.watches[watchID]
	if !exists {
		return nil, fmt.Errorf("watch %s not found", watchID)
	}

	// Return a copy to avoid race conditions
	return &WatchRequest{
		Prefix:  watch.Prefix,
		Handler: watch.Handler,
		Options: watch.Options,
	}, nil
}

// watchLoop runs the watch loop for a specific watch
func (wm *WatchManager) watchLoop(ctx context.Context, watchID, key string, handler WatchHandler, opts ...clientv3.OpOption) {
	defer wm.wg.Done()
	defer func() {
		wm.mu.Lock()
		delete(wm.watches, watchID)
		wm.mu.Unlock()
	}()

	watchCh := wm.client.Watch(ctx, key, opts...)
	wm.logger.Debugf("Watch loop started for: %s", key)

	for {
		select {
		case <-wm.stopCh:
			return
		case <-ctx.Done():
			wm.logger.Debugf("Watch cancelled: %s", watchID)
			return
		case watchResp, ok := <-watchCh:
			if !ok {
				wm.logger.Warnf("Watch channel closed: %s", watchID)
				return
			}

			if watchResp.Err() != nil {
				wm.logger.Errorf("Watch error for %s: %v", watchID, watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				wm.handleWatchEvent(event, handler)
			}
		}
	}
}

// handleWatchEvent handles a single watch event
func (wm *WatchManager) handleWatchEvent(event *clientv3.Event, handler WatchHandler) {
	defer func() {
		if r := recover(); r != nil {
			wm.logger.Errorf("Watch handler panicked: %v", r)
		}
	}()

	watchEvent := WatchEvent{
		Key:      string(event.Kv.Key),
		Value:    event.Kv.Value,
		PrevKv:   event.PrevKv,
		Revision: event.Kv.ModRevision,
	}

	switch event.Type {
	case clientv3.EventTypePut:
		watchEvent.Type = WatchEventPut
	case clientv3.EventTypeDelete:
		watchEvent.Type = WatchEventDelete
	}

	handler(watchEvent)
}

// WatchWithRecovery watches with automatic recovery on connection failures
func (wm *WatchManager) WatchWithRecovery(prefix string, handler WatchHandler, opts ...clientv3.OpOption) (string, error) {
	watchID := fmt.Sprintf("%s-recovery-%d", prefix, time.Now().UnixNano())
	
	ctx, cancel := context.WithCancel(context.Background())
	
	request := &WatchRequest{
		Prefix:   prefix,
		Handler:  handler,
		Options:  opts,
		CancelFn: cancel,
	}

	wm.mu.Lock()
	wm.watches[watchID] = request
	wm.mu.Unlock()

	// Start watching with recovery
	wm.wg.Add(1)
	go wm.watchWithRecoveryLoop(ctx, watchID, prefix, handler, opts...)

	wm.logger.Debugf("Started watch with recovery for prefix: %s (ID: %s)", prefix, watchID)
	return watchID, nil
}

// watchWithRecoveryLoop runs a watch loop with automatic recovery
func (wm *WatchManager) watchWithRecoveryLoop(ctx context.Context, watchID, key string, handler WatchHandler, opts ...clientv3.OpOption) {
	defer wm.wg.Done()
	defer func() {
		wm.mu.Lock()
		delete(wm.watches, watchID)
		wm.mu.Unlock()
	}()

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-wm.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Start watch
		watchCtx, watchCancel := context.WithCancel(ctx)
		watchCh := wm.client.Watch(watchCtx, key, opts...)

		// Process events
		watchFailed := false
		for !watchFailed {
			select {
			case <-wm.stopCh:
				watchCancel()
				return
			case <-ctx.Done():
				watchCancel()
				return
			case watchResp, ok := <-watchCh:
				if !ok {
					wm.logger.Warnf("Watch channel closed, will retry: %s", watchID)
					watchFailed = true
					break
				}

				if watchResp.Err() != nil {
					wm.logger.Errorf("Watch error for %s, will retry: %v", watchID, watchResp.Err())
					watchFailed = true
					break
				}

				// Reset backoff on successful event
				backoff = time.Second

				for _, event := range watchResp.Events {
					wm.handleWatchEvent(event, handler)
				}
			}
		}

		watchCancel()

		// Wait before retrying
		select {
		case <-wm.stopCh:
			return
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			// Exponential backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// GetStats returns watch manager statistics
func (wm *WatchManager) GetStats() WatchStats {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	prefixWatches := 0
	keyWatches := 0

	for _, watch := range wm.watches {
		// Simple heuristic: if prefix contains wildcards or common prefixes
		if strings.Contains(watch.Prefix, "/") || len(watch.Prefix) < 10 {
			prefixWatches++
		} else {
			keyWatches++
		}
	}

	return WatchStats{
		TotalWatches:  len(wm.watches),
		PrefixWatches: prefixWatches,
		KeyWatches:    keyWatches,
	}
}

// WatchStats represents watch manager statistics
type WatchStats struct {
	TotalWatches  int `json:"total_watches"`
	PrefixWatches int `json:"prefix_watches"`
	KeyWatches    int `json:"key_watches"`
}
