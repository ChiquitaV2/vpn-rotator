package nodeinteractor

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// GetWireGuardStatus retrieves WireGuard interface status and configuration
func (s *SSHNodeInteractor) uninstrumentedGetWireGuardStatus(ctx context.Context, nodeHost string) (*WireGuardStatus, error) {
	s.logger.Debug("getting WireGuard status", slog.String("host", nodeHost))

	// Get WireGuard interface information
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s", s.config.WireGuardInterface))
	if err != nil {
		return nil, NewWireGuardOperationError(nodeHost, "status", s.config.WireGuardInterface, "", err)
	}

	status := &WireGuardStatus{
		InterfaceName: s.config.WireGuardInterface,
		LastUpdated:   time.Now(),
		IsRunning:     true, // If command succeeded, interface is running
	}

	// Parse WireGuard show output
	if err := s.parseWireGuardStatus(result.Stdout, status); err != nil {
		return nil, NewWireGuardOperationError(nodeHost, "parse_status", s.config.WireGuardInterface, "", err)
	}

	// Count peers
	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		s.logger.Warn("failed to parse peers for status", slog.String("host", nodeHost), slog.String("error", err.Error()))
	} else {
		status.PeerCount = len(peers)
	}

	s.logger.Debug("WireGuard status retrieved",
		slog.String("host", nodeHost),
		slog.String("interface", status.InterfaceName),
		slog.String("public_key", status.PublicKey[:8]+"..."),
		slog.Int("peer_count", status.PeerCount))

	return status, nil
}

