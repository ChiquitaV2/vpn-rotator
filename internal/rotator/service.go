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
	logger     *slog.Logger

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
func NewService(cfg *config.Config, logger *slog.Logger) (*Service, error) {
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

	// Create unified scheduler that uses VPN service for rotation and cleanup
	schedulerManager := scheduler.NewUnifiedManager(
		s.config.Scheduler.RotationInterval,
		s.config.Scheduler.CleanupInterval,
		s.config.Scheduler.CleanupAge,
		s.components.VPNService,
		s.logger,
	)

	s.scheduler = schedulerManager
	s.logger.Debug("scheduler initialized successfully")
	return nil
}

// initializeAPIServer creates the API server using application services
func (s *Service) initializeAPIServer() error {
	s.logger.Debug("initializing API server with application services")

	// Create API server that uses application services
	apiServer := api.NewServerWithApplicationServices(
		api.ServerConfig{
			Address:     s.config.API.ListenAddr,
			CORSOrigins: []string{"*"}, // TODO: Make configurable
		},
		s.components.VPNService,
		s.components.AdminService,
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
		return fmt.Errorf("service is already running")
	}

	s.logger.Info("starting rotator service")

	// Set up signal handling for graceful shutdown (unless disabled for testing)
	if !s.disableSignalHandling {
		s.setupSignalHandling()
	}

	// Start components in dependency order
	// 1. manager is a dependency for scheduler, so it should be ready first
	s.logger.Info("orchestrator ready (no startup required)")
	go s.components.ProvisioningService.ProvisionNode(ctx)

	// 2. Start scheduler (depends on orchestrator)
	if err := s.scheduler.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}
	s.logger.Info("scheduler started successfully")

	// 3. Start API server (depends on orchestrator)
	if err := s.apiServer.Start(s.ctx); err != nil {
		// If API server fails, stop scheduler before returning error
		if stopErr := s.scheduler.Stop(s.ctx); stopErr != nil {
			s.logger.Error("failed to stop scheduler during cleanup", "error", stopErr)
		}
		return fmt.Errorf("failed to start API server: %w", err)
	}
	s.logger.Info("API server started successfully")

	s.isRunning = true
	s.logger.Info("rotator service started successfully")
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
		s.logger.Info("received shutdown signal", "signal", sig.String())

		// Create shutdown context with timeout
		shutdownTimeout := 30 * time.Second
		if s.config != nil && s.config.Service.ShutdownTimeout > 0 {
			shutdownTimeout = s.config.Service.ShutdownTimeout
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		// Initiate graceful shutdown
		if err := s.Stop(shutdownCtx); err != nil {
			s.logger.Error("error during graceful shutdown", "error", err)
		}

	case <-s.ctx.Done():
		// Service context was cancelled, exit gracefully
		s.logger.Debug("signal handler exiting due to service context cancellation")
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
		s.logger.Warn("service is not running")
		return nil
	}

	s.logger.Info("stopping rotator service")

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
		s.logger.Info("stopping API server")
		if err := s.apiServer.Stop(shutdownCtx); err != nil {
			s.logger.Error("failed to stop API server", "error", err)
			lastErr = err
		} else {
			s.logger.Info("API server stopped successfully")
		}
	}

	// 2. Stop scheduler
	if s.scheduler != nil {
		s.logger.Info("stopping scheduler")
		if err := s.scheduler.Stop(shutdownCtx); err != nil {
			s.logger.Error("failed to stop scheduler", "error", err)
			lastErr = err
		} else {
			s.logger.Info("scheduler stopped successfully")
		}
	}

	// 3. Clean up active nodes before shutdown
	if s.components.VPNService != nil {
		s.logger.Info("cleaning up active nodes")
		if err := s.cleanupActiveNodes(shutdownCtx); err != nil {
			s.logger.Error("failed to cleanup active nodes", "error", err)
			lastErr = err
		} else {
			s.logger.Info("active nodes cleaned up successfully")
		}
	}

	// 4. Close database store
	if s.components != nil && s.components.Store != nil {
		s.logger.Info("closing database store")
		if err := s.components.Store.Close(); err != nil {
			s.logger.Error("failed to close database store", "error", err)
			lastErr = err
		} else {
			s.logger.Info("database store closed successfully")
		}
	}

	// 5. Cancel service context to signal any remaining goroutines
	s.cancel()
	s.logger.Info("service context cancelled")

	// 6. Wait for all background goroutines to finish
	s.logger.Debug("waiting for background goroutines to finish")
	done := make(chan struct{})
	go func() {
		s.shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Debug("all background goroutines finished")
	case <-shutdownCtx.Done():
		s.logger.Warn("timeout waiting for background goroutines to finish")
		if lastErr == nil {
			lastErr = shutdownCtx.Err()
		}
	}

	s.isRunning = false

	if lastErr != nil {
		return fmt.Errorf("service shutdown completed with errors: %w", lastErr)
	}

	s.logger.Info("rotator service stopped successfully")
	return nil
}

// Health checks the health of all service components
func (s *Service) Health() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isRunning {
		return fmt.Errorf("service is not running")
	}

	// Check if the service context is still active
	select {
	case <-s.ctx.Done():
		return fmt.Errorf("service context cancelled")
	default:
		// Optionally check database connectivity
		if s.components != nil && s.components.Store != nil {
			if err := s.components.Store.Ping(context.Background()); err != nil {
				return fmt.Errorf("database health check failed: %w", err)
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

	// Get all active nodes using node service
	activeNodes, err := s.components.NodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		return fmt.Errorf("failed to get active nodes: %w", err)
	}

	if len(activeNodes) == 0 {
		s.logger.Debug("no active nodes to cleanup")
		return nil
	}

	s.logger.Info("cleaning up active nodes", slog.Int("count", len(activeNodes)))

	var lastErr error
	for _, activeNode := range activeNodes {
		s.logger.Info("destroying active node", slog.String("node_id", activeNode.ID))

		if err := s.components.NodeService.DestroyNode(ctx, activeNode.ID); err != nil {
			s.logger.Error("failed to destroy active node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			lastErr = err
		} else {
			s.logger.Info("destroyed active node", slog.String("node_id", activeNode.ID))
		}
	}

	return lastErr
}
