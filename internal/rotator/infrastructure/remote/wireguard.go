package remote

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// GetWireGuardStatus retrieves WireGuard interface status and configuration
func (s *SSHNodeInteractor) uninstrumentedGetWireGuardStatus(ctx context.Context, nodeHost string) (*node.WireGuardStatus, error) {
	op := s.logger.StartOp(ctx, "GetWireGuardStatus", slog.String("host", nodeHost))

	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s", s.config.WireGuardInterface))
	if err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeSSHCommand, "failed to get WireGuard status", true)
		op.Fail(err, "failed to execute wg show")
		return nil, err
	}

	status := &node.WireGuardStatus{
		InterfaceName: s.config.WireGuardInterface,
		LastUpdated:   time.Now(),
		IsRunning:     true,
	}

	if err := s.parseWireGuardStatus(result.Stdout, status); err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to parse WireGuard status", false)
		op.Fail(err, "failed to parse status")
		return nil, err
	}

	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to parse peers for status", "host", nodeHost, "error", err)
	} else {
		status.PeerCount = len(peers)
	}

	op.Complete("retrieved wireguard status", slog.Int("peer_count", status.PeerCount))
	return status, nil
}

// AddPeer adds a WireGuard peer with proper validation and rollback on failure
func (s *SSHNodeInteractor) uninstrumentedAddPeer(ctx context.Context, nodeHost string, config node.PeerWireGuardConfig) error {
	op := s.logger.StartOp(ctx, "AddPeer", slog.String("host", nodeHost), slog.String("public_key", config.PublicKey[:8]+"..."))

	if err := s.validatePeerConfig(config); err != nil {
		op.Fail(err, "peer config validation failed")
		return err
	}

	existingPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to check existing peers, proceeding with caution", "host", nodeHost, "error", err)
	} else {
		for _, peer := range existingPeers {
			if peer.PublicKey == config.PublicKey {
				err = errors.NewPeerError(errors.ErrCodePeerKeyConflict, "peer already exists", false, nil)
				op.Fail(err, "peer already exists")
				return err
			}
			for _, allowedIP := range config.AllowedIPs {
				for _, existingIP := range peer.AllowedIPs {
					if allowedIP == existingIP {
						err = errors.NewPeerError(errors.ErrCodePeerIPConflict, "IP address conflict", false, nil)
						op.Fail(err, "ip conflict")
						return err
					}
				}
			}
		}
	}
	op.Progress("uniqueness check passed")

	cmd := s.buildAddPeerCommand(config)
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to add peer", true)
		op.Fail(err, "add peer command failed")
		return err
	}

	if result.ExitCode != 0 {
		err = errors.NewInfrastructureError(errors.ErrCodeWireGuardError, "add peer command failed with non-zero exit code", true, nil)
		op.Fail(err, "add peer command failed", slog.Int("exit_code", result.ExitCode), slog.String("stderr", result.Stderr))
		return err
	}

	if err := s.validatePeerAddition(ctx, nodeHost, config.PublicKey); err != nil {
		op.Fail(err, "peer addition validation failed, attempting rollback")
		if rollbackErr := s.uninstrumentedRemovePeer(ctx, nodeHost, config.PublicKey); rollbackErr != nil {
			s.logger.ErrorCtx(ctx, "rollback failed", rollbackErr, slog.String("host", nodeHost))
		}
		return err
	}

	if err := s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost); err != nil {
		s.logger.WarnContext(ctx, "failed to save WireGuard config after peer addition", "host", nodeHost, "error", err)
	}

	op.Complete("peer added successfully")
	return nil
}