// AddPeer adds a WireGuard peer with proper validation and rollback on failure
func (s *SSHNodeInteractor) uninstrumentedAddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	s.logger.Debug("adding peer to WireGuard",
		slog.String("host", nodeHost),
		slog.String("public_key", config.PublicKey[:8]+"..."),
		slog.String("allowed_ips", strings.Join(config.AllowedIPs, ",")))

	// Validate peer configuration
	if err := s.validatePeerConfig(config); err != nil {
		return NewWireGuardConfigError(nodeHost, s.config.WireGuardInterface, "peer_config", "", err)
	}

	existingPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		s.logger.Warn("failed to check existing peers", slog.String("host", nodeHost), slog.String("error", err.Error()))
	} else {
		for _, peer := range existingPeers {
			if peer.PublicKey == config.PublicKey {
				return NewPeerAlreadyExistsError(nodeHost, config.PublicKey, s.config.WireGuardInterface)
			}
			// Check for IP conflicts
			for _, allowedIP := range config.AllowedIPs {
				for _, existingIP := range peer.AllowedIPs {
					if allowedIP == existingIP {
						return NewIPConflictError(nodeHost, allowedIP, peer.PublicKey, config.PublicKey)
					}
				}
			}
		}
	}

	// Build WireGuard peer add command
	cmd := s.buildAddPeerCommand(config)

	// Execute command with rollback capability
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		return NewWireGuardOperationError(nodeHost, "add_peer", s.config.WireGuardInterface, config.PublicKey, err)
	}

	if result.ExitCode != 0 {
		return NewWireGuardOperationError(nodeHost, "add_peer", s.config.WireGuardInterface, config.PublicKey,
			fmt.Errorf("command failed with exit code %d: %s", result.ExitCode, result.Stderr))
	}
	s.logger.Warn("peer add command executed successfully", slog.String("dump", result.Stderr))
	// Validate that peer was added successfully
	if err := s.validatePeerAddition(ctx, nodeHost, config.PublicKey); err != nil {
		// Attempt rollback
		s.logger.Warn("peer addition validation failed, attempting rollback",
			slog.String("host", nodeHost),
			slog.String("public_key", config.PublicKey[:8]+"..."))

		if rollbackErr := s.uninstrumentedRemovePeer(ctx, nodeHost, config.PublicKey); rollbackErr != nil {
			s.logger.Error("rollback failed", slog.String("host", nodeHost), slog.String("error", rollbackErr.Error()))
		}

		return NewWireGuardOperationError(nodeHost, "validate_add_peer", s.config.WireGuardInterface, config.PublicKey, err)
	}

	// Save configuration to persist across reboots
	if err := s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost); err != nil {
		s.logger.Warn("failed to save WireGuard config after peer addition", slog.String("host", nodeHost),
			slog.String("public_key", config.PublicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	s.logger.Info("successfully added peer to WireGuard",
		slog.String("host", nodeHost),
		slog.String("public_key", config.PublicKey[:8]+"..."),
		slog.String("allowed_ips", strings.Join(config.AllowedIPs, ",")))

	return nil
}

// RemovePeer removes a WireGuard peer with existence checking and validation
func (s *SSHNodeInteractor) uninstrumentedRemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	s.logger.Debug("removing peer from WireGuard",
		slog.String("host", nodeHost),
		slog.String("public_key", publicKey[:8]+"..."))

	// Validate public key format
	if !crypto.IsValidWireGuardKey(publicKey) {
		return NewValidationError("public_key", publicKey, "wireguard_key_format", "invalid WireGuard public key format")
	}

	// Check if peer exists before attempting removal
	existingPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		s.logger.Warn("failed to check existing peers before removal", slog.String("host", nodeHost), slog.String("error", err.Error()))
	} else {
		peerExists := false
		for _, peer := range existingPeers {
			if peer.PublicKey == publicKey {
				peerExists = true
				break
			}
		}
		if !peerExists {
			s.logger.Debug("peer not found, skipping removal",
				slog.String("host", nodeHost),
				slog.String("public_key", publicKey[:8]+"..."))
			return nil // Not an error if peer doesn't exist
		}
	}

	// Build WireGuard peer remove command
	cmd := fmt.Sprintf("wg set %s peer %s remove", s.config.WireGuardInterface, publicKey)

	// Execute command
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		return NewWireGuardOperationError(nodeHost, "remove_peer", s.config.WireGuardInterface, publicKey, err)
	}

	if result.ExitCode != 0 {
		return NewWireGuardOperationError(nodeHost, "remove_peer", s.config.WireGuardInterface, publicKey,
			fmt.Errorf("command failed with exit code %d: %s", result.ExitCode, result.Stderr))
	}

	// Validate that peer was removed successfully
	if err := s.validatePeerRemoval(ctx, nodeHost, publicKey); err != nil {
		s.logger.Warn("peer removal validation failed",
			slog.String("host", nodeHost),
			slog.String("public_key", publicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	// Save configuration to persist across reboots
	if err := s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost); err != nil {
		s.logger.Warn("failed to save WireGuard config after peer removal",
			slog.String("host", nodeHost),
			slog.String("public_key", publicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	s.logger.Info("successfully removed peer from WireGuard",
		slog.String("host", nodeHost),
		slog.String("public_key", publicKey[:8]+"..."))

	return nil
}

// UpdatePeer updates a peer's configuration by removing and re-adding it
func (s *SSHNodeInteractor) uninstrumentedUpdatePeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	s.logger.Debug("updating peer configuration",
		slog.String("host", nodeHost),
		slog.String("public_key", config.PublicKey[:8]+"..."))

	// Validate peer configuration
	if err := s.validatePeerConfig(config); err != nil {
		return NewWireGuardConfigError(nodeHost, s.config.WireGuardInterface, "peer_config", "", err)
	}

	// Remove existing peer first
	if err := s.uninstrumentedRemovePeer(ctx, nodeHost, config.PublicKey); err != nil {
		s.logger.Warn("failed to remove existing peer during update",
			slog.String("host", nodeHost),
			slog.String("public_key", config.PublicKey[:8]+"..."),
			slog.String("error", err.Error()))
	}

	// Add peer with new configuration
	if err := s.uninstrumentedAddPeer(ctx, nodeHost, config); err != nil {
		return err
	}

	s.logger.Info("successfully updated peer configuration",
		slog.String("host", nodeHost),
		slog.String("public_key", config.PublicKey[:8]+"..."))

	return nil
}

// ListPeers lists all peers with WireGuard dump parsing
func (s *SSHNodeInteractor) uninstrumentedListPeers(ctx context.Context, nodeHost string) ([]*WireGuardPeerStatus, error) {
	s.logger.Debug("listing WireGuard peers", slog.String("host", nodeHost))

	// Get WireGuard dump output
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s dump", s.config.WireGuardInterface))
	if err != nil {
		return nil, NewWireGuardOperationError(nodeHost, "list_peers", s.config.WireGuardInterface, "", err)
	}

	// Parse WireGuard dump output
	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		return nil, NewWireGuardOperationError(nodeHost, "parse_peers", s.config.WireGuardInterface, "", err)
	}

	s.logger.Debug("listed WireGuard peers",
		slog.String("host", nodeHost),
		slog.Int("peer_count", len(peers)))

	return peers, nil
}

// SyncPeers synchronizes peers to match desired configuration state
func (s *SSHNodeInteractor) uninstrumentedSyncPeers(ctx context.Context, nodeHost string, configs []PeerWireGuardConfig) error {
	s.logger.Debug("synchronizing WireGuard peers",
		slog.String("host", nodeHost),
		slog.Int("desired_peer_count", len(configs)))

	// Get current peers
	currentPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		return NewWireGuardOperationError(nodeHost, "sync_peers_list", s.config.WireGuardInterface, "", err)
	}

	// Create maps for easier comparison
	currentPeerMap := make(map[string]*WireGuardPeerStatus)
	for _, peer := range currentPeers {
		currentPeerMap[peer.PublicKey] = peer
	}

	desiredPeerMap := make(map[string]PeerWireGuardConfig)
	for _, config := range configs {
		desiredPeerMap[config.PublicKey] = config
	}

	// Remove peers that are not in desired state
	for publicKey := range currentPeerMap {
		if _, exists := desiredPeerMap[publicKey]; !exists {
			s.logger.Debug("removing unwanted peer during sync",
				slog.String("host", nodeHost),
				slog.String("public_key", publicKey[:8]+"..."))

			if err := s.uninstrumentedRemovePeer(ctx, nodeHost, publicKey); err != nil {
				s.logger.Error("failed to remove peer during sync",
					slog.String("host", nodeHost),
					slog.String("public_key", publicKey[:8]+"..."),
					slog.String("error", err.Error()))
			}
		}
	}

	// Add or update peers to match desired state
	for _, config := range configs {
		if currentPeer, exists := currentPeerMap[config.PublicKey]; exists {
			// Check if peer needs updating
			if s.peerNeedsUpdate(currentPeer, config) {
				s.logger.Debug("updating peer during sync",
					slog.String("host", nodeHost),
					slog.String("public_key", config.PublicKey[:8]+"..."))

				if err := s.uninstrumentedUpdatePeer(ctx, nodeHost, config); err != nil {
					s.logger.Error("failed to update peer during sync",
						slog.String("host", nodeHost),
						slog.String("public_key", config.PublicKey[:8]+"..."),
						slog.String("error", err.Error()))
				}
			}
		} else {
			// Add new peer
			s.logger.Debug("adding new peer during sync",
				slog.String("host", nodeHost),
				slog.String("public_key", config.PublicKey[:8]+"..."))

			if err := s.uninstrumentedAddPeer(ctx, nodeHost, config); err != nil {
				s.logger.Error("failed to add peer during sync",
					slog.String("host", nodeHost),
					slog.String("public_key", config.PublicKey[:8]+"..."),
					slog.String("error", err.Error()))
			}
		}
	}

	s.logger.Info("WireGuard peer synchronization completed",
		slog.String("host", nodeHost),
		slog.Int("desired_peers", len(configs)),
		slog.Int("current_peers", len(currentPeers)))

	return nil
}

// UpdateWireGuardConfig updates the complete WireGuard interface configuration
func (s *SSHNodeInteractor) uninstrumentedUpdateWireGuardConfig(ctx context.Context, nodeHost string, config WireGuardConfig) error {
	s.logger.Debug("updating WireGuard configuration",
		slog.String("host", nodeHost),
		slog.String("interface", config.InterfaceName))

	// This is a complex operation that would typically involve:
	// 1. Backing up current configuration
	// 2. Writing new configuration file
	// 3. Restarting WireGuard service
	// 4. Validating new configuration

	// For now, return not implemented
	return NewWireGuardOperationError(nodeHost, "update_config", config.InterfaceName, "",
		fmt.Errorf("full configuration update not implemented yet"))
}

// RestartWireGuard restarts the WireGuard service
func (s *SSHNodeInteractor) uninstrumentedRestartWireGuard(ctx context.Context, nodeHost string) error {
	s.logger.Debug("restarting WireGuard service", slog.String("host", nodeHost))

	// Try wg-quick restart first
	cmd := fmt.Sprintf("wg-quick down %s && wg-quick up %s", s.config.WireGuardInterface, s.config.WireGuardInterface)
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		// Fallback to systemctl if wg-quick fails
		s.logger.Debug("wg-quick restart failed, trying systemctl",
			slog.String("host", nodeHost),
			slog.String("error", err.Error()))

		systemctlCmd := fmt.Sprintf("systemctl restart wg-quick@%s", s.config.WireGuardInterface)
		result, err = s.executeWithRetry(ctx, nodeHost, systemctlCmd)
		if err != nil {
			return NewWireGuardOperationError(nodeHost, "restart", s.config.WireGuardInterface, "", err)
		}
	}

	if result.ExitCode != 0 {
		return NewWireGuardOperationError(nodeHost, "restart", s.config.WireGuardInterface, "",
			fmt.Errorf("restart failed with exit code %d: %s", result.ExitCode, result.Stderr))
	}

	// Validate that WireGuard is running after restart
	time.Sleep(2 * time.Second) // Give it a moment to start
	if _, err := s.uninstrumentedGetWireGuardStatus(ctx, nodeHost); err != nil {
		return NewWireGuardOperationError(nodeHost, "restart_validate", s.config.WireGuardInterface, "", err)
	}

	s.logger.Info("WireGuard service restarted successfully", slog.String("host", nodeHost))
	return nil
}

