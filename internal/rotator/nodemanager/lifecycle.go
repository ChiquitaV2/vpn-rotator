package nodemanager

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ssh"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// CreateNode provisions a new VPN node and returns its configuration.
// This method prevents race conditions by creating the database record immediately.
func (m *Manager) CreateNode(ctx context.Context) (*NodeConfig, error) {
	m.logger.Info("provisioning new node with race condition protection")

	// Generate a unique node ID for the database record
	nodeID := fmt.Sprintf("node-%d", time.Now().UnixNano())

	// Step 1: Create database record immediately with "provisioning" status
	// This prevents race conditions by claiming the provisioning slot
	dbNode, err := m.store.CreateNode(ctx, db.CreateNodeParams{
		ID:              nodeID,
		IpAddress:       "0.0.0.0", // Placeholder, will be updated after provisioning
		ServerPublicKey: "",        // Placeholder, will be updated after provisioning
		Port:            51820,     // Default WireGuard port
		Status:          "provisioning",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create node record in database: %w", err)
	}

	m.logger.Info("created provisioning node record in database", slog.String("node_id", dbNode.ID))

	// Step 2: Allocate subnet for this node (now that the node record exists)
	subnet, err := m.ipManager.AllocateNodeSubnet(ctx, nodeID)
	if err != nil {
		// Clean up the database record since subnet allocation failed
		if cleanupErr := m.store.DeleteNode(ctx, nodeID); cleanupErr != nil {
			m.logger.Error("failed to cleanup database node after subnet allocation failure",
				slog.String("node_id", nodeID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to allocate subnet for node: %w", err)
	}

	m.logger.Info("allocated subnet for new node",
		slog.String("node_id", nodeID),
		slog.String("subnet", subnet.String()))

	// Step 3: Now provision the actual infrastructure
	provisionedNode, err := m.provisioner.ProvisionNodeWithSubnet(ctx, subnet)
	if err != nil {
		// Clean up the subnet and database record since provisioning failed
		if cleanupErr := m.ipManager.ReleaseNodeSubnet(ctx, nodeID); cleanupErr != nil {
			m.logger.Error("failed to cleanup allocated subnet after provisioning failure",
				slog.String("node_id", nodeID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		if cleanupErr := m.store.DeleteNode(ctx, dbNode.ID); cleanupErr != nil {
			m.logger.Error("failed to cleanup database node after provisioning failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to provision new node: %w", err)
	}

	m.logger.Info("provisioned new node infrastructure",
		slog.String("node_id", dbNode.ID),
		slog.String("server_id", fmt.Sprintf("%d", provisionedNode.ID)),
		slog.String("ip", provisionedNode.IP),
		slog.String("public_key", provisionedNode.PublicKey),
		slog.String("status", string(provisionedNode.Status)))

	// Step 4: Update the database record with actual provisioned details
	// We need to update the node with the real IP and public key
	err = m.updateNodeDetails(ctx, dbNode.ID, fmt.Sprintf("%d", provisionedNode.ID), provisionedNode.IP, provisionedNode.PublicKey, dbNode.Version)
	if err != nil {
		// Clean up everything since we can't update the database
		if cleanupErr := m.provisioner.DestroyNode(ctx, fmt.Sprintf("%d", provisionedNode.ID)); cleanupErr != nil {
			m.logger.Error("failed to cleanup provisioned node after database update failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		if cleanupErr := m.store.DeleteNode(ctx, dbNode.ID); cleanupErr != nil {
			m.logger.Error("failed to cleanup database node after update failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		if cleanupErr := m.ipManager.ReleaseNodeSubnet(ctx, nodeID); cleanupErr != nil {
			m.logger.Error("failed to cleanup allocated subnet after update failure",
				slog.String("node_id", nodeID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to update node details in database: %w", err)
	}

	// Step 5: Mark the node as active since provisioning is complete
	// The version was incremented by updateNodeDetails, so we use the incremented version
	err = m.store.MarkNodeActive(ctx, db.MarkNodeActiveParams{
		ID:      dbNode.ID,
		Version: dbNode.Version + 1, // Version was incremented by updateNodeDetails
	})
	if err != nil {
		m.logger.Error("failed to mark node as active",
			slog.String("node_id", dbNode.ID),
			slog.String("ip", provisionedNode.IP),
			slog.String("error", err.Error()))

		// Clean up everything since we can't mark it active
		if cleanupErr := m.provisioner.DestroyNode(ctx, fmt.Sprintf("%d", provisionedNode.ID)); cleanupErr != nil {
			m.logger.Error("failed to cleanup provisioned node after activation failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		if cleanupErr := m.store.DeleteNode(ctx, dbNode.ID); cleanupErr != nil {
			m.logger.Error("failed to cleanup database node after activation failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		if cleanupErr := m.ipManager.ReleaseNodeSubnet(ctx, nodeID); cleanupErr != nil {
			m.logger.Error("failed to cleanup allocated subnet after activation failure",
				slog.String("node_id", nodeID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to mark node as active: %w", err)
	}

	m.logger.Info("node is now active and ready",
		slog.String("node_id", dbNode.ID),
		slog.String("ip", provisionedNode.IP),
		slog.String("public_key", provisionedNode.PublicKey),
		slog.String("subnet", subnet.String()))

	// Return the node configuration
	return &NodeConfig{
		ServerPublicKey: provisionedNode.PublicKey,
		ServerIP:        provisionedNode.IP,
	}, nil
}

// DestroyNode destroys a VPN node.
func (m *Manager) DestroyNode(ctx context.Context, node db.Node) error {
	m.logger.Info("destroying node", slog.String("node_id", node.ID))

	// Check if there are active peers that need migration
	peers, err := m.store.GetPeersForMigration(ctx, node.ID)
	if err != nil {
		m.logger.Warn("failed to check for peers to migrate",
			slog.String("node_id", node.ID),
			slog.String("error", err.Error()))
	} else if len(peers) > 0 {
		m.logger.Info("found active peers that need migration before node destruction",
			slog.String("node_id", node.ID),
			slog.Int("peer_count", len(peers)))

		// For now, we'll log this but continue with destruction
		// In a full implementation, this would trigger peer migration to other nodes
		// This is handled by the orchestrator in the rotation process
		for _, peer := range peers {
			m.logger.Warn("peer will be disconnected during node destruction",
				slog.String("node_id", node.ID),
				slog.String("peer_id", peer.ID),
				slog.String("allocated_ip", peer.AllocatedIp))
		}
	}

	// Release the node's subnet allocation
	if err := m.ipManager.ReleaseNodeSubnet(ctx, node.ID); err != nil {
		m.logger.Warn("failed to release node subnet",
			slog.String("node_id", node.ID),
			slog.String("error", err.Error()))
		// Continue with destruction even if subnet cleanup fails
	} else {
		m.logger.Info("released subnet for node", slog.String("node_id", node.ID))
	}

	// Destroy the node on the cloud provider
	if !node.ServerID.Valid {
		m.logger.Warn("node has no server ID, skipping provider destruction",
			slog.String("node_id", node.ID))
	}
	if err := m.provisioner.DestroyNode(ctx, node.ServerID.String); err != nil {
		// Check if it's a specific Hetzner error (not found)
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			m.logger.Warn("node not found on provider, assuming already destroyed",
				slog.String("node_id", node.ID))
		} else {
			return fmt.Errorf("failed to destroy node on provider: %w", err)
		}
	}

	// Remove the node from the database (this will cascade delete peers and subnet via foreign keys)
	if err := m.store.DeleteNode(ctx, node.ID); err != nil {
		return fmt.Errorf("failed to delete node from db: %w", err)
	}

	m.logger.Info("successfully destroyed node", slog.String("node_id", node.ID))
	return nil
}

// GetNodeHealth checks if a node is healthy and reachable.
func (m *Manager) GetNodeHealth(ctx context.Context, nodeIP string) error {
	m.logger.Debug("checking node health", slog.String("ip", nodeIP))

	if err := m.health.Check(ctx, nodeIP); err != nil {
		return fmt.Errorf("node health check failed for %s: %w", nodeIP, err)
	}

	return nil
}

// GetNodePublicKey retrieves the WireGuard public key from a node via SSH.
func (m *Manager) GetNodePublicKey(ctx context.Context, nodeIP string) (string, error) {
	m.logger.Debug("retrieving public key from node", slog.String("ip", nodeIP))

	sshClient, err := ssh.NewClient(nodeIP, "root", m.sshPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create ssh client: %w", err)
	}

	var publicKey string
	var lastErr error

	// Retry up to 5 times with 5-second intervals
	for i := 0; i < 5; i++ {
		publicKey, err = sshClient.RunCommand(ctx, "cat /etc/wireguard/publickey")
		if err == nil {
			m.logger.Debug("successfully retrieved public key",
				slog.String("ip", nodeIP),
				slog.String("public_key", publicKey))
			return publicKey, nil
		}
		lastErr = err

		if i < 4 { // Don't sleep on the last iteration
			m.logger.Debug("retrying public key retrieval",
				slog.String("ip", nodeIP),
				slog.Int("attempt", i+1),
				slog.String("error", err.Error()))
			time.Sleep(5 * time.Second)
		}
	}

	return "", fmt.Errorf("failed to get public key after 5 retries: %w", lastErr)
}

// WaitForNode waits for a node to become healthy with timeout.
func (m *Manager) WaitForNode(ctx context.Context, nodeIP string) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out waiting for node %s to be ready", nodeIP)
		case <-ticker.C:
			m.logger.Debug("checking node readiness", slog.String("ip", nodeIP))
			if err := m.GetNodeHealth(timeoutCtx, nodeIP); err == nil {
				m.logger.Info("node is ready", slog.String("ip", nodeIP))
				return nil
			}
		}
	}
}

// updateNodeDetails updates the node record with actual provisioned details
func (m *Manager) updateNodeDetails(ctx context.Context, nodeID, serverID, ipAddress, publicKey string, currentVersion int64) error {
	// Use the proper update query to update node details while keeping provisioning status
	err := m.store.UpdateNodeDetails(ctx, db.UpdateNodeDetailsParams{
		ID:              nodeID,
		ServerID:        sql.NullString{String: serverID, Valid: true},
		IpAddress:       ipAddress,
		ServerPublicKey: publicKey,
		Version:         currentVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to update node details: %w", err)
	}

	m.logger.Debug("updated node details",
		slog.String("node_id", nodeID),
		slog.String("ip", ipAddress),
		slog.String("public_key", publicKey))

	return nil
}
