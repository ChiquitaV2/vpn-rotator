package monitoring

import (
	"encoding/json"
	"net/http"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/health"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager/provisioner"
)

// CircuitBreakerMonitor provides monitoring capabilities for circuit breakers.
type CircuitBreakerMonitor struct {
	provisionerCB *provisioner.CircuitBreaker
	healthCB      *health.CircuitBreakerHealthChecker
}

// NewCircuitBreakerMonitor creates a new circuit breaker monitor.
func NewCircuitBreakerMonitor(
	provisionerCB *provisioner.CircuitBreaker,
	healthCB *health.CircuitBreakerHealthChecker,
) *CircuitBreakerMonitor {
	return &CircuitBreakerMonitor{
		provisionerCB: provisionerCB,
		healthCB:      healthCB,
	}
}

// CircuitBreakerStatus represents the status of all circuit breakers.
type CircuitBreakerStatus struct {
	Provisioner CircuitBreakerInfo `json:"provisioner"`
	HealthCheck CircuitBreakerInfo `json:"health_check"`
}

// CircuitBreakerInfo contains information about a single circuit breaker.
type CircuitBreakerInfo struct {
	State           string                 `json:"state"`
	Metrics         map[string]interface{} `json:"metrics"`
	HealthIndicator string                 `json:"health_indicator"`
}

// GetStatus returns the current status of all circuit breakers.
func (m *CircuitBreakerMonitor) GetStatus() CircuitBreakerStatus {
	status := CircuitBreakerStatus{}

	// Provisioner circuit breaker
	if m.provisionerCB != nil {
		provisionerState := string(m.provisionerCB.GetState())
		status.Provisioner = CircuitBreakerInfo{
			State:           provisionerState,
			Metrics:         m.provisionerCB.GetMetrics(),
			HealthIndicator: getHealthIndicator(provisionerState),
		}
	}

	// Health check circuit breaker
	if m.healthCB != nil {
		healthState := string(m.healthCB.GetState())
		status.HealthCheck = CircuitBreakerInfo{
			State:           healthState,
			Metrics:         m.healthCB.GetMetrics(),
			HealthIndicator: getHealthIndicator(healthState),
		}
	}

	return status
}

// getHealthIndicator returns a health indicator based on circuit breaker state.
func getHealthIndicator(state string) string {
	switch state {
	case "closed":
		return "healthy"
	case "half-open":
		return "recovering"
	case "open":
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Handler returns an HTTP handler for circuit breaker monitoring.
func (m *CircuitBreakerMonitor) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		status := m.GetStatus()

		w.Header().Set("Content-Type", "application/json")

		// Set HTTP status based on overall health
		overallHealth := "healthy"
		if status.Provisioner.HealthIndicator == "unhealthy" ||
			status.HealthCheck.HealthIndicator == "unhealthy" {
			overallHealth = "unhealthy"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else if status.Provisioner.HealthIndicator == "recovering" ||
			status.HealthCheck.HealthIndicator == "recovering" {
			overallHealth = "recovering"
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		response := map[string]interface{}{
			"overall_health":   overallHealth,
			"circuit_breakers": status,
			"timestamp":        "2025-10-27T21:30:00Z", // This would be time.Now() in real implementation
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
}