// SaveWireGuardConfig saves the current WireGuard configuration to persist across reboots
func (s *SSHNodeInteractor) uninstrumentedSaveWireGuardConfig(ctx context.Context, nodeHost string) error {
	s.logger.Debug("saving WireGuard configuration", slog.String("host", nodeHost))

	// Try wg-quick save first
	saveCmd := fmt.Sprintf("wg-quick save %s", s.config.WireGuardInterface)
	result, err := s.executeCommandOnce(ctx, nodeHost, saveCmd)
	if err != nil {
		s.logger.Debug("wg-quick save failed, trying alternative method",
			slog.String("host", nodeHost),
			slog.String("error", err.Error()))
		return s.saveWireGuardConfigAlternative(ctx, nodeHost)
	}

	if result.ExitCode != 0 {
		s.logger.Debug("wg-quick save failed with non-zero exit, trying alternative method",
			slog.String("host", nodeHost),
			slog.String("stderr", result.Stderr))
		return s.saveWireGuardConfigAlternative(ctx, nodeHost)
	}

	s.logger.Debug("WireGuard configuration saved successfully", slog.String("host", nodeHost))
	return nil
}

// saveWireGuardConfigAlternative saves WireGuard config using wg showconf
func (s *SSHNodeInteractor) saveWireGuardConfigAlternative(ctx context.Context, nodeHost string) error {
	// Get current configuration
	showCmd := fmt.Sprintf("wg showconf %s", s.config.WireGuardInterface)
	result, err := s.executeCommandOnce(ctx, nodeHost, showCmd)
	if err != nil {
		return NewWireGuardOperationError(nodeHost, "showconf", s.config.WireGuardInterface, "", err)
	}

	if result.ExitCode != 0 {
		return NewWireGuardOperationError(nodeHost, "showconf", s.config.WireGuardInterface, "",
			fmt.Errorf("showconf failed with exit code %d: %s", result.ExitCode, result.Stderr))
	}

	// Write to configuration file
	writeCmd := fmt.Sprintf("echo '%s' > %s", result.Stdout, s.config.WireGuardConfigPath)
	writeResult, err := s.executeCommandOnce(ctx, nodeHost, writeCmd)
	if err != nil {
		return NewWireGuardOperationError(nodeHost, "writeconf", s.config.WireGuardInterface, "", err)
	}

	if writeResult.ExitCode != 0 {
		return NewWireGuardOperationError(nodeHost, "writeconf", s.config.WireGuardInterface, "",
			fmt.Errorf("writeconf failed with exit code %d: %s", writeResult.ExitCode, writeResult.Stderr))
	}

	s.logger.Debug("WireGuard configuration saved using alternative method", slog.String("host", nodeHost))
	return nil
}

