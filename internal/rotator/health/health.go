package health

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HealthChecker defines the interface for checking the health of a VPN node.
type HealthChecker interface {
	Check(ctx context.Context, ip string) error
}

// NewHTTPHealthChecker creates a new HTTP health checker.
func NewHTTPHealthChecker() HealthChecker {
	return &httpHealthChecker{}
}

type httpHealthChecker struct{}

func (h *httpHealthChecker) Check(ctx context.Context, ip string) error {
	client := &http.Client{
		Timeout: 10 * time.Second, // Increased timeout for cloud-init completion
	}

	// Use the health endpoint on port 8080
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:8080/health", ip), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status code: %d", resp.StatusCode)
	}

	return nil
}