// RemovePeer removes a WireGuard peer with existence checking and validation
func (s *SSHNodeInteractor) uninstrumentedRemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	op := s.logger.StartOp(ctx, "RemovePeer", slog.String("host", nodeHost), slog.String("public_key", publicKey[:8]+"..."))

	if !crypto.IsValidWireGuardKey(publicKey) {
		err := errors.NewPeerError(errors.ErrCodePeerValidation, "invalid WireGuard public key format", false, nil)
		op.Fail(err, "invalid public key")
		return err
	}

	existingPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to check existing peers before removal", "host", nodeHost, "error", err)
	} else {
		peerExists := false
		for _, peer := range existingPeers {
			if peer.PublicKey == publicKey {
				peerExists = true
				break
			}
		}
		if !peerExists {
			op.Complete("peer not found, skipping removal")
			return nil
		}
	}

	cmd := fmt.Sprintf("wg set %s peer %s remove", s.config.WireGuardInterface, publicKey)
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to remove peer", true)
		op.Fail(err, "remove peer command failed")
		return err
	}

	if result.ExitCode != 0 {
		err = errors.NewInfrastructureError(errors.ErrCodeWireGuardError, "remove peer command failed with non-zero exit code", true, nil)
		op.Fail(err, "remove peer command failed", slog.Int("exit_code", result.ExitCode), slog.String("stderr", result.Stderr))
		return err
	}

	if err := s.validatePeerRemoval(ctx, nodeHost, publicKey); err != nil {
		s.logger.WarnContext(ctx, "peer removal validation failed", "host", nodeHost, "error", err)
	}

	if err := s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost); err != nil {
		s.logger.WarnContext(ctx, "failed to save WireGuard config after peer removal", "host", nodeHost, "error", err)
	}

	op.Complete("peer removed successfully")
	return nil
}

// UpdatePeer updates a peer's configuration by removing and re-adding it
func (s *SSHNodeInteractor) uninstrumentedUpdatePeer(ctx context.Context, nodeHost string, config node.PeerWireGuardConfig) error {
	op := s.logger.StartOp(ctx, "UpdatePeer", slog.String("host", nodeHost), slog.String("public_key", config.PublicKey[:8]+"..."))

	if err := s.validatePeerConfig(config); err != nil {
		op.Fail(err, "peer config validation failed")
		return err
	}

	if err := s.uninstrumentedRemovePeer(ctx, nodeHost, config.PublicKey); err != nil {
		s.logger.WarnContext(ctx, "failed to remove existing peer during update, proceeding anyway", "host", nodeHost, "error", err)
	}

	if err := s.uninstrumentedAddPeer(ctx, nodeHost, config); err != nil {
		op.Fail(err, "failed to add peer during update")
		return err
	}

	op.Complete("peer updated successfully")
	return nil
}

// ListPeers lists all peers with WireGuard dump parsing
func (s *SSHNodeInteractor) uninstrumentedListPeers(ctx context.Context, nodeHost string) ([]*node.WireGuardPeerStatus, error) {
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s dump", s.config.WireGuardInterface))
	if err != nil {
		return nil, errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeSSHCommand, "failed to list peers", true)
	}

	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		return nil, errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to parse peers", false)
	}

	return peers, nil
}

// SyncPeers synchronizes peers to match desired configuration state
func (s *SSHNodeInteractor) uninstrumentedSyncPeers(ctx context.Context, nodeHost string, configs []node.PeerWireGuardConfig) error {
	op := s.logger.StartOp(ctx, "SyncPeers", slog.String("host", nodeHost), slog.Int("desired_peer_count", len(configs)))

	currentPeers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to list current peers during sync", true)
		op.Fail(err, "failed to list peers")
		return err
	}
	op.Progress("listed current peers", slog.Int("current_peer_count", len(currentPeers)))

	currentPeerMap := make(map[string]*node.WireGuardPeerStatus)
	for _, peer := range currentPeers {
		currentPeerMap[peer.PublicKey] = peer
	}

	desiredPeerMap := make(map[string]node.PeerWireGuardConfig)
	for _, config := range configs {
		desiredPeerMap[config.PublicKey] = config
	}

	for publicKey := range currentPeerMap {
		if _, exists := desiredPeerMap[publicKey]; !exists {
			op.Progress("removing unwanted peer", slog.String("public_key", publicKey[:8]+"..."))
			if err := s.uninstrumentedRemovePeer(ctx, nodeHost, publicKey); err != nil {
				s.logger.ErrorCtx(ctx, "failed to remove peer during sync", err, slog.String("public_key", publicKey[:8]+"..."))
			}
		}
	}

	for _, config := range configs {
		if currentPeer, exists := currentPeerMap[config.PublicKey]; exists {
			if s.peerNeedsUpdate(currentPeer, config) {
				op.Progress("updating peer", slog.String("public_key", config.PublicKey[:8]+"..."))
				if err := s.uninstrumentedUpdatePeer(ctx, nodeHost, config); err != nil {
					s.logger.ErrorCtx(ctx, "failed to update peer during sync", err, slog.String("public_key", config.PublicKey[:8]+"..."))
				}
			}
		} else {
			op.Progress("adding new peer", slog.String("public_key", config.PublicKey[:8]+"..."))
			if err := s.uninstrumentedAddPeer(ctx, nodeHost, config); err != nil {
				s.logger.ErrorCtx(ctx, "failed to add peer during sync", err, slog.String("public_key", config.PublicKey[:8]+"..."))
			}
		}
	}

	op.Complete("peer synchronization complete")
	return nil
}

