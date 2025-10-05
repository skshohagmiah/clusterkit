package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Store wraps etcd client for ClusterKit operations
type Store struct {
	client *clientv3.Client
	Prefix string
	logger Logger
}

// Logger interface for etcd store
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NewStore creates a new etcd store
func NewStore(endpoints []string, prefix string, logger Logger) (*Store, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &Store{
		client: client,
		Prefix: strings.TrimSuffix(prefix, "/"),
		logger: logger,
	}, nil
}

// Close closes the etcd client
func (s *Store) Close() error {
	return s.client.Close()
}

// Put stores a key-value pair
func (s *Store) Put(ctx context.Context, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	fullKey := s.Prefix + "/" + key
	_, err = s.client.Put(ctx, fullKey, string(data))
	if err != nil {
		return fmt.Errorf("failed to put key %s: %w", fullKey, err)
	}

	s.logger.Debugf("Put key: %s", fullKey)
	return nil
}

// Get retrieves a value by key
func (s *Store) Get(ctx context.Context, key string, value interface{}) error {
	fullKey := s.Prefix + "/" + key
	resp, err := s.client.Get(ctx, fullKey)
	if err != nil {
		return fmt.Errorf("failed to get key %s: %w", fullKey, err)
	}

	if len(resp.Kvs) == 0 {
		return fmt.Errorf("key not found: %s", fullKey)
	}

	err = json.Unmarshal(resp.Kvs[0].Value, value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal value for key %s: %w", fullKey, err)
	}

	s.logger.Debugf("Get key: %s", fullKey)
	return nil
}

// Delete removes a key
func (s *Store) Delete(ctx context.Context, key string) error {
	fullKey := s.Prefix + "/" + key
	_, err := s.client.Delete(ctx, fullKey)
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", fullKey, err)
	}

	s.logger.Debugf("Delete key: %s", fullKey)
	return nil
}

// List retrieves all keys with a given prefix
func (s *Store) List(ctx context.Context, keyPrefix string) (map[string][]byte, error) {
	fullPrefix := s.Prefix + "/" + keyPrefix
	resp, err := s.client.Get(ctx, fullPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list keys with prefix %s: %w", fullPrefix, err)
	}

	result := make(map[string][]byte)
	for _, kv := range resp.Kvs {
		// Remove the full prefix to get the relative key
		relativeKey := strings.TrimPrefix(string(kv.Key), s.Prefix+"/")
		result[relativeKey] = kv.Value
	}

	s.logger.Debugf("List keys with prefix: %s, found %d keys", fullPrefix, len(result))
	return result, nil
}

// Watch watches for changes to keys with a given prefix
func (s *Store) Watch(ctx context.Context, keyPrefix string) clientv3.WatchChan {
	fullPrefix := s.Prefix + "/" + keyPrefix
	s.logger.Debugf("Watching keys with prefix: %s", fullPrefix)
	return s.client.Watch(ctx, fullPrefix, clientv3.WithPrefix())
}

// PutWithLease stores a key-value pair with a lease
func (s *Store) PutWithLease(ctx context.Context, key string, value interface{}, ttl time.Duration) (clientv3.LeaseID, error) {
	// Create lease
	lease, err := s.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("failed to create lease: %w", err)
	}

	data, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal value: %w", err)
	}

	fullKey := s.Prefix + "/" + key
	_, err = s.client.Put(ctx, fullKey, string(data), clientv3.WithLease(lease.ID))
	if err != nil {
		return 0, fmt.Errorf("failed to put key %s with lease: %w", fullKey, err)
	}

	s.logger.Debugf("Put key with lease: %s, lease ID: %d", fullKey, lease.ID)
	return lease.ID, nil
}

// KeepAlive keeps a lease alive
func (s *Store) KeepAlive(ctx context.Context, leaseID clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	ch, err := s.client.KeepAlive(ctx, leaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to keep lease alive: %w", err)
	}

	s.logger.Debugf("Keeping lease alive: %d", leaseID)
	return ch, nil
}

