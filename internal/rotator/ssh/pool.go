package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Connection represents a pooled SSH connection
type Connection struct {
	client   Client
	lastUsed time.Time
	nodeIP   string
}

// Pool manages SSH connections with pooling and idle timeout
type Pool struct {
	connections map[string]*Connection
	mutex       sync.RWMutex
	maxIdle     time.Duration
	privateKey  string
	logger      *slog.Logger
}

// NewPool creates a new SSH connection pool
func NewPool(privateKey string, logger *slog.Logger, maxIdle time.Duration) *Pool {
	if maxIdle == 0 {
		maxIdle = 5 * time.Minute
	}

	return &Pool{
		connections: make(map[string]*Connection),
		maxIdle:     maxIdle,
		privateKey:  privateKey,
		logger:      logger,
	}
}

// GetConnection retrieves or creates an SSH connection from the pool
func (p *Pool) GetConnection(nodeIP string) (Client, error) {
	p.mutex.RLock()
	if conn, exists := p.connections[nodeIP]; exists {
		// Check if connection is still alive
		if conn.client != nil && p.isConnectionHealthy(conn.client) {
			// Update last used time
			conn.lastUsed = time.Now()
			p.mutex.RUnlock()
			return conn.client, nil
		}
		// Connection is stale, will be replaced
	}
	p.mutex.RUnlock()

	// Create new connection with write lock
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := p.connections[nodeIP]; exists && conn.client != nil && p.isConnectionHealthy(conn.client) {
		conn.lastUsed = time.Now()
		return conn.client, nil
	}

	// Clean up any existing stale connection
	if _, exists := p.connections[nodeIP]; exists {
		delete(p.connections, nodeIP)
	}

	// Create new connection
	client, err := p.createNewConnection(nodeIP)
	if err != nil {
		return nil, err
	}

	// Store connection in pool
	p.connections[nodeIP] = &Connection{
		client:   client,
		lastUsed: time.Now(),
		nodeIP:   nodeIP,
	}

	p.logger.Debug("established new SSH connection", slog.String("node_ip", nodeIP))
	return client, nil
}

// createNewConnection creates a new SSH connection with retry logic
func (p *Pool) createNewConnection(nodeIP string) (Client, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		client, err := NewClient(nodeIP, "root", p.privateKey)
		if err == nil {
			return client, nil
		}
		lastErr = err

		if attempt < 2 {
			// Exponential backoff: 1s, 2s
			backoff := time.Duration(1<<attempt) * time.Second
			p.logger.Debug("SSH connection failed, retrying",
				slog.String("node_ip", nodeIP),
				slog.Int("attempt", attempt+1),
				slog.Duration("backoff", backoff),
				slog.String("error", err.Error()))
			time.Sleep(backoff)
		}
	}

	return nil, fmt.Errorf("failed to establish SSH connection after 3 attempts: %w", lastErr)
}

// isConnectionHealthy checks if an SSH connection is still healthy
func (p *Pool) isConnectionHealthy(client Client) bool {
	if client == nil {
		return false
	}

	// Try a simple command to test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.RunCommand(ctx, "echo test")
	return err == nil
}

// CloseConnection closes and removes a connection from the pool
func (p *Pool) CloseConnection(nodeIP string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if _, exists := p.connections[nodeIP]; exists {
		delete(p.connections, nodeIP)
		p.logger.Debug("closed SSH connection", slog.String("node_ip", nodeIP))
	}
}

// CleanupIdleConnections removes idle connections from the pool
func (p *Pool) CleanupIdleConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()
	for nodeIP, conn := range p.connections {
		if now.Sub(conn.lastUsed) > p.maxIdle {
			delete(p.connections, nodeIP)
			p.logger.Debug("cleaned up idle SSH connection", slog.String("node_ip", nodeIP))
		}
	}
}

// CloseAllConnections closes all connections in the pool
func (p *Pool) CloseAllConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for nodeIP := range p.connections {
		delete(p.connections, nodeIP)
		p.logger.Debug("closed SSH connection during shutdown", slog.String("node_ip", nodeIP))
	}
}

// ExecuteCommand executes a command on a node with comprehensive retry logic and error handling
func (p *Pool) ExecuteCommand(ctx context.Context, nodeIP, command string) (string, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		output, err := p.executeCommandOnce(ctx, nodeIP, command)
		if err == nil {
			return output, nil
		}

		lastErr = err

		// Check if error is retryable
		if !p.isRetryableSSHError(err) {
			p.logger.Debug("non-retryable SSH error encountered",
				slog.String("node_ip", nodeIP),
				slog.String("command", command),
				slog.String("error", err.Error()))
			break
		}

		// Remove potentially stale connection from pool
		p.CloseConnection(nodeIP)

		if attempt < maxRetries-1 {
			// Exponential backoff: 1s, 2s, 4s
			backoff := time.Duration(1<<attempt) * time.Second
			p.logger.Debug("SSH command failed, retrying",
				slog.String("node_ip", nodeIP),
				slog.String("command", command),
				slog.Int("attempt", attempt+1),
				slog.Duration("backoff", backoff),
				slog.String("error", err.Error()))

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
				// Continue to next attempt
			}
		}
	}

	p.logger.Debug("SSH command %s failed after %d attempts", slog.String("command", command), slog.Int("max_retries", maxRetries))

	return "", fmt.Errorf("SSH command failed after %d attempts: %w", maxRetries, lastErr)
}

// executeCommandOnce executes a command once without retry logic
func (p *Pool) executeCommandOnce(ctx context.Context, nodeIP, command string) (string, error) {
	client, err := p.GetConnection(nodeIP)
	if err != nil {
		return "", fmt.Errorf("failed to get SSH connection: %w", err)
	}

	output, err := client.RunCommand(ctx, command)
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}

	return strings.TrimSpace(output), nil
}

// isRetryableSSHError determines if an SSH error is worth retrying
func (p *Pool) isRetryableSSHError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Network-related errors that are typically retryable
	retryableErrors := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"network is unreachable",
		"no route to host",
		"broken pipe",
		"i/o timeout",
		"connection lost",
		"ssh: handshake failed",
		"ssh: unable to authenticate",
	}

	for _, retryableErr := range retryableErrors {
		if strings.Contains(errStr, retryableErr) {
			return true
		}
	}

	return false
}

// PoolStats provides statistics about the SSH connection pool
type PoolStats struct {
	TotalConnections  int                  `json:"total_connections"`
	ActiveConnections int                  `json:"active_connections"`
	IdleConnections   int                  `json:"idle_connections"`
	ConnectionsByNode map[string]time.Time `json:"connections_by_node"`
}

// GetStats returns statistics about the SSH connection pool
func (p *Pool) GetStats() *PoolStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stats := &PoolStats{
		TotalConnections:  len(p.connections),
		ConnectionsByNode: make(map[string]time.Time),
	}

	now := time.Now()
	for nodeIP, connection := range p.connections {
		stats.ConnectionsByNode[nodeIP] = connection.lastUsed

		if now.Sub(connection.lastUsed) > p.maxIdle {
			stats.IdleConnections++
		} else {
			stats.ActiveConnections++
		}
	}

	return stats
}

// StartCleanupRoutine starts a background goroutine to clean up idle connections
func (p *Pool) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			p.CleanupIdleConnections()
		}
	}()
}