// Helper functions

// buildAddPeerCommand builds the WireGuard command to add a peer
func (s *SSHNodeInteractor) buildAddPeerCommand(config PeerWireGuardConfig) string {
	cmd := fmt.Sprintf("wg set %s peer %s allowed-ips %s",
		s.config.WireGuardInterface,
		config.PublicKey,
		strings.Join(config.AllowedIPs, ","))

	if config.PresharedKey != nil && *config.PresharedKey != "" {
		// Use a temporary file for preshared key to avoid command line exposure
		cmd = fmt.Sprintf("echo '%s' | wg set %s peer %s allowed-ips %s preshared-key /dev/stdin",
			*config.PresharedKey,
			s.config.WireGuardInterface,
			config.PublicKey,
			strings.Join(config.AllowedIPs, ","))
	}

	if config.Endpoint != nil && *config.Endpoint != "" {
		cmd += fmt.Sprintf(" endpoint %s", *config.Endpoint)
	}

	return cmd
}

// validatePeerConfig validates a peer configuration
func (s *SSHNodeInteractor) validatePeerConfig(config PeerWireGuardConfig) error {
	if !crypto.IsValidWireGuardKey(config.PublicKey) {
		return NewValidationError("public_key", config.PublicKey, "wireguard_key_format", "invalid WireGuard public key format")
	}

	if len(config.AllowedIPs) == 0 {
		return NewValidationError("allowed_ips", config.AllowedIPs, "non_empty", "allowed IPs cannot be empty")
	}

	// Validate each allowed IP
	for i, ip := range config.AllowedIPs {
		if ip == "" {
			return NewValidationError(fmt.Sprintf("allowed_ips[%d]", i), ip, "non_empty", "allowed IP cannot be empty")
		}
		// Additional IP validation could be added here
	}

	if config.PresharedKey != nil && *config.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*config.PresharedKey) {
			return NewValidationError("preshared_key", *config.PresharedKey, "wireguard_key_format", "invalid WireGuard preshared key format")
		}
	}

	return nil
}

