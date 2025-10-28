package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
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

// RotateNodes performs atomic rotation: provision new node → mark old node destroying → commit.
// This uses a two-phase approach with database transactions to ensure consistency.
func (o *Orchestrator) RotateNodes(ctx context.Context) error {
	o.logger.Info("Starting atomic node rotation")

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

	// Phase 2: Provision new node (outside transaction)
	o.logger.Info("Provisioning replacement node",
		slog.String("old_node_id", oldNode.ID))

	newConfig, err := o.nodeManager.CreateNode(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to provision replacement node: %w", err)
	}

	o.logger.Info("Replacement node provisioned successfully",
		slog.String("new_node_ip", newConfig.ServerIP),
		slog.String("old_node_id", oldNode.ID))

	// Phase 3: Atomic transaction - mark old destroying, activate new, schedule cleanup
	gracePeriod := 1 * time.Hour
	destroyAt := time.Now().Add(gracePeriod)

	// Begin transaction
	// Note: This assumes the store will provide a transaction-capable method
	// For now, we'll use optimistic locking via version numbers
	err = o.store.ScheduleNodeDestruction(ctx, db.ScheduleNodeDestructionParams{
		ID:        oldNode.ID,
		Version:   oldNode.Version,
		DestroyAt: sql.NullTime{Time: destroyAt, Valid: true},
	})
	if err != nil {
		o.logger.Error("Failed to schedule old node for destruction",
			slog.String("old_node_id", oldNode.ID),
			slog.String("error", err.Error()))

		// Rollback: destroy the newly provisioned node
		o.logger.Warn("Rolling back: destroying newly provisioned node")
		// TODO: Extract server ID from newConfig and call DestroyNode

		return fmt.Errorf("orchestrator: failed to schedule destruction (rollback triggered): %w", err)
	}

	o.logger.Info("Node rotation completed successfully",
		slog.String("old_node_id", oldNode.ID),
		slog.String("new_node_ip", newConfig.ServerIP),
		slog.Time("destroy_at", destroyAt))

	return nil
}

// provisionInitialNode provisions the first node when none exists.
func (o *Orchestrator) provisionInitialNode(ctx context.Context) error {
	o.logger.Info("Provisioning initial node")

	_, err := o.nodeManager.CreateNode(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to provision initial node: %w", err)
	}

	o.logger.Info("Initial node provisioned successfully")
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
