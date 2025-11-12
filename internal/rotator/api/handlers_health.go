package api

import (
	"net/http"
)

// healthHandler returns the service health status.
func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := GetLogger(r.Context())
		op := logger.StartOp(r.Context(), "healthHandler")

		response, err := s.healthService.GetHealth(r.Context())
		if err != nil {
			op.Fail(err, "failed to get health status")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode health response")
			return
		}
		op.Complete("health check successful")
	}
}
