package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
)

// GetNodeConfig returns the VPN configuration for a specific node.
func (o *Orchestrator) GetNodeConfig(ctx context.Context, nodeID string) (*nodemanager.NodeConfig, error) {
	node, err := o.store.GetNode(ctx, nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("orchestrator: node not found: %s", nodeID)
		}
		return nil, fmt.Errorf("orchestrator: failed to get node: %w", err)
	}

	// Ensure the node is active
	if node.Status != "active" {
		return nil, fmt.Errorf("orchestrator: node %s is not active (status: %s)", nodeID, node.Status)
	}

	return &nodemanager.NodeConfig{
		ServerPublicKey: node.ServerPublicKey,
		ServerIP:        node.IpAddress,
	}, nil
}

// DestroyNode destroys a VPN node using the node manager.
func (o *Orchestrator) DestroyNode(ctx context.Context, node db.Node) error {
	return o.nodeManager.DestroyNode(ctx, node)
}

// GetNodesByStatus returns nodes with the given status.
func (o *Orchestrator) GetNodesByStatus(ctx context.Context, status string) ([]db.Node, error) {
	return o.store.GetNodesByStatus(ctx, status)
}

// SelectNodeForPeer selects the optimal node for a new peer based on peer count and capacity
func (o *Orchestrator) SelectNodeForPeer(ctx context.Context) (string, error) {
	o.logger.Debug("selecting optimal node for new peer")

	// Get all active nodes
	activeNodes, err := o.store.GetNodesByStatus(ctx, "active")
	if err != nil {
		return "", fmt.Errorf("orchestrator: failed to get active nodes: %w", err)
	}

	if len(activeNodes) == 0 {
		o.logger.Info("no active nodes available, provisioning a new one for peer")
		nodeConfig, err := o.provisionNodeSafely(ctx)
		if err != nil {
			return "", fmt.Errorf("orchestrator: failed to provision node for peer: %w", err)
		}

		// Get the node ID from the database using the IP address
		node, err := o.store.GetNodeByIP(ctx, nodeConfig.ServerIP)
		if err != nil {
			return "", fmt.Errorf("orchestrator: failed to get provisioned node ID: %w", err)
		}

		return node.ID, nil
	}

	// Get load balance information for all nodes
	nodeLoads, err := o.GetNodeLoadBalance(ctx)
	if err != nil {
		return "", fmt.Errorf("orchestrator: failed to get node load balance: %w", err)
	}

	// Find the node with the lowest peer count
	var selectedNodeID string
	minPeerCount := int64(-1)

	for _, node := range activeNodes {
		peerCount, exists := nodeLoads[node.ID]
		if !exists {
			peerCount = 0 // No peers on this node
		}

		// Check if this node has available capacity
		availableIPs, err := o.ipManager.GetAvailableIPCount(ctx, node.ID)
		if err != nil {
			o.logger.Warn("failed to get available IP count for node",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
			continue
		}

		if availableIPs == 0 {
			o.logger.Debug("node has no available IPs", slog.String("node_id", node.ID))
			continue
		}

		// Select node with lowest peer count that has available capacity
		if minPeerCount == -1 || int64(peerCount) < minPeerCount {
			minPeerCount = int64(peerCount)
			selectedNodeID = node.ID
		}
	}

	if selectedNodeID == "" {
		return "", fmt.Errorf("orchestrator: no nodes with available capacity found")
	}

	o.logger.Info("selected node for peer",
		slog.String("node_id", selectedNodeID),
		slog.Int64("peer_count", minPeerCount))

	return selectedNodeID, nil
}

// provisionNodeSafely provisions a new node with race condition protection
func (o *Orchestrator) provisionNodeSafely(ctx context.Context) (*nodemanager.NodeConfig, error) {
	o.logger.Info("safely provisioning new node with race condition protection")

	// First, check if there's already a node being provisioned to avoid race conditions
	provisioningNodes, err := o.store.GetNodesByStatus(ctx, "provisioning")
	if err != nil {
		return nil, fmt.Errorf("failed to check for provisioning nodes: %w", err)
	}

	// If there's already a node being provisioned, wait for it
	if len(provisioningNodes) > 0 {
		o.logger.Info("found existing provisioning node, waiting for completion",
			slog.String("node_id", provisioningNodes[0].ID))

		return o.waitForProvisioningNode(ctx, provisioningNodes[0].ID)
	}

	// No provisioning nodes found, we can safely provision a new one
	return o.nodeManager.CreateNode(ctx)
}

// waitForProvisioningNode waits for an existing provisioning node to become active
func (o *Orchestrator) waitForProvisioningNode(ctx context.Context, nodeID string) (*nodemanager.NodeConfig, error) {
	o.logger.Info("waiting for provisioning node to become active", slog.String("node_id", nodeID))

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(5 * time.Minute) // 5 minute timeout for provisioning
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			o.logger.Warn("timeout waiting for provisioning node", slog.String("node_id", nodeID))
			// Timeout reached, try to provision a new node
			return o.nodeManager.CreateNode(ctx)
		case <-ticker.C:
			node, err := o.store.GetNode(ctx, nodeID)
			if err != nil {
				if err == sql.ErrNoRows {
					o.logger.Warn("provisioning node disappeared, creating new one", slog.String("node_id", nodeID))
					return o.nodeManager.CreateNode(ctx)
				}
				o.logger.Warn("error checking provisioning node status",
					slog.String("node_id", nodeID),
					slog.String("error", err.Error()))
				continue
			}

			switch node.Status {
			case "active":
				o.logger.Info("provisioning node is now active", slog.String("node_id", nodeID))
				return &nodemanager.NodeConfig{
					ServerPublicKey: node.ServerPublicKey,
					ServerIP:        node.IpAddress,
				}, nil
			case "provisioning":
				o.logger.Debug("node still provisioning", slog.String("node_id", nodeID))
				continue
			default:
				o.logger.Warn("provisioning node in unexpected state",
					slog.String("node_id", nodeID),
					slog.String("status", node.Status))
				// Node failed, try to provision a new one
				return o.nodeManager.CreateNode(ctx)
			}
		}
	}
}

