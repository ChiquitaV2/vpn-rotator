package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
)

// ShouldRotate checks if a rotation is needed based on the current node status.
// The rotation timing logic is now handled by the scheduler component.
func (o *Orchestrator) ShouldRotate(ctx context.Context) (bool, error) {
	_, err := o.store.GetActiveNode(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			// No active node exists, rotation needed to create one
			return true, nil
		}
		return false, fmt.Errorf("orchestrator: failed to get active node: %w", err)
	}

	// Node exists and is active - scheduler determines timing
	return false, nil
}

// RotateNodes performs atomic rotation with peer migration: provision new node → migrate peers → mark old node destroying → commit.
// This uses a multi-phase approach with rollback mechanisms to ensure peer continuity.
func (o *Orchestrator) RotateNodes(ctx context.Context) error {
	o.logger.Info("Starting atomic node rotation with peer migration")

	// Phase 1: Get current active node
	oldNode, err := o.store.GetActiveNode(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			// No active node, just provision a new one
			o.logger.Info("No active node found, provisioning initial node")
			return o.provisionInitialNode(ctx)
		}
		return fmt.Errorf("orchestrator: failed to get active node: %w", err)
	}

	// Phase 2: Check if old node has active peers
	activePeers, err := o.store.GetPeersForMigration(ctx, oldNode.ID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get peers for migration: %w", err)
	}

	o.logger.Info("Found peers to migrate during rotation",
		slog.String("old_node_id", oldNode.ID),
		slog.Int("peer_count", len(activePeers)))

	// Phase 3: Provision new node (outside transaction)
	o.logger.Info("Provisioning replacement node",
		slog.String("old_node_id", oldNode.ID))

	newConfig, err := o.nodeManager.CreateNode(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to provision replacement node: %w", err)
	}

	// Get the new node ID from database to use for peer migration
	newNode, err := o.store.GetLatestNode(ctx)
	if err != nil {
		// Rollback: destroy the newly provisioned node
		o.rollbackNodeCreation(ctx, newConfig, "failed to get new node from database")
		return fmt.Errorf("orchestrator: failed to get new node from database: %w", err)
	}

	o.logger.Info("Replacement node provisioned successfully",
		slog.String("new_node_id", newNode.ID),
		slog.String("new_node_ip", newConfig.ServerIP),
		slog.String("old_node_id", oldNode.ID))

	// Phase 4: Allocate subnet for new node if peers need to be migrated
	if len(activePeers) > 0 {
		_, err = o.ipManager.AllocateNodeSubnet(ctx, newNode.ID)
		if err != nil {
			o.rollbackNodeCreation(ctx, newConfig, "failed to allocate subnet for new node")
			return fmt.Errorf("orchestrator: failed to allocate subnet for new node: %w", err)
		}

		o.logger.Info("Allocated subnet for new node", slog.String("new_node_id", newNode.ID))
	}

	// Phase 5: Migrate peers from old node to new node
	if len(activePeers) > 0 {
		o.logger.Info("Starting peer migration",
			slog.String("old_node_id", oldNode.ID),
			slog.String("new_node_id", newNode.ID),
			slog.Int("peer_count", len(activePeers)))

		err = o.MigratePeersFromNode(ctx, oldNode.ID, newNode.ID)
		if err != nil {
			o.logger.Error("Peer migration failed during rotation",
				slog.String("old_node_id", oldNode.ID),
				slog.String("new_node_id", newNode.ID),
				slog.String("error", err.Error()))

			// Rollback: destroy new node and keep old node active
			o.rollbackRotationWithPeerMigration(ctx, newNode, oldNode, "peer migration failed")
			return fmt.Errorf("orchestrator: peer migration failed during rotation: %w", err)
		}

		o.logger.Info("Peer migration completed successfully",
			slog.String("old_node_id", oldNode.ID),
			slog.String("new_node_id", newNode.ID),
			slog.Int("migrated_peers", len(activePeers)))
	}

	// Phase 6: Atomic transaction - mark old destroying, schedule cleanup
	gracePeriod := 1 * time.Hour
	destroyAt := time.Now().Add(gracePeriod)

	err = o.store.ScheduleNodeDestruction(ctx, db.ScheduleNodeDestructionParams{
		ID:        oldNode.ID,
		Version:   oldNode.Version,
		DestroyAt: sql.NullTime{Time: destroyAt, Valid: true},
	})
	if err != nil {
		o.logger.Error("Failed to schedule old node for destruction",
			slog.String("old_node_id", oldNode.ID),
			slog.String("error", err.Error()))

		// Rollback: migrate peers back and destroy new node
		if len(activePeers) > 0 {
			o.rollbackRotationWithPeerMigration(ctx, newNode, oldNode, "failed to schedule destruction")
		} else {
			o.rollbackNodeCreation(ctx, newConfig, "failed to schedule destruction")
		}

		return fmt.Errorf("orchestrator: failed to schedule destruction (rollback triggered): %w", err)
	}

	o.logger.Info("Node rotation completed successfully",
		slog.String("old_node_id", oldNode.ID),
		slog.String("new_node_id", newNode.ID),
		slog.String("new_node_ip", newConfig.ServerIP),
		slog.Int("migrated_peers", len(activePeers)),
		slog.Time("destroy_at", destroyAt))

	return nil
}

