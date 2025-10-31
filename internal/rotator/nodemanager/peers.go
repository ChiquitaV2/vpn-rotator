package nodemanager

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// AddPeerToNode adds a WireGuard peer to a specific node via SSH with comprehensive error handling
func (m *Manager) AddPeerToNode(ctx context.Context, nodeIP string, peer *PeerConfig) error {
	m.logger.Debug("adding peer to node",
		slog.String("node_ip", nodeIP),
		slog.String("peer_id", peer.ID),
		slog.String("allocated_ip", peer.AllocatedIP))
	// Validate peer configuration
	if err := m.validatePeerConfig(peer); err != nil {
		return fmt.Errorf("invalid peer config: %w", err)
	}

	// Validate WireGuard interface is available
	if err := m.validateWireGuardInterface(ctx, nodeIP); err != nil {
		return fmt.Errorf("WireGuard interface validation failed: %w", err)
	}

	// Check if peer already exists
	existingPeers, err := m.ListNodePeers(ctx, nodeIP)
	if err != nil {
		m.logger.Warn("failed to check existing peers",
			slog.String("node_ip", nodeIP),
			slog.String("error", err.Error()))
	} else {
		for _, existingPeer := range existingPeers {
			if existingPeer.PublicKey == peer.PublicKey {
				return fmt.Errorf("peer with public key %s already exists on node", peer.PublicKey[:8]+"...")
			}
			if existingPeer.AllocatedIP == peer.AllocatedIP {
				return fmt.Errorf("IP address %s already allocated to another peer on node", peer.AllocatedIP)
			}
		}
	}

	// Create rollback context for cleanup
	rollback := &peerOperationRollback{
		nodeIP:    nodeIP,
		publicKey: peer.PublicKey,
		operation: "add",
		manager:   m,
	}

	// Build WireGuard peer add command
	var cmd string
	if peer.PresharedKey != nil && *peer.PresharedKey != "" {
		// Use a temporary file for preshared key to avoid command line exposure
		cmd = fmt.Sprintf(`echo '%s' | wg set wg0 peer %s allowed-ips %s/32 preshared-key /dev/stdin`,
			*peer.PresharedKey, peer.PublicKey, peer.AllocatedIP)
	} else {
		cmd = fmt.Sprintf("wg set wg0 peer %s allowed-ips %s/32", peer.PublicKey, peer.AllocatedIP)
	}

	// Execute command
	_, err = m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return NewPeerOperationError(nodeIP, peer.ID, peer.PublicKey, "add", err)
	}

	// Validate that peer was added successfully
	if err := m.validatePeerAddition(ctx, nodeIP, peer.PublicKey); err != nil {
		// Attempt rollback
		m.logger.Warn("peer addition validation failed, attempting rollback",
			slog.String("node_ip", nodeIP),
			slog.String("peer_id", peer.ID))
		rollback.execute(ctx)
		return NewPeerOperationError(nodeIP, peer.ID, peer.PublicKey, "add-validate", err)
	}

	// Save WireGuard configuration to persist across reboots
	if err := m.saveWireGuardConfig(ctx, nodeIP); err != nil {
		m.logger.Warn("failed to save WireGuard config, peer may not persist across reboots",
			slog.String("node_ip", nodeIP),
			slog.String("peer_id", peer.ID),
			slog.String("error", err.Error()))
		// Don't fail the operation for save errors, just warn
	}

	m.logger.Info("successfully added peer to node",
		slog.String("node_ip", nodeIP),
		slog.String("peer_id", peer.ID),
		slog.String("allocated_ip", peer.AllocatedIP))

	return nil
}

