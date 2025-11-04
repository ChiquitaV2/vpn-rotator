package api

import (
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
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

		// Add provisioning information if provisioning orchestrator is available
		if s.provisioningOrchestrator != nil {
			provisioningStatus := s.provisioningOrchestrator.GetCurrentStatus()
			response.Provisioning = &api.ProvisioningInfo{
				IsActive:     provisioningStatus.IsActive,
				Phase:        provisioningStatus.Phase,
				Progress:     provisioningStatus.Progress,
				EstimatedETA: provisioningStatus.EstimatedETA,
			}
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
		if req.GenerateKeys {
			keyPair, err := crypto.GenerateKeyPair()
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to generate key pair", "error", err)
				_ = WriteErrorWithRequestID(w, http.StatusInternalServerError, "key_generation_error", "Failed to generate WireGuard keys", requestID)
				return
			}
			req.PublicKey = &keyPair.PublicKey
		}

		// Check if provisioning is active and warn user about potential delays
		if s.provisioningOrchestrator != nil && s.provisioningOrchestrator.IsProvisioning() {
			waitTime := s.provisioningOrchestrator.GetEstimatedWaitTime()
			status := s.provisioningOrchestrator.GetCurrentStatus()
			s.logger.InfoContext(ctx, "provisioning active during peer connection",
				"estimated_wait", waitTime,
				"phase", status.Phase)
		}

		// Use VPN service to connect peer
		response, err := s.vpnService.ConnectPeer(ctx, req)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to connect peer", "error", err)

			// Handle provisioning required error specially
			if provErr, ok := err.(*application.ProvisioningRequiredError); ok {
				s.logger.InfoContext(ctx, "provisioning required for peer connection",
					"estimated_wait", provErr.EstimatedWait,
					"retry_after", provErr.RetryAfter,
					"message", provErr.Message)

				// Create a detailed provisioning response
				provisioningResponse := map[string]interface{}{
					"error":          "provisioning_required",
					"message":        provErr.Message,
					"estimated_wait": provErr.EstimatedWait,
					"retry_after":    provErr.RetryAfter,
					"request_id":     requestID,
				}

				// Add current provisioning status if available
				if s.provisioningOrchestrator != nil {
					status := s.provisioningOrchestrator.GetCurrentStatus()
					provisioningResponse["provisioning_status"] = map[string]interface{}{
						"is_active":     status.IsActive,
						"phase":         status.Phase,
						"progress":      status.Progress,
						"estimated_eta": status.EstimatedETA,
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				if err := WriteSuccess(w, provisioningResponse); err != nil {
					s.logger.ErrorContext(ctx, "failed to encode provisioning response", "error", err)
				}
				return
			}

			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "peer connected successfully", "peer_id", response.PeerID, "client_ip", response.ClientIP)

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

		// Use VPN service to disconnect peer
		err := s.vpnService.DisconnectPeer(ctx, req.PeerID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to disconnect peer", "error", err, "peer_id", req.PeerID)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
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

		// Get active peers from VPN service
		peers, err := s.vpnService.ListActivePeers(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to list peers", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		// Apply client-side filtering (simplified for now)
		filteredPeers := peers
		if nodeID, exists := params["node_id"]; exists {
			nodeIDStr := nodeID.(string)
			var filtered []*api.PeerInfo
			for _, peer := range peers {
				if peer.NodeID == nodeIDStr {
					filtered = append(filtered, peer)
				}
			}
			filteredPeers = filtered
		}

		// Apply pagination
		offset := 0
		limit := len(filteredPeers)

		if offsetParam, exists := params["offset"]; exists {
			offset = offsetParam.(int)
		}
		if limitParam, exists := params["limit"]; exists {
			limit = limitParam.(int)
		}

		// Calculate pagination bounds
		start := offset
		if start > len(filteredPeers) {
			start = len(filteredPeers)
		}

		end := start + limit
		if end > len(filteredPeers) {
			end = len(filteredPeers)
		}

		paginatedPeers := filteredPeers[start:end]

		// Convert to response format
		peerInfos := make([]api.PeerInfo, len(paginatedPeers))
		for i, peer := range paginatedPeers {
			peerInfos[i] = *peer
		}

		// Build response with pagination info
		response := api.PeersListResponse{
			Peers:      peerInfos,
			TotalCount: len(filteredPeers),
			Offset:     offset,
			Limit:      limit,
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

		// Get peer status from VPN service
		peerStatus, err := s.vpnService.GetPeerStatus(ctx, peerID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get peer status", "error", err, "peer_id", peerID)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "retrieved peer status successfully", "peer_id", peerID)

		if err := WriteSuccess(w, peerStatus); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode peer status response", "error", err)
		}
	}
}

// Admin handlers

// systemStatusHandler handles system status requests
func (s *Server) systemStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		status, err := s.adminService.GetSystemStatus(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get system status", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "retrieved system status successfully")

		if err := WriteSuccess(w, status); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode system status response", "error", err)
		}
	}
}

// nodeStatsHandler handles node statistics requests
func (s *Server) nodeStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		stats, err := s.adminService.GetNodeStatistics(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get node statistics", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "retrieved node statistics successfully")

		if err := WriteSuccess(w, stats); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode node statistics response", "error", err)
		}
	}
}