// validatePeerAddition validates that a peer was successfully added
func (s *SSHNodeInteractor) validatePeerAddition(ctx context.Context, nodeHost, publicKey string) error {
	peers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		return fmt.Errorf("failed to list peers for validation: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			return nil // Peer found, addition successful
		}
	}

	return NewPeerNotFoundError(nodeHost, publicKey, s.config.WireGuardInterface)
}

// validatePeerRemoval validates that a peer was successfully removed
func (s *SSHNodeInteractor) validatePeerRemoval(ctx context.Context, nodeHost, publicKey string) error {
	peers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		return fmt.Errorf("failed to list peers for validation: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			return fmt.Errorf("peer %s still found after removal", publicKey[:8]+"...")
		}
	}

	return nil // Peer not found, removal successful
}

// peerNeedsUpdate checks if a peer needs to be updated
func (s *SSHNodeInteractor) peerNeedsUpdate(current *WireGuardPeerStatus, desired PeerWireGuardConfig) bool {
	// Check allowed IPs
	if len(current.AllowedIPs) != len(desired.AllowedIPs) {
		return true
	}

	currentIPMap := make(map[string]bool)
	for _, ip := range current.AllowedIPs {
		currentIPMap[ip] = true
	}

	for _, ip := range desired.AllowedIPs {
		if !currentIPMap[ip] {
			return true
		}
	}

	// Check endpoint
	if desired.Endpoint != nil {
		if current.Endpoint == nil || *current.Endpoint != *desired.Endpoint {
			return true
		}
	} else if current.Endpoint != nil {
		return true
	}

	// Note: We can't easily check preshared key changes without exposing the key
	// In practice, preshared key changes would require explicit update calls

	return false
}

