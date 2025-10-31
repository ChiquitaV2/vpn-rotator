package nodemanager

import (
	"context"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ipmanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ssh"
)

// HealthChecker defines the interface for checking node health.
type HealthChecker interface {
	Check(ctx context.Context, nodeIP string) error
}

// NodeManager defines the interface for direct node lifecycle operations.
type NodeManager interface {
	NodeLifecycleManager
	NodePeerManager
	NodeInfoManager
}

// NodeLifecycleManager defines methods for node lifecycle.
type NodeLifecycleManager interface {
	CreateNode(ctx context.Context) (*NodeConfig, error)
	DestroyNode(ctx context.Context, node db.Node) error
}

// NodePeerManager defines methods for peer management on a node.
type NodePeerManager interface {
	AddPeerToNode(ctx context.Context, nodeIP string, peer *PeerConfig) error
	RemovePeerFromNode(ctx context.Context, nodeIP string, publicKey string) error
	ListNodePeers(ctx context.Context, nodeIP string) ([]*PeerInfo, error)
	UpdateNodePeerConfig(ctx context.Context, nodeIP string, peer *PeerConfig) error
}

// NodeInfoManager defines methods for getting information from a node.
type NodeInfoManager interface {
	GetNodeHealth(ctx context.Context, nodeIP string) error
	GetNodePublicKey(ctx context.Context, nodeIP string) (string, error)
}

// Manager implements the NodeManager interface for direct node operations.
type Manager struct {
	store         db.Store
	provisioner   provisioner.Provisioner
	healthChecker HealthChecker
	logger        *slog.Logger
	sshPrivateKey string
	sshPool       *ssh.Pool
	ipManager     ipmanager.IPManager
}

// New creates a new NodeManager.
func New(
	store db.Store,
	provisioner provisioner.Provisioner,
	healthChecker HealthChecker,
	logger *slog.Logger,
	sshPrivateKey string,
	ipManager ipmanager.IPManager,
) NodeManager {
	sshPool := ssh.NewPool(sshPrivateKey, logger, 5*time.Minute)

	manager := &Manager{
		store:         store,
		provisioner:   provisioner,
		healthChecker: healthChecker,
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
