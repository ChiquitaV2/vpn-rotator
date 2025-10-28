package nodemanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/health"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ssh"
	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// NodeManager defines the interface for direct node lifecycle operations.
type NodeManager interface {
	// CreateNode provisions a new VPN node and returns its configuration
	CreateNode(ctx context.Context) (*models.NodeConfig, error)

	// DestroyNode destroys a VPN node by ID
	DestroyNode(ctx context.Context, node db.Node) error

	// GetNodeHealth checks if a node is healthy and reachable
	GetNodeHealth(ctx context.Context, nodeIP string) error

	// GetNodePublicKey retrieves the WireGuard public key from a node
	GetNodePublicKey(ctx context.Context, nodeIP string) (string, error)
}

// Manager implements the NodeManager interface for direct node operations.
type Manager struct {
	store         db.Store
	provisioner   provisioner.Provisioner
	health        health.HealthChecker
	logger        *slog.Logger
	sshPrivateKey string
}

// New creates a new NodeManager.
func New(
	store db.Store,
	provisioner provisioner.Provisioner,
	health health.HealthChecker,
	logger *slog.Logger,
	sshPrivateKey string,
) *Manager {
	return &Manager{
		store:         store,
		provisioner:   provisioner,
		health:        health,
		logger:        logger,
		sshPrivateKey: sshPrivateKey,
	}
}

// CreateNode provisions a new VPN node and returns its configuration.
func (m *Manager) CreateNode(ctx context.Context) (*models.NodeConfig, error) {
	m.logger.Info("provisioning new node")

	// Use provisioner to create the node (handles keys, cloud-init, and health checks)
	provisionedNode, err := m.provisioner.ProvisionNode(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to provision new node: %w", err)
	}

	m.logger.Info("provisioned new node",
		slog.String("ip", provisionedNode.IP),
		slog.String("public_key", provisionedNode.PublicKey),
		slog.String("status", string(provisionedNode.Status)))

	// Store the node in the database using the actual server ID from the provisioner
	nodeID := fmt.Sprintf("%d", provisionedNode.ID)
	dbNode, err := m.store.CreateNode(ctx, db.CreateNodeParams{
		ID:              nodeID,
		IpAddress:       provisionedNode.IP,
		ServerPublicKey: provisionedNode.PublicKey,
		Port:            51820, // Default WireGuard port
		Status:          "provisioning",
	})
	if err != nil {
		// If database storage fails, clean up the provisioned node
		if cleanupErr := m.provisioner.DestroyNode(ctx, nodeID); cleanupErr != nil {
			m.logger.Error("failed to cleanup provisioned node after database error",
				slog.String("node_id", nodeID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to store node in database: %w", err)
	}

	m.logger.Info("stored node in database", slog.String("node_id", dbNode.ID))

	// The provisioner has already verified the node is healthy, so mark it as active immediately
	err = m.store.MarkNodeActive(ctx, db.MarkNodeActiveParams{
		ID:      dbNode.ID,
		Version: dbNode.Version,
	})
	if err != nil {
		m.logger.Error("failed to mark node as active",
			slog.String("node_id", dbNode.ID),
			slog.String("ip", provisionedNode.IP),
			slog.String("error", err.Error()))

		// Clean up the node since we can't mark it active
		if cleanupErr := m.DestroyNode(ctx, dbNode); cleanupErr != nil {
			m.logger.Error("failed to cleanup node after activation failure",
				slog.String("node_id", dbNode.ID),
				slog.String("cleanup_error", cleanupErr.Error()))
		}
		return nil, fmt.Errorf("failed to mark node as active: %w", err)
	}

	m.logger.Info("node is now active and ready",
		slog.String("node_id", dbNode.ID),
		slog.String("ip", provisionedNode.IP),
		slog.String("public_key", provisionedNode.PublicKey))

	// Return the node configuration
	return &models.NodeConfig{
		ServerPublicKey: provisionedNode.PublicKey,
		ServerIP:        provisionedNode.IP,
	}, nil
}

// DestroyNode destroys a VPN node.
func (m *Manager) DestroyNode(ctx context.Context, node db.Node) error {
	m.logger.Info("destroying node", slog.String("node_id", node.ID))

	// Destroy the node on the cloud provider
	if err := m.provisioner.DestroyNode(ctx, node.ID); err != nil {
		// Check if it's a specific Hetzner error (not found)
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			m.logger.Warn("node not found on provider, assuming already destroyed",
				slog.String("node_id", node.ID))
		} else {
			return fmt.Errorf("failed to destroy node on provider: %w", err)
		}
	}

	// Remove the node from the database
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
