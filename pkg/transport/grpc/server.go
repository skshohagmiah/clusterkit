package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// Server provides gRPC transport for ClusterKit
type Server struct {
	mu       sync.RWMutex
	config   *Config
	server   *grpc.Server
	listener net.Listener
	logger   Logger
	services map[string]interface{}
}

// Config represents gRPC server configuration
type Config struct {
	Port                int           `json:"port"`
	MaxRecvMsgSize      int           `json:"max_recv_msg_size"`
	MaxSendMsgSize      int           `json:"max_send_msg_size"`
	ConnectionTimeout   time.Duration `json:"connection_timeout"`
	MaxConnectionIdle   time.Duration `json:"max_connection_idle"`
	MaxConnectionAge    time.Duration `json:"max_connection_age"`
	MaxConnectionAgeGrace time.Duration `json:"max_connection_age_grace"`
	Time                time.Duration `json:"time"`
	Timeout             time.Duration `json:"timeout"`
}

// Logger interface for gRPC transport
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NewServer creates a new gRPC server
func NewServer(config *Config, logger Logger) *Server {
	if config.MaxRecvMsgSize == 0 {
		config.MaxRecvMsgSize = 4 * 1024 * 1024 // 4MB
	}
	if config.MaxSendMsgSize == 0 {
		config.MaxSendMsgSize = 4 * 1024 * 1024 // 4MB
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 120 * time.Second
	}
	if config.MaxConnectionIdle == 0 {
		config.MaxConnectionIdle = 15 * time.Minute
	}
	if config.MaxConnectionAge == 0 {
		config.MaxConnectionAge = 30 * time.Minute
	}
	if config.MaxConnectionAgeGrace == 0 {
		config.MaxConnectionAgeGrace = 5 * time.Minute
	}
	if config.Time == 0 {
		config.Time = 30 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	return &Server{
		config:   config,
		logger:   logger,
		services: make(map[string]interface{}),
	}
}

// Start starts the gRPC server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.config.Port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.config.Port, err)
	}
	s.listener = listener

	// Create gRPC server with options
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(s.config.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(s.config.MaxSendMsgSize),
		grpc.ConnectionTimeout(s.config.ConnectionTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     s.config.MaxConnectionIdle,
			MaxConnectionAge:      s.config.MaxConnectionAge,
			MaxConnectionAgeGrace: s.config.MaxConnectionAgeGrace,
			Time:                  s.config.Time,
			Timeout:               s.config.Timeout,
		}),
		grpc.UnaryInterceptor(s.unaryInterceptor),
		grpc.StreamInterceptor(s.streamInterceptor),
	}

	s.server = grpc.NewServer(opts...)

	// Register services
	s.registerClusterKitService()

	go func() {
		s.logger.Infof("gRPC server starting on port %d", s.config.Port)
		if err := s.server.Serve(listener); err != nil {
			s.logger.Errorf("gRPC server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the gRPC server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}

	s.logger.Infof("Stopping gRPC server")
	
	// Graceful stop with timeout
	stopped := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		s.logger.Infof("gRPC server stopped gracefully")
	case <-time.After(30 * time.Second):
		s.logger.Warnf("gRPC server graceful stop timed out, forcing stop")
		s.server.Stop()
	}

	return nil
}

// RegisterService registers a gRPC service
func (s *Server) RegisterService(name string, service interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.services[name] = service
	s.logger.Debugf("Registered gRPC service: %s", name)
}

// registerClusterKitService registers the ClusterKit service
func (s *Server) registerClusterKitService() {
	// This would register the actual ClusterKit gRPC service
	// For now, we'll just log that it's registered
	s.logger.Debugf("Registered ClusterKit gRPC service")
}

// unaryInterceptor provides middleware for unary RPC calls
func (s *Server) unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()

	// Call the handler
	resp, err := handler(ctx, req)

	// Log the request
	duration := time.Since(start)
	if err != nil {
		s.logger.Errorf("gRPC %s failed: %v (%v)", info.FullMethod, err, duration)
	} else {
		s.logger.Debugf("gRPC %s completed (%v)", info.FullMethod, duration)
	}

	return resp, err
}

// streamInterceptor provides middleware for streaming RPC calls
func (s *Server) streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()

	// Call the handler
	err := handler(srv, ss)

	// Log the request
	duration := time.Since(start)
	if err != nil {
		s.logger.Errorf("gRPC stream %s failed: %v (%v)", info.FullMethod, err, duration)
	} else {
		s.logger.Debugf("gRPC stream %s completed (%v)", info.FullMethod, duration)
	}

	return err
}

// ClusterKitService implements the ClusterKit gRPC service
type ClusterKitService struct {
	ck     *clusterkit.ClusterKit
	logger Logger
}

