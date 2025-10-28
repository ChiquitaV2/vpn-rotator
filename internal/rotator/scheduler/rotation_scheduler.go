package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// RotationScheduler handles periodic rotation checks based on configured intervals.
type RotationScheduler struct {
	rotationInterval time.Duration
	manager          RotationManager
	logger           *slog.Logger
}

// RotationManager defines the interface for rotation operations.
type RotationManager interface {
	ShouldRotate(ctx context.Context) (bool, error)
	RotateNodes(ctx context.Context) error
}

// NewRotationScheduler creates a new rotation scheduler.
func NewRotationScheduler(rotationInterval time.Duration, manager RotationManager, logger *slog.Logger) *RotationScheduler {
	return &RotationScheduler{
		rotationInterval: rotationInterval,
		manager:          manager,
		logger:           logger,
	}
}

// Start begins the rotation check loop. Blocks until ctx is canceled.
func (s *RotationScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.rotationInterval)
	defer ticker.Stop()

	s.logger.Info("Rotation scheduler started",
		slog.Duration("interval", s.rotationInterval))

	// Perform initial check immediately
	s.checkAndRotate(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Rotation scheduler stopped")
			return
		case <-ticker.C:
			s.checkAndRotate(ctx)
		}
	}
}

// checkAndRotate performs a single rotation check and initiates rotation if needed.
func (s *RotationScheduler) checkAndRotate(ctx context.Context) {
	s.logger.Debug("Checking if rotation is needed")

	shouldRotate, err := s.manager.ShouldRotate(ctx)
	if err != nil {
		s.logger.Error("Failed to check rotation status",
			slog.String("error", err.Error()))
		return
	}

	if !shouldRotate {
		s.logger.Debug("Rotation not needed")
		return
	}

	s.logger.Info("Initiating node rotation")
	if err := s.manager.RotateNodes(ctx); err != nil {
		s.logger.Error("Failed to rotate nodes",
			slog.String("error", err.Error()))
		return
	}

	s.logger.Info("Node rotation completed successfully")
}
