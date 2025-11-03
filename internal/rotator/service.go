package rotator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/api"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/health"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/monitoring"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/orchestrator"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/scheduler"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// SchedulerInterface defines the interface for scheduler operations
type SchedulerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// APIServerInterface defines the interface for API server operations
type APIServerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Service coordinates all rotator service components and manages their lifecycle
type Service struct {
	config       *config.Config
	orchestrator orchestrator.Orchestrator
	scheduler    SchedulerInterface
	apiServer    APIServerInterface
	logger       *slog.Logger

	// Component instances for cleanup
	store       db.Store
	provisioner provisioner.Provisioner

	// Circuit breaker instances for monitoring
	provisionerCB *provisioner.CircuitBreaker
	healthCB      *health.CircuitBreakerHealthChecker
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

// NewService creates a new Service instance and initializes all components in proper dependency order
func NewService(cfg *config.Config, logger *slog.Logger) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:     cfg,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		signalChan: make(chan os.Signal, 1),
	}

	// Initialize components in dependency order
	if err := service.initializeComponents(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize service components: %w", err)
	}

	return service, nil
}

// initializeComponents creates and wires all service components in proper dependency order
func (s *Service) initializeComponents() error {
	s.logger.Info("initializing service components")

	// 1. Initialize database store (foundational dependency)
	s.logger.Debug("initializing database store")
	baseStore, err := db.NewStore(&db.Config{
		Path:            s.config.DB.Path,
		MaxOpenConns:    s.config.DB.MaxOpenConns,
		MaxIdleConns:    s.config.DB.MaxIdleConns,
		ConnMaxLifetime: s.config.DB.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize database store: %w", err)
	}

	s.store = baseStore
	s.logger.Debug("database store with initialized successfully")

	// 2. Initialize provisioner (infrastructure dependency)
	s.logger.Debug("initializing provisioner")
	hetznerProvisioner, err := provisioner.NewHetzner(
		s.config.Hetzner.APIToken,
		&provisioner.HetznerConfig{
			ServerType:   s.config.Hetzner.ServerTypes[0], // Use first server type
			Image:        s.config.Hetzner.Image,
			Location:     s.config.Hetzner.Locations[0], // Use first location
			SSHPublicKey: s.config.Hetzner.SSHKey,
		},
		s.logger,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize Hetzner provisioner: %w", err)
	}

	// Wrap provisioner with circuit breaker for resilience
	s.logger.Debug("initializing provisioner circuit breaker")
	circuitBreakerConfig := provisioner.DefaultCircuitBreakerConfig()
	s.provisionerCB = provisioner.NewCircuitBreaker(hetznerProvisioner, circuitBreakerConfig, s.logger)
	s.provisioner = s.provisionerCB
	s.logger.Debug("provisioner with circuit breaker initialized successfully")

	// 3. Initialize health checker
	s.logger.Debug("initializing health checker")
	baseHealthChecker := health.NewHTTPHealthChecker()
	// Wrap health checker with circuit breaker for resilience
	s.logger.Debug("initializing health check circuit breaker")
	s.logger.Debug("health checker with circuit breaker initialized successfully")
	healthCircuitBreakerConfig := health.DefaultCircuitBreakerConfig()
	s.healthCB = health.NewCircuitBreakerHealthChecker(baseHealthChecker, healthCircuitBreakerConfig, s.logger)
	healthChecker := s.healthCB

	// 4. Read SSH private key
	s.logger.Debug("reading SSH private key")
	privateKeyBytes, err := os.ReadFile(s.config.Hetzner.SSHPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH private key from %s: %w", s.config.Hetzner.SSHPrivateKeyPath, err)
	}

	// 5. Initialize IP service (depends on store)
	s.logger.Debug("initializing IP service")
	ipRepo := store.NewSubnetRepository(s.store)
	ipConfig := ip.DefaultNetworkConfig()
	ipService, err := ip.NewService(ipRepo, ipConfig, s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize IP service: %w", err)
	}
	s.logger.Debug("IP service initialized successfully")

	// 6. Initialize node manager (depends on store, provisioner, health checker, IP service)
	s.logger.Debug("initializing node manager")
	nodeManager := nodemanager.New(
		s.store,
		s.provisioner,
		healthChecker,
		s.logger,
		string(privateKeyBytes),
		ipService,
	)
	s.logger.Debug("node manager initialized successfully")

	// 7. Initialize peer manager (depends on store and repository)
	s.logger.Debug("initializing peer manager")
	// Create a logger wrapper for peer manager
	peerLogger := &logger.Logger{Logger: s.logger}
	// Create repository manager for peermanager
	//reposManager := repository.NewRepositoryManager(s.store, peerLogger)
	peerManager := peermanager.NewManager(s.store, peerLogger)
	s.logger.Debug("peer manager initialized successfully")

	// 8. Initialize orchestrator (depends on store, node manager, peer manager, IP service)
	s.logger.Debug("initializing orchestrator")
	orch := orchestrator.New(s.store, nodeManager, peerManager, ipService, s.logger)
	s.orchestrator = orch
	s.logger.Debug("orchestrator initialized successfully")

	// 9. Initialize scheduler manager (depends on orchestrator)
	s.logger.Debug("initializing scheduler manager")
	schedulerManager := scheduler.NewManager(
		s.config.Scheduler.RotationInterval,
		s.config.Scheduler.CleanupInterval,
		s.config.Scheduler.CleanupAge,
		orch, // RotationManager interface
		orch, // CleanupManager interface (orchestrator implements both)
		s.logger,
	)
	s.scheduler = schedulerManager
	s.logger.Debug("scheduler manager initialized successfully")

	// 10. Initialize circuit breaker monitoring
	s.logger.Debug("initializing circuit breaker monitoring")
	cbMonitor := monitoring.NewCircuitBreakerMonitor(s.provisionerCB, s.healthCB)
	s.logger.Debug("circuit breaker monitoring initialized successfully")

	// 11. Initialize API server (depends on orchestrator, peer manager, IP manager, node manager, and monitoring)
	s.logger.Debug("initializing API server")
	apiServer := api.NewServerWithMonitoring(
		api.ServerConfig{
			Address:     s.config.API.ListenAddr,
			CORSOrigins: []string{"*"}, // TODO: Make configurable
		},
		s.orchestrator,
		peerManager,
		cbMonitor,
		s.logger,
	)
	s.apiServer = apiServer
	s.logger.Debug("API server initialized successfully")

	s.logger.Info("all service components initialized successfully")
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
	if s.orchestrator != nil {
		s.logger.Info("cleaning up active nodes")
		if err := s.cleanupActiveNodes(shutdownCtx); err != nil {
			s.logger.Error("failed to cleanup active nodes", "error", err)
			lastErr = err
		} else {
			s.logger.Info("active nodes cleaned up successfully")
		}
	}

	// 4. Close database store
	if s.store != nil {
		s.logger.Info("closing database store")
		if err := s.store.Close(); err != nil {
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
		if s.store != nil {
			if err := s.store.Ping(context.Background()); err != nil {
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
	if s.store == nil {
		return nil
	}

	// Get all active nodes
	activeNodes, err := s.store.GetNodesByStatus(ctx, "active")
	if err != nil {
		if err == sql.ErrNoRows {
			s.logger.Debug("no active nodes to cleanup")
			return nil
		}
		return fmt.Errorf("failed to get active nodes: %w", err)
	}

	if len(activeNodes) == 0 {
		s.logger.Debug("no active nodes to cleanup")
		return nil
	}

	s.logger.Info("cleaning up active nodes", slog.Int("count", len(activeNodes)))

	var lastErr error
	for _, node := range activeNodes {
		s.logger.Info("destroying active node", slog.String("node_id", node.ID))

		if err := s.orchestrator.DestroyNode(ctx, node); err != nil {
			s.logger.Error("failed to destroy active node",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
			lastErr = err
		} else {
			s.logger.Info("destroyed active node", slog.String("node_id", node.ID))
		}
	}

	return lastErr
}
