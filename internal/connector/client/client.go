package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/gookit/goutil"
)

// ProvisioningInProgressError indicates that node provisioning is in progress
type ProvisioningInProgressError struct {
	Message       string
	EstimatedWait int
	RetryAfter    int
}

func (e *ProvisioningInProgressError) Error() string {
	return e.Message
}

// Client represents the API client for the rotator service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger

	// Provisioning wait configuration
	maxProvisioningWaits            int
	provisioningStatusCheckInterval time.Duration
}

// NewClient creates a new API client.
func NewClient(baseURL string, log *logger.Logger) *Client {
	if log == nil {
		log = logger.NewDevelopment("client")
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:                          log,
		maxProvisioningWaits:            5,
		provisioningStatusCheckInterval: 15 * time.Second,
	}
}

// SetProvisioningWaitConfig configures the provisioning wait behavior
func (c *Client) SetProvisioningWaitConfig(maxWaits int, statusCheckInterval time.Duration) {
	c.maxProvisioningWaits = maxWaits
	c.provisioningStatusCheckInterval = statusCheckInterval
}

// ConnectPeer connects a peer to the VPN service with key management options.
// This method includes intelligent handling of provisioning waits.
func (c *Client) ConnectPeer(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	return c.connectPeerWithRetry(ctx, req)
}

// ConnectPeerWithoutWait connects a peer to the VPN service without waiting for provisioning.
// Returns ProvisioningInProgressError immediately if provisioning is needed.
func (c *Client) ConnectPeerWithoutWait(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	return c.connectPeerOnce(ctx, req)
}

// DisconnectPeer disconnects a peer from the VPN service.
func (c *Client) DisconnectPeer(ctx context.Context, req *api.DisconnectRequest) (*api.DisconnectResponse, error) {
	return c.disconnectPeerWithRetry(ctx, req)
}

