package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// NodeConfig represents the VPN node configuration from the API.
type NodeConfig struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	Port            int    `json:"port"`
}

// APIResponse represents the standard API response wrapper.
type APIResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *ErrorInfo      `json:"error,omitempty"`
}

// ErrorInfo contains detailed error information.
type ErrorInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// ProvisioningResponse represents a response when provisioning is in progress.
type ProvisioningResponse struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	EstimatedWait int    `json:"estimated_wait_seconds"`
	RetryAfter    int    `json:"retry_after_seconds"`
}

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

// FetchNodeConfig fetches the latest active VPN node configuration.
func (c *Client) FetchNodeConfig(ctx context.Context) (*NodeConfig, error) {
	return c.fetchNodeConfigWithRetry(ctx)
}

// fetchNodeConfigWithRetry implements retry logic with exponential backoff.
func (c *Client) fetchNodeConfigWithRetry(ctx context.Context) (*NodeConfig, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		config, err := c.fetchNodeConfigOnce(ctx)
		if err == nil {
			if attempt > 0 {
				c.logger.Info("successfully fetched config after retry", "attempt", attempt+1)
			}
			return config, nil
		}

		lastErr = err
		c.logger.Warn("failed to fetch config, will retry", "attempt", attempt+1, "error", err)

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

	return nil, fmt.Errorf("failed to fetch node config after retries: %w", lastErr)
}

// fetchNodeConfigOnce makes a single API call to fetch node config.
func (c *Client) fetchNodeConfigOnce(ctx context.Context) (*NodeConfig, error) {
	url := fmt.Sprintf("%s/api/v1/config/latest", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Debug("making API request", "url", url)
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
		var apiResp APIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}

		if !apiResp.Success {
			return nil, fmt.Errorf("API returned success=false without error details")
		}

		var config NodeConfig
		if err := json.Unmarshal(apiResp.Data, &config); err != nil {
			return nil, fmt.Errorf("failed to decode node config: %w", err)
		}

		// Validate required fields
		if config.ServerPublicKey == "" || config.ServerIP == "" {
			return nil, fmt.Errorf("invalid response: missing required fields")
		}

		// Set default port if not provided
		if config.Port == 0 {
			config.Port = 51820
		}

		c.logger.Info("fetched node config", "server_ip", config.ServerIP, "port", config.Port)
		return &config, nil

	case http.StatusServiceUnavailable:
		var provResp ProvisioningResponse
		if err := json.Unmarshal(body, &provResp); err != nil {
			return nil, fmt.Errorf("received 503 but failed to decode provisioning response: %w", err)
		}

		c.logger.Warn("no active node available",
			"message", provResp.Message,
			"estimated_wait", provResp.EstimatedWait,
			"retry_after", provResp.RetryAfter,
		)
		return nil, fmt.Errorf("no active node available: %s (retry after %d seconds)",
			provResp.Message, provResp.RetryAfter)

	case http.StatusInternalServerError:
		var apiResp APIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("received 500 but failed to decode error: %w", err)
		}

		if apiResp.Error != nil {
			c.logger.Error("internal server error from API",
				"request_id", apiResp.Error.RequestID,
				"code", apiResp.Error.Code,
				"message", apiResp.Error.Message,
			)
			return nil, fmt.Errorf("internal server error: %s (request ID: %s)",
				apiResp.Error.Message, apiResp.Error.RequestID)
		}

		return nil, fmt.Errorf("internal server error (no details provided)")

	default:
		c.logger.Error("unexpected API response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API returned unexpected status %d", resp.StatusCode)
	}
}
