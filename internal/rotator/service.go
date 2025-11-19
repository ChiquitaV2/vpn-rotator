package rotator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/api"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/scheduler"
	"github.com/chiquitav2/vpn-rotator/pkg/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// SchedulerManager defines the interface for scheduler operations
type SchedulerManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
}

// APIServer defines the interface for API server operations
type APIServer interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Service coordinates all rotator service components and manages their lifecycle
type Service struct {
	config     *config.Config
	components *ServiceComponents
	scheduler  SchedulerManager
	apiServer  APIServer
	logger     *logger.Logger

	// Internal state for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Signal handling and graceful shutdown
	signalChan            chan os.Signal
	shutdownWg            sync.WaitGroup
	isRunning             bool
	mu                    sync.RWMutex
	disableSignalHandling bool // For testing
}

// NewService creates a new Service instance using the layered architecture
func NewService(cfg *config.Config, logger *logger.Logger) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:     cfg,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		signalChan: make(chan os.Signal, 1),
	}

	// Initialize components using service factory
	if err := service.initializeComponents(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize service components: %w", err)
	}

	return service, nil
}

// initializeComponents creates and wires all service components using the layered architecture
func (s *Service) initializeComponents() error {
	s.logger.Info("initializing service components with layered architecture")

	// Create service factory
	factory := NewServiceFactory(s.config, s.logger)

	// Create all components with proper dependency injection
	components, err := factory.CreateComponents()
	if err != nil {
		return fmt.Errorf("failed to create service components: %w", err)
	}

	s.components = components

	// Initialize scheduler using application services
	if err := s.initializeScheduler(); err != nil {
		return fmt.Errorf("failed to initialize scheduler: %w", err)
	}

	// Initialize API server using application services
	if err := s.initializeAPIServer(); err != nil {
		return fmt.Errorf("failed to initialize API server: %w", err)
	}

	s.logger.Info("all service components initialized successfully")
	return nil
}

// initializeScheduler creates the scheduler using application services
func (s *Service) initializeScheduler() error {
	s.logger.Debug("initializing scheduler with application services")

	// Create unified scheduler that uses SchedulerAdapter for rotation and cleanup
	// Use internal defaults for scheduler configuration
	schedulerDefaults := config.NewInternalDefaults().SchedulerDefaults()
	schedulerManager := scheduler.NewUnifiedManager(
		s.config.Rotation.Interval,             // User-configurable rotation interval
		schedulerDefaults.CleanupCheckInterval, // Internal default cleanup check interval
		s.config.Rotation.CleanupAge,           // User-configurable cleanup age
		s.components.SchedulerAdapter, s.logger,
	)

	s.scheduler = schedulerManager
	s.logger.Debug("scheduler initialized successfully")
	return nil
}

// initializeAPIServer creates the API server using application services
func (s *Service) initializeAPIServer() error {
	s.logger.Debug("initializing API server with services")

	// Create API server - uses services directly where they're simple pass-throughs
	apiServer := api.NewServer(
		api.ServerConfig{
			Address:     s.config.API.ListenAddr,
			CORSOrigins: s.config.API.CORSOrigins,
		},
		s.components.PeerConnectionSvr,
		s.components.NodeOrchestrator,
		s.components.ResourceCleanupService,
		s.components.AdminOrchestrator,
		s.logger,
	)

	s.apiServer = apiServer
	s.logger.Debug("API server initialized successfully")
	return nil
}

