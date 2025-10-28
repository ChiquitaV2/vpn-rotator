package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Manager coordinates multiple schedulers and provides unified lifecycle management.
type Manager struct {
	rotationScheduler *RotationScheduler
	cleanupScheduler  *CleanupScheduler
	logger            *slog.Logger

	// Internal state for lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	running bool
}

// ManagerInterface defines the interface for scheduler lifecycle management.
type ManagerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
}

// NewManager creates a new scheduler manager that coordinates rotation and cleanup schedulers.
func NewManager(
	rotationInterval time.Duration,
	cleanupInterval time.Duration,
	cleanupAge time.Duration,
	rotationManager RotationManager,
	cleanupManager CleanupManager,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		rotationScheduler: NewRotationScheduler(rotationInterval, rotationManager, logger),
		cleanupScheduler:  NewCleanupScheduler(cleanupInterval, cleanupAge, cleanupManager, logger),
		logger:            logger,
	}
}

// Start initializes and starts both rotation and cleanup schedulers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		m.logger.Warn("Scheduler manager is already running")
		return nil
	}

	m.logger.Info("Starting scheduler manager")

	// Create a new context for the schedulers
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start rotation scheduler
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.logger.Debug("Starting rotation scheduler")
		m.rotationScheduler.Start(m.ctx)
	}()

	// Start cleanup scheduler
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.logger.Debug("Starting cleanup scheduler")
		m.cleanupScheduler.Start(m.ctx)
	}()

	m.running = true
	m.logger.Info("Scheduler manager started successfully")

	return nil
}

// Stop gracefully shuts down both schedulers and waits for them to complete.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		m.logger.Warn("Scheduler manager is not running")
		return nil
	}

	m.logger.Info("Stopping scheduler manager")

	// Cancel the scheduler context to signal shutdown
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for schedulers to finish with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("Scheduler manager stopped successfully")
	case <-ctx.Done():
		m.logger.Warn("Scheduler manager stop timed out")
		return ctx.Err()
	}

	m.running = false
	return nil
}

// IsRunning returns whether the scheduler manager is currently running.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}
