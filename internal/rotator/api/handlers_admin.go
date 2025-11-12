package api

import (
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// --- Admin handlers ---

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

// New-style route names used by server.go
func (s *Server) adminStatusHandler() http.HandlerFunc         { return s.systemStatusHandler() }
func (s *Server) adminNodeStatsHandler() http.HandlerFunc      { return s.nodeStatsHandler() }
func (s *Server) adminPeerStatsHandler() http.HandlerFunc      { return s.peerStatsHandler() }
func (s *Server) adminForceRotationHandler() http.HandlerFunc  { return s.forceRotationHandler() }
func (s *Server) adminRotationStatusHandler() http.HandlerFunc { return s.rotationStatusHandler() }
func (s *Server) adminManualCleanupHandler() http.HandlerFunc  { return s.manualCleanupHandler() }
func (s *Server) adminSystemHealthHandler() http.HandlerFunc   { return s.systemHealthHandler() }

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

// forceRotationHandler triggers manual rotation via AdminOrchestrator
func (s *Server) forceRotationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "forceRotationHandler")
		if err := s.adminOrchestrator.ForceNodeRotation(r.Context()); err != nil {
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

// rotationStatusHandler returns rotation status via AdminOrchestrator
func (s *Server) rotationStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "rotationStatusHandler")
		status, err := s.adminOrchestrator.GetRotationStatus(r.Context())
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

// manualCleanupHandler performs manual cleanup via AdminOrchestrator
func (s *Server) manualCleanupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := GetLogger(r.Context()).StartOp(r.Context(), "manualCleanupHandler")
		var options services.CleanupOptions
		if err := ParseJSONRequest(r, &options); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}
		result, err := s.adminOrchestrator.ManualCleanup(r.Context(), options)
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

		if !s.vpnService.IsProvisioning() {
			err := WriteJSON(w, http.StatusServiceUnavailable, api.ProvisioningInfo{IsActive: false})
			if err != nil {
				op.Fail(err, "failed to write response")
			}
			op.Complete("provisioning not active")
			return
		}

		status, err := s.vpnService.GetProvisioningStatus(r.Context())
		if err != nil {
			op.Fail(err, "failed to get provisioning status")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, status); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved provisioning status")
	}
}