// peerStatsHandler handles peer statistics requests
func (s *Server) peerStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		stats, err := s.adminService.GetPeerStatistics(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get peer statistics", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "retrieved peer statistics successfully")

		if err := WriteSuccess(w, stats); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode peer statistics response", "error", err)
		}
	}
}

// forceRotationHandler handles forced node rotation requests
func (s *Server) forceRotationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		err := s.adminService.ForceNodeRotation(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to force node rotation", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		response := map[string]string{
			"message": "Node rotation initiated successfully",
		}

		s.logger.InfoContext(ctx, "forced node rotation initiated successfully")

		if err := WriteSuccess(w, response); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode force rotation response", "error", err)
		}
	}
}

// rotationStatusHandler handles rotation status requests
func (s *Server) rotationStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		status, err := s.adminService.GetRotationStatus(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get rotation status", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "retrieved rotation status successfully")

		if err := WriteSuccess(w, status); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode rotation status response", "error", err)
		}
	}
}

// manualCleanupHandler handles manual cleanup requests
func (s *Server) manualCleanupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Parse cleanup options from request body
		var options application.CleanupOptions
		if err := ParseJSONRequest(r, &options); err != nil {
			s.logger.ErrorContext(ctx, "failed to parse cleanup options", "error", err)
			_ = WriteValidationError(w, err, requestID)
			return
		}

		result, err := s.adminService.ManualCleanup(ctx, options)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to perform manual cleanup", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "manual cleanup completed successfully")

		if err := WriteSuccess(w, result); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode cleanup result response", "error", err)
		}
	}
}

// systemHealthHandler handles system health validation requests
func (s *Server) systemHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		healthReport, err := s.adminService.ValidateSystemHealth(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to validate system health", "error", err)
			statusCode, errorCode, message := translateDomainError(err)
			_ = WriteErrorWithRequestID(w, statusCode, errorCode, message, requestID)
			return
		}

		s.logger.InfoContext(ctx, "system health validation completed successfully")

		if err := WriteSuccess(w, healthReport); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode health report response", "error", err)
		}
	}
}

// provisioningStatusHandler handles provisioning status requests
func (s *Server) provisioningStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := GetRequestID(ctx)

		// Check if provisioning orchestrator is available
		if s.provisioningOrchestrator == nil {
			s.logger.ErrorContext(ctx, "provisioning orchestrator not available")
			_ = WriteErrorWithRequestID(w, http.StatusServiceUnavailable,
				"service_unavailable",
				"Provisioning orchestrator is not available",
				requestID)
			return
		}

		// Get current provisioning status
		status := s.provisioningOrchestrator.GetCurrentStatus()

		s.logger.InfoContext(ctx, "retrieved provisioning status successfully",
			"is_active", status.IsActive,
			"phase", status.Phase,
			"progress", status.Progress)

		if err := WriteSuccess(w, status); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode provisioning status response", "error", err)
		}
	}
}

// translateDomainError translates domain errors to appropriate HTTP status codes and error messages
func translateDomainError(err error) (statusCode int, errorCode string, message string) {
	// Default to internal server error
	statusCode = http.StatusInternalServerError
	errorCode = "internal_error"
	message = "An internal server error occurred"

	if err == nil {
		return
	}

	// Check for specific error types first
	if provErr, ok := err.(*application.ProvisioningRequiredError); ok {
		statusCode = http.StatusAccepted
		errorCode = "provisioning_required"
		message = provErr.Message
		return
	}

	errStr := err.Error()

	// Check for specific domain error patterns
	switch {
	case contains(errStr, "not found") || contains(errStr, "peer not found") || contains(errStr, "node not found"):
		statusCode = http.StatusNotFound
		errorCode = "resource_not_found"
		message = "The requested resource was not found"

	case contains(errStr, "invalid") || contains(errStr, "validation"):
		statusCode = http.StatusBadRequest
		errorCode = "validation_error"
		message = "Invalid request parameters"

	case contains(errStr, "capacity") || contains(errStr, "full") || contains(errStr, "exhausted"):
		statusCode = http.StatusServiceUnavailable
		errorCode = "capacity_exceeded"
		message = "System capacity exceeded, please try again later"

	case contains(errStr, "timeout") || contains(errStr, "connection"):
		statusCode = http.StatusServiceUnavailable
		errorCode = "service_unavailable"
		message = "Service temporarily unavailable"

	case contains(errStr, "conflict") || contains(errStr, "already exists"):
		statusCode = http.StatusConflict
		errorCode = "resource_conflict"
		message = "Resource conflict detected"

	case contains(errStr, "unauthorized") || contains(errStr, "permission"):
		statusCode = http.StatusForbidden
		errorCode = "access_denied"
		message = "Access denied"

	case contains(errStr, "rate limit") || contains(errStr, "too many"):
		statusCode = http.StatusTooManyRequests
		errorCode = "rate_limit_exceeded"
		message = "Rate limit exceeded"
	}

	return statusCode, errorCode, message
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsSubstring(s, substr))))
}

// containsSubstring performs a simple substring search
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