// GetProvisioningStatus gets the current provisioning status from the rotator service.
func (c *Client) GetProvisioningStatus(ctx context.Context) (*api.ProvisioningInfo, error) {
	url := fmt.Sprintf("%s/api/v1/provisioning/status", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Debug("making provisioning status API request", "url", url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp api.Response[api.ProvisioningInfo]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		provisioningInfo := apiResp.Data
		c.logger.Debug("fetched provisioning status",
			"is_active", provisioningInfo.IsActive,
			"phase", provisioningInfo.Phase,
			"progress", provisioningInfo.Progress)
		return &provisioningInfo, nil

	case http.StatusServiceUnavailable:
		var apiResp api.Response[api.ProvisioningInfo]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 503 but failed to decode error: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		provisioningInfo := apiResp.Data
		c.logger.Debug("fetched provisioning status",
			"is_active", provisioningInfo.IsActive,
			"phase", provisioningInfo.Phase,
			"progress", provisioningInfo.Progress)
		return &provisioningInfo, nil

	case http.StatusInternalServerError:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 500 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("internal server error: %s (request ID: %s)",
				apiResp.Error.Message, apiResp.Error.RequestID)
		}
		return nil, fmt.Errorf("internal server error (no details provided)")

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}

// GetPeerStatus gets the current status of a peer (for rotation monitoring).
func (c *Client) GetPeerStatus(ctx context.Context, peerID string) (*api.Peer, error) {
	url := fmt.Sprintf("%s/api/v1/peers/%s", c.baseURL, peerID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Debug("making peer status API request", "url", url, "peer_id", peerID)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp api.Response[api.Peer]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		peerInfo := apiResp.Data
		c.logger.Debug("fetched peer status", "peer_id", peerInfo.ID, "status", peerInfo.Status, "node_id", peerInfo.NodeID)
		return &peerInfo, nil

	case http.StatusNotFound:
		return nil, fmt.Errorf("peer not found")

	case http.StatusInternalServerError:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 500 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("internal server error: %s (request ID: %s)",
				apiResp.Error.Message, apiResp.Error.RequestID)
		}
		return nil, fmt.Errorf("internal server error (no details provided)")

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}

// connectPeerWithRetry implements retry logic for peer connection with intelligent provisioning wait handling.
func (c *Client) connectPeerWithRetry(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	var lastErr error
	maxAttempts := 3
	provisioningWaitAttempts := 0
	maxProvisioningWaits := c.maxProvisioningWaits

	for attempt := 0; attempt < maxAttempts; attempt++ {
		response, err := c.connectPeerOnce(ctx, req)
		if err == nil {
			if attempt > 0 || provisioningWaitAttempts > 0 {
				c.logger.Info("successfully connected peer after retry",
					"attempt", attempt+1,
					"provisioning_waits", provisioningWaitAttempts)
			}
			return response, nil
		}

		// Check if this is a provisioning in progress error
		if provErr, ok := err.(*ProvisioningInProgressError); ok {
			provisioningWaitAttempts++

			if provisioningWaitAttempts > maxProvisioningWaits {
				c.logger.Error("exceeded maximum provisioning wait attempts",
					"max_waits", maxProvisioningWaits,
					"total_wait_time", provisioningWaitAttempts*provErr.RetryAfter)
				return nil, fmt.Errorf("provisioning taking too long, exceeded %d wait attempts: %w", maxProvisioningWaits, err)
			}

			c.logger.Info("node provisioning in progress, waiting before retry",
				"attempt", provisioningWaitAttempts,
				"max_attempts", maxProvisioningWaits,
				"message", provErr.Message,
				"estimated_wait", provErr.EstimatedWait,
				"retry_after", provErr.RetryAfter)

			// Wait for the suggested retry time
			waitTime := time.Duration(provErr.RetryAfter) * time.Second

			// Also check provisioning status periodically while waiting
			if err := c.waitWithProvisioningStatusCheck(ctx, waitTime); err != nil {
				return nil, fmt.Errorf("context cancelled during provisioning wait: %w", err)
			}

			// Reset regular retry attempt counter for provisioning waits
			// but continue with the same attempt number
			continue
		}

		lastErr = err
		c.logger.Warn("failed to connect peer, will retry", "attempt", attempt+1, "error", err)

		// Don't sleep on the last attempt or if context is cancelled
		if attempt == maxAttempts-1 || ctx.Err() != nil {
			break
		}

		// Exponential backoff for regular errors: 1s, 2s, 4s
		waitTime := time.Duration(1<<uint(attempt)) * time.Second
		select {
		case <-time.After(waitTime):
			c.logger.Debug("retrying after backoff", "wait_time", waitTime)
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		}
	}

	return nil, fmt.Errorf("failed to connect peer after retries: %w", lastErr)
}

// waitWithProvisioningStatusCheck waits for the specified duration while periodically checking provisioning status
func (c *Client) waitWithProvisioningStatusCheck(ctx context.Context, waitTime time.Duration) error {
	c.logger.Debug("waiting with provisioning status monitoring", "wait_time", waitTime)

	// Check provisioning status at configured intervals while waiting
	statusCheckInterval := c.provisioningStatusCheckInterval
	if waitTime < statusCheckInterval {
		statusCheckInterval = waitTime / 3 // Check at least 3 times during the wait
		if statusCheckInterval < 5*time.Second {
			statusCheckInterval = 5 * time.Second // Minimum 5 second intervals
		}
	}

	deadline := time.Now().Add(waitTime)
	ticker := time.NewTicker(statusCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Check if we've waited long enough
			if time.Now().After(deadline) {
				c.logger.Debug("provisioning wait period completed")
				return nil
			}

			// Check provisioning status
			status, err := c.GetProvisioningStatus(ctx)
			if err != nil {
				c.logger.Warn("failed to check provisioning status during wait", "error", err)
				continue
			}

			if status.IsActive {
				c.logger.Info("provisioning status update",
					"phase", status.Phase,
					"progress", fmt.Sprintf("%.1f%%", status.Progress*100))

				if status.EstimatedETA != nil {
					remaining := time.Until(*status.EstimatedETA)
					if remaining > 0 {
						c.logger.Info("provisioning ETA update", "remaining", remaining)
					}
				}
			} else {
				c.logger.Info("provisioning appears to be complete, will retry connection soon")
				// If provisioning is no longer active, we can try connecting sooner
				return nil
			}
		}
	}
}

