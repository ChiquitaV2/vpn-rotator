package peermanager

import "time"

// PeerStatus defines the status of a peer
type PeerStatus string

const (
	PeerStatusActive       PeerStatus = "active"
	PeerStatusDisconnected PeerStatus = "disconnected"
	PeerStatusRemoving     PeerStatus = "removing"
)

// PeerConfig represents a WireGuard peer configuration (consolidated from shared models)
type PeerConfig struct {
	ID           string     `json:"id"`
	NodeID       string     `json:"node_id"`
	PublicKey    string     `json:"public_key"`
	AllocatedIP  string     `json:"allocated_ip"`
	PresharedKey *string    `json:"preshared_key,omitempty"`
	Status       PeerStatus `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// PeerInfo represents peer information with additional details (consolidated from shared models)
// This combines PeerConfig with additional fields like LastHandshakeAt for API responses
type PeerInfo struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"node_id"`
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
}

// CreatePeerRequest represents a request to create a new peer
type CreatePeerRequest struct {
	NodeID       string  `json:"node_id"`
	PublicKey    string  `json:"public_key"`
	AllocatedIP  string  `json:"allocated_ip"`
	PresharedKey *string `json:"preshared_key,omitempty"`
}

// PeerFilters represents filters for listing peers
type PeerFilters struct {
	NodeID    *string     `json:"node_id,omitempty"`
	Status    *PeerStatus `json:"status,omitempty"`
	PublicKey *string     `json:"public_key,omitempty"`
	Limit     *int        `json:"limit,omitempty"`
	Offset    *int        `json:"offset,omitempty"`
}

// PeerStatistics represents peer statistics across the system
type PeerStatistics struct {
	TotalPeers        int64 `json:"total_peers"`
	ActivePeers       int64 `json:"active_peers"`
	DisconnectedPeers int64 `json:"disconnected_peers"`
	RemovingPeers     int64 `json:"removing_peers"`
}

// NodePeerSummary provides a summary of peers for a specific node
type NodePeerSummary struct {
	NodeID      string `json:"node_id"`
	TotalPeers  int64  `json:"total_peers"`
	ActivePeers int64  `json:"active_peers"`
	Capacity    int64  `json:"capacity"`
	Usage       string `json:"usage"` // e.g., "75%" or "150/200"
}

// PeersListResponse represents the response for listing peers (consolidated from shared models)
type PeersListResponse struct {
	Peers      []PeerInfo `json:"peers"`
	TotalCount int        `json:"total_count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}
