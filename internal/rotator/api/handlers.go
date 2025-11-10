package api

import (
	"log/slog"
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// healthHandler returns the service health status.
func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := GetLogger(r.Context())
		op := logger.StartOp(r.Context(), "healthHandler")

		response := api.HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
		}

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
			op.Fail(err, "failed to encode health response")
			return
		}
		op.Complete("health check successful")
	}
}

// connectHandler handles peer connection requests
func (s *Server) connectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "connectHandler")

		var req api.ConnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}
		op.Progress("request parsed")

		if err := ValidateConnectRequest(&req); err != nil {
			op.Fail(err, "request validation failed")
			WriteErrorResponse(w, r, err)
			return
		}
		op.Progress("request validated")

		if req.GenerateKeys {
			keyPair, err := crypto.GenerateKeyPair()
			if err != nil {
				err = apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to generate key pair", false, err)
				op.Fail(err, "key generation failed")
				WriteErrorResponse(w, r, err)
				return
			}
			req.PublicKey = &keyPair.PublicKey
			op.Progress("keys generated")
		}

		if s.provisioningOrchestrator != nil && s.provisioningOrchestrator.IsProvisioning() {
			status := s.provisioningOrchestrator.GetCurrentStatus()
			op.Progress("provisioning active", slog.String("phase", status.Phase))
		}

		response, err := s.vpnService.ConnectPeer(ctx, req)
		if err != nil {
			op.Fail(err, "failed to connect peer")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("peer connected successfully", slog.String("peer_id", response.PeerID))
	}
}

// disconnectHandler handles peer disconnection requests
func (s *Server) disconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "disconnectHandler")

		var req api.DisconnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := ValidateDisconnectRequest(&req); err != nil {
			op.Fail(err, "request validation failed")
			WriteErrorResponse(w, r, err)
			return
		}
		op.With("peer_id", req.PeerID)

		err := s.vpnService.DisconnectPeer(ctx, req.PeerID)
		if err != nil {
			op.Fail(err, "failed to disconnect peer")
			WriteErrorResponse(w, r, err)
			return
		}

		response := api.DisconnectResponse{
			Message: "Peer disconnected successfully",
			PeerID:  req.PeerID,
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("peer disconnected successfully")
	}
}

// listPeersHandler handles peer listing requests
func (s *Server) listPeersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "listPeersHandler")

		params, err := ValidatePeerListParams(r)
		if err != nil {
			op.Fail(err, "failed to validate params")
			WriteErrorResponse(w, r, err)
			return
		}

		peers, err := s.vpnService.ListActivePeers(ctx)
		if err != nil {
			op.Fail(err, "failed to list active peers")
			WriteErrorResponse(w, r, err)
			return
		}

		// This filtering logic should ideally be in the service layer
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

		offset, limit := 0, len(filteredPeers)
		if offsetParam, exists := params["offset"]; exists {
			offset = offsetParam.(int)
		}
		if limitParam, exists := params["limit"]; exists {
			limit = limitParam.(int)
		}

		start, end := offset, offset+limit
		if start > len(filteredPeers) {
			start = len(filteredPeers)
		}
		if end > len(filteredPeers) {
			end = len(filteredPeers)
		}
		paginatedPeers := filteredPeers[start:end]

		peerInfos := make([]api.PeerInfo, len(paginatedPeers))
		for i, peer := range paginatedPeers {
			peerInfos[i] = *peer
		}

		response := api.PeersListResponse{
			Peers:      peerInfos,
			TotalCount: len(filteredPeers),
			Offset:     offset,
			Limit:      limit,
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("listed peers successfully", slog.Int("count", len(peerInfos)))
	}
}

// getPeerHandler handles individual peer status requests
func (s *Server) getPeerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		peerID := r.PathValue("peerID")
		op := logger.StartOp(ctx, "getPeerHandler", slog.String("peer_id", peerID))

		if peerID == "" {
			err := apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "Peer ID is required in URL path", false, nil)
			op.Fail(err, "missing peer id")
			WriteErrorResponse(w, r, err)
			return
		}

		peerStatus, err := s.vpnService.GetPeerStatus(ctx, peerID)
		if err != nil {
			op.Fail(err, "failed to get peer status")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, peerStatus); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("retrieved peer status successfully")
	}
}

// --- Admin handlers ---

func (s *Server) systemStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "systemStatusHandler")
		status, err := s.adminService.GetSystemStatus(r.Context())
		if err != nil {
			op.Fail(err, "failed to get system status")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, status); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved system status")
	}
}

func (s *Server) nodeStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "nodeStatsHandler")
		stats, err := s.adminService.GetNodeStatistics(r.Context())
		if err != nil {
			op.Fail(err, "failed to get node statistics")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, stats); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved node statistics")
	}
}

func (s *Server) peerStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "peerStatsHandler")
		stats, err := s.adminService.GetPeerStatistics(r.Context())
		if err != nil {
			op.Fail(err, "failed to get peer statistics")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, stats); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved peer statistics")
	}
}

func (s *Server) forceRotationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "forceRotationHandler")
		if err := s.adminService.ForceNodeRotation(r.Context()); err != nil {
			op.Fail(err, "failed to force node rotation")
			WriteErrorResponse(w, r, err)
			return
		}
		response := map[string]string{"message": "Node rotation initiated successfully"}
		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("forced node rotation")
	}
}

func (s *Server) rotationStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "rotationStatusHandler")
		status, err := s.adminService.GetRotationStatus(r.Context())
		if err != nil {
			op.Fail(err, "failed to get rotation status")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, status); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved rotation status")
	}
}

func (s *Server) manualCleanupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "manualCleanupHandler")
		var options application.CleanupOptions
		if err := ParseJSONRequest(r, &options); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}
		result, err := s.adminService.ManualCleanup(r.Context(), options)
		if err != nil {
			op.Fail(err, "failed to run manual cleanup")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, result); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("manual cleanup finished")
	}
}

func (s *Server) systemHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "systemHealthHandler")
		report, err := s.adminService.ValidateSystemHealth(r.Context())
		if err != nil {
			op.Fail(err, "failed to validate system health")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := WriteSuccess(w, report); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("system health validation finished")
	}
}

func (s *Server) provisioningStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "provisioningStatusHandler")
		if s.provisioningOrchestrator == nil {
			err := apperrors.NewSystemError(apperrors.ErrCodeInternal, "provisioning orchestrator is not available", false, nil)
			op.Fail(err, "provisioning orchestrator not available")
			WriteErrorResponse(w, r, err)
			return
		}
		if !s.provisioningOrchestrator.IsProvisioning() {
			err := WriteJSON(w, http.StatusServiceUnavailable, api.ProvisioningInfo{IsActive: false})
			if err != nil {
				op.Fail(err, "failed to write response")
			}
			op.Complete("provisioning not active")
			return
		}
		status := s.provisioningOrchestrator.GetCurrentStatus()
		if err := WriteSuccess(w, status); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved provisioning status")
	}
}