// Start initializes and starts all service components in proper dependency order
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return errors.NewSystemError(errors.ErrCodeValidation, "service is already running", false, nil)
	}

	op := s.logger.StartOp(ctx, "service_start")

	// Set up signal handling for graceful shutdown (unless disabled for testing)
	if !s.disableSignalHandling {
		s.setupSignalHandling()
	}

	// Start components in dependency order
	// 1. Start provisioning orchestrator worker if available
	if s.components.ProvisioningService != nil {
		op.Progress("starting component", slog.String("component", "provisioning_service"))

		s.components.ProvisioningService.StartWorker(s.ctx)
	}

	// 2. manager is a dependency for scheduler, so it should be ready first
	s.logger.InfoContext(ctx, "orchestrator ready (no startup required)")

	// 3. Start scheduler (depends on orchestrator)
	op.Progress("starting component", slog.String("component", "scheduler"))

	if err := s.scheduler.Start(s.ctx); err != nil {
		schedulerErr := errors.NewSystemError(errors.ErrCodeInternal, "failed to start scheduler", true, err)
		s.logger.ErrorCtx(ctx, "scheduler startup failed", schedulerErr)
		op.Fail(schedulerErr, "scheduler startup failed")
		return schedulerErr
	}
	s.logger.InfoContext(ctx, "scheduler started successfully")

	// 4. Start API server (depends on orchestrator)
	op.Progress("starting component", slog.String("component", "api_server"))

	if err := s.apiServer.Start(s.ctx); err != nil {
		apiErr := errors.NewSystemError(errors.ErrCodeInternal, "failed to start API server", true, err)
		s.logger.ErrorCtx(ctx, "API server startup failed", apiErr)

		// If API server fails, stop scheduler before returning error
		if stopErr := s.scheduler.Stop(s.ctx); stopErr != nil {
			cleanupErr := errors.NewSystemError(errors.ErrCodeInternal, "failed to stop scheduler during cleanup", false, stopErr)
			s.logger.ErrorCtx(ctx, "cleanup failed during API server startup failure", cleanupErr)
		}

		op.Fail(apiErr, "api server startup failed")
		return apiErr
	}

	s.isRunning = true
	op.Complete("rotator service started successfully")
	return nil
}