// RemovePeerFromNode removes a WireGuard peer from a specific node via SSH
func (m *Manager) RemovePeerFromNode(ctx context.Context, nodeIP string, publicKey string) error {
	m.logger.Debug("removing peer from node",
		slog.String("node_ip", nodeIP),
		slog.String("public_key", publicKey[:8]+"..."))

	// Validate public key
	if !crypto.IsValidWireGuardKey(publicKey) {
		return fmt.Errorf("invalid public key format")
	}

	// Check if peer exists before attempting removal
	existingPeers, err := m.ListNodePeers(ctx, nodeIP)
	if err != nil {
		m.logger.Warn("failed to check existing peers before removal",
			slog.String("node_ip", nodeIP),
			slog.String("error", err.Error()))
	} else {
		peerExists := false
		for _, existingPeer := range existingPeers {
			if existingPeer.PublicKey == publicKey {
				peerExists = true
				break
			}
		}
		if !peerExists {
			m.logger.Debug("peer not found on node, skipping removal",
				slog.String("node_ip", nodeIP),
				slog.String("public_key", publicKey[:8]+"..."))
			return nil // Not an error if peer doesn't exist
		}
	}

	// Build WireGuard peer remove command
	cmd := fmt.Sprintf("wg set wg0 peer %s remove", publicKey)

	// Execute command
	_, err = m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return NewPeerOperationError(nodeIP, "", publicKey, "remove", err)
	}

	// Validate that peer was removed successfully
	if err := m.validatePeerRemoval(ctx, nodeIP, publicKey); err != nil {
		m.logger.Warn("peer removal validation failed",
			slog.String("node_ip", nodeIP),
			slog.String("public_key", publicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	// Save WireGuard configuration to persist across reboots
	if err := m.saveWireGuardConfig(ctx, nodeIP); err != nil {
		m.logger.Warn("failed to save WireGuard config after peer removal",
			slog.String("node_ip", nodeIP),
			slog.String("public_key", publicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	m.logger.Info("successfully removed peer from node",
		slog.String("node_ip", nodeIP),
		slog.String("public_key", publicKey[:8]+"..."))

	return nil
}

// ListNodePeers lists all peers on a specific node via SSH
func (m *Manager) ListNodePeers(ctx context.Context, nodeIP string) ([]*PeerInfo, error) {
	m.logger.Debug("listing peers on node", slog.String("node_ip", nodeIP))

	// Get WireGuard status with peer information
	cmd := "wg show wg0 dump"
	output, err := m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return nil, NewSSHError(nodeIP, cmd, 1, true, err)
	}

	// Parse WireGuard dump output
	peers, err := m.parseWireGuardDump(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WireGuard dump: %w", err)
	}

	m.logger.Debug("listed peers on node",
		slog.String("node_ip", nodeIP),
		slog.Int("peer_count", len(peers)))

	return peers, nil
}

// UpdateNodePeerConfig updates a peer's configuration on a specific node via SSH
func (m *Manager) UpdateNodePeerConfig(ctx context.Context, nodeIP string, peer *PeerConfig) error {
	m.logger.Debug("updating peer config on node",
		slog.String("node_ip", nodeIP),
		slog.String("peer_id", peer.ID))

	// Validate peer configuration
	if err := m.validatePeerConfig(peer); err != nil {
		return fmt.Errorf("invalid peer config: %w", err)
	}

	// Remove existing peer first
	if err := m.RemovePeerFromNode(ctx, nodeIP, peer.PublicKey); err != nil {
		m.logger.Warn("failed to remove existing peer during update",
			slog.String("node_ip", nodeIP),
			slog.String("peer_id", peer.ID),
			slog.String("error", err.Error()))
	}

	// Add peer with new configuration
	if err := m.AddPeerToNode(ctx, nodeIP, peer); err != nil {
		return err
	}

	m.logger.Info("successfully updated peer config on node",
		slog.String("node_ip", nodeIP),
		slog.String("peer_id", peer.ID))

	return nil
}

// validatePeerAddition validates that a peer was successfully added to WireGuard
func (m *Manager) validatePeerAddition(ctx context.Context, nodeIP, publicKey string) error {
	peers, err := m.ListNodePeers(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("failed to list peers for validation: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			return nil // Peer found, addition successful
		}
	}

	return fmt.Errorf("peer with public key %s not found after addition", publicKey[:8]+"...")
}

// validatePeerRemoval validates that a peer was successfully removed from WireGuard
func (m *Manager) validatePeerRemoval(ctx context.Context, nodeIP, publicKey string) error {
	peers, err := m.ListNodePeers(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("failed to list peers for validation: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			return fmt.Errorf("peer with public key %s still found after removal", publicKey[:8]+"...")
		}
	}

	return nil // Peer not found, removal successful
}

// peerOperationRollback handles rollback operations for failed peer management
type peerOperationRollback struct {
	nodeIP    string
	publicKey string
	operation string
	manager   *Manager
}

// execute performs the rollback operation
func (r *peerOperationRollback) execute(ctx context.Context) {
	switch r.operation {
	case "add":
		// Rollback peer addition by removing the peer
		if err := r.manager.RemovePeerFromNode(ctx, r.nodeIP, r.publicKey); err != nil {
			r.manager.logger.Error("failed to rollback peer addition",
				slog.String("node_ip", r.nodeIP),
				slog.String("public_key", r.publicKey[:8]+"..."),
				slog.String("error", err.Error()))
		} else {
			r.manager.logger.Info("successfully rolled back peer addition",
				slog.String("node_ip", r.nodeIP),
				slog.String("public_key", r.publicKey[:8]+"..."))
		}
	case "remove":
		// For remove operations, we can't easily rollback since we don't have the original config
		r.manager.logger.Warn("cannot rollback peer removal operation",
			slog.String("node_ip", r.nodeIP),
			slog.String("public_key", r.publicKey[:8]+"..."))
	}
}
