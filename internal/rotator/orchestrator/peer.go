package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
)

// MigratePeersFromNode migrates all active peers from a source node to target nodes
func (o *manager) MigratePeersFromNode(ctx context.Context, sourceNodeID string, targetNodeID string) error {
	o.logger.Info("starting peer migration",
		slog.String("source_node", sourceNodeID),
		slog.String("target_node", targetNodeID))

	// Validate target node exists and is active
	targetNode, err := o.store.GetNode(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: target node not found: %w", err)
	}
	if targetNode.Status != "active" {
		return fmt.Errorf("orchestrator: target node is not active (status: %s)", targetNode.Status)
	}

	// Get all active peers from the source node using peer manager
	peerConfigs, err := o.peerManager.GetPeersForMigration(ctx, sourceNodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get peers for migration: %w", err)
	}

	if len(peerConfigs) == 0 {
		o.logger.Info("no peers to migrate", slog.String("source_node", sourceNodeID))
		return nil
	}

	o.logger.Info("found peers to migrate",
		slog.String("source_node", sourceNodeID),
		slog.Int("peer_count", len(peerConfigs)))

	// Check target node capacity
	availableIPs, err := o.ipService.GetAvailableIPCount(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to check target node capacity: %w", err)
	}
	if availableIPs < len(peerConfigs) {
		return fmt.Errorf("orchestrator: target node has insufficient capacity: need %d, available %d",
			len(peerConfigs), availableIPs)
	}

	// Convert peer configs to db.Peer for migration
	var peers []db.Peer
	for _, peerConfig := range peerConfigs {
		dbPeer, err := o.store.GetPeer(ctx, peerConfig.ID)
		if err != nil {
			o.logger.Warn("failed to get peer from database during migration",
				slog.String("peer_id", peerConfig.ID),
				slog.String("error", err.Error()))
			continue
		}
		peers = append(peers, dbPeer)
	}

	// Migrate each peer
	var migrationErrors []error
	successCount := 0

	for _, peer := range peers {
		err := o.migrateSinglePeer(ctx, peer, sourceNodeID, targetNodeID)
		if err != nil {
			o.logger.Error("failed to migrate peer",
				slog.String("peer_id", peer.ID),
				slog.String("source_node", sourceNodeID),
				slog.String("target_node", targetNodeID),
				slog.String("error", err.Error()))
			migrationErrors = append(migrationErrors, fmt.Errorf("peer %s: %w", peer.ID, err))
		} else {
			successCount++
			o.logger.Debug("successfully migrated peer",
				slog.String("peer_id", peer.ID),
				slog.String("target_node", targetNodeID))
		}
	}

	o.logger.Info("peer migration completed",
		slog.String("source_node", sourceNodeID),
		slog.String("target_node", targetNodeID),
		slog.Int("success_count", successCount),
		slog.Int("error_count", len(migrationErrors)))

	// Return error if any migrations failed
	if len(migrationErrors) > 0 {
		return fmt.Errorf("orchestrator: %d peer migrations failed: %v", len(migrationErrors), migrationErrors)
	}

	return nil
}

