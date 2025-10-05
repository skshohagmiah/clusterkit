package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// Server provides HTTP transport for ClusterKit
type Server struct {
	mu       sync.RWMutex
	config   *Config
	server   *http.Server
	handlers map[string]http.HandlerFunc
	logger   Logger
}

// Config represents HTTP server configuration
type Config struct {
	Port         int           `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
}

// Logger interface for HTTP transport
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NewServer creates a new HTTP server
func NewServer(config *Config, logger Logger) *Server {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 60 * time.Second
	}

	return &Server{
		config:   config,
		handlers: make(map[string]http.HandlerFunc),
		logger:   logger,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mux := http.NewServeMux()

	// Register all handlers
	for path, handler := range s.handlers {
		mux.HandleFunc(path, s.wrapHandler(handler))
	}

	// Add default handlers
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	go func() {
		s.logger.Infof("HTTP server starting on port %d", s.config.Port)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}

	s.logger.Infof("Stopping HTTP server")
	return s.server.Shutdown(ctx)
}

// RegisterHandler registers a handler for a path
func (s *Server) RegisterHandler(path string, handler http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handlers[path] = handler
	s.logger.Debugf("Registered handler for path: %s", path)
}

// RegisterClusterKitHandlers registers standard ClusterKit handlers
func (s *Server) RegisterClusterKitHandlers(ck *clusterkit.ClusterKit) {
	s.RegisterHandler("/cluster/nodes", s.handleNodes(ck))
	s.RegisterHandler("/cluster/partitions", s.handlePartitions(ck))
	s.RegisterHandler("/cluster/stats", s.handleStats(ck))
	s.RegisterHandler("/cluster/partition", s.handlePartition(ck))
	s.RegisterHandler("/cluster/leader", s.handleLeader(ck))
	s.RegisterHandler("/cluster/replicas", s.handleReplicas(ck))
}

// wrapHandler wraps a handler with common middleware
func (s *Server) wrapHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Add CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Set content type
		w.Header().Set("Content-Type", "application/json")

		// Call the actual handler
		handler(w, r)

		// Log request
		duration := time.Since(start)
		s.logger.Debugf("%s %s - %v", r.Method, r.URL.Path, duration)
	}
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
	}

	json.NewEncoder(w).Encode(response)
}

// handleMetrics handles metrics requests
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Basic metrics - could be extended with Prometheus
	response := map[string]interface{}{
		"http_requests_total": 0, // Would be tracked in real implementation
		"uptime_seconds":      0, // Would be calculated in real implementation
	}

	json.NewEncoder(w).Encode(response)
}

// handleNodes returns cluster nodes
func (s *Server) handleNodes(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		nodes := ck.GetNodes()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"nodes": nodes,
			"count": len(nodes),
		})
	}
}

// handlePartitions returns partition information
func (s *Server) handlePartitions(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		partitionMap := ck.GetPartitionMap()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"partitions": partitionMap,
			"count":      len(partitionMap),
		})
	}
}

// handleStats returns cluster statistics
func (s *Server) handleStats(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stats := ck.GetStats()
		json.NewEncoder(w).Encode(stats)
	}
}

// handlePartition returns partition for a key
func (s *Server) handlePartition(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Key parameter required", http.StatusBadRequest)
			return
		}

		partition := ck.GetPartition(key)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key":       key,
			"partition": partition,
		})
	}
}

// handleLeader returns leader for a partition
func (s *Server) handleLeader(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		partitionStr := r.URL.Query().Get("partition")
		if partitionStr == "" {
			http.Error(w, "Partition parameter required", http.StatusBadRequest)
			return
		}

		partition, err := strconv.Atoi(partitionStr)
		if err != nil {
			http.Error(w, "Invalid partition number", http.StatusBadRequest)
			return
		}

		leader := ck.GetLeader(partition)
		if leader == nil {
			http.Error(w, "No leader found", http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"partition": partition,
			"leader":    leader,
		})
	}
}

// handleReplicas returns replicas for a partition
func (s *Server) handleReplicas(ck *clusterkit.ClusterKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		partitionStr := r.URL.Query().Get("partition")
		if partitionStr == "" {
			http.Error(w, "Partition parameter required", http.StatusBadRequest)
			return
		}

		partition, err := strconv.Atoi(partitionStr)
		if err != nil {
			http.Error(w, "Invalid partition number", http.StatusBadRequest)
			return
		}

		replicas := ck.GetReplicas(partition)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"partition": partition,
			"replicas":  replicas,
			"count":     len(replicas),
		})
	}
}

// ServeJSON is a helper to serve JSON responses
func ServeJSON(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

// ServeError is a helper to serve JSON error responses
func ServeError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
