package api

import (
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// systemStatusHandler returns overall system status via AdminOrchestrator
func (s *Server) systemStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "systemStatusHandler")
		status, err := s.adminOrchestrator.GetSystemStatus(r.Context())
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

// nodeStatsHandler returns node statistics via AdminOrchestrator
func (s *Server) nodeStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "nodeStatsHandler")
		stats, err := s.adminOrchestrator.GetNodeStatistics(r.Context())
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

// peerStatsHandler returns peer statistics via AdminOrchestrator
func (s *Server) peerStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "peerStatsHandler")
		stats, err := s.adminOrchestrator.GetPeerStatistics(r.Context())
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

// forceRotationHandler triggers manual rotation via NodeOrchestrator
func (s *Server) forceRotationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "forceRotationHandler")
		if err := s.nodeOrchestrator.ForceRotation(r.Context()); err != nil {
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

// rotationStatusHandler returns rotation status via NodeOrchestrator
func (s *Server) rotationStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "rotationStatusHandler")
		status, err := s.nodeOrchestrator.GetRotationStatus(r.Context())
		if err != nil {
			op.Fail(err, "failed to get rotation status")
			WriteErrorResponse(w, r, err)
			return
		}

		// Convert to API response
		apiStatus := api.RotationStatus{
			InProgress:        status.InProgress,
			LastRotation:      status.LastRotation,
			NodesRotated:      status.NodesRotated,
			PeersMigrated:     status.PeersMigrated,
			RotationReason:    status.RotationReason,
			EstimatedComplete: status.EstimatedComplete,
		}

		if err := WriteSuccess(w, apiStatus); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved rotation status")
	}
}

// manualCleanupHandler performs manual cleanup via ResourceCleanupService
func (s *Server) manualCleanupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "manualCleanupHandler")
		var options services.CleanupOptions
		if err := ParseJSONRequest(r, &options); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}
		if err := s.resourceCleanupService.CleanupInactiveResourcesWithOptions(r.Context(), options); err != nil {
			op.Fail(err, "failed to run manual cleanup")
			WriteErrorResponse(w, r, err)
			return
		}

		result := api.CleanupResult{
			Timestamp: time.Now(),
			Duration:  time.Since(op.StartTime).Milliseconds(),
		}

		if err := WriteSuccess(w, result); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("manual cleanup finished")
	}
}

// systemHealthHandler validates system health via AdminOrchestrator
func (s *Server) systemHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "systemHealthHandler")
		report, err := s.adminOrchestrator.ValidateSystemHealth(r.Context())
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

		status, err := s.nodeOrchestrator.GetProvisioningStatus(r.Context())
		if err != nil {
			op.Fail(err, "failed to get provisioning status")
			WriteErrorResponse(w, r, err)
			return
		}

		// Return empty provisioning info if not active
		if status == nil {
			if err := WriteSuccess(w, api.ProvisioningInfo{IsActive: false}); err != nil {
				op.Fail(err, "failed to write response")
			}
			op.Complete("provisioning not active")
			return
		}

		// Convert and return active status
		response := api.ProvisioningInfo{
			IsActive:     status.IsActive,
			Phase:        status.Phase,
			Progress:     status.Progress,
			EstimatedETA: status.EstimatedETA,
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved provisioning status")
	}
}
