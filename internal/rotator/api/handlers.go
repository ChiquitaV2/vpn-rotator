package api

import (
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

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

		if err := ValidateConnectRequest(&req); err != nil {
			s.logger.ErrorContext(ctx, "invalid connect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Handle key management
		var publicKey string
		var privateKey *string

		if req.GenerateKeys {
			keyPair, err := crypto.GenerateKeyPair()
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to generate key pair", "error", err)
				_ = WriteErrorWithRequestID(w, http.StatusInternalServerError, "key_generation_error", "Failed to generate WireGuard keys", requestID)
				return
			}
			publicKey = keyPair.PublicKey
			privateKey = &keyPair.PrivateKey
		} else {
			publicKey = *req.PublicKey
		}

		// Use orchestrator to create peer on an optimal node
		peerConfig, err := s.orchestrator.CreatePeerOnOptimalNode(ctx, publicKey, nil) // Assuming no preshared key from this endpoint
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to create peer on optimal node", "error", err)
			_ = WriteErrorWithRequestID(w, http.StatusServiceUnavailable, "peer_creation_error", "Failed to create peer", requestID)
			return
		}

		// Get node config for the response
		nodeConfig, err := s.orchestrator.GetNodeConfig(ctx, peerConfig.NodeID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get node config after peer creation", "error", err, "peer_id", peerConfig.ID)
			// Attempt to roll back by removing the peer
			if cleanupErr := s.orchestrator.RemovePeerFromSystem(ctx, peerConfig.ID); cleanupErr != nil {
				s.logger.ErrorContext(ctx, "failed to cleanup peer after node config failure", "error", cleanupErr, "peer_id", peerConfig.ID)
			}
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError, "config_error", "Failed to retrieve VPN configuration after peer creation", requestID)
			return
		}

		// Build response
		response := api.ConnectResponse{
			PeerID:           peerConfig.ID,
			ServerPublicKey:  nodeConfig.ServerPublicKey,
			ServerIP:         nodeConfig.ServerIP,
			ServerPort:       nodeConfig.ServerPort,
			ClientIP:         peerConfig.AllocatedIP,
			ClientPrivateKey: privateKey,
			DNS:              []string{"9.9.9.9", "149.112.112.112"}, // Default DNS servers
			AllowedIPs:       []string{"0.0.0.0/0"},                  // Route all traffic through VPN
		}

		s.logger.InfoContext(ctx, "peer connected successfully", "peer_id", peerConfig.ID, "node_id", peerConfig.NodeID, "client_ip", peerConfig.AllocatedIP)

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

		if err := ValidateDisconnectRequest(&req); err != nil {
			s.logger.ErrorContext(ctx, "invalid disconnect request", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		// Use orchestrator to remove peer from the system
		err := s.orchestrator.RemovePeerFromSystem(ctx, req.PeerID)
		if err != nil {
			// TODO: Differentiate between not found and other errors
			s.logger.ErrorContext(ctx, "failed to remove peer from system", "error", err, "peer_id", req.PeerID)
			_ = WriteErrorWithRequestID(w, http.StatusInternalServerError, "peer_removal_error", "Failed to remove peer", requestID)
			return
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