// ValidateNodePeerCapacity validates that a node can accommodate additional peers
func (o *Orchestrator) ValidateNodePeerCapacity(ctx context.Context, nodeID string, additionalPeers int) error {
	o.logger.Debug("validating node peer capacity",
		slog.String("node_id", nodeID),
		slog.Int("additional_peers", additionalPeers))

	// Get current peer count
	currentPeers, err := o.peerManager.CountActivePeersByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to count current peers: %w", err)
	}

	// Get available IP count
	availableIPs, err := o.ipManager.GetAvailableIPCount(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get available IP count: %w", err)
	}

	if availableIPs < additionalPeers {
		return fmt.Errorf("orchestrator: node %s has insufficient capacity: current=%d, available=%d, requested=%d",
			nodeID, currentPeers, availableIPs, additionalPeers)
	}

	o.logger.Debug("node has sufficient capacity",
		slog.String("node_id", nodeID),
		slog.Int64("current_peers", currentPeers),
		slog.Int("available_ips", availableIPs),
		slog.Int("additional_peers", additionalPeers))

	return nil
}

// SyncNodePeers synchronizes the peer state between the database and the actual node
func (o *Orchestrator) SyncNodePeers(ctx context.Context, nodeID string) error {
	o.logger.Info("synchronizing node peers", slog.String("node_id", nodeID))

	// Get node information
	node, err := o.store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get node: %w", err)
	}

	// Get peers from database
	dbPeers, err := o.peerManager.GetActivePeersByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get database peers: %w", err)
	}

	// Get peers from node via SSH
	nodePeers, err := o.nodeManager.ListNodePeers(ctx, node.IpAddress)
	if err != nil {
		return fmt.Errorf("orchestrator: failed to get node peers: %w", err)
	}

	// Create maps for comparison
	dbPeerMap := make(map[string]*peermanager.PeerConfig)
	for _, peer := range dbPeers {
		dbPeerMap[peer.PublicKey] = peer
	}

	nodePeerMap := make(map[string]*nodemanager.PeerInfo)
	for _, peer := range nodePeers {
		nodePeerMap[peer.PublicKey] = peer
	}

	var syncErrors []error
	addedCount := 0
	removedCount := 0

	// Add missing peers to node
	for publicKey, dbPeer := range dbPeerMap {
		if _, exists := nodePeerMap[publicKey]; !exists {
			o.logger.Debug("adding missing peer to node",
				slog.String("peer_id", dbPeer.ID),
				slog.String("public_key", publicKey[:8]+"..."))

			peerConfig := &nodemanager.PeerConfig{
				ID:           dbPeer.ID,
				PublicKey:    dbPeer.PublicKey,
				AllocatedIP:  dbPeer.AllocatedIP,
				PresharedKey: dbPeer.PresharedKey,
			}

			err := o.nodeManager.AddPeerToNode(ctx, node.IpAddress, peerConfig)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Errorf("failed to add peer %s to node: %w", dbPeer.ID, err))
			} else {
				addedCount++
			}
		}
	}

	// Remove extra peers from node (peers that exist on node but not in database)
	for publicKey, _ := range nodePeerMap {
		if _, exists := dbPeerMap[publicKey]; !exists {
			o.logger.Debug("removing extra peer from node",
				slog.String("public_key", publicKey[:8]+"..."))

			err := o.nodeManager.RemovePeerFromNode(ctx, node.IpAddress, publicKey)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Errorf("failed to remove peer %s from node: %w", publicKey[:8]+"...", err))
			} else {
				removedCount++
			}
		}
	}

	o.logger.Info("node peer synchronization completed",
		slog.String("node_id", nodeID),
		slog.Int("added_peers", addedCount),
		slog.Int("removed_peers", removedCount),
		slog.Int("error_count", len(syncErrors)))

	if len(syncErrors) > 0 {
		return fmt.Errorf("orchestrator: %d sync operations failed: %v", len(syncErrors), syncErrors)
	}

	return nil
}

// GetNodeLoadBalance returns a map of node IDs to their current peer counts
func (o *Orchestrator) GetNodeLoadBalance(ctx context.Context) (map[string]int, error) {
	o.logger.Debug("getting node load balance")

	// Get all active nodes
	activeNodes, err := o.store.GetNodesByStatus(ctx, "active")
	if err != nil {
		return nil, fmt.Errorf("orchestrator: failed to get active nodes: %w", err)
	}

	nodeLoads := make(map[string]int)

	// Get peer count for each node using peer manager
	for _, node := range activeNodes {
		peerCount, err := o.peerManager.CountActivePeersByNode(ctx, node.ID)
		if err != nil {
			o.logger.Warn("failed to get peer count for node",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
			nodeLoads[node.ID] = 0 // Default to 0 on error
		} else {
			nodeLoads[node.ID] = int(peerCount)
		}
	}

	o.logger.Debug("node load balance calculated", slog.Any("loads", nodeLoads))
	return nodeLoads, nil
}