// connectPeerOnce makes a single API call to connect a peer.
func (c *Client) connectPeerOnce(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	url := fmt.Sprintf("%s/api/v1/connect", c.baseURL)

	// Marshal request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connect request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	c.logger.Debug("making connect API request", "url", url)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp api.Response[api.ConnectResponse]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		connectResp := apiResp.Data

		// Validate required fields
		if connectResp.ServerPublicKey == "" || connectResp.ServerIP == "" || connectResp.ClientIP == "" {
			return nil, fmt.Errorf("invalid response: missing required fields")
		}

		// Set default port if not provided
		if connectResp.ServerPort == 0 {
			connectResp.ServerPort = 51820
		}

		c.logger.Info("successfully connected peer",
			"peer_id", connectResp.PeerID,
			"server_ip", connectResp.ServerIP,
			"client_ip", connectResp.ClientIP,
		)
		return &connectResp, nil

	case http.StatusBadRequest:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 400 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("bad request: %s", apiResp.Error.Message)
		}
		return nil, fmt.Errorf("bad request (no details provided)")

	case http.StatusAccepted:
		// HTTP 202 - Provisioning in progress
		var provisioningResp map[string]interface{}
		if err := json.Unmarshal(body, &provisioningResp); err != nil {
			return nil, fmt.Errorf("received 202 but failed to decode provisioning response: %w", err)
		}

		message := "Node provisioning in progress"
		estimatedWait := 180 // Default 3 minutes
		retryAfter := 90     // Default 1.5 minutes

		if msg, ok := provisioningResp["message"].(string); ok {
			message = msg
		}
		if wait, ok := provisioningResp["estimated_wait"].(float64); ok {
			estimatedWait = int(wait)
		}
		if retry, ok := provisioningResp["retry_after"].(float64); ok {
			retryAfter = int(retry)
		}

		c.logger.Info("node provisioning triggered for peer connection",
			"message", message,
			"estimated_wait", estimatedWait,
			"retry_after", retryAfter,
		)

		// Return a special error that indicates provisioning is in progress
		return nil, &ProvisioningInProgressError{
			Message:       message,
			EstimatedWait: estimatedWait,
			RetryAfter:    retryAfter,
		}

	case http.StatusServiceUnavailable:
		var provResp api.ProvisioningInfo
		if err := json.Unmarshal(body, &provResp); err != nil {
			return nil, fmt.Errorf("received 503 but failed to decode provisioning response: %w", err)
		}

		// Extract estimated wait from provisioning info
		estimatedWait := 0
		if provResp.EstimatedETA != nil {
			estimatedWait = int(time.Until(*provResp.EstimatedETA).Seconds())
		}

		retryAfter := 0
		if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
			if val, err := goutil.ToInt(retryHeader); err == nil {
				retryAfter = val
			}
		}

		c.logger.Warn("no active node available for peer connection",
			"phase", provResp.Phase,
			"progress", provResp.Progress,
			"estimated_wait", estimatedWait,
			"retry_after", retryAfter,
		)

		message := fmt.Sprintf("Service provisioning in progress (phase: %s, progress: %.1f%%)", provResp.Phase, provResp.Progress*100)
		return nil, &ProvisioningInProgressError{
			Message:       message,
			EstimatedWait: estimatedWait,
			RetryAfter:    retryAfter,
		}

	case http.StatusInternalServerError:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 500 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("internal server error: %s (request ID: %s)",
				apiResp.Error.Message, apiResp.Error.RequestID)
		}
		return nil, fmt.Errorf("internal server error (no details provided)")

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}

// disconnectPeerWithRetry implements retry logic for peer disconnection.
func (c *Client) disconnectPeerWithRetry(ctx context.Context, req *api.DisconnectRequest) (*api.DisconnectResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		response, err := c.disconnectPeerOnce(ctx, req)
		if err == nil {
			if attempt > 0 {
				c.logger.Info("successfully disconnected peer after retry", "attempt", attempt+1)
			}
			return response, nil
		}

		lastErr = err
		c.logger.Warn("failed to disconnect peer, will retry", "attempt", attempt+1, "error", err)

		// Don't sleep on the last attempt or if context is cancelled
		if attempt == 2 || ctx.Err() != nil {
			break
		}

		// Exponential backoff: 1s, 2s, 4s
		waitTime := time.Duration(1<<uint(attempt)) * time.Second
		select {
		case <-time.After(waitTime):
			c.logger.Debug("retrying after backoff", "wait_time", waitTime)
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		}
	}

	return nil, fmt.Errorf("failed to disconnect peer after retries: %w", lastErr)
}

// GetHealth gets the health status of the rotator service including provisioning information.
func (c *Client) GetHealth(ctx context.Context) (*api.HealthResponse, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Debug("making health API request", "url", url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp api.Response[api.HealthResponse]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		healthResp := apiResp.Data
		c.logger.Debug("fetched health status", "status", healthResp.Status)
		if healthResp.Provisioning != nil {
			c.logger.Debug("provisioning status in health",
				"is_active", healthResp.Provisioning.IsActive,
				"phase", healthResp.Provisioning.Phase)
		}
		return &healthResp, nil

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}

// disconnectPeerOnce makes a single API call to disconnect a peer.
func (c *Client) disconnectPeerOnce(ctx context.Context, req *api.DisconnectRequest) (*api.DisconnectResponse, error) {
	url := fmt.Sprintf("%s/api/v1/disconnect", c.baseURL)

	// Marshal request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal disconnect request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	c.logger.Debug("making disconnect API request", "url", url)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp api.Response[api.DisconnectResponse]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		disconnectResp := apiResp.Data
		c.logger.Info("successfully disconnected peer", "peer_id", disconnectResp.PeerID)
		return &disconnectResp, nil

	case http.StatusNotFound:
		return nil, fmt.Errorf("peer not found")

	case http.StatusBadRequest:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 400 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("bad request: %s", apiResp.Error.Message)
		}
		return nil, fmt.Errorf("bad request (no details provided)")

	case http.StatusInternalServerError:
		var apiResp api.Response[any]
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 500 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			return nil, fmt.Errorf("internal server error: %s (request ID: %s)",
				apiResp.Error.Message, apiResp.Error.RequestID)
		}
		return nil, fmt.Errorf("internal server error (no details provided)")

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}
