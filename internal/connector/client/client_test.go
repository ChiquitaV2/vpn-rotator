package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

			resp := api.Response[api.Peer]{
				Success: true,
				Data: api.Peer{
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

func TestClient_GetConnectionStatus(t *testing.T) {
	log := logger.NewDevelopment("client_test")

	t.Run("success - completed connection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/connections/req-123" {
				t.Errorf("expected path /api/v1/connections/req-123, got %s", r.URL.Path)
			}
			if r.Method != "GET" {
				t.Errorf("expected method GET, got %s", r.Method)
			}

			resp := api.Response[api.PeerConnectionStatus]{
				Success: true,
				Data: api.PeerConnectionStatus{
					RequestID: "req-123",
					PeerID:    "peer-456",
					Phase:     "completed",
					Progress:  100.0,
					IsActive:  false,
					Message:   "Connection completed successfully",
					ConnectionDetails: &api.ConnectResponse{
						PeerID:          "peer-456",
						ServerPublicKey: "server-key",
						ServerIP:        "1.2.3.4",
						ServerPort:      51820,
						ClientIP:        "10.0.0.2",
						DNS:             []string{"8.8.8.8"},
						AllowedIPs:      []string{"0.0.0.0/0"},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		status, err := client.GetConnectionStatus(context.Background(), "req-123")

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if status == nil {
			t.Fatal("expected status, got nil")
		}
		if status.Phase != "completed" {
			t.Errorf("expected phase 'completed', got %s", status.Phase)
		}
		if status.ConnectionDetails == nil {
			t.Fatal("expected connection details, got nil")
		}
		if status.ConnectionDetails.PeerID != "peer-456" {
			t.Errorf("expected peer ID peer-456, got %s", status.ConnectionDetails.PeerID)
		}
	})

	t.Run("success - in progress", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := api.Response[api.PeerConnectionStatus]{
				Success: true,
				Data: api.PeerConnectionStatus{
					RequestID: "req-123",
					Phase:     "provisioning_node",
					Progress:  40.0,
					IsActive:  true,
					Message:   "Provisioning WireGuard node...",
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		status, err := client.GetConnectionStatus(context.Background(), "req-123")

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if status.Phase != "provisioning_node" {
			t.Errorf("expected phase 'provisioning_node', got %s", status.Phase)
		}
		if status.Progress != 40.0 {
			t.Errorf("expected progress 40.0, got %f", status.Progress)
		}
		if !status.IsActive {
			t.Error("expected IsActive to be true")
		}
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.GetConnectionStatus(context.Background(), "req-unknown")

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "connection request not found") {
			t.Errorf("expected error to contain 'connection request not found', got '%v'", err)
		}
	})
}

func TestClient_ConnectPeer(t *testing.T) {
	log := logger.NewDevelopment("client_test")
	key := "test-key"
	connectReq := &api.ConnectRequest{PublicKey: &key, GenerateKeys: false}

	t.Run("async success - immediate completion", func(t *testing.T) {
		pollCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/connect" && r.Method == "POST" {
				// Return 202 Accepted with request ID
				resp := api.Response[map[string]string]{
					Success: true,
					Data: map[string]string{
						"request_id": "req-123",
						"message":    "Connection request accepted. Poll /api/v1/connections/req-123 for status",
					},
				}
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(resp)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/api/v1/connections/") && r.Method == "GET" {
				pollCount++
				// Return completed status immediately
				resp := api.Response[api.PeerConnectionStatus]{
					Success: true,
					Data: api.PeerConnectionStatus{
						RequestID: "req-123",
						PeerID:    "peer-456",
						Phase:     "completed",
						Progress:  100.0,
						IsActive:  false,
						Message:   "Connection completed",
						ConnectionDetails: &api.ConnectResponse{
							PeerID:          "peer-456",
							ServerPublicKey: "server-key",
							ServerIP:        "1.2.3.4",
							ServerPort:      51820,
							ClientIP:        "10.0.0.2",
							DNS:             []string{"8.8.8.8"},
							AllowedIPs:      []string{"0.0.0.0/0"},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
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
		if resp.PeerID != "peer-456" {
			t.Errorf("expected peer ID peer-456, got %s", resp.PeerID)
		}
		if resp.ServerPort != 51820 {
			t.Errorf("expected port 51820, got %d", resp.ServerPort)
		}
		if pollCount < 1 {
			t.Errorf("expected at least 1 poll, got %d", pollCount)
		}
	})

	t.Run("async success - progressive phases", func(t *testing.T) {
		pollCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/connect" && r.Method == "POST" {
				resp := api.Response[map[string]string]{
					Success: true,
					Data: map[string]string{
						"request_id": "req-456",
						"message":    "Connection request accepted",
					},
				}
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(resp)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/api/v1/connections/") && r.Method == "GET" {
				pollCount++

				// Simulate progressive phases
				var phase string
				var progress float64
				var isActive bool
				var connectionDetails *api.ConnectResponse

				if pollCount == 1 {
					phase = "selecting_node"
					progress = 20.0
					isActive = true
				} else if pollCount == 2 {
					phase = "configuring_peer"
					progress = 60.0
					isActive = true
				} else {
					phase = "completed"
					progress = 100.0
					isActive = false
					connectionDetails = &api.ConnectResponse{
						PeerID:          "peer-789",
						ServerPublicKey: "server-key",
						ServerIP:        "1.2.3.4",
						ServerPort:      51820,
						ClientIP:        "10.0.0.3",
						DNS:             []string{"8.8.8.8"},
						AllowedIPs:      []string{"0.0.0.0/0"},
					}
				}

				resp := api.Response[api.PeerConnectionStatus]{
					Success: true,
					Data: api.PeerConnectionStatus{
						RequestID:         "req-456",
						PeerID:            "peer-789",
						Phase:             phase,
						Progress:          progress,
						IsActive:          isActive,
						ConnectionDetails: connectionDetails,
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
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
		if resp.PeerID != "peer-789" {
			t.Errorf("expected peer ID peer-789, got %s", resp.PeerID)
		}
		if pollCount < 3 {
			t.Errorf("expected at least 3 polls (for phases), got %d", pollCount)
		}
	})

	t.Run("connection failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/connect" && r.Method == "POST" {
				resp := api.Response[map[string]string]{
					Success: true,
					Data: map[string]string{
						"request_id": "req-fail",
						"message":    "Connection request accepted",
					},
				}
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(resp)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/api/v1/connections/") && r.Method == "GET" {
				resp := api.Response[api.PeerConnectionStatus]{
					Success: true,
					Data: api.PeerConnectionStatus{
						RequestID:    "req-fail",
						Phase:        "failed",
						Progress:     0.0,
						IsActive:     false,
						Message:      "Connection failed",
						ErrorMessage: "Failed to provision node: timeout",
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewClient(server.URL, log)
		_, err := client.ConnectPeer(context.Background(), connectReq)

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "peer connection failed") {
			t.Errorf("expected error to contain 'peer connection failed', got '%v'", err)
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

}
