package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// OrchestratorInterface defines the interface that the API server depends on.
type OrchestratorInterface interface {
	SelectNodeForPeer(ctx context.Context) (string, error)
	GetNodeLoadBalance(ctx context.Context) (map[string]int, error)
	GetNodeConfig(ctx context.Context, nodeID string) (*nodemanager.NodeConfig, error)
}

// PeerManagerInterface defines the interface for peer management operations.
type PeerManagerInterface interface {
	CreatePeer(ctx context.Context, req *peermanager.CreatePeerRequest) (*peermanager.PeerConfig, error)
	RemovePeer(ctx context.Context, peerID string) error
	GetPeer(ctx context.Context, peerID string) (*peermanager.PeerConfig, error)
	ListPeers(ctx context.Context, filters *peermanager.PeerFilters) ([]*peermanager.PeerConfig, error)
	ValidatePeerConfig(peer *peermanager.PeerConfig) error
}

// IPManagerInterface defines the interface for IP management operations.
type IPManagerInterface interface {
	AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error)
	ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error
	GetAvailableIPCount(ctx context.Context, nodeID string) (int, error)
}

// NodeManagerInterface defines the interface for node management operations.
type NodeManagerInterface interface {
	AddPeerToNode(ctx context.Context, nodeIP string, peer *nodemanager.PeerConfig) error
	RemovePeerFromNode(ctx context.Context, nodeIP string, publicKey string) error
	GetNodePublicKey(ctx context.Context, nodeIP string) (string, error)
}

// Server represents the HTTP API server with proper lifecycle management.
type Server struct {
	server       *http.Server
	orchestrator OrchestratorInterface
	peerManager  PeerManagerInterface
	ipManager    IPManagerInterface
	nodeManager  NodeManagerInterface
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
func NewServer(config ServerConfig, orchestrator OrchestratorInterface, peerManager PeerManagerInterface, ipManager IPManagerInterface, nodeManager NodeManagerInterface, logger *slog.Logger) *Server {
	return &Server{
		orchestrator: orchestrator,
		peerManager:  peerManager,
		ipManager:    ipManager,
		nodeManager:  nodeManager,
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
func NewServerWithMonitoring(config ServerConfig, orchestrator OrchestratorInterface, peerManager PeerManagerInterface, ipManager IPManagerInterface, nodeManager NodeManagerInterface, cbMonitor CircuitBreakerMonitor, logger *slog.Logger) *Server {
	server := &Server{
		orchestrator: orchestrator,
		peerManager:  peerManager,
		ipManager:    ipManager,
		nodeManager:  nodeManager,
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

// healthHandler returns the service health status.
func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := api.HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
		}

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(r.Context(), "failed to encode health response", "error", err)
		}
	}
}

// connectHandler handles peer connection requests
func (s *Server) connectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Parse and validate request
		var req api.ConnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			s.logger.ErrorContext(ctx, "failed to parse connect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Validate request
		if err := ValidateConnectRequest(&req); err != nil {
			s.logger.ErrorContext(ctx, "invalid connect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Handle key management
		var publicKey string
		var privateKey *string

		if req.GenerateKeys {
			// Generate new key pair on server
			keyPair, err := GenerateWireGuardKeyPair()
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to generate key pair", "error", err)
				_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
					"key_generation_error",
					"Failed to generate WireGuard keys",
					requestID,
				)
				return
			}
			publicKey = keyPair.PublicKey
			privateKey = &keyPair.PrivateKey
		} else {
			// Use provided public key
			publicKey = *req.PublicKey
		}

		// Select optimal node for the peer
		nodeID, err := s.orchestrator.SelectNodeForPeer(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to select node for peer", "error", err)
			_ = WriteErrorWithRequestID(w, http.StatusServiceUnavailable,
				"node_selection_error",
				"No available nodes for peer assignment",
				requestID,
			)
			return
		}

		// Allocate IP address for the peer
		clientIP, err := s.ipManager.AllocateClientIP(ctx, nodeID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to allocate IP for peer", "error", err, "node_id", nodeID)
			_ = WriteErrorWithRequestID(w, http.StatusServiceUnavailable,
				"ip_allocation_error",
				"Failed to allocate IP address for peer",
				requestID,
			)
			return
		}

		// Create peer in database
		createReq := &peermanager.CreatePeerRequest{
			NodeID:      nodeID,
			PublicKey:   publicKey,
			AllocatedIP: clientIP.String(),
		}

		peer, err := s.peerManager.CreatePeer(ctx, createReq)
		if err != nil {
			// Rollback: release allocated IP
			s.ipManager.ReleaseClientIP(ctx, nodeID, clientIP)
			s.logger.ErrorContext(ctx, "failed to create peer", "error", err)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"peer_creation_error",
				"Failed to create peer configuration",
				requestID,
			)
			return
		}

		// Get node information to get the IP address for SSH
		nodeConfig, err := s.orchestrator.GetNodeConfig(ctx, nodeID)
		if err != nil {
			// Rollback: remove peer from database and release IP
			s.peerManager.RemovePeer(ctx, peer.ID)
			s.ipManager.ReleaseClientIP(ctx, nodeID, clientIP)
			s.logger.ErrorContext(ctx, "failed to get node config for SSH", "error", err, "node_id", nodeID)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"config_error",
				"Failed to retrieve VPN configuration",
				requestID,
			)
			return
		}

		// Add peer to node via SSH
		nodePeerConfig := &nodemanager.PeerConfig{
			ID:          peer.ID,
			PublicKey:   publicKey,
			AllocatedIP: clientIP.String(),
		}

		err = s.nodeManager.AddPeerToNode(ctx, nodeConfig.ServerIP, nodePeerConfig)
		if err != nil {
			// Rollback: remove peer from database and release IP
			s.peerManager.RemovePeer(ctx, peer.ID)
			s.ipManager.ReleaseClientIP(ctx, nodeID, clientIP)
			s.logger.ErrorContext(ctx, "failed to add peer to node", "error", err, "node_id", nodeID)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"node_configuration_error",
				"Failed to configure peer on VPN node",
				requestID,
			)
			return
		}

		// Build response
		response := api.ConnectResponse{
			PeerID:           peer.ID,
			ServerPublicKey:  nodeConfig.ServerPublicKey,
			ServerIP:         nodeConfig.ServerIP,
			ServerPort:       nodeConfig.ServerPort,
			ClientIP:         clientIP.String(),
			ClientPrivateKey: privateKey,
			DNS:              []string{"9.9.9.9", "149.112.112.112"}, // Default DNS servers
			AllowedIPs:       []string{"0.0.0.0/0"},                  // Route all traffic through VPN
		}

		s.logger.InfoContext(ctx, "peer connected successfully",
			"peer_id", peer.ID,
			"node_id", nodeID,
			"client_ip", clientIP.String())

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode connect response", "error", err)
		}
	}
}

