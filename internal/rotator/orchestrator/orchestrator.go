package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ipmanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
)

// Orchestrator defines the interface for managing the VPN node lifecycle.
// It encapsulates node provisioning, peer management, rotation, and cleanup logic.
type Orchestrator interface {
	// Node management operations
	NodeManager

	// Peer management operations
	PeerManager

	// Rotation and lifecycle operations
	RotationManager

	// Cleanup operations
	CleanupManager
}

// NodeManager defines methods for managing nodes.
type NodeManager interface {
	GetNodeConfig(ctx context.Context, nodeID string) (*nodemanager.NodeConfig, error)
	GetNodesByStatus(ctx context.Context, status string) ([]db.Node, error)
	GetNodeLoadBalance(ctx context.Context) (map[string]int, error)
	SelectNodeForPeer(ctx context.Context) (string, error)
	ValidateNodePeerCapacity(ctx context.Context, nodeID string, additionalPeers int) error
	SyncNodePeers(ctx context.Context, nodeID string) error
	DestroyNode(ctx context.Context, node db.Node) error
}

// PeerManager defines methods for managing peers.
type PeerManager interface {
	CreatePeerOnOptimalNode(ctx context.Context, publicKey string, presharedKey *string) (*peermanager.PeerConfig, error)
	RemovePeerFromSystem(ctx context.Context, peerID string) error
	GetPeerStatistics(ctx context.Context) (*peermanager.PeerStatistics, error)
	MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error
}

// RotationManager defines methods for node rotation and lifecycle.
type RotationManager interface {
	ShouldRotate(ctx context.Context) (bool, error)
	RotateNodes(ctx context.Context) error
	GetRotationStatus(ctx context.Context) (*RotationStatus, error)
	ValidateRotationPreconditions(ctx context.Context) error
}

// CleanupManager defines methods for cleaning up resources.
type CleanupManager interface {
	CleanupNodes(ctx context.Context) error
	CleanupInactivePeers(ctx context.Context, inactiveMinutes int) error
	GetOrphanedNodes(ctx context.Context, age time.Duration) ([]string, error)
	DeleteNode(ctx context.Context, serverID string) error
}

// manager orchestrates VPN node lifecycle: provisioning, rotation, and cleanup.
type manager struct {
	store       db.Store
	nodeManager nodemanager.NodeManager
	peerManager peermanager.Manager
	ipManager   ipmanager.IPManager
	logger      *slog.Logger
}

// New creates a new manager.
func New(store db.Store, nodeManager nodemanager.NodeManager, peerManager peermanager.Manager, ipManager ipmanager.IPManager, logger *slog.Logger) Orchestrator {
	return &manager{
		store:       store,
		nodeManager: nodeManager,
		peerManager: peerManager,
		ipManager:   ipManager,
		logger:      logger,
	}
}
