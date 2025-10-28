package api

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
)

// OrchestratorInterface defines the interface that the API server depends on.
type OrchestratorInterface interface {
	GetLatestConfig(ctx context.Context) (*models.NodeConfig, error)
}

// Server represents the HTTP API server with proper lifecycle management.
type Server struct {
	server       *http.Server
	orchestrator OrchestratorInterface
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
func NewServer(config ServerConfig, orchestrator OrchestratorInterface, logger *slog.Logger) *Server {
	return &Server{
		orchestrator: orchestrator,
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
func NewServerWithMonitoring(config ServerConfig, orchestrator OrchestratorInterface, cbMonitor CircuitBreakerMonitor, logger *slog.Logger) *Server {
	server := &Server{
		orchestrator: orchestrator,
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
	// Register routes
	mux.HandleFunc("/api/v1/config/latest", s.latestConfigHandler())
	mux.HandleFunc("/health", s.healthHandler())

	// Register circuit breaker monitoring endpoint
	if s.cbMonitor != nil {
		mux.HandleFunc("/api/v1/circuit-breakers", s.cbMonitor.Handler())
	}

	// Apply middleware chain
	handler := Chain(
		Recovery(s.logger),
		RequestID,
		Logging(s.logger),
		CORS(s.corsOrigins),
	)(mux)

	return handler
}

// latestConfigHandler returns the latest VPN configuration.
func (s *Server) latestConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		config, err := s.orchestrator.GetLatestConfig(ctx)
		if err != nil {
			// Check if it's a "no active node" error - provisioning in progress
			if err == sql.ErrNoRows || stderrors.Is(err, errors.ErrNoActiveNode) {
				s.logger.InfoContext(ctx, "no active node, provisioning in progress")
				_ = WriteJSON(w, http.StatusServiceUnavailable, ProvisioningResponse{
					Status:        "provisioning",
					Message:       "VPN node is being provisioned",
					EstimatedWait: 120,
					RetryAfter:    30,
				})
				return
			}

			s.logger.ErrorContext(ctx, "failed to get latest config", "error", err)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"config_error",
				"Failed to retrieve VPN configuration",
				requestID,
			)
			return
		}

		response := ConfigResponse{
			ServerPublicKey: config.ServerPublicKey,
			ServerIP:        config.ServerIP,
			Port:            51820, // Default WireGuard port
		}

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode response", "error", err)
		}
	}
}

// healthHandler returns the service health status.
func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
		}

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(r.Context(), "failed to encode health response", "error", err)
		}
	}
}
