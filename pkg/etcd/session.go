package etcd

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Session represents an etcd session with lease management
type Session struct {
	client  *clientv3.Client
	leaseID clientv3.LeaseID
	ttl     time.Duration
	logger  Logger
	closeCh chan struct{}
}

// NewSession creates a new etcd session
func (s *Store) NewSession(ttl time.Duration) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create lease
	lease, err := s.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("failed to create lease: %w", err)
	}

	session := &Session{
		client:  s.client,
		leaseID: lease.ID,
		ttl:     ttl,
		logger:  s.logger,
		closeCh: make(chan struct{}),
	}

	// Start keep-alive goroutine
	go session.keepAlive()

	s.logger.Infof("Created session with lease ID: %d, TTL: %v", lease.ID, ttl)
	return session, nil
}

// LeaseID returns the lease ID for this session
func (s *Session) LeaseID() clientv3.LeaseID {
	return s.leaseID
}

// TTL returns the TTL for this session
func (s *Session) TTL() time.Duration {
	return s.ttl
}

// Close closes the session and revokes the lease
func (s *Session) Close() error {
	close(s.closeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.client.Revoke(ctx, s.leaseID)
	if err != nil {
		s.logger.Errorf("Failed to revoke lease %d: %v", s.leaseID, err)
		return fmt.Errorf("failed to revoke lease: %w", err)
	}

	s.logger.Infof("Closed session and revoked lease ID: %d", s.leaseID)
	return nil
}

// keepAlive maintains the lease by sending keep-alive requests
func (s *Session) keepAlive() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := s.client.KeepAlive(ctx, s.leaseID)
	if err != nil {
		s.logger.Errorf("Failed to start keep-alive for lease %d: %v", s.leaseID, err)
		return
	}

	s.logger.Debugf("Started keep-alive for lease ID: %d", s.leaseID)

	for {
		select {
		case <-s.closeCh:
			s.logger.Debugf("Stopping keep-alive for lease ID: %d", s.leaseID)
			return
		case resp, ok := <-ch:
			if !ok {
				s.logger.Warnf("Keep-alive channel closed for lease ID: %d", s.leaseID)
				return
			}
			if resp != nil {
				s.logger.Debugf("Keep-alive response for lease ID: %d, TTL: %d", s.leaseID, resp.TTL)
			}
		}
	}
}
