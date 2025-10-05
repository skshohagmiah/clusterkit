package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/shohag/clusterkit/pkg/clusterkit"
)

// Client provides HTTP client functionality for ClusterKit
type Client struct {
	mu         sync.RWMutex
	httpClient *http.Client
	config     *ClientConfig
	logger     Logger
}

// ClientConfig represents HTTP client configuration
type ClientConfig struct {
	Timeout         time.Duration `json:"timeout"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	IdleConnTimeout time.Duration `json:"idle_conn_timeout"`
	RetryAttempts   int           `json:"retry_attempts"`
	RetryDelay      time.Duration `json:"retry_delay"`
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// NewClient creates a new HTTP client
func NewClient(config *ClientConfig, logger Logger) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 100
	}
	if config.IdleConnTimeout == 0 {
		config.IdleConnTimeout = 90 * time.Second
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}

	transport := &http.Transport{
		MaxIdleConns:        config.MaxIdleConns,
		IdleConnTimeout:     config.IdleConnTimeout,
		DisableCompression:  false,
		MaxIdleConnsPerHost: 10,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	return &Client{
		httpClient: httpClient,
		config:     config,
		logger:     logger,
	}
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, url string, headers map[string]string) (*Response, error) {
	return c.doRequest(ctx, "GET", url, nil, headers)
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*Response, error) {
	return c.doRequest(ctx, "POST", url, body, headers)
}

// Put performs a PUT request
func (c *Client) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*Response, error) {
	return c.doRequest(ctx, "PUT", url, body, headers)
}

// Delete performs a DELETE request
func (c *Client) Delete(ctx context.Context, url string, headers map[string]string) (*Response, error) {
	return c.doRequest(ctx, "DELETE", url, nil, headers)
}

// doRequest performs an HTTP request with retry logic
func (c *Client) doRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryDelay * time.Duration(attempt)):
			}
		}

		resp, err := c.performRequest(ctx, method, url, body, headers)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		c.logger.Warnf("HTTP request failed (attempt %d/%d): %v", attempt+1, c.config.RetryAttempts+1, err)
	}

	return nil, fmt.Errorf("HTTP request failed after %d attempts: %w", c.config.RetryAttempts+1, lastErr)
}

// performRequest performs a single HTTP request
func (c *Client) performRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*Response, error) {
	var bodyReader io.Reader

	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ClusterKit-HTTP-Client/1.0")

	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)
	c.logger.Debugf("%s %s - %d (%v)", method, url, resp.StatusCode, duration)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       bodyBytes,
		Headers:    resp.Header,
	}, nil
}

// GetJSON performs a GET request and unmarshals JSON response
func (c *Client) GetJSON(ctx context.Context, url string, result interface{}, headers map[string]string) error {
	resp, err := c.Get(ctx, url, headers)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return json.Unmarshal(resp.Body, result)
}

// PostJSON performs a POST request and unmarshals JSON response
func (c *Client) PostJSON(ctx context.Context, url string, body interface{}, result interface{}, headers map[string]string) error {
	resp, err := c.Post(ctx, url, body, headers)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(resp.Body))
	}

	if result != nil {
		return json.Unmarshal(resp.Body, result)
	}

	return nil
}

// ClusterKitClient provides ClusterKit-specific HTTP client methods
type ClusterKitClient struct {
	client *Client
	logger Logger
}

// NewClusterKitClient creates a new ClusterKit HTTP client
func NewClusterKitClient(config *ClientConfig, logger Logger) *ClusterKitClient {
	return &ClusterKitClient{
		client: NewClient(config, logger),
		logger: logger,
	}
}

// GetNodes retrieves cluster nodes from a remote node
func (ckc *ClusterKitClient) GetNodes(ctx context.Context, nodeAddr string) ([]*clusterkit.Node, error) {
	url := fmt.Sprintf("http://%s/cluster/nodes", nodeAddr)
	
	var response struct {
		Nodes []*clusterkit.Node `json:"nodes"`
		Count int                `json:"count"`
	}

	err := ckc.client.GetJSON(ctx, url, &response, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes from %s: %w", nodeAddr, err)
	}

	return response.Nodes, nil
}

// GetPartitionMap retrieves partition map from a remote node
func (ckc *ClusterKitClient) GetPartitionMap(ctx context.Context, nodeAddr string) (map[int][]*clusterkit.Node, error) {
	url := fmt.Sprintf("http://%s/cluster/partitions", nodeAddr)
	
	var response struct {
		Partitions map[int][]*clusterkit.Node `json:"partitions"`
		Count      int                        `json:"count"`
	}

	err := ckc.client.GetJSON(ctx, url, &response, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get partitions from %s: %w", nodeAddr, err)
	}

	return response.Partitions, nil
}

// GetStats retrieves cluster statistics from a remote node
func (ckc *ClusterKitClient) GetStats(ctx context.Context, nodeAddr string) (*clusterkit.ClusterStats, error) {
	url := fmt.Sprintf("http://%s/cluster/stats", nodeAddr)
	
	var stats clusterkit.ClusterStats
	err := ckc.client.GetJSON(ctx, url, &stats, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats from %s: %w", nodeAddr, err)
	}

	return &stats, nil
}

// GetPartitionForKey gets the partition for a key from a remote node
func (ckc *ClusterKitClient) GetPartitionForKey(ctx context.Context, nodeAddr, key string) (int, error) {
	url := fmt.Sprintf("http://%s/cluster/partition?key=%s", nodeAddr, key)
	
	var response struct {
		Key       string `json:"key"`
		Partition int    `json:"partition"`
	}

	err := ckc.client.GetJSON(ctx, url, &response, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get partition for key %s from %s: %w", key, nodeAddr, err)
	}

	return response.Partition, nil
}

// GetLeader gets the leader for a partition from a remote node
func (ckc *ClusterKitClient) GetLeader(ctx context.Context, nodeAddr string, partition int) (*clusterkit.Node, error) {
	url := fmt.Sprintf("http://%s/cluster/leader?partition=%d", nodeAddr, partition)
	
	var response struct {
		Partition int                `json:"partition"`
		Leader    *clusterkit.Node   `json:"leader"`
	}

	err := ckc.client.GetJSON(ctx, url, &response, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get leader for partition %d from %s: %w", partition, nodeAddr, err)
	}

	return response.Leader, nil
}

// GetReplicas gets the replicas for a partition from a remote node
func (ckc *ClusterKitClient) GetReplicas(ctx context.Context, nodeAddr string, partition int) ([]*clusterkit.Node, error) {
	url := fmt.Sprintf("http://%s/cluster/replicas?partition=%d", nodeAddr, partition)
	
	var response struct {
		Partition int                  `json:"partition"`
		Replicas  []*clusterkit.Node   `json:"replicas"`
		Count     int                  `json:"count"`
	}

	err := ckc.client.GetJSON(ctx, url, &response, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get replicas for partition %d from %s: %w", partition, nodeAddr, err)
	}

	return response.Replicas, nil
}

// Close closes the HTTP client
func (c *Client) Close() error {
	// Close idle connections
	c.httpClient.CloseIdleConnections()
	return nil
}