// provisionInitialNode provisions the first node when none exists.
func (o *Orchestrator) provisionInitialNode(ctx context.Context) error {
	o.logger.Info("Provisioning initial node")

	newConfig, err := o.nodeManager.CreateNode(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to provision initial node: %w", err)
	}

	// Get the new node from database
	newNode, err := o.store.GetLatestNode(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get initial node from database: %w", err)
	}

	// Allocate subnet for the initial node
	_, err = o.ipManager.AllocateNodeSubnet(ctx, newNode.ID)
	if err != nil {
		o.logger.Error("Failed to allocate subnet for initial node",
			slog.String("node_id", newNode.ID),
			slog.String("error", err.Error()))
		// Continue without failing - subnet can be allocated later if needed
	}

	o.logger.Info("Initial node provisioned successfully",
		slog.String("node_id", newNode.ID),
		slog.String("node_ip", newConfig.ServerIP))
	return nil
}

// CleanupNodes destroys nodes scheduled for destruction.
func (o *Orchestrator) CleanupNodes(ctx context.Context) error {
	nodes, err := o.store.GetNodesForDestruction(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("orchestrator: failed to get nodes for destruction: %w", err)
	}

	if len(nodes) == 0 {
		o.logger.Info("no nodes to cleanup")
		return nil
	}

	for _, node := range nodes {
		o.logger.Info("cleaning up node", slog.String("node_id", node.ID))
		if err := o.DestroyNode(ctx, node); err != nil {
			o.logger.Error("failed to destroy node",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
		}
	}
	return nil
}

// rollbackNodeCreation rolls back a newly created node when rotation fails
func (o *Orchestrator) rollbackNodeCreation(ctx context.Context, nodeConfig *nodemanager.NodeConfig, reason string) {
	o.logger.Warn("Rolling back node creation",
		slog.String("node_ip", nodeConfig.ServerIP),
		slog.String("reason", reason))

	// Get the node from database to destroy it
	newNode, err := o.store.GetLatestNode(ctx)
	if err != nil {
		o.logger.Error("Failed to get new node for rollback",
			slog.String("node_ip", nodeConfig.ServerIP),
			slog.String("error", err.Error()))
		return
	}

	// Destroy the node
	if err := o.DestroyNode(ctx, newNode); err != nil {
		o.logger.Error("Failed to destroy node during rollback",
			slog.String("node_id", newNode.ID),
			slog.String("node_ip", nodeConfig.ServerIP),
			slog.String("error", err.Error()))
	} else {
		o.logger.Info("Successfully rolled back node creation",
			slog.String("node_id", newNode.ID),
			slog.String("node_ip", nodeConfig.ServerIP))
	}
}

// rollbackRotationWithPeerMigration rolls back a rotation that included peer migration
func (o *Orchestrator) rollbackRotationWithPeerMigration(ctx context.Context, newNode, oldNode db.Node, reason string) {
	o.logger.Warn("Rolling back rotation with peer migration",
		slog.String("new_node_id", newNode.ID),
		slog.String("old_node_id", oldNode.ID),
		slog.String("reason", reason))

	// Step 1: Migrate peers back from new node to old node
	o.logger.Info("Migrating peers back to old node during rollback",
		slog.String("new_node_id", newNode.ID),
		slog.String("old_node_id", oldNode.ID))

	err := o.MigratePeersFromNode(ctx, newNode.ID, oldNode.ID)
	if err != nil {
		o.logger.Error("Failed to migrate peers back during rollback",
			slog.String("new_node_id", newNode.ID),
			slog.String("old_node_id", oldNode.ID),
			slog.String("error", err.Error()))
		// Continue with rollback even if peer migration fails
	} else {
		o.logger.Info("Successfully migrated peers back during rollback",
			slog.String("new_node_id", newNode.ID),
			slog.String("old_node_id", oldNode.ID))
	}

	// Step 2: Cancel any scheduled destruction of old node
	err = o.store.CancelNodeDestruction(ctx, db.CancelNodeDestructionParams{
		ID:      oldNode.ID,
		Version: oldNode.Version,
	})
	if err != nil {
		o.logger.Error("Failed to cancel old node destruction during rollback",
			slog.String("old_node_id", oldNode.ID),
			slog.String("error", err.Error()))
	} else {
		o.logger.Info("Cancelled old node destruction during rollback",
			slog.String("old_node_id", oldNode.ID))
	}

	// Step 3: Release subnet allocated to new node
	err = o.ipManager.ReleaseNodeSubnet(ctx, newNode.ID)
	if err != nil {
		o.logger.Error("Failed to release new node subnet during rollback",
			slog.String("new_node_id", newNode.ID),
			slog.String("error", err.Error()))
	}

	// Step 4: Destroy the new node
	if err := o.DestroyNode(ctx, newNode); err != nil {
		o.logger.Error("Failed to destroy new node during rollback",
			slog.String("new_node_id", newNode.ID),
			slog.String("error", err.Error()))
	} else {
		o.logger.Info("Successfully destroyed new node during rollback",
			slog.String("new_node_id", newNode.ID))
	}

	o.logger.Info("Rollback completed - old node remains active",
		slog.String("old_node_id", oldNode.ID))
}

// ValidateRotationPreconditions checks if rotation can proceed safely
func (o *Orchestrator) ValidateRotationPreconditions(ctx context.Context) error {
	o.logger.Debug("Validating rotation preconditions")

	// Check if there's an active node
	activeNode, err := o.store.GetActiveNode(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // No active node is fine, we'll provision initial node
		}
		return fmt.Errorf("failed to get active node: %w", err)
	}

	// Check if there are any nodes already scheduled for destruction
	scheduledNodes, err := o.store.GetNodesScheduledForDestruction(ctx)
	if err != nil {
		return fmt.Errorf("failed to check scheduled nodes: %w", err)
	}

	if len(scheduledNodes) > 0 {
		return fmt.Errorf("rotation blocked: %d nodes already scheduled for destruction", len(scheduledNodes))
	}

	// Check if there are any orphaned nodes that might interfere
	orphanedNodes, err := o.store.GetOrphanedNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to check orphaned nodes: %w", err)
	}

	if len(orphanedNodes) > 0 {
		o.logger.Warn("Found orphaned nodes that may interfere with rotation",
			slog.Int("orphaned_count", len(orphanedNodes)))
		// Don't block rotation, but warn about potential issues
	}

	// Validate node health before rotation
	err = o.nodeManager.GetNodeHealth(ctx, activeNode.IpAddress)
	if err != nil {
		o.logger.Warn("Active node health check failed before rotation",
			slog.String("node_id", activeNode.ID),
			slog.String("node_ip", activeNode.IpAddress),
			slog.String("error", err.Error()))
		// Don't block rotation for health issues - rotation might fix them
	}

	o.logger.Debug("Rotation preconditions validated successfully")
	return nil
}

