package api

import (
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

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
		if s.provisioningService == nil {
			err := apperrors.NewSystemError(apperrors.ErrCodeInternal, "provisioning orchestrator is not available", false, nil)
			op.Fail(err, "provisioning orchestrator not available")
			WriteErrorResponse(w, r, err)
			return
		}
		if !s.provisioningService.IsProvisioning() {
			err := WriteJSON(w, http.StatusServiceUnavailable, api.ProvisioningInfo{IsActive: false})
			if err != nil {
				op.Fail(err, "failed to write response")
			}
			op.Complete("provisioning not active")
			return
		}
		status := s.provisioningService.GetCurrentStatus()
		if err := WriteSuccess(w, status); err != nil {
			op.Fail(err, "failed to write response")
			return
		}
		op.Complete("retrieved provisioning status")
	}
}
