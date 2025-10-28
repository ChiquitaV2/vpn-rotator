package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// CleanupScheduler handles periodic cleanup of orphaned resources.
type CleanupScheduler struct {
	checkInterval time.Duration
	cleanupAge    time.Duration
	manager       CleanupManager
	logger        *slog.Logger
}

// CleanupManager defines the interface for cleanup operations.
type CleanupManager interface {
	GetOrphanedNodes(ctx context.Context, age time.Duration) ([]string, error)
	DeleteNode(ctx context.Context, serverID string) error
}

// NewCleanupScheduler creates a new cleanup scheduler.
func NewCleanupScheduler(checkInterval, cleanupAge time.Duration, manager CleanupManager, logger *slog.Logger) *CleanupScheduler {
	return &CleanupScheduler{
		checkInterval: checkInterval,
		cleanupAge:    cleanupAge,
		manager:       manager,
		logger:        logger,
	}
}

// Start begins the cleanup check loop. Blocks until ctx is canceled.
func (s *CleanupScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	s.logger.Info("Cleanup scheduler started",
		slog.Duration("check_interval", s.checkInterval),
		slog.Duration("cleanup_age", s.cleanupAge))

	// Perform initial cleanup immediately
	s.cleanupOrphanedNodes(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Cleanup scheduler stopped")
			return
		case <-ticker.C:
			s.cleanupOrphanedNodes(ctx)
		}
	}
}

// cleanupOrphanedNodes performs a single cleanup operation.
func (s *CleanupScheduler) cleanupOrphanedNodes(ctx context.Context) {
	s.logger.Debug("Checking for orphaned nodes",
		slog.Duration("cleanup_age", s.cleanupAge))

	orphanedNodes, err := s.manager.GetOrphanedNodes(ctx, s.cleanupAge)
	if err != nil {
		s.logger.Error("Failed to get orphaned nodes",
			slog.String("error", err.Error()))
		return
	}

	if len(orphanedNodes) == 0 {
		s.logger.Debug("No orphaned nodes found")
		return
	}

	s.logger.Info("Found orphaned nodes",
		slog.Int("count", len(orphanedNodes)),
		slog.Any("server_ids", orphanedNodes))

	for _, serverID := range orphanedNodes {
		if err := s.manager.DeleteNode(ctx, serverID); err != nil {
			s.logger.Error("Failed to delete orphaned node",
				slog.String("server_id", serverID),
				slog.String("error", err.Error()))
		} else {
			s.logger.Info("Deleted orphaned node",
				slog.String("server_id", serverID))
		}
	}
}
