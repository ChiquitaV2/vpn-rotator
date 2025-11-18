package api

import (
	"net/http"

	pkgapi "github.com/chiquitav2/vpn-rotator/pkg/api"
)

// healthHandler returns the service health status by querying NodeOrchestrator for provisioning status.
func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := GetLogger(r.Context())
		op := logger.StartOp(r.Context(), "healthHandler")

		// Build health response directly without intermediate service layer
		response := &pkgapi.HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
		}

		// Check provisioning status
		if s.nodeOrchestrator.IsProvisioning() {
			provStatus, err := s.nodeOrchestrator.GetProvisioningStatus(r.Context())
			if err == nil && provStatus != nil {
				response.Provisioning = &pkgapi.ProvisioningInfo{
					IsActive:     provStatus.IsActive,
					Phase:        provStatus.Phase,
					Progress:     provStatus.Progress,
					EstimatedETA: provStatus.EstimatedETA,
				}
			}
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode health response")
			return
		}
		op.Complete("health check successful")
	}
}
