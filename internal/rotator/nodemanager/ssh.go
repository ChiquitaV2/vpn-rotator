package nodemanager

import (
	"context"
	"fmt"
	"strings"
)

// executeSSHCommand executes a command on a node with comprehensive retry logic and error handling
func (m *Manager) executeSSHCommand(ctx context.Context, nodeIP, command string) (string, error) {
	return m.sshPool.ExecuteCommand(ctx, nodeIP, command)
}

// validateNodeConnectivity performs a basic connectivity check to a node
func (m *Manager) validateNodeConnectivity(ctx context.Context, nodeIP string) error {
	// Simple ping-like test using SSH
	cmd := "echo 'connectivity_test'"
	output, err := m.executeSSHCommand(ctx, nodeIP, cmd)
	if err != nil {
		return fmt.Errorf("node connectivity test failed: %w", err)
	}

	if !strings.Contains(output, "connectivity_test") {
		return fmt.Errorf("unexpected response from connectivity test: %s", output)
	}

	return nil
}
