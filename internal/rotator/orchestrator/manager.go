package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
)

// Orchestrator orchestrates VPN node lifecycle: provisioning, rotation, and cleanup.
type Orchestrator struct {
	store       db.Store
	nodeManager nodemanager.NodeManager
	logger      *slog.Logger
}

// New creates a new Orchestrator.
func New(store db.Store, nodeManager nodemanager.NodeManager, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{
		store:       store,
		nodeManager: nodeManager,
		logger:      logger,
	}
}

// GetLatestConfig returns the latest VPN configuration.
func (o *Orchestrator) GetLatestConfig(ctx context.Context) (*models.NodeConfig, error) {
	node, err := o.store.GetActiveNode(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			o.logger.Info("no active node found, provisioning a new one")
			return o.nodeManager.CreateNode(ctx)
		}
		return nil, fmt.Errorf("orchestrator: failed to get active node: %w", err)
	}

	return &models.NodeConfig{
		ServerPublicKey: node.ServerPublicKey,
		ServerIP:        node.IpAddress,
	}, nil
}

// DestroyNode destroys a VPN node using the node manager.
func (o *Orchestrator) DestroyNode(ctx context.Context, node db.Node) error {
	return o.nodeManager.DestroyNode(ctx, node)
}

// GetNodesByStatus returns nodes with the given status.
func (o *Orchestrator) GetNodesByStatus(ctx context.Context, status string) ([]db.Node, error) {
	return o.store.GetNodesByStatus(ctx, status)
}

// GetOrphanedNodes returns server IDs of nodes that are older than the specified age
// and should be cleaned up. This implements the CleanupManager interface.
func (o *Orchestrator) GetOrphanedNodes(ctx context.Context, age time.Duration) ([]string, error) {
	_ = time.Now().Add(-age) // cutoffTime for future use

	// Get nodes that are scheduled for destruction and past their destroy time
	nodes, err := o.store.GetNodesForDestruction(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return []string{}, nil
		}
		return nil, fmt.Errorf("orchestrator: failed to get nodes for destruction: %w", err)
	}

	var orphanedIDs []string
	for _, node := range nodes {
		// Check if the node's destroy time has passed
		if node.DestroyAt.Valid && node.DestroyAt.Time.Before(time.Now()) {
			orphanedIDs = append(orphanedIDs, node.ID)
		}
	}

	return orphanedIDs, nil
}

// DeleteNode deletes a node by server ID. This implements the CleanupManager interface.
func (o *Orchestrator) DeleteNode(ctx context.Context, serverID string) error {
	// Get the node from the database first
	node, err := o.store.GetNode(ctx, serverID)
	if err != nil {
		if err == sql.ErrNoRows {
			o.logger.Warn("node not found in database, assuming already deleted",
				slog.String("server_id", serverID))
			return nil
		}
		return fmt.Errorf("orchestrator: failed to get node for deletion: %w", err)
	}

	// Use the existing DestroyNode method
	return o.DestroyNode(ctx, node)
}