// GetRotationStatus returns the current status of any ongoing rotation
func (o *Orchestrator) GetRotationStatus(ctx context.Context) (*RotationStatus, error) {
	// Get active node
	activeNode, err := o.store.GetActiveNode(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get active node: %w", err)
	}

	// Get nodes scheduled for destruction
	scheduledNodes, err := o.store.GetNodesScheduledForDestruction(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduled nodes: %w", err)
	}

	// Get provisioning nodes
	provisioningNodes, err := o.store.GetNodesByStatus(ctx, "provisioning")
	if err != nil {
		return nil, fmt.Errorf("failed to get provisioning nodes: %w", err)
	}

	status := &RotationStatus{
		HasActiveNode:      activeNode.ID != "",
		ProvisioningNodes:  len(provisioningNodes),
		ScheduledNodes:     len(scheduledNodes),
		RotationInProgress: len(provisioningNodes) > 0 || len(scheduledNodes) > 0,
	}

	if activeNode.ID != "" {
		status.ActiveNodeID = activeNode.ID
		status.ActiveNodeIP = activeNode.IpAddress

		// Get peer count for active node
		peerCount, err := o.store.CountActivePeersByNode(ctx, activeNode.ID)
		if err != nil {
			o.logger.Warn("Failed to get peer count for active node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
		} else {
			status.ActiveNodePeerCount = int(peerCount)
		}
	}

	return status, nil
}