// UpdateWireGuardConfig updates the complete WireGuard interface configuration
func (s *SSHNodeInteractor) uninstrumentedUpdateWireGuardConfig(ctx context.Context, nodeHost string, config node.WireGuardConfig) error {
	return errors.NewSystemError("not_implemented", "full configuration update not implemented yet", false, nil)
}

// RestartWireGuard restarts the WireGuard service
func (s *SSHNodeInteractor) uninstrumentedRestartWireGuard(ctx context.Context, nodeHost string) error {
	op := s.logger.StartOp(ctx, "RestartWireGuard", slog.String("host", nodeHost))

	cmd := fmt.Sprintf("wg-quick down %s && wg-quick up %s", s.config.WireGuardInterface, s.config.WireGuardInterface)
	result, err := s.executeWithRetry(ctx, nodeHost, cmd)
	if err != nil {
		s.logger.DebugContext(ctx, "wg-quick restart failed, trying systemctl", "host", nodeHost, "error", err)
		systemctlCmd := fmt.Sprintf("systemctl restart wg-quick@%s", s.config.WireGuardInterface)
		result, err = s.executeWithRetry(ctx, nodeHost, systemctlCmd)
		if err != nil {
			err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to restart WireGuard", true)
			op.Fail(err, "restart command failed")
			return err
		}
	}

	if result.ExitCode != 0 {
		err = errors.NewInfrastructureError(errors.ErrCodeWireGuardError, "restart command failed with non-zero exit code", true, nil)
		op.Fail(err, "restart command failed", slog.Int("exit_code", result.ExitCode), slog.String("stderr", result.Stderr))
		return err
	}

	time.Sleep(2 * time.Second)
	if _, err := s.uninstrumentedGetWireGuardStatus(ctx, nodeHost); err != nil {
		err = errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to validate WireGuard status after restart", false)
		op.Fail(err, "validation after restart failed")
		return err
	}

	op.Complete("WireGuard service restarted")
	return nil
}

// SaveWireGuardConfig saves the current WireGuard configuration to persist across reboots
func (s *SSHNodeInteractor) uninstrumentedSaveWireGuardConfig(ctx context.Context, nodeHost string) error {
	saveCmd := fmt.Sprintf("wg-quick save %s", s.config.WireGuardInterface)
	result, err := s.executeCommandOnce(ctx, nodeHost, saveCmd)
	if err != nil || result.ExitCode != 0 {
		s.logger.DebugContext(ctx, "wg-quick save failed, trying alternative method", "host", nodeHost, "error", err, "stderr", result.Stderr)
		return s.saveWireGuardConfigAlternative(ctx, nodeHost)
	}
	s.logger.DebugContext(ctx, "WireGuard configuration saved successfully", "host", nodeHost)
	return nil
}

