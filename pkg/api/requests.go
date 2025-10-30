package api

// ConnectRequest represents a request to connect a peer to the VPN
type ConnectRequest struct {
	PublicKey    *string `json:"public_key,omitempty"`    // User-provided public key
	GenerateKeys bool    `json:"generate_keys,omitempty"` // Request server-side key generation
}

// DisconnectRequest represents a request to disconnect a peer
type DisconnectRequest struct {
	PeerID string `json:"peer_id"`
}

// ConfigRequest represents the VPN configuration request
type ConfigRequest struct {
	ClientPublicKey *string `json:"client_public_key,omitempty"`
}

// PeerListParams represents query parameters for listing peers
type PeerListParams struct {
	NodeID *string `json:"node_id,omitempty"`
	Status *string `json:"status,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Offset *int    `json:"offset,omitempty"`
}