// migrateSinglePeer migrates a single peer from source to target node
func (o *manager) migrateSinglePeer(ctx context.Context, peer db.Peer, sourceNodeID, targetNodeID string) error {
	// Get source and target node information
	sourceNode, err := o.store.GetNode(ctx, sourceNodeID)
	if err != nil {
		return fmt.Errorf("failed to get source node: %w", err)
	}

	targetNode, err := o.store.GetNode(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Allocate new IP on target node
	newIP, err := o.ipService.AllocateClientIP(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("failed to allocate IP on target node: %w", err)
	}

	// Mark peer as migrating
	err = o.peerManager.UpdatePeerStatus(ctx, peer.ID, "removing")
	if err != nil {
		o.logger.Warn("failed to mark peer as migrating",
			slog.String("peer_id", peer.ID),
			slog.String("error", err.Error()))
	}

	// Remove peer from source node via SSH
	err = o.nodeManager.RemovePeerFromNode(ctx, sourceNode.IpAddress, peer.PublicKey)
	if err != nil {
		// Log warning but continue - database cleanup is more important
		o.logger.Warn("failed to remove peer from source node via SSH",
			slog.String("peer_id", peer.ID),
			slog.String("source_node", sourceNodeID),
			slog.String("source_ip", sourceNode.IpAddress),
			slog.String("error", err.Error()))
	}

	// Add peer to target node via SSH
	peerConfig := &nodemanager.PeerConfig{
		ID:          peer.ID,
		PublicKey:   peer.PublicKey,
		AllocatedIP: newIP.String(),
	}
	if peer.PresharedKey.Valid {
		peerConfig.PresharedKey = &peer.PresharedKey.String
	}

	err = o.nodeManager.AddPeerToNode(ctx, targetNode.IpAddress, peerConfig)
	if err != nil {
		// Rollback: release the allocated IP and restore peer status
		o.ipService.ReleaseClientIP(ctx, targetNodeID, newIP)
		o.peerManager.UpdatePeerStatus(ctx, peer.ID, "active")
		return fmt.Errorf("failed to add peer to target node: %w", err)
	}

	// Update peer in database with new node and IP using peer manager
	err = o.peerManager.MigratePeerToNode(ctx, peer.ID, targetNodeID, newIP.String())
	if err != nil {
		// Rollback: remove peer from target node and release IP
		o.nodeManager.RemovePeerFromNode(ctx, targetNode.IpAddress, peer.PublicKey)
		o.ipService.ReleaseClientIP(ctx, targetNodeID, newIP)
		o.peerManager.UpdatePeerStatus(ctx, peer.ID, "active")
		return fmt.Errorf("failed to update peer in database: %w", err)
	}

	// Mark peer as active on new node
	err = o.peerManager.UpdatePeerStatus(ctx, peer.ID, "active")
	if err != nil {
		o.logger.Warn("failed to mark migrated peer as active",
			slog.String("peer_id", peer.ID),
			slog.String("error", err.Error()))
	}

	return nil
}

// CreatePeerOnOptimalNode creates a new peer on the most optimal node
func (o *manager) CreatePeerOnOptimalNode(ctx context.Context, publicKey string, presharedKey *string) (*peermanager.PeerConfig, error) {
	o.logger.Debug("creating peer on optimal node", slog.String("public_key", publicKey[:8]+"..."))

	// Select the optimal node for the peer
	nodeID, err := o.SelectNodeForPeer(ctx)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: failed to select optimal node: %w", err)
	}

	// Allocate IP for the peer
	allocatedIP, err := o.ipService.AllocateClientIP(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: failed to allocate IP: %w", err)
	}

	// Create peer request
	createReq := &peermanager.CreatePeerRequest{
		NodeID:       nodeID,
		PublicKey:    publicKey,
		AllocatedIP:  allocatedIP.String(),
		PresharedKey: presharedKey,
	}

	// Create peer in database using peer manager
	peerConfig, err := o.peerManager.CreatePeerWithValidation(ctx, createReq)
	if err != nil {
		// Rollback: release the allocated IP
		o.ipService.ReleaseClientIP(ctx, nodeID, allocatedIP)
		return nil, fmt.Errorf("orchestrator: failed to create peer: %w", err)
	}

	// Get node information for SSH operations
	node, err := o.store.GetNode(ctx, nodeID)
	if err != nil {
		// Rollback: remove peer and release IP
		o.peerManager.RemovePeer(ctx, peerConfig.ID)
		o.ipService.ReleaseClientIP(ctx, nodeID, allocatedIP)
		return nil, fmt.Errorf("orchestrator: failed to get node information: %w", err)
	}

	// Add peer to the node via SSH
	nodePeerConfig := &nodemanager.PeerConfig{
		ID:           peerConfig.ID,
		PublicKey:    peerConfig.PublicKey,
		AllocatedIP:  peerConfig.AllocatedIP,
		PresharedKey: peerConfig.PresharedKey,
	}

	err = o.nodeManager.AddPeerToNode(ctx, node.IpAddress, nodePeerConfig)
	if err != nil {
		// Rollback: remove peer from database and release IP
		o.peerManager.RemovePeer(ctx, peerConfig.ID)
		o.ipService.ReleaseClientIP(ctx, nodeID, allocatedIP)
		return nil, fmt.Errorf("orchestrator: failed to add peer to node: %w", err)
	}

	o.logger.Info("successfully created peer on optimal node",
		slog.String("peer_id", peerConfig.ID),
		slog.String("node_id", nodeID),
		slog.String("allocated_ip", peerConfig.AllocatedIP))

	return peerConfig, nil
}

// RemovePeerFromSystem removes a peer from both the node and database
func (o *manager) RemovePeerFromSystem(ctx context.Context, peerID string) error {
	o.logger.Debug("removing peer from system", slog.String("peer_id", peerID))

	// Get peer information
	peerConfig, err := o.peerManager.GetPeer(ctx, peerID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get peer: %w", err)
	}

	// Get node information
	node, err := o.store.GetNode(ctx, peerConfig.NodeID)
	if err != nil {
		o.logger.Warn("failed to get node information for peer removal",
			slog.String("peer_id", peerID),
			slog.String("node_id", peerConfig.NodeID),
			slog.String("error", err.Error()))
	} else {
		// Remove peer from node via SSH
		err = o.nodeManager.RemovePeerFromNode(ctx, node.IpAddress, peerConfig.PublicKey)
		if err != nil {
			o.logger.Warn("failed to remove peer from node via SSH",
				slog.String("peer_id", peerID),
				slog.String("node_ip", node.IpAddress),
				slog.String("error", err.Error()))
		}
	}

	// Release the allocated IP
	if peerConfig.AllocatedIP != "" {
		allocatedIP := net.ParseIP(peerConfig.AllocatedIP)
		if allocatedIP != nil {
			err = o.ipService.ReleaseClientIP(ctx, peerConfig.NodeID, allocatedIP)
			if err != nil {
				o.logger.Warn("failed to release IP address",
					slog.String("peer_id", peerID),
					slog.String("allocated_ip", peerConfig.AllocatedIP),
					slog.String("error", err.Error()))
			}
		}
	}

	// Remove peer from database using peer manager
	err = o.peerManager.RemovePeerWithCleanup(ctx, peerID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to remove peer from database: %w", err)
	}

	o.logger.Info("successfully removed peer from system",
		slog.String("peer_id", peerID),
		slog.String("node_id", peerConfig.NodeID))

	return nil
}

// GetPeerStatistics returns comprehensive peer statistics across all nodes
func (o *manager) GetPeerStatistics(ctx context.Context) (*peermanager.PeerStatistics, error) {
	o.logger.Debug("getting peer statistics")

	stats, err := o.peerManager.GetPeerStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: failed to get peer statistics: %w", err)
	}

	return stats, nil
}