// saveWireGuardConfigAlternative saves WireGuard config using wg showconf
func (s *SSHNodeInteractor) saveWireGuardConfigAlternative(ctx context.Context, nodeHost string) error {
	showCmd := fmt.Sprintf("wg showconf %s", s.config.WireGuardInterface)
	result, err := s.executeCommandOnce(ctx, nodeHost, showCmd)
	if err != nil {
		return errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeWireGuardError, "failed to get WireGuard config for saving", true)
	}

	if result.ExitCode != 0 {
		return errors.NewInfrastructureError(errors.ErrCodeWireGuardError, "showconf command failed", true, nil)
	}

	writeCmd := fmt.Sprintf("echo '%s' > %s", result.Stdout, s.config.WireGuardConfigPath)
	writeResult, err := s.executeCommandOnce(ctx, nodeHost, writeCmd)
	if err != nil {
		return errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeFileOperation, "failed to write WireGuard config", true)
	}

	if writeResult.ExitCode != 0 {
		return errors.NewInfrastructureError(errors.ErrCodeSSHCommand, "write config command failed", true, nil)
	}

	s.logger.DebugContext(ctx, "WireGuard configuration saved using alternative method", "host", nodeHost)
	return nil
}

// Helper functions

func (s *SSHNodeInteractor) buildAddPeerCommand(config node.PeerWireGuardConfig) string {
	cmd := fmt.Sprintf("wg set %s peer %s allowed-ips %s",
		s.config.WireGuardInterface,
		config.PublicKey,
		strings.Join(config.AllowedIPs, ","))

	if config.PresharedKey != nil && *config.PresharedKey != "" {
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

func (s *SSHNodeInteractor) validatePeerConfig(config node.PeerWireGuardConfig) error {
	if !crypto.IsValidWireGuardKey(config.PublicKey) {
		return errors.NewPeerError(errors.ErrCodePeerValidation, "invalid WireGuard public key format", false, nil)
	}

	if len(config.AllowedIPs) == 0 {
		return errors.NewPeerError(errors.ErrCodePeerValidation, "allowed IPs cannot be empty", false, nil)
	}

	for _, ip := range config.AllowedIPs {
		if ip == "" {
			return errors.NewPeerError(errors.ErrCodePeerValidation, "allowed IP cannot be empty", false, nil)
		}
	}

	if config.PresharedKey != nil && *config.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*config.PresharedKey) {
			return errors.NewPeerError(errors.ErrCodePeerValidation, "invalid WireGuard preshared key format", false, nil)
		}
	}

	return nil
}

func (s *SSHNodeInteractor) validatePeerAddition(ctx context.Context, nodeHost, publicKey string) error {
	peers, err := s.uninstrumentedListPeers(ctx, nodeHost)
	if err != nil {
		return fmt.Errorf("failed to list peers for validation: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			return nil
		}
	}

	return errors.NewPeerError(errors.ErrCodePeerNotFound, "peer not found after addition", false, nil).
		WithMetadata("host", nodeHost).
		WithMetadata("public_key", publicKey)
}

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

	return nil
}

func (s *SSHNodeInteractor) peerNeedsUpdate(current *node.WireGuardPeerStatus, desired node.PeerWireGuardConfig) bool {
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

	if desired.Endpoint != nil {
		if current.Endpoint == nil || *current.Endpoint != *desired.Endpoint {
			return true
		}
	} else if current.Endpoint != nil {
		return true
	}

	return false
}

func (s *SSHNodeInteractor) parseWireGuardStatus(output string, status *node.WireGuardStatus) error {
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

func (s *SSHNodeInteractor) parseWireGuardPeers(output string) ([]*node.WireGuardPeerStatus, error) {
	var peers []*node.WireGuardPeerStatus
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		peer := &node.WireGuardPeerStatus{
			PublicKey: parts[0],
		}

		get := func(i int) (string, bool) {
			if i < len(parts) {
				return parts[i], true
			}
			return "", false
		}

		if v, ok := get(2); ok && v != "(none)" {
			ep := v
			peer.Endpoint = &ep
		}

		if v, ok := get(3); ok {
			peer.AllowedIPs = strings.Split(v, ",")
		} else {
			peer.AllowedIPs = []string{}
		}

		if v, ok := get(4); ok && v != "0" {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				tm := time.Unix(ts, 0)
				peer.LastHandshake = &tm
			}
		}

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

		if v, ok := get(7); ok {
			if ka, err := strconv.Atoi(v); err == nil {
				peer.PersistentKeepalive = ka
			}
		}

		peers = append(peers, peer)
	}

	return peers, nil
}
