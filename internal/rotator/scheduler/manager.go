package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SchedulerManager defines the interface for scheduler lifecycle management
type SchedulerManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
}

// VPNService defines the interface for VPN service operations needed by the scheduler
type VPNService interface {
	RotateNodes(ctx context.Context) error
	CleanupInactiveResources(ctx context.Context) error
}

// UnifiedManager manages a single unified scheduler for both rotation and cleanup operations
type UnifiedManager struct {
	scheduler *UnifiedScheduler
	logger    *slog.Logger

	// Internal state for lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	mu      sync.RWMutex
}

// NewUnifiedManager creates a new scheduler manager using the unified scheduler approach
func NewUnifiedManager(
	rotationInterval, cleanupInterval, cleanupAge time.Duration,
	vpnService VPNService,
	logger *slog.Logger,
) *UnifiedManager {
	return &UnifiedManager{
		scheduler: NewUnifiedScheduler(rotationInterval, cleanupInterval, cleanupAge, vpnService, logger),
		logger:    logger,
	}
}

// Start initializes and starts the unified scheduler
func (m *UnifiedManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		m.logger.Warn("scheduler manager is already running")
		return nil
	}

	m.logger.Info("starting scheduler manager")

	// Create a new context for the scheduler
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start unified scheduler
	if err := m.scheduler.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	m.running = true
	m.logger.Info("scheduler manager started successfully")
	return nil
}

// Stop gracefully shuts down the unified scheduler
func (m *UnifiedManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		m.logger.Warn("scheduler manager is not running")
		return nil
	}

	m.logger.Info("stopping scheduler manager")

	// Cancel the scheduler context to signal shutdown
	if m.cancel != nil {
		m.cancel()
	}

	// Stop the unified scheduler
	if err := m.scheduler.Stop(ctx); err != nil {
		m.logger.Error("failed to stop scheduler", "error", err)
		return err
	}

	m.running = false
	m.logger.Info("scheduler manager stopped successfully")
	return nil
}

// IsRunning returns whether the scheduler manager is currently running
func (m *UnifiedManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}
