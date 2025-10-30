package api

import "time"

// ConnectResponse represents the response for a successful peer connection
type ConnectResponse struct {
	PeerID           string   `json:"peer_id"`
	ServerPublicKey  string   `json:"server_public_key"`
	ServerIP         string   `json:"server_ip"`
	ServerPort       int      `json:"server_port"`
	ClientIP         string   `json:"client_ip"`
	ClientPrivateKey *string  `json:"client_private_key,omitempty"` // Only if server-generated
	DNS              []string `json:"dns"`
	AllowedIPs       []string `json:"allowed_ips"`
}

// DisconnectResponse represents the response for a successful peer disconnection
type DisconnectResponse struct {
	Message string `json:"message"`
	PeerID  string `json:"peer_id"`
}

// ConfigResponse represents the VPN configuration response
type ConfigResponse struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	Port            int    `json:"port"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// PeerInfo represents peer information for listing operations
type PeerInfo struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"node_id"`
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
}

// PeersListResponse represents the response for listing peers
type PeersListResponse struct {
	Peers      []PeerInfo `json:"peers"`
	TotalCount int        `json:"total_count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}

// PeerStatsResponse represents the peer statistics response
type PeerStatsResponse struct {
	TotalPeers   int            `json:"total_peers"`
	ActiveNodes  int            `json:"active_nodes"`
	Distribution map[string]int `json:"distribution"` // nodeID -> peer count
	LastUpdated  time.Time      `json:"last_updated"`
}

// ProvisioningResponse represents a response when provisioning is in progress
type ProvisioningResponse struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	EstimatedWait int    `json:"estimated_wait_seconds"`
	RetryAfter    int    `json:"retry_after_seconds"`
}
