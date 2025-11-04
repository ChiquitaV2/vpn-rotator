package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
)

// Server represents the HTTP API server with proper lifecycle management.
type Server struct {
	server       *http.Server
	vpnService   application.VPNService
	adminService application.AdminService
	logger       *slog.Logger
	corsOrigins  []string
	cbMonitor    CircuitBreakerMonitor
}

// ServerConfig contains configuration for the API server.
type ServerConfig struct {
	Address     string
	CORSOrigins []string
}

// CircuitBreakerMonitor defines the interface for circuit breaker monitoring.
type CircuitBreakerMonitor interface {
	Handler() http.HandlerFunc
}

// NewServer creates a new API server instance.
func NewServer(config ServerConfig, vpnService application.VPNService, adminService application.AdminService, logger *slog.Logger) *Server {
	return &Server{
		vpnService:   vpnService,
		adminService: adminService,
		logger:       logger,
		corsOrigins:  config.CORSOrigins,
		server: &http.Server{
			Addr:         config.Address,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

// NewServerWithMonitoring creates a new API server instance with circuit breaker monitoring.
func NewServerWithMonitoring(config ServerConfig, vpnService application.VPNService, adminService application.AdminService, cbMonitor CircuitBreakerMonitor, logger *slog.Logger) *Server {
	server := &Server{
		vpnService:   vpnService,
		adminService: adminService,
		logger:       logger,
		corsOrigins:  config.CORSOrigins,
		server: &http.Server{
			Addr:         config.Address,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Store circuit breaker monitor for route registration
	server.cbMonitor = cbMonitor

	return server
}

// Start starts the HTTP server and begins serving requests.
func (s *Server) Start(ctx context.Context) error {
	// Set up routes
	mux := http.NewServeMux()
	handler := s.registerRoutes(mux)
	s.server.Handler = handler

	s.logger.InfoContext(ctx, "starting API server", "address", s.server.Addr)

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("api server failed to start: %w", err)
		}
	}()

	// Check if server started successfully
	select {
	case err := <-errChan:
		return err
	case <-time.After(100 * time.Millisecond):
		s.logger.InfoContext(ctx, "API server started successfully", "address", s.server.Addr)
		return nil
	}
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.InfoContext(ctx, "shutting down API server")

	// Create a context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("api server shutdown failed: %w", err)
	}

	s.logger.InfoContext(ctx, "API server shut down successfully")
	return nil
}

// registerRoutes registers API routes with middleware.
func (s *Server) registerRoutes(mux *http.ServeMux) http.Handler {
	// Health check endpoint
	mux.HandleFunc("/health", s.healthHandler())

	// Peer management routes
	mux.HandleFunc("POST /api/v1/connect", s.connectHandler())
	mux.HandleFunc("DELETE /api/v1/disconnect", s.disconnectHandler())
	mux.HandleFunc("GET /api/v1/peers", s.listPeersHandler())
	mux.HandleFunc("GET /api/v1/peers/{peerID}", s.getPeerHandler())

	// Admin routes
	mux.HandleFunc("GET /api/v1/admin/status", s.systemStatusHandler())
	mux.HandleFunc("GET /api/v1/admin/nodes/stats", s.nodeStatsHandler())
	mux.HandleFunc("GET /api/v1/admin/peers/stats", s.peerStatsHandler())
	mux.HandleFunc("POST /api/v1/admin/rotation/force", s.forceRotationHandler())
	mux.HandleFunc("GET /api/v1/admin/rotation/status", s.rotationStatusHandler())
	mux.HandleFunc("POST /api/v1/admin/cleanup", s.manualCleanupHandler())
	mux.HandleFunc("GET /api/v1/admin/health", s.systemHealthHandler())

	// Register circuit breaker monitoring endpoint
	if s.cbMonitor != nil {
		mux.HandleFunc("/api/v1/circuit-breakers", s.cbMonitor.Handler())
	}

	// Apply middleware chain
	handler := Chain(
		Recovery(s.logger),
		RequestID,
		Logging(s.logger),
		ErrorHandling(s.logger),
		CORS(s.corsOrigins),
	)(mux)

	return handler
}

// NewServerWithApplicationServices creates a new API server instance using application services
func NewServerWithApplicationServices(
	config ServerConfig,
	vpnService application.VPNService,
	adminService application.AdminService,
	logger *slog.Logger,
) *Server {
	return &Server{
		vpnService:   vpnService,
		adminService: adminService,
		logger:       logger,
		corsOrigins:  config.CORSOrigins,
		server: &http.Server{
			Addr:         config.Address,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}