// parseWireGuardStatus parses WireGuard show output to populate status
func (s *SSHNodeInteractor) parseWireGuardStatus(output string, status *WireGuardStatus) error {
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				switch key {
				case "public key":
					status.PublicKey = value
				case "listening port":
					if port, err := strconv.Atoi(value); err == nil {
						status.ListenPort = port
					}
				}
			}
		}
	}

	return nil
}

// parseWireGuardPeers parses WireGuard dump output to extract peer information
func (s *SSHNodeInteractor) parseWireGuardPeers(output string) ([]*WireGuardPeerStatus, error) {
	var peers []*WireGuardPeerStatus
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		// Skip interface line which typically has 4 parts
		if len(parts) == 4 {
			continue
		}
		// Need at least a public key and allowed IPs to consider this a peer line
		if len(parts) < 3 {
			continue
		}

		peer := &WireGuardPeerStatus{
			PublicKey: parts[0],
		}

		// Helper to safely get part by index
		get := func(i int) (string, bool) {
			if i < len(parts) {
				return parts[i], true
			}
			return "", false
		}

		// preshared key is parts[1] (ignored here, but checked for "(none)")
		if v, ok := get(1); ok && v != "(none)" {
			// if you have a field to store it in the struct later, set it here
			_ = v
		}

		// endpoint is parts[2]
		if v, ok := get(2); ok && v != "(none)" {
			ep := v
			peer.Endpoint = &ep
		}

		// allowed IPs is typically parts[3] (but if columns shift, try to find a comma or IP-looking token)
		if v, ok := get(3); ok {
			peer.AllowedIPs = strings.Split(v, ",")
		} else {
			peer.AllowedIPs = []string{}
		}

		// latest handshake timestamp is usually parts[4]
		if v, ok := get(4); ok && v != "0" {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				tm := time.Unix(ts, 0)
				peer.LastHandshake = &tm
			}
		}

		// transfer rx (parts[5]) and tx (parts[6])
		if v, ok := get(5); ok {
			if rx, err := strconv.ParseInt(v, 10, 64); err == nil {
				peer.TransferRx = rx
			}
		}
		if v, ok := get(6); ok {
			if tx, err := strconv.ParseInt(v, 10, 64); err == nil {
				peer.TransferTx = tx
			}
		}

		// persistent keepalive is commonly at parts[7]; be tolerant of non-numeric trailing tokens
		if v, ok := get(7); ok {
			if ka, err := strconv.Atoi(v); err == nil {
				peer.PersistentKeepalive = ka
			}
		}

		peers = append(peers, peer)
	}

	return peers, nil
}
