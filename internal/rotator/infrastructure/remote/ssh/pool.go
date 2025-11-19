package ssh

import (
	"context"
	"sync"
	"time"

	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
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
	logger      *applogger.Logger
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
func NewPool(privateKey string, logger *applogger.Logger, maxIdle time.Duration, hostKeyCallback ssh.HostKeyCallback) *Pool {
	scopedLogger := logger.WithComponent("ssh.pool")

	// Parse the private key once for the pool
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		scopedLogger.ErrorCtx(context.Background(), "failed to parse SSH private key", err)
		return nil
	}

	// Create SSH client configuration
	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	pool := &Pool{
		connections: make(map[string]*Connection),
		maxIdle:     maxIdle,
		privateKey:  privateKey,
		logger:      scopedLogger,
		sshConfig:   sshConfig,
	}

	// Start cleanup routine
	pool.StartCleanupRoutine()

	return pool
}

// GetConnection retrieves or creates an SSH connection from the pool
func (p *Pool) GetConnection(ctx context.Context, nodeIP string) (Client, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Check if we have an existing connection
	if conn, exists := p.connections[nodeIP]; exists {
		// Check if connection is still healthy
		if p.isConnectionHealthy(conn.client) {
			conn.lastUsed = time.Now()
			p.logger.DebugContext(ctx, "reusing existing SSH connection", "node_ip", nodeIP)
			return conn.client, nil
		} else {
			// Connection is unhealthy, remove it
			p.logger.DebugContext(ctx, "removing unhealthy SSH connection", "node_ip", nodeIP)
			conn.client.Close()
			delete(p.connections, nodeIP)
		}
	}

	// Create new connection
	client, err := p.createNewConnection(ctx, nodeIP)
	if err != nil {
		// createNewConnection already wraps and logs
		return nil, err
	}

	// Store in pool
	p.connections[nodeIP] = &Connection{
		client:   client,
		lastUsed: time.Now(),
		nodeIP:   nodeIP,
	}

	p.logger.DebugContext(ctx, "created new SSH connection", "node_ip", nodeIP)
	return client, nil
}

// ExecuteCommand executes a command on a node.
func (p *Pool) ExecuteCommand(ctx context.Context, nodeIP, command string) (string, error) {
	client, err := p.GetConnection(ctx, nodeIP)
	if err != nil {
		// If we can't get a connection, we should close it to ensure it's re-established next time.
		p.CloseConnection(ctx, nodeIP)
		// GetConnection already returns a DomainError
		return "", err
	}

	output, err := client.RunCommand(ctx, command)
	if err != nil {
		// If the command fails, the connection might be stale. Close it so a fresh
		// one is created on the next attempt.
		p.CloseConnection(ctx, nodeIP)
		// RunCommand already returns a DomainError
		return output, err
	}

	return output, nil
}

// CloseConnection closes and removes a connection from the pool
func (p *Pool) CloseConnection(ctx context.Context, nodeIP string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if conn, exists := p.connections[nodeIP]; exists {
		conn.client.Close()
		delete(p.connections, nodeIP)
		p.logger.DebugContext(ctx, "closed SSH connection", "node_ip", nodeIP)
	}
}

// CloseAllConnections closes all connections in the pool
func (p *Pool) CloseAllConnections(ctx context.Context) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for nodeIP, conn := range p.connections {
		conn.client.Close()
		p.logger.DebugContext(ctx, "closed SSH connection during shutdown", "node_ip", nodeIP)
	}

	p.connections = make(map[string]*Connection)
	p.logger.DebugContext(ctx, "closed all SSH connections")
}

// CleanupIdleConnections removes idle connections from the pool
func (p *Pool) CleanupIdleConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	ctx := context.Background() // Use background context for internal cleanup
	now := time.Now()
	var removedCount int

	for nodeIP, conn := range p.connections {
		if now.Sub(conn.lastUsed) > p.maxIdle {
			conn.client.Close()
			delete(p.connections, nodeIP)
			removedCount++
			p.logger.DebugContext(ctx, "removed idle SSH connection",
				"node_ip", nodeIP,
				"idle_time", now.Sub(conn.lastUsed))
		}
	}

	if removedCount > 0 {
		p.logger.DebugContext(ctx, "cleaned up idle SSH connections", "removed", removedCount)
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
func (p *Pool) createNewConnection(ctx context.Context, nodeIP string) (Client, error) {
	// NewClient now requires the logger
	return NewClient(nodeIP, "root", p.privateKey, p.logger)
}

// isConnectionHealthy checks if an SSH connection is still healthy
func (p *Pool) isConnectionHealthy(client Client) bool {
	return client.IsHealthy()
}
