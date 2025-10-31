package nodemanager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// saveWireGuardConfig saves the current WireGuard configuration to persist across reboots
func (m *Manager) saveWireGuardConfig(ctx context.Context, nodeIP string) error {
	// First, check if wg-quick save is available
	checkCmd := "which wg-quick"
	_, err := m.executeSSHCommand(ctx, nodeIP, checkCmd)
	if err != nil {
		m.logger.Debug("wg-quick not available, using alternative save method",
			slog.String("node_ip", nodeIP))
		return m.saveWireGuardConfigAlternative(ctx, nodeIP)
	}

	// Use wg-quick save
	saveCmd := "wg-quick save wg0"
	_, err = m.executeSSHCommand(ctx, nodeIP, saveCmd)
	if err != nil {
		m.logger.Debug("wg-quick save failed, trying alternative method",
			slog.String("node_ip", nodeIP),
			slog.String("error", err.Error()))
		return m.saveWireGuardConfigAlternative(ctx, nodeIP)
	}

	return nil
}

// saveWireGuardConfigAlternative saves WireGuard config using wg showconf
func (m *Manager) saveWireGuardConfigAlternative(ctx context.Context, nodeIP string) error {
	// Get current configuration
	showCmd := "wg showconf wg0"
	config, err := m.executeSSHCommand(ctx, nodeIP, showCmd)
	if err != nil {
		return NewWireguardError(nodeIP, "showconf", err)
	}

	// Write to configuration file
	writeCmd := fmt.Sprintf("echo '%s' > /etc/wireguard/wg0.conf", config)
	_, err = m.executeSSHCommand(ctx, nodeIP, writeCmd)
	if err != nil {
		return NewWireguardError(nodeIP, "writeconf", err)
	}

	return nil
}

// validateWireGuardInterface validates that the WireGuard interface is properly configured
func (m *Manager) validateWireGuardInterface(ctx context.Context, nodeIP string) error {
	// Check if interface exists and is up
	cmd := "wg show wg0"
	output, err := m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return NewWireguardError(nodeIP, "show", fmt.Errorf("WireGuard interface wg0 not found or not configured: %w", err))
	}

	// Basic validation that output contains interface information
	if !strings.Contains(output, "interface: wg0") && !strings.Contains(output, "public key:") {
		return fmt.Errorf("WireGuard interface wg0 appears to be misconfigured")
	}

	return nil
}

// getWireGuardInterfaceInfo retrieves basic information about the WireGuard interface
func (m *Manager) getWireGuardInterfaceInfo(ctx context.Context, nodeIP string) (map[string]string, error) {
	cmd := "wg show wg0"
	output, err := m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return nil, NewWireguardError(nodeIP, "show", fmt.Errorf("failed to get WireGuard interface info: %w", err))
	}

	info := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				info[key] = value
			}
		}
	}

	return info, nil
}
