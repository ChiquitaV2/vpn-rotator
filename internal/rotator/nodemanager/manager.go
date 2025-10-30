package nodemanager

import (
	"context"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/health"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ipmanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ssh"
)

// NodeManager defines the interface for direct node lifecycle operations.
type NodeManager interface {
	// CreateNode provisions a new VPN node and returns its configuration
	CreateNode(ctx context.Context) (*NodeConfig, error)

	// DestroyNode destroys a VPN node by ID
	DestroyNode(ctx context.Context, node db.Node) error

	// GetNodeHealth checks if a node is healthy and reachable
	GetNodeHealth(ctx context.Context, nodeIP string) error

	// GetNodePublicKey retrieves the WireGuard public key from a node
	GetNodePublicKey(ctx context.Context, nodeIP string) (string, error)

	// Peer management methods for individual nodes
	AddPeerToNode(ctx context.Context, nodeIP string, peer *PeerConfig) error
	RemovePeerFromNode(ctx context.Context, nodeIP string, publicKey string) error
	ListNodePeers(ctx context.Context, nodeIP string) ([]*PeerInfo, error)
	UpdateNodePeerConfig(ctx context.Context, nodeIP string, peer *PeerConfig) error
}

// Manager implements the NodeManager interface for direct node operations.
type Manager struct {
	store         db.Store
	provisioner   provisioner.Provisioner
	health        health.HealthChecker
	logger        *slog.Logger
	sshPrivateKey string
	sshPool       *ssh.Pool
	ipManager     ipmanager.IPManager
}

// New creates a new NodeManager.
func New(
	store db.Store,
	provisioner provisioner.Provisioner,
	health health.HealthChecker,
	logger *slog.Logger,
	sshPrivateKey string,
	ipManager ipmanager.IPManager,
) *Manager {
	sshPool := ssh.NewPool(sshPrivateKey, logger, 5*time.Minute)

	manager := &Manager{
		store:         store,
		provisioner:   provisioner,
		health:        health,
		logger:        logger,
		sshPrivateKey: sshPrivateKey,
		sshPool:       sshPool,
		ipManager:     ipManager,
	}

	// Start cleanup goroutine for idle connections
	sshPool.StartCleanupRoutine()

	return manager
}

// GetConnectionPoolStats returns statistics about the SSH connection pool
func (m *Manager) GetConnectionPoolStats() *ssh.PoolStats {
	return m.sshPool.GetStats()
}

// CloseAllConnections closes all connections in the pool
func (m *Manager) CloseAllConnections() {
	m.sshPool.CloseAllConnections()
}
