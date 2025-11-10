package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

func TestClient_GetPeerStatus(t *testing.T) {
	log := logger.NewDevelopment("client_test")

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/peers/peer-123" {
				t.Errorf("expected path /api/v1/peers/peer-123, got %s", r.URL.Path)
			}
			if r.Method != "GET" {
				t.Errorf("expected method GET, got %s", r.Method)
			}

			resp := api.Response[api.PeerInfo]{
				Success: true,
				Data: api.PeerInfo{
					ID:     "peer-123",
					Status: "connected",
					NodeID: "node-456",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		peerInfo, err := client.GetPeerStatus(context.Background(), "peer-123")

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if peerInfo == nil {
			t.Fatal("expected peer info, got nil")
		}
		if peerInfo.ID != "peer-123" {
			t.Errorf("expected peer ID peer-123, got %s", peerInfo.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.GetPeerStatus(context.Background(), "peer-123")

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if err.Error() != "peer not found" {
			t.Errorf("expected error 'peer not found', got '%v'", err)
		}
	})

	t.Run("internal server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := api.Response[any]{
				Success: false,
				Error: &api.ErrorInfo{
					Message:   "db is down",
					RequestID: "req-abc",
				},
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.GetPeerStatus(context.Background(), "peer-123")

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "internal server error: db is down") {
			t.Errorf("expected error to contain 'internal server error: db is down', got '%v'", err)
		}
	})
}

func TestClient_ConnectPeer(t *testing.T) {
	log := logger.NewDevelopment("client_test")
	key := "test-key"
	connectReq := &api.ConnectRequest{PublicKey: &key, GenerateKeys: false}

	t.Run("success on first attempt", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/connect" {
				t.Errorf("expected path /api/v1/connect, got %s", r.URL.Path)
			}
			if r.Method != "POST" {
				t.Errorf("expected method POST, got %s", r.Method)
			}

			resp := api.Response[api.ConnectResponse]{
				Success: true,
				Data: api.ConnectResponse{
					PeerID:          "peer-123",
					ServerPublicKey: "server-key",
					ServerIP:        "1.2.3.4",
					ClientIP:        "10.0.0.2",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		resp, err := client.ConnectPeer(context.Background(), connectReq)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected a response, got nil")
		}
		if resp.PeerID != "peer-123" {
			t.Errorf("expected peer ID peer-123, got %s", resp.PeerID)
		}
		if resp.ServerPort != 51820 {
			t.Errorf("expected default port 51820, got %d", resp.ServerPort)
		}
	})

	t.Run("success on retry", func(t *testing.T) {
		attempt := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if attempt == 0 {
				w.WriteHeader(http.StatusInternalServerError)
				attempt++
				return
			}
			resp := api.Response[api.ConnectResponse]{
				Success: true,
				Data: api.ConnectResponse{
					PeerID:          "peer-123",
					ServerPublicKey: "server-key",
					ServerIP:        "1.2.3.4",
					ClientIP:        "10.0.0.2",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		client.httpClient.Timeout = 5 * time.Second
		resp, err := client.ConnectPeer(context.Background(), connectReq)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected a response, got nil")
		}
		if resp.PeerID != "peer-123" {
			t.Errorf("expected peer ID peer-123, got %s", resp.PeerID)
		}
	})

	t.Run("failure after retries", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		client.httpClient.Timeout = 5 * time.Second
		_, err := client.ConnectPeer(context.Background(), connectReq)

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to connect peer after retries") {
			t.Errorf("expected error to contain 'failed to connect peer after retries', got '%v'", err)
		}
	})
}

