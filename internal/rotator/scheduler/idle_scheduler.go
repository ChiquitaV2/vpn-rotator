package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// IdleScheduler handles periodic checks for idle nodes.
type IdleScheduler struct {
	checkInterval time.Duration
	idleThreshold time.Duration
	manager       IdleManager
	logger        *slog.Logger
}

// IdleManager defines the interface for idle node operations.
type IdleManager interface {
	GetIdleNodes(ctx context.Context, idleThreshold time.Duration) ([]string, error)
	MarkNodeIdle(ctx context.Context, serverID string) error
}

// NewIdleScheduler creates a new idle node scheduler.
func NewIdleScheduler(checkInterval, idleThreshold time.Duration, manager IdleManager, logger *slog.Logger) *IdleScheduler {
	return &IdleScheduler{
		checkInterval: checkInterval,
		idleThreshold: idleThreshold,
		manager:       manager,
		logger:        logger,
	}
}

// Start begins the idle node check loop. Blocks until ctx is canceled.
func (s *IdleScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	s.logger.Info("Idle scheduler started",
		slog.Duration("check_interval", s.checkInterval),
		slog.Duration("idle_threshold", s.idleThreshold))

	// Perform initial check immediately
	s.checkIdleNodes(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Idle scheduler stopped")
			return
		case <-ticker.C:
			s.checkIdleNodes(ctx)
		}
	}
}

// checkIdleNodes performs a single check for idle nodes.
func (s *IdleScheduler) checkIdleNodes(ctx context.Context) {
	s.logger.Debug("Checking for idle nodes",
		slog.Duration("idle_threshold", s.idleThreshold))

	idleNodes, err := s.manager.GetIdleNodes(ctx, s.idleThreshold)
	if err != nil {
		s.logger.Error("Failed to get idle nodes",
			slog.String("error", err.Error()))
		return
	}

	if len(idleNodes) == 0 {
		s.logger.Debug("No idle nodes found")
		return
	}

	s.logger.Info("Found idle nodes",
		slog.Int("count", len(idleNodes)),
		slog.Any("server_ids", idleNodes))

	for _, serverID := range idleNodes {
		if err := s.manager.MarkNodeIdle(ctx, serverID); err != nil {
			s.logger.Error("Failed to mark node as idle",
				slog.String("server_id", serverID),
				slog.String("error", err.Error()))
		} else {
			s.logger.Info("Marked node as idle",
				slog.String("server_id", serverID))
		}
	}
}
