package nodemanager

import "time"

// NodeConfig represents the VPN node configuration sent to clients
type NodeConfig struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	ServerPort      int    `json:"server_port,omitempty"`
}

// PeerConfig represents a WireGuard peer configuration for node operations
type PeerConfig struct {
	ID           string  `json:"id"`
	PublicKey    string  `json:"public_key"`
	AllocatedIP  string  `json:"allocated_ip"`
	PresharedKey *string `json:"preshared_key,omitempty"`
}

// PeerInfo represents peer information returned from node operations
type PeerInfo struct {
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
	TransferRx      int64      `json:"transfer_rx"`
	TransferTx      int64      `json:"transfer_tx"`
}
