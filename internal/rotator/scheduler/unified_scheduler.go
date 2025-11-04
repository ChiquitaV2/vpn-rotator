package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// UnifiedScheduler handles both rotation and cleanup operations using a VPN service
type UnifiedScheduler struct {
	rotationInterval time.Duration
	cleanupInterval  time.Duration
	cleanupAge       time.Duration
	vpnService       VPNService
	logger           *slog.Logger

	// Internal state
	rotationTicker *time.Ticker
	cleanupTicker  *time.Ticker
	stopChan       chan struct{}
	running        bool
}

// NewUnifiedScheduler creates a new unified scheduler that uses VPN service directly
func NewUnifiedScheduler(
	rotationInterval time.Duration,
	cleanupInterval time.Duration,
	cleanupAge time.Duration,
	vpnService VPNService,
	logger *slog.Logger,
) *UnifiedScheduler {
	return &UnifiedScheduler{
		rotationInterval: rotationInterval,
		cleanupInterval:  cleanupInterval,
		cleanupAge:       cleanupAge,
		vpnService:       vpnService,
		logger:           logger,
		stopChan:         make(chan struct{}),
	}
}

// Start begins both rotation and cleanup scheduling loops
func (s *UnifiedScheduler) Start(ctx context.Context) error {
	if s.running {
		s.logger.Warn("unified scheduler is already running")
		return nil
	}

	s.logger.Info("starting unified scheduler",
		slog.Duration("rotation_interval", s.rotationInterval),
		slog.Duration("cleanup_interval", s.cleanupInterval),
		slog.Duration("cleanup_age", s.cleanupAge))

	s.running = true

	// Create tickers for both operations
	s.rotationTicker = time.NewTicker(s.rotationInterval)
	s.cleanupTicker = time.NewTicker(s.cleanupInterval)

	// Start the main scheduling loop
	go s.schedulingLoop(ctx)

	// Perform initial operations immediately
	go s.performRotationCheck(ctx)
	go s.performCleanupCheck(ctx)

	s.logger.Info("unified scheduler started successfully")
	return nil
}

// Stop gracefully shuts down the scheduler
func (s *UnifiedScheduler) Stop(ctx context.Context) error {
	if !s.running {
		s.logger.Warn("unified scheduler is not running")
		return nil
	}

	s.logger.Info("stopping unified scheduler")

	// Stop tickers
	if s.rotationTicker != nil {
		s.rotationTicker.Stop()
	}
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}

	// Signal stop and wait for scheduling loop to finish
	close(s.stopChan)
	s.running = false

	s.logger.Info("unified scheduler stopped successfully")
	return nil
}

// IsRunning returns whether the scheduler is currently running
func (s *UnifiedScheduler) IsRunning() bool {
	return s.running
}

// schedulingLoop is the main loop that handles both rotation and cleanup scheduling
func (s *UnifiedScheduler) schedulingLoop(ctx context.Context) {
	defer func() {
		if s.rotationTicker != nil {
			s.rotationTicker.Stop()
		}
		if s.cleanupTicker != nil {
			s.cleanupTicker.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("unified scheduler stopping due to context cancellation")
			return

		case <-s.stopChan:
			s.logger.Info("unified scheduler stopping due to stop signal")
			return

		case <-s.rotationTicker.C:
			go s.performRotationCheck(ctx)

		case <-s.cleanupTicker.C:
			go s.performCleanupCheck(ctx)
		}
	}
}

// performRotationCheck performs a single rotation check and initiates rotation if needed
func (s *UnifiedScheduler) performRotationCheck(ctx context.Context) {
	s.logger.Debug("performing rotation check")

	startTime := time.Now()
	err := s.vpnService.RotateNodes(ctx)
	duration := time.Since(startTime)

	if err != nil {
		s.logger.Error("rotation check failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", duration))
		return
	}

	s.logger.Info("rotation check completed successfully",
		slog.Duration("duration", duration))
}

// performCleanupCheck performs a single cleanup operation
func (s *UnifiedScheduler) performCleanupCheck(ctx context.Context) {
	s.logger.Debug("performing cleanup check")

	startTime := time.Now()
	err := s.vpnService.CleanupInactiveResources(ctx)
	duration := time.Since(startTime)

	if err != nil {
		s.logger.Error("cleanup check failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", duration))
		return
	}

	s.logger.Info("cleanup check completed successfully",
		slog.Duration("duration", duration))
}
