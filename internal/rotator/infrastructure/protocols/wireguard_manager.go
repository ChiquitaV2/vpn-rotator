package protocols

import (
	"context"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/pkg/protocol"
)

// WireGuardProtocolManager adapts a node.WireGuardManager to the protocol.Manager interface.
type WireGuardProtocolManager struct {
	wg node.WireGuardManager
}

func NewWireGuardProtocolManager(wg node.WireGuardManager) *WireGuardProtocolManager {
	return &WireGuardProtocolManager{wg: wg}
}

func (m *WireGuardProtocolManager) AddPeer(ctx context.Context, nodeHost string, peer protocol.PeerConfig) error {
	// Map protocol-agnostic peer to WireGuard-specific config
	var psk *string
	if v, ok := peer.Config["preshared_key"].(string); ok {
		if v != "" {
			psk = &v
		}
	}
	wgCfg := node.PeerWireGuardConfig{
		PublicKey:    peer.Identifier,
		PresharedKey: psk,
		AllowedIPs:   peer.AllowedIPs,
	}
	return m.wg.AddPeer(ctx, nodeHost, wgCfg)
}

func (m *WireGuardProtocolManager) RemovePeer(ctx context.Context, nodeHost string, identifier string) error {
	// identifier is the WireGuard public key in this adapter
	return m.wg.RemovePeer(ctx, nodeHost, identifier)
}

func (m *WireGuardProtocolManager) SyncPeers(ctx context.Context, nodeHost string, peers []protocol.PeerConfig) error {
	cfgs := make([]node.PeerWireGuardConfig, 0, len(peers))
	for _, p := range peers {
		var psk *string
		if v, ok := p.Config["preshared_key"].(string); ok {
			if v != "" {
				psk = &v
			}
		}
		cfgs = append(cfgs, node.PeerWireGuardConfig{
			PublicKey:    p.Identifier,
			PresharedKey: psk,
			AllowedIPs:   p.AllowedIPs,
		})
	}
	return m.wg.SyncPeers(ctx, nodeHost, cfgs)
}