// RevokeLease revokes a lease
func (s *Store) RevokeLease(ctx context.Context, leaseID clientv3.LeaseID) error {
	_, err := s.client.Revoke(ctx, leaseID)
	if err != nil {
		return fmt.Errorf("failed to revoke lease: %w", err)
	}

	s.logger.Debugf("Revoked lease: %d", leaseID)
	return nil
}

// CompareAndSwap performs an atomic compare-and-swap operation
func (s *Store) CompareAndSwap(ctx context.Context, key string, oldValue, newValue interface{}) (bool, error) {
	oldData, err := json.Marshal(oldValue)
	if err != nil {
		return false, fmt.Errorf("failed to marshal old value: %w", err)
	}

	newData, err := json.Marshal(newValue)
	if err != nil {
		return false, fmt.Errorf("failed to marshal new value: %w", err)
	}

	fullKey := s.Prefix + "/" + key
	txn := s.client.Txn(ctx)
	resp, err := txn.If(
		clientv3.Compare(clientv3.Value(fullKey), "=", string(oldData)),
	).Then(
		clientv3.OpPut(fullKey, string(newData)),
	).Commit()

	if err != nil {
		return false, fmt.Errorf("failed to compare and swap key %s: %w", fullKey, err)
	}

	success := resp.Succeeded
	s.logger.Debugf("Compare and swap key: %s, success: %v", fullKey, success)
	return success, nil
}

// Election represents a leader election
type Election struct {
	store    *Store
	session  *Session
	key      string
	value    string
	leaderCh chan bool
}

// NewElection creates a new leader election
func (s *Store) NewElection(key, value string, ttl time.Duration) (*Election, error) {
	session, err := s.NewSession(ttl)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &Election{
		store:    s,
		session:  session,
		key:      key,
		value:    value,
		leaderCh: make(chan bool, 1),
	}, nil
}

// Campaign starts campaigning for leadership
func (e *Election) Campaign(ctx context.Context) error {
	fullKey := e.store.Prefix + "/elections/" + e.key
	
	// Try to acquire leadership
	txn := e.store.client.Txn(ctx)
	resp, err := txn.If(
		clientv3.Compare(clientv3.CreateRevision(fullKey), "=", 0),
	).Then(
		clientv3.OpPut(fullKey, e.value, clientv3.WithLease(e.session.LeaseID())),
	).Commit()

	if err != nil {
		return fmt.Errorf("failed to campaign for leadership: %w", err)
	}

	if resp.Succeeded {
		e.store.logger.Infof("Became leader for key: %s", e.key)
		e.leaderCh <- true
		return nil
	}

	// Watch for leadership changes
	go e.watchLeadership(ctx)
	return nil
}

// IsLeader returns true if this node is the leader
func (e *Election) IsLeader() bool {
	select {
	case isLeader := <-e.leaderCh:
		return isLeader
	default:
		return false
	}
}

// Resign gives up leadership
func (e *Election) Resign(ctx context.Context) error {
	fullKey := e.store.Prefix + "/elections/" + e.key
	_, err := e.store.client.Delete(ctx, fullKey)
	if err != nil {
		return fmt.Errorf("failed to resign leadership: %w", err)
	}

	e.store.logger.Infof("Resigned leadership for key: %s", e.key)
	e.leaderCh <- false
	return nil
}

// Close closes the election
func (e *Election) Close() error {
	return e.session.Close()
}

// watchLeadership watches for leadership changes
func (e *Election) watchLeadership(ctx context.Context) {
	fullKey := e.store.Prefix + "/elections/" + e.key
	watchCh := e.store.client.Watch(ctx, fullKey)

	for watchResp := range watchCh {
		for _, event := range watchResp.Events {
			if event.Type == clientv3.EventTypeDelete {
				// Leadership is available, try to acquire it
				e.Campaign(ctx)
				return
			}
		}
	}
}
