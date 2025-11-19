package protocol

import "context"

// PeerConfig is a protocol-agnostic peer configuration DTO.
// Protocol-specific options can be supplied via Config.
type PeerConfig struct {
	Protocol   string                 // e.g., "wireguard", "algo", "shadowsocks"
	Identifier string                 // protocol-specific identifier (e.g., WireGuard public key)
	AllowedIPs []string               // desired allowed IPs/routes for this peer
	Config     map[string]interface{} // protocol-specific extras (e.g., preshared_key)
}

// Manager defines protocol-agnostic operations to manage peers on a node.
// nodeHost is the address or hostname where protocol operations apply.
type Manager interface {
	AddPeer(ctx context.Context, nodeHost string, peer PeerConfig) error
	RemovePeer(ctx context.Context, nodeHost string, identifier string) error
	SyncPeers(ctx context.Context, nodeHost string, peers []PeerConfig) error
}