// disconnectHandler handles peer disconnection requests
func (s *Server) disconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Parse and validate request
		var req api.DisconnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			s.logger.ErrorContext(ctx, "failed to parse disconnect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Validate request
		if err := ValidateDisconnectRequest(&req); err != nil {
			s.logger.ErrorContext(ctx, "invalid disconnect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Get peer information
		peer, err := s.peerManager.GetPeer(ctx, req.PeerID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get peer", "error", err, "peer_id", req.PeerID)
			_ = WriteErrorWithRequestID(w, http.StatusNotFound,
				"peer_not_found",
				"Peer not found",
				requestID,
			)
			return
		}

		// Get node information to get the IP address for SSH
		nodeConfig, err := s.orchestrator.GetNodeConfig(ctx, peer.NodeID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to get node config for SSH removal",
				"error", err,
				"peer_id", req.PeerID,
				"node_id", peer.NodeID)
			// Continue with database cleanup even if we can't get node config
		} else {
			// Remove peer from node via SSH
			err = s.nodeManager.RemovePeerFromNode(ctx, nodeConfig.ServerIP, peer.PublicKey)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to remove peer from node via SSH",
					"error", err,
					"peer_id", req.PeerID,
					"node_id", peer.NodeID)
				// Continue with database cleanup even if SSH removal fails
			}
		}

		// Remove peer from database
		err = s.peerManager.RemovePeer(ctx, req.PeerID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to remove peer from database", "error", err, "peer_id", req.PeerID)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"peer_removal_error",
				"Failed to remove peer configuration",
				requestID,
			)
			return
		}

		// Release IP address
		clientIP := net.ParseIP(peer.AllocatedIP)
		if clientIP != nil {
			err = s.ipManager.ReleaseClientIP(ctx, peer.NodeID, clientIP)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to release client IP",
					"error", err,
					"peer_id", req.PeerID,
					"ip", peer.AllocatedIP)
				// Continue - peer is already removed
			}
		}

		response := api.DisconnectResponse{
			Message: "Peer disconnected successfully",
			PeerID:  req.PeerID,
		}

		s.logger.InfoContext(ctx, "peer disconnected successfully", "peer_id", req.PeerID)

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode disconnect response", "error", err)
		}
	}
}

