package nodemanager

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// validatePeerConfig validates a peer configuration
func (m *Manager) validatePeerConfig(peer *PeerConfig) error {
	if peer == nil {
		return fmt.Errorf("peer config cannot be nil")
	}

	if !crypto.IsValidWireGuardKey(peer.PublicKey) {
		return fmt.Errorf("invalid public key")
	}

	if err := m.validateIPAddress(peer.AllocatedIP); err != nil {
		return fmt.Errorf("invalid allocated IP: %w", err)
	}

	if peer.PresharedKey != nil && *peer.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*peer.PresharedKey) {
			return fmt.Errorf("invalid preshared key")
		}
	}

	return nil
}

// validateIPAddress validates an IP address format
func (m *Manager) validateIPAddress(ip string) error {
	if ip == "" {
		return fmt.Errorf("IP address cannot be empty")
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address format: %s", ip)
	}

	if parsedIP.To4() == nil {
		return fmt.Errorf("IP address must be IPv4: %s", ip)
	}

	return nil
}

// parseWireGuardDump parses the output of 'wg show wg0 dump'
func (m *Manager) parseWireGuardDump(output string) ([]*PeerInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var peers []*PeerInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// WireGuard dump format: interface_name public_key preshared_key endpoint allowed_ips latest_handshake transfer_rx transfer_tx persistent_keepalive
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		// Skip interface line (first line)
		if fields[0] == "wg0" && len(fields) == 3 {
			continue
		}

		publicKey := fields[0]
		allowedIPs := fields[3]
		latestHandshake := fields[4]
		transferRx := fields[5]
		transferTx := fields[6]

		peer := &PeerInfo{
			PublicKey: publicKey,
		}

		// Parse allowed IPs to get allocated IP
		if allowedIPs != "(none)" {
			// Extract IP from CIDR (e.g., "10.8.1.5/32" -> "10.8.1.5")
			if idx := strings.Index(allowedIPs, "/"); idx > 0 {
				peer.AllocatedIP = allowedIPs[:idx]
			} else {
				peer.AllocatedIP = allowedIPs
			}
		}

		// Parse latest handshake
		if latestHandshake != "0" {
			if timestamp, err := strconv.ParseInt(latestHandshake, 10, 64); err == nil {
				handshakeTime := time.Unix(timestamp, 0)
				peer.LastHandshakeAt = &handshakeTime
			}
		}

		// Parse transfer statistics
		if rx, err := strconv.ParseInt(transferRx, 10, 64); err == nil {
			peer.TransferRx = rx
		}
		if tx, err := strconv.ParseInt(transferTx, 10, 64); err == nil {
			peer.TransferTx = tx
		}

		peers = append(peers, peer)
	}

	return peers, nil
}
