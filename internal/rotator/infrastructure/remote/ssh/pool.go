package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Connection represents a pooled SSH connection
type Connection struct {
	client   Client
	lastUsed time.Time
	nodeIP   string
}

// Pool manages a pool of SSH connections for efficient reuse
type Pool struct {
	connections map[string]*Connection
	mutex       sync.RWMutex
	maxIdle     time.Duration
	privateKey  string
	logger      *slog.Logger
	sshConfig   *ssh.ClientConfig
}

// PoolStats provides statistics about the SSH connection pool
type PoolStats struct {
	TotalConnections  int                  `json:"total_connections"`
	ActiveConnections int                  `json:"active_connections"`
	IdleConnections   int                  `json:"idle_connections"`
	ConnectionsByNode map[string]time.Time `json:"connections_by_node"`
}

// NewPool creates a new SSH connection pool
func NewPool(privateKey string, logger *slog.Logger, maxIdle time.Duration) *Pool {
	// Parse the private key once for the pool
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		logger.Error("failed to parse SSH private key", slog.String("error", err.Error()))
		return nil
	}

	// Create SSH client configuration
	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Implement proper host key verification
		Timeout:         30 * time.Second,
	}

	pool := &Pool{
		connections: make(map[string]*Connection),
		maxIdle:     maxIdle,
		privateKey:  privateKey,
		logger:      logger,
		sshConfig:   sshConfig,
	}

	// Start cleanup routine
	pool.StartCleanupRoutine()

	return pool
}

// GetConnection retrieves or creates an SSH connection from the pool
func (p *Pool) GetConnection(nodeIP string) (Client, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Check if we have an existing connection
	if conn, exists := p.connections[nodeIP]; exists {
		// Check if connection is still healthy
		if p.isConnectionHealthy(conn.client) {
			conn.lastUsed = time.Now()
			p.logger.Debug("reusing existing SSH connection", slog.String("node_ip", nodeIP))
			return conn.client, nil
		} else {
			// Connection is unhealthy, remove it
			p.logger.Debug("removing unhealthy SSH connection", slog.String("node_ip", nodeIP))
			conn.client.Close()
			delete(p.connections, nodeIP)
		}
	}

	// Create new connection
	client, err := p.createNewConnection(nodeIP)
	if err != nil {
		return nil, err
	}

	// Store in pool
	p.connections[nodeIP] = &Connection{
		client:   client,
		lastUsed: time.Now(),
		nodeIP:   nodeIP,
	}

	p.logger.Debug("created new SSH connection", slog.String("node_ip", nodeIP))
	return client, nil
}

// ExecuteCommand executes a command on a node with comprehensive retry logic and error handling
func (p *Pool) ExecuteCommand(ctx context.Context, nodeIP, command string) (string, error) {
	maxRetries := 3
	baseDelay := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := time.Duration(attempt) * baseDelay
			p.logger.Debug("retrying SSH command after delay",
				slog.String("node_ip", nodeIP),
				slog.Int("attempt", attempt+1),
				slog.Duration("delay", delay))

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := p.executeCommandOnce(ctx, nodeIP, command)
		if err == nil {
			return result, nil
		}

		p.logger.Warn("SSH command failed",
			slog.String("node_ip", nodeIP),
			slog.String("command", command),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()))
		p.logger.Debug("result from failed SSH command",
			slog.String("node_ip", nodeIP),
			slog.String("command", command),
			slog.String("result", result))

		// Check if error is retryable
		if !p.isRetryableSSHError(err) {
			return "", fmt.Errorf("non-retryable SSH error: %w", err)
		}

		// Remove connection from pool on error
		p.CloseConnection(nodeIP)
	}

	return "", fmt.Errorf("SSH command failed after %d attempts", maxRetries)
}

// executeCommandOnce executes a command once without retry logic
func (p *Pool) executeCommandOnce(ctx context.Context, nodeIP, command string) (string, error) {
	client, err := p.GetConnection(nodeIP)
	if err != nil {
		return "", fmt.Errorf("failed to get SSH connection: %w", err)
	}

	return client.RunCommand(ctx, command)
}

// CloseConnection closes and removes a connection from the pool
func (p *Pool) CloseConnection(nodeIP string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if conn, exists := p.connections[nodeIP]; exists {
		conn.client.Close()
		delete(p.connections, nodeIP)
		p.logger.Debug("closed SSH connection", slog.String("node_ip", nodeIP))
	}
}

// CloseAllConnections closes all connections in the pool
func (p *Pool) CloseAllConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for nodeIP, conn := range p.connections {
		conn.client.Close()
		p.logger.Debug("closed SSH connection during shutdown", slog.String("node_ip", nodeIP))
	}

	p.connections = make(map[string]*Connection)
	p.logger.Info("closed all SSH connections")
}

// CleanupIdleConnections removes idle connections from the pool
func (p *Pool) CleanupIdleConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()
	var removedCount int

	for nodeIP, conn := range p.connections {
		if now.Sub(conn.lastUsed) > p.maxIdle {
			conn.client.Close()
			delete(p.connections, nodeIP)
			removedCount++
			p.logger.Debug("removed idle SSH connection",
				slog.String("node_ip", nodeIP),
				slog.Duration("idle_time", now.Sub(conn.lastUsed)))
		}
	}

	if removedCount > 0 {
		p.logger.Info("cleaned up idle SSH connections", slog.Int("removed", removedCount))
	}
}

// StartCleanupRoutine starts a background goroutine to clean up idle connections
func (p *Pool) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(p.maxIdle / 2) // Clean up twice as often as max idle time
		defer ticker.Stop()

		for range ticker.C {
			p.CleanupIdleConnections()
		}
	}()
}

// GetStats returns statistics about the SSH connection pool
func (p *Pool) GetStats() *PoolStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stats := &PoolStats{
		TotalConnections:  len(p.connections),
		ActiveConnections: 0,
		IdleConnections:   0,
		ConnectionsByNode: make(map[string]time.Time),
	}

	now := time.Now()
	for nodeIP, conn := range p.connections {
		stats.ConnectionsByNode[nodeIP] = conn.lastUsed

		// Consider connection active if used within last minute
		if now.Sub(conn.lastUsed) < time.Minute {
			stats.ActiveConnections++
		} else {
			stats.IdleConnections++
		}
	}

	return stats
}

// createNewConnection creates a new SSH connection with retry logic
func (p *Pool) createNewConnection(nodeIP string) (Client, error) {
	return NewClient(nodeIP, "root", p.privateKey)
}

// isConnectionHealthy checks if an SSH connection is still healthy
func (p *Pool) isConnectionHealthy(client Client) bool {
	return client.IsHealthy()
}

// isRetryableSSHError determines if an SSH error is worth retrying
func (p *Pool) isRetryableSSHError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network-related errors that are typically retryable
	retryableErrors := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"network is unreachable",
		"no route to host",
		"temporary failure",
		"i/o timeout",
		"broken pipe",
		"connection lost",
	}

	errLower := strings.ToLower(errStr)
	for _, retryable := range retryableErrors {
		if strings.Contains(errLower, retryable) {
			return true
		}
	}

	// Check for specific network error types
	if netErr, ok := err.(net.Error); ok {
		return netErr.Temporary() || netErr.Timeout()
	}

	return false
}