// setupSignalHandling configures signal handling for graceful shutdown
func (s *Service) setupSignalHandling() {
	// Register for shutdown signals
	signal.Notify(s.signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler goroutine
	s.shutdownWg.Add(1)
	go s.handleSignals()
}

// handleSignals processes shutdown signals and initiates graceful shutdown
func (s *Service) handleSignals() {
	defer s.shutdownWg.Done()

	select {
	case sig := <-s.signalChan:
		signalCtx := logger.WithOperation(context.Background(), "signal_handling")
		s.logger.InfoContext(signalCtx, "received shutdown signal", "signal", sig.String())

		// Create shutdown context with timeout
		shutdownTimeout := 30 * time.Second
		if s.config != nil && s.config.Service.ShutdownTimeout > 0 {
			shutdownTimeout = s.config.Service.ShutdownTimeout
		}

		shutdownCtx, cancel := context.WithTimeout(signalCtx, shutdownTimeout)
		defer cancel()

		shutdownOp := s.logger.StartOp(shutdownCtx, "graceful_shutdown", slog.String("trigger", "signal"), slog.String("signal", sig.String()))

		// Initiate graceful shutdown
		if err := s.Stop(shutdownCtx); err != nil {
			s.logger.ErrorCtx(shutdownCtx, "error during graceful shutdown", err)
			shutdownOp.Fail(err, "graceful shutdown failed")
		} else {
			shutdownOp.Complete("graceful shutdown completed successfully")
		}

	case <-s.ctx.Done():
		// Service context was cancelled, exit gracefully
		cancelCtx := logger.WithOperation(context.Background(), "context_cancellation")
		s.logger.DebugContext(cancelCtx, "signal handler exiting due to service context cancellation")
	}
}

// WaitForShutdown blocks until the service receives a shutdown signal or context is cancelled
func (s *Service) WaitForShutdown() {
	s.logger.Info("service running, waiting for shutdown signal")

	// Wait for signal handler to complete
	s.shutdownWg.Wait()

	s.logger.Info("service shutdown complete")
}

// Stop gracefully shuts down all service components with proper cleanup order
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		s.logger.WarnContext(ctx, "service is not running")
		return nil
	}

	op := s.logger.StartOp(ctx, "service_stop")

	// Create shutdown timeout context if not already provided
	shutdownCtx := ctx
	if ctx == context.Background() || ctx == nil {
		shutdownTimeout := 30 * time.Second
		if s.config != nil && s.config.Service.ShutdownTimeout > 0 {
			shutdownTimeout = s.config.Service.ShutdownTimeout
		}

		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
	}

	var lastErr error

	// Stop signal handling first (if it was enabled)
	if !s.disableSignalHandling {
		signal.Stop(s.signalChan)
		close(s.signalChan)
	}

	// Stop components in reverse dependency order
	// 1. Stop API server first (external interface)
	if s.apiServer != nil {
		op.Progress("stopping component", slog.String("component", "api_server"))

		if err := s.apiServer.Stop(shutdownCtx); err != nil {
			apiErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal, "failed to stop API server", false)
			s.logger.ErrorCtx(shutdownCtx, "API server shutdown failed", apiErr)
			lastErr = apiErr
		} else {
			s.logger.InfoContext(shutdownCtx, "API server stopped successfully")
		}
	}

	// 2. Stop scheduler
	if s.scheduler != nil {
		op.Progress("stopping component", slog.String("component", "scheduler"))

		if err := s.scheduler.Stop(shutdownCtx); err != nil {
			schedulerErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal, "failed to stop scheduler", false)
			s.logger.ErrorCtx(shutdownCtx, "scheduler shutdown failed", schedulerErr)
			lastErr = schedulerErr
		} else {
			s.logger.InfoContext(shutdownCtx, "scheduler stopped successfully")
		}
	}

	// 3. Clean up active nodes before shutdown
	if s.components.PeerConnectionSvr != nil {
		op.Progress("stopping component", slog.String("component", "node_cleanup"))

		if err := s.cleanupActiveNodes(shutdownCtx); err != nil {
			cleanupErr := errors.WrapWithDomain(err, errors.DomainNode, errors.ErrCodeInternal, "failed to cleanup active nodes", false)
			s.logger.ErrorCtx(shutdownCtx, "node cleanup failed", cleanupErr)
			lastErr = cleanupErr
		} else {
			s.logger.InfoContext(shutdownCtx, "active nodes cleaned up successfully")
		}
	}

	// 4. Close event bus
	if s.components != nil && s.components.EventBus != nil {
		op.Progress("stopping component", slog.String("component", "event_bus"))

		if err := s.components.EventBus.Close(); err != nil {
			eventErr := errors.WrapWithDomain(err, errors.DomainEvent, errors.ErrCodeInternal, "failed to close event bus", false)
			s.logger.ErrorCtx(shutdownCtx, "event bus shutdown failed", eventErr)
			lastErr = eventErr
		} else {
			s.logger.InfoContext(shutdownCtx, "event bus closed successfully")
		}
	}

	// 5. Close database store
	if s.components != nil && s.components.Store != nil {
		op.Progress("stopping component", slog.String("component", "database_store"))

		if err := s.components.Store.Close(); err != nil {
			dbErr := errors.WrapWithDomain(err, errors.DomainDatabase, errors.ErrCodeDatabase, "failed to close database store", false)
			s.logger.ErrorCtx(shutdownCtx, "database store shutdown failed", dbErr)
			lastErr = dbErr
		} else {
			s.logger.InfoContext(shutdownCtx, "database store closed successfully")
		}
	}

	// 6. Cancel service context to signal any remaining goroutines
	s.cancel()
	s.logger.InfoContext(shutdownCtx, "service context cancelled")

	// 7. Wait for all background goroutines to finish
	s.logger.DebugContext(shutdownCtx, "waiting for background goroutines to finish")
	done := make(chan struct{})
	go func() {
		s.shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.DebugContext(shutdownCtx, "all background goroutines finished")
	case <-shutdownCtx.Done():
		timeoutErr := errors.NewSystemError(errors.ErrCodeTimeout, "timeout waiting for background goroutines to finish", false, shutdownCtx.Err())
		s.logger.ErrorCtx(shutdownCtx, "shutdown timeout reached", timeoutErr)
		if lastErr == nil {
			lastErr = timeoutErr
		}
	}

	s.isRunning = false

	if lastErr != nil {
		op.Fail(lastErr, "service shutdown completed with errors")
		return errors.WrapWithDomain(lastErr, errors.DomainSystem, errors.ErrCodeInternal, "service shutdown completed with errors", false)
	}

	op.Complete("rotator service stopped successfully")
	return nil
}

