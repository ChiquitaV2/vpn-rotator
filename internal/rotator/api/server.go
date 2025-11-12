package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/orchestrator"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// Server represents the HTTP API server with proper lifecycle management.
type Server struct {
	server            *http.Server
	vpnService        orchestrator.VPNService
	adminOrchestrator orchestrator.AdminOrchestrator
	healthService     services.HealthService
	logger            *applogger.Logger
	corsOrigins       []string
}

// ServerConfig contains configuration for the API server.
type ServerConfig struct {
	Address     string
	CORSOrigins []string
}

// NewServer creates a new API server instance.
func NewServer(
	config ServerConfig,
	vpnService orchestrator.VPNService,
	adminOrchestrator orchestrator.AdminOrchestrator,
	healthService services.HealthService,
	logger *applogger.Logger,
) *Server {
	return &Server{
		vpnService:        vpnService,
		adminOrchestrator: adminOrchestrator,
		healthService:     healthService,
		logger:            logger.WithComponent("api.server"),
		corsOrigins:       config.CORSOrigins,
		server: &http.Server{
			Addr:         config.Address,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
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
			// This error can't be a DomainError, it's a critical failure
			errChan <- fmt.Errorf("api server failed to start: %w", err)
		}
	}()

	// Check if server started successfully
	select {
	case err := <-errChan:
		s.logger.ErrorCtx(ctx, "API server failed to start", err)
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
		s.logger.ErrorCtx(ctx, "API server shutdown failed", err)
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
	mux.HandleFunc("GET /api/v1/admin/status", s.adminStatusHandler())
	mux.HandleFunc("GET /api/v1/admin/nodes/stats", s.adminNodeStatsHandler())
	mux.HandleFunc("GET /api/v1/admin/peers/stats", s.adminPeerStatsHandler())
	mux.HandleFunc("POST /api/v1/admin/rotation/force", s.adminForceRotationHandler())
	mux.HandleFunc("GET /api/v1/admin/rotation/status", s.adminRotationStatusHandler())
	mux.HandleFunc("POST /api/v1/admin/cleanup", s.adminManualCleanupHandler())
	mux.HandleFunc("GET /api/v1/admin/health", s.adminSystemHealthHandler())

	// Provisioning routes
	mux.HandleFunc("GET /api/v1/provisioning/status", s.provisioningStatusHandler())

	// Apply middleware chain
	// Swapped old Logging/ErrorHandling with new integrated versions.
	handler := Chain(
		RequestID(s.logger), // Injects RequestID and Scoped Logger
		Recovery(),          // Recovers panics, uses logger from context
		Logging(),           // Logs requests, uses logger from context
		CORS(s.corsOrigins),
	)(mux)

	return handler
}