// NewClusterKitService creates a new ClusterKit gRPC service
func NewClusterKitService(ck *clusterkit.ClusterKit, logger Logger) *ClusterKitService {
	return &ClusterKitService{
		ck:     ck,
		logger: logger,
	}
}

// GetNodes returns cluster nodes
func (cks *ClusterKitService) GetNodes(ctx context.Context, req *GetNodesRequest) (*GetNodesResponse, error) {
	nodes := cks.ck.GetNodes()
	
	var grpcNodes []*Node
	for _, node := range nodes {
		grpcNodes = append(grpcNodes, &Node{
			Id:           node.ID,
			Addr:         node.Addr,
			HttpEndpoint: node.HTTPEndpoint,
			GrpcEndpoint: node.GRPCEndpoint,
			Metadata:     node.Metadata,
		})
	}

	return &GetNodesResponse{
		Nodes: grpcNodes,
		Count: int32(len(grpcNodes)),
	}, nil
}

// GetPartitionMap returns partition assignments
func (cks *ClusterKitService) GetPartitionMap(ctx context.Context, req *GetPartitionMapRequest) (*GetPartitionMapResponse, error) {
	partitionMap := cks.ck.GetPartitionMap()
	
	grpcPartitions := make(map[int32]*PartitionInfo)
	for partition, nodes := range partitionMap {
		var grpcNodes []*Node
		for _, node := range nodes {
			grpcNodes = append(grpcNodes, &Node{
				Id:           node.ID,
				Addr:         node.Addr,
				HttpEndpoint: node.HTTPEndpoint,
				GrpcEndpoint: node.GRPCEndpoint,
				Metadata:     node.Metadata,
			})
		}

		var leader *Node
		if len(grpcNodes) > 0 {
			leader = grpcNodes[0]
		}

		grpcPartitions[int32(partition)] = &PartitionInfo{
			Id:       int32(partition),
			Leader:   leader,
			Replicas: grpcNodes,
		}
	}

	return &GetPartitionMapResponse{
		Partitions: grpcPartitions,
		Count:      int32(len(grpcPartitions)),
	}, nil
}

// GetStats returns cluster statistics
func (cks *ClusterKitService) GetStats(ctx context.Context, req *GetStatsRequest) (*GetStatsResponse, error) {
	stats := cks.ck.GetStats()

	return &GetStatsResponse{
		NodeCount:        int32(stats.NodeCount),
		PartitionCount:   int32(stats.PartitionCount),
		LocalPartitions:  int32(stats.LocalPartitions),
		LeaderPartitions: int32(stats.LeaderPartitions),
		ReplicaFactor:    int32(stats.ReplicaFactor),
	}, nil
}

// GetPartition returns partition for a key
func (cks *ClusterKitService) GetPartition(ctx context.Context, req *GetPartitionRequest) (*GetPartitionResponse, error) {
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	partition := cks.ck.GetPartition(req.Key)

	return &GetPartitionResponse{
		Key:       req.Key,
		Partition: int32(partition),
	}, nil
}

// GetLeader returns leader for a partition
func (cks *ClusterKitService) GetLeader(ctx context.Context, req *GetLeaderRequest) (*GetLeaderResponse, error) {
	leader := cks.ck.GetLeader(int(req.Partition))
	if leader == nil {
		return nil, status.Error(codes.NotFound, "no leader found for partition")
	}

	return &GetLeaderResponse{
		Partition: req.Partition,
		Leader: &Node{
			Id:           leader.ID,
			Addr:         leader.Addr,
			HttpEndpoint: leader.HTTPEndpoint,
			GrpcEndpoint: leader.GRPCEndpoint,
			Metadata:     leader.Metadata,
		},
	}, nil
}

// GetReplicas returns replicas for a partition
func (cks *ClusterKitService) GetReplicas(ctx context.Context, req *GetReplicasRequest) (*GetReplicasResponse, error) {
	replicas := cks.ck.GetReplicas(int(req.Partition))
	
	var grpcReplicas []*Node
	for _, replica := range replicas {
		grpcReplicas = append(grpcReplicas, &Node{
			Id:           replica.ID,
			Addr:         replica.Addr,
			HttpEndpoint: replica.HTTPEndpoint,
			GrpcEndpoint: replica.GRPCEndpoint,
			Metadata:     replica.Metadata,
		})
	}

	return &GetReplicasResponse{
		Partition: req.Partition,
		Replicas:  grpcReplicas,
		Count:     int32(len(grpcReplicas)),
	}, nil
}

// Health check implementation
func (cks *ClusterKitService) Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	healthy := cks.ck.IsHealthy()
	
	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	return &HealthResponse{
		Status:  status,
		Healthy: healthy,
		NodeId:  cks.ck.NodeID(),
	}, nil
}
