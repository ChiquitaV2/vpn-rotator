package nodemanager

import (
	"fmt"
	"net"
)

// validatePeerConfig validates a peer configuration
func (m *Manager) validatePeerConfig(peer *PeerConfig) error {
	if peer == nil {
		return fmt.Errorf("peer config cannot be nil")
	}

	if err := m.validateWireGuardPublicKey(peer.PublicKey); err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	if err := m.validateIPAddress(peer.AllocatedIP); err != nil {
		return fmt.Errorf("invalid allocated IP: %w", err)
	}

	if peer.PresharedKey != nil && *peer.PresharedKey != "" {
		if err := m.validateWireGuardPublicKey(*peer.PresharedKey); err != nil {
			return fmt.Errorf("invalid preshared key: %w", err)
		}
	}

	return nil
}

// validateWireGuardPublicKey validates a WireGuard public key format
func (m *Manager) validateWireGuardPublicKey(key string) error {
	if key == "" {
		return fmt.Errorf("public key cannot be empty")
	}

	if len(key) != 44 {
		return fmt.Errorf("public key must be 44 characters long, got %d", len(key))
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
