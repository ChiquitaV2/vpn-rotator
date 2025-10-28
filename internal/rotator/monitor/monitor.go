package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ssh"
)

// NodeActivity represents WireGuard activity metrics for a node.
type NodeActivity struct {
	ServerID         string
	ConnectedClients int
	LastHandshakeAt  *time.Time
}

// ActivityStore defines the interface for persisting node activity.
type ActivityStore interface {
	UpdateNodeActivity(ctx context.Context, serverID string, connectedClients int, lastHandshakeAt *time.Time) error
}

// NodeProvider defines the interface for getting active nodes.
type NodeProvider interface {
	GetActiveNodes(ctx context.Context) ([]NodeInfo, error)
}

// NodeInfo contains information about a node.
type NodeInfo struct {
	ServerID  string
	IPAddress string
}

// Monitor monitors VPN node activity via SSH.
type Monitor struct {
	sshUser       string
	sshPrivateKey string
	store         ActivityStore
	nodeProvider  NodeProvider
	logger        *slog.Logger
}

// NewMonitor creates a new activity monitor.
func NewMonitor(sshUser, sshPrivateKey string, store ActivityStore, nodeProvider NodeProvider, logger *slog.Logger) *Monitor {
	return &Monitor{
		sshUser:       sshUser,
		sshPrivateKey: sshPrivateKey,
		store:         store,
		nodeProvider:  nodeProvider,
		logger:        logger,
	}
}

// Start begins the activity monitoring loop. Blocks until ctx is canceled.
func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.logger.Info("Activity monitor started",
		slog.Duration("interval", interval))

	// Perform initial check immediately
	m.updateAllNodeActivity(ctx)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Activity monitor stopped")
			return
		case <-ticker.C:
			m.updateAllNodeActivity(ctx)
		}
	}
}

// updateAllNodeActivity checks activity for all active nodes.
func (m *Monitor) updateAllNodeActivity(ctx context.Context) {
	nodes, err := m.nodeProvider.GetActiveNodes(ctx)
	if err != nil {
		m.logger.Error("Failed to get active nodes",
			slog.String("error", err.Error()))
		return
	}

	m.logger.Debug("Checking activity for nodes",
		slog.Int("count", len(nodes)))

	for _, node := range nodes {
		if err := m.updateNodeActivity(ctx, node); err != nil {
			m.logger.Error("Failed to update node activity",
				slog.String("server_id", node.ServerID),
				slog.String("error", err.Error()))
		}
	}
}

// updateNodeActivity updates activity metrics for a single node.
func (m *Monitor) updateNodeActivity(ctx context.Context, node NodeInfo) error {
	m.logger.Debug("Updating node activity",
		slog.String("server_id", node.ServerID),
		slog.String("ip_address", node.IPAddress))

	// Create SSH client
	sshClient, err := ssh.NewClient(node.IPAddress, m.sshUser, m.sshPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to create SSH client: %w", err)
	}

	// Run 'wg show wg0 dump' command
	output, err := sshClient.RunCommand(ctx, "sudo wg show wg0 dump")
	if err != nil {
		return fmt.Errorf("failed to run wg command: %w", err)
	}

	// Parse WireGuard dump output
	activity, err := m.parseWireGuardDump(output, node.ServerID)
	if err != nil {
		return fmt.Errorf("failed to parse wg dump: %w", err)
	}

	// Store activity in database
	if err := m.store.UpdateNodeActivity(ctx, activity.ServerID, activity.ConnectedClients, activity.LastHandshakeAt); err != nil {
		return fmt.Errorf("failed to store activity: %w", err)
	}

	m.logger.Debug("Node activity updated",
		slog.String("server_id", node.ServerID),
		slog.Int("connected_clients", activity.ConnectedClients))

	return nil
}

// parseWireGuardDump parses the output of 'wg show wg0 dump'.
//
// Format:
// Line 1: private-key	public-key	listen-port	fwmark
// Line 2+: public-key	preshared-key	endpoint	allowed-ips	latest-handshake	transfer-rx	transfer-tx	persistent-keepalive
func (m *Monitor) parseWireGuardDump(output, serverID string) (*NodeActivity, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 {
		return &NodeActivity{
			ServerID:         serverID,
			ConnectedClients: 0,
			LastHandshakeAt:  nil,
		}, nil
	}

	connectedClients := 0
	var latestHandshake *time.Time

	// Skip first line (server info)
	for i := 1; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) < 8 {
			continue // Invalid peer line
		}

		// Field 4 is latest-handshake (Unix timestamp)
		handshakeStr := fields[4]
		if handshakeStr == "0" {
			continue // No handshake yet
		}

		handshakeUnix, err := strconv.ParseInt(handshakeStr, 10, 64)
		if err != nil {
			m.logger.Warn("Failed to parse handshake timestamp",
				slog.String("value", handshakeStr),
				slog.String("error", err.Error()))
			continue
		}

		handshakeTime := time.Unix(handshakeUnix, 0)

		// Consider peer connected if handshake is recent (within 3 minutes)
		if time.Since(handshakeTime) < 3*time.Minute {
			connectedClients++

			// Track the latest handshake
			if latestHandshake == nil || handshakeTime.After(*latestHandshake) {
				latestHandshake = &handshakeTime
			}
		}
	}

	return &NodeActivity{
		ServerID:         serverID,
		ConnectedClients: connectedClients,
		LastHandshakeAt:  latestHandshake,
	}, nil
}
