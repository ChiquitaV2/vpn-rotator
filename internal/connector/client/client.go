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
)

// Client represents the API client for the rotator service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
}

// NewClient creates a new API client.
func NewClient(baseURL string, log *logger.Logger) *Client {
	if log == nil {
		log = logger.New("info", "text")
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log,
	}
}

// ConnectPeer connects a peer to the VPN service with key management options.
func (c *Client) ConnectPeer(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	return c.connectPeerWithRetry(ctx, req)
}

// DisconnectPeer disconnects a peer from the VPN service.
func (c *Client) DisconnectPeer(ctx context.Context, req *api.DisconnectRequest) (*api.DisconnectResponse, error) {
	return c.disconnectPeerWithRetry(ctx, req)
}

// GetPeerStatus gets the current status of a peer (for rotation monitoring).
func (c *Client) GetPeerStatus(ctx context.Context, peerID string) (*api.PeerInfo, error) {
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
		var apiResp api.Response[api.PeerInfo]
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

// connectPeerWithRetry implements retry logic for peer connection.
func (c *Client) connectPeerWithRetry(ctx context.Context, req *api.ConnectRequest) (*api.ConnectResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		response, err := c.connectPeerOnce(ctx, req)
		if err == nil {
			if attempt > 0 {
				c.logger.Info("successfully connected peer after retry", "attempt", attempt+1)
			}
			return response, nil
		}

		lastErr = err
		c.logger.Warn("failed to connect peer, will retry", "attempt", attempt+1, "error", err)

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

	return nil, fmt.Errorf("failed to connect peer after retries: %w", lastErr)
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

	case http.StatusServiceUnavailable:
		var provResp api.ProvisioningResponse
		if err := json.Unmarshal(body, &provResp); err != nil {
			return nil, fmt.Errorf("received 503 but failed to decode provisioning response: %w", err)
		}

		c.logger.Warn("no active node available for peer connection",
			"message", provResp.Message,
			"estimated_wait", provResp.EstimatedWait,
			"retry_after", provResp.RetryAfter,
		)
		return nil, fmt.Errorf("no active node available: %s (retry after %d seconds)",
			provResp.Message, provResp.RetryAfter)

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
