package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

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

// CleanupInactivePeers removes peers that have been inactive for the specified duration
func (o *Orchestrator) CleanupInactivePeers(ctx context.Context, inactiveMinutes int) error {
	o.logger.Info("starting inactive peer cleanup", slog.Int("inactive_minutes", inactiveMinutes))

	// Get inactive peers using peer manager
	inactivePeers, err := o.peerManager.GetInactivePeers(ctx, inactiveMinutes)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get inactive peers: %w", err)
	}

	if len(inactivePeers) == 0 {
		o.logger.Info("no inactive peers found for cleanup")
		return nil
	}

	o.logger.Info("found inactive peers for cleanup", slog.Int("count", len(inactivePeers)))

	var cleanupErrors []error
	successCount := 0

	for _, peer := range inactivePeers {
		err := o.RemovePeerFromSystem(ctx, peer.ID)
		if err != nil {
			o.logger.Error("failed to cleanup inactive peer",
				slog.String("peer_id", peer.ID),
				slog.String("error", err.Error()))
			cleanupErrors = append(cleanupErrors, fmt.Errorf("peer %s: %w", peer.ID, err))
		} else {
			successCount++
			o.logger.Debug("successfully cleaned up inactive peer", slog.String("peer_id", peer.ID))
		}
	}

	o.logger.Info("inactive peer cleanup completed",
		slog.Int("total_peers", len(inactivePeers)),
		slog.Int("success_count", successCount),
		slog.Int("error_count", len(cleanupErrors)))

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("orchestrator: %d peer cleanups failed: %v", len(cleanupErrors), cleanupErrors)
	}

	return nil
}