// Health checks the health of all service components
func (s *Service) Health() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := logger.WithOperation(context.Background(), "service_health_check")

	if !s.isRunning {
		return errors.NewSystemError(errors.ErrCodeValidation, "service is not running", false, nil)
	}

	// Check if the service context is still active
	select {
	case <-s.ctx.Done():
		return errors.NewSystemError(errors.ErrCodeInternal, "service context cancelled", false, s.ctx.Err())
	default:
		// Optionally check database connectivity
		if s.components != nil && s.components.Store != nil {
			if err := s.components.Store.Ping(ctx); err != nil {
				return errors.WrapWithDomain(err, errors.DomainDatabase, errors.ErrCodeDatabase, "database health check failed", true)
			}
		}
		return nil
	}
}

// IsRunning returns whether the service is currently running
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// Context returns the service context for components that need it
func (s *Service) Context() context.Context {
	return s.ctx
}

// cleanupActiveNodes destroys all active nodes during service shutdown
func (s *Service) cleanupActiveNodes(ctx context.Context) error {
	if s.components == nil || s.components.NodeService == nil {
		return nil
	}

	op := s.logger.StartOp(ctx, "node_cleanup")

	// Get all active nodes using node service
	activeNodes, err := s.components.NodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		listErr := errors.WrapWithDomain(err, errors.DomainNode, errors.ErrCodeInternal, "failed to get active nodes", true)
		s.logger.ErrorCtx(ctx, "failed to list active nodes for cleanup", listErr)
		op.Fail(listErr, "failed to list active nodes for cleanup")
		return listErr
	}

	if len(activeNodes) == 0 {
		s.logger.DebugContext(ctx, "no active nodes to cleanup")
		op.Complete("no active nodes found for cleanup")
		return nil
	}

	s.logger.InfoContext(ctx, "cleaning up active nodes", "count", len(activeNodes))
	op.With(slog.Int("nodes_count", len(activeNodes)))

	var lastErr error
	successCount := 0

	for _, activeNode := range activeNodes {
		nodeCtx := logger.WithNodeID(ctx, activeNode.ID)
		s.logger.InfoContext(nodeCtx, "destroying active node", "node_id", activeNode.ID)

		if err := s.components.NodeService.DestroyNode(nodeCtx, activeNode.ID); err != nil {
			destroyErr := errors.WrapWithDomain(err, errors.DomainNode, errors.ErrCodeDestructionFailed, "failed to destroy active node", true)
			destroyErr.(*errors.BaseError).WithMetadata("node_id", activeNode.ID)
			s.logger.ErrorCtx(nodeCtx, "failed to destroy active node", destroyErr)
			lastErr = destroyErr
		} else {
			s.logger.InfoContext(nodeCtx, "destroyed active node", "node_id", activeNode.ID)
			successCount++
		}
	}

	op.With(slog.Int("success_count", successCount))
	op.With(slog.Int("failure_count", len(activeNodes)-successCount))

	if lastErr != nil {
		op.Fail(lastErr, "node cleanup completed with errors")
		return errors.WrapWithDomain(lastErr, errors.DomainNode, errors.ErrCodeInternal, "node cleanup completed with errors", false)
	}

	op.Complete("node cleanup completed successfully", slog.Int("success_count", successCount))
	return nil
}