// listPeersHandler handles peer listing requests
func (s *Server) listPeersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Validate and parse query parameters
		params, err := ValidatePeerListParams(r)
		if err != nil {
			s.logger.ErrorContext(ctx, "invalid peer list parameters", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Build filters from parameters
		filters := &peermanager.PeerFilters{}

		if nodeID, exists := params["node_id"]; exists {
			nodeIDStr := nodeID.(string)
			filters.NodeID = &nodeIDStr
		}

		if status, exists := params["status"]; exists {
			statusStr := status.(string)
			statusEnum := peermanager.PeerStatus(statusStr)
			filters.Status = &statusEnum
		}

		if limit, exists := params["limit"]; exists {
			limitInt := limit.(int)
			filters.Limit = &limitInt
		}

		if offset, exists := params["offset"]; exists {
			offsetInt := offset.(int)
			filters.Offset = &offsetInt
		}

		// Get peers from peer manager
		peers, err := s.peerManager.ListPeers(ctx, filters)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to list peers", "error", err)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError,
				"peer_list_error",
				"Failed to retrieve peer list",
				requestID,
			)
			return
		}

		// Convert to response format
		peerInfos := make([]peermanager.PeerInfo, len(peers))
		for i, peer := range peers {
			peerInfos[i] = peermanager.PeerInfo{
				ID:          peer.ID,
				NodeID:      peer.NodeID,
				PublicKey:   peer.PublicKey,
				AllocatedIP: peer.AllocatedIP,
				Status:      string(peer.Status),
				CreatedAt:   peer.CreatedAt,
				// LastHandshakeAt is not available in current peer config
			}
		}

		// Build response with pagination info
		response := peermanager.PeersListResponse{
			Peers:      peerInfos,
			TotalCount: len(peerInfos), // This is the filtered count, not total in DB
			Offset:     0,
			Limit:      len(peerInfos),
		}

		if filters.Offset != nil {
			response.Offset = *filters.Offset
		}
		if filters.Limit != nil {
			response.Limit = *filters.Limit
		}

		s.logger.InfoContext(ctx, "listed peers successfully", "count", len(peerInfos))

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode peers list response", "error", err)
		}
	}
}

// getPeerHandler handles individual peer status requests
func (s *Server) getPeerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Extract peer ID from URL path
		peerID := r.PathValue("peerID")
		if peerID == "" {
			s.logger.ErrorContext(ctx, "missing peer ID in request path")
			_ = WriteErrorWithRequestID(w, http.StatusBadRequest,
				"missing_peer_id",
				"Peer ID is required",
				requestID,
			)
			return
		}

		// Get peer information
		peer, err := s.peerManager.GetPeer(ctx, peerID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get peer", "error", err, "peer_id", peerID)
			_ = WriteErrorWithRequestID(w, http.StatusNotFound,
				"peer_not_found",
				"Peer not found",
				requestID,
			)
			return
		}

		// Convert to response format
		peerInfo := peermanager.PeerInfo{
			ID:          peer.ID,
			NodeID:      peer.NodeID,
			PublicKey:   peer.PublicKey,
			AllocatedIP: peer.AllocatedIP,
			Status:      string(peer.Status),
			CreatedAt:   peer.CreatedAt,
			// LastHandshakeAt is not available in current peer config
		}

		s.logger.InfoContext(ctx, "retrieved peer successfully", "peer_id", peerID)

		if err := WriteSuccess(w, peerInfo); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode peer response", "error", err)
		}
	}
}