func TestClient_DisconnectPeer(t *testing.T) {
	log := logger.NewDevelopment("client_test")
	disconnectReq := &api.DisconnectRequest{PeerID: "peer-123"}

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/disconnect" {
				t.Errorf("expected path /api/v1/disconnect, got %s", r.URL.Path)
			}
			if r.Method != "DELETE" {
				t.Errorf("expected method DELETE, got %s", r.Method)
			}

			resp := api.Response[api.DisconnectResponse]{
				Success: true,
				Data: api.DisconnectResponse{
					PeerID:  "peer-123",
					Message: "disconnected",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		resp, err := client.DisconnectPeer(context.Background(), disconnectReq)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected a response, got nil")
		}
		if resp.PeerID != "peer-123" {
			t.Errorf("expected peer ID peer-123, got %s", resp.PeerID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.DisconnectPeer(context.Background(), disconnectReq)

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "peer not found") {
			t.Errorf("expected error to contain 'peer not found', got '%v'", err)
		}
	})

	t.Run("provisioning in progress - wait and retry", func(t *testing.T) {
		key := "test-key"
		connectReq := &api.ConnectRequest{PublicKey: &key, GenerateKeys: false}
		attempt := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/provisioning/status" {
				// Mock provisioning status endpoint
				provisioningInfo := api.ProvisioningInfo{
					IsActive: attempt == 0, // Active on first check, inactive on second
					Phase:    "cloud_provision",
					Progress: 0.7,
				}
				resp := api.Response[api.ProvisioningInfo]{
					Success: true,
					Data:    provisioningInfo,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}

			if attempt == 0 {
				// First attempt - return provisioning in progress
				provisioningResp := map[string]interface{}{
					"error":          "provisioning_required",
					"message":        "Node provisioning in progress",
					"estimated_wait": 10,
					"retry_after":    5,
				}
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(provisioningResp)
				attempt++
				return
			}

			// Second attempt - success
			resp := api.Response[api.ConnectResponse]{
				Success: true,
				Data: api.ConnectResponse{
					PeerID:          "peer-123",
					ServerPublicKey: "server-key",
					ServerIP:        "1.2.3.4",
					ClientIP:        "10.0.0.2",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		// Configure shorter waits for testing
		client.SetProvisioningWaitConfig(2, 1*time.Second)

		start := time.Now()
		resp, err := client.ConnectPeer(context.Background(), connectReq)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected a response, got nil")
		}
		if resp.PeerID != "peer-123" {
			t.Errorf("expected peer ID peer-123, got %s", resp.PeerID)
		}

		// Should have waited some time for provisioning (but mock completes quickly)
		if duration < 500*time.Millisecond {
			t.Errorf("expected to wait at least 500ms for provisioning, but only waited %v", duration)
		}
	})

	t.Run("provisioning timeout", func(t *testing.T) {
		key := "test-key"
		connectReq := &api.ConnectRequest{PublicKey: &key, GenerateKeys: false}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/provisioning/status" {
				// Mock provisioning status endpoint - always active
				provisioningInfo := api.ProvisioningInfo{
					IsActive: true,
					Phase:    "cloud_provision",
					Progress: 0.3,
				}
				resp := api.Response[api.ProvisioningInfo]{
					Success: true,
					Data:    provisioningInfo,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}

			// Always return provisioning in progress
			provisioningResp := map[string]interface{}{
				"error":          "provisioning_required",
				"message":        "Node provisioning in progress",
				"estimated_wait": 60,
				"retry_after":    2,
			}
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(provisioningResp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		// Configure to timeout quickly for testing
		client.SetProvisioningWaitConfig(2, 500*time.Millisecond)

		_, err := client.ConnectPeer(context.Background(), connectReq)

		if err == nil {
			t.Fatal("expected an error due to provisioning timeout, got nil")
		}
		if !strings.Contains(err.Error(), "exceeded") {
			t.Errorf("expected error to contain 'exceeded', got '%v'", err)
		}
	})

	t.Run("connect without wait - immediate provisioning error", func(t *testing.T) {
		key := "test-key"
		connectReq := &api.ConnectRequest{PublicKey: &key, GenerateKeys: false}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provisioningResp := map[string]interface{}{
				"error":          "provisioning_required",
				"message":        "Node provisioning in progress",
				"estimated_wait": 180,
				"retry_after":    90,
			}
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(provisioningResp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.ConnectPeerWithoutWait(context.Background(), connectReq)

		if err == nil {
			t.Fatal("expected a provisioning error, got nil")
		}

		if provErr, ok := err.(*ProvisioningInProgressError); ok {
			if provErr.EstimatedWait != 180 {
				t.Errorf("expected estimated wait 180, got %d", provErr.EstimatedWait)
			}
			if provErr.RetryAfter != 90 {
				t.Errorf("expected retry after 90, got %d", provErr.RetryAfter)
			}
		} else {
			t.Errorf("expected ProvisioningInProgressError, got %T: %v", err, err)
		}
	})
}
