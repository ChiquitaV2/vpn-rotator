package peermanager

import (
	"fmt"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
)

// validateIPAddress validates an IP address format
func validateIPAddress(ip string) error {
	if ip == "" {
		return fmt.Errorf("IP address cannot be empty")
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address format: %s", ip)
	}

	// Ensure it's IPv4
	if parsedIP.To4() == nil {
		return fmt.Errorf("IP address must be IPv4: %s", ip)
	}

	return nil
}

// isValidPeerStatus checks if a peer status is valid
func isValidPeerStatus(status PeerStatus) bool {
	switch status {
	case PeerStatusActive, PeerStatusDisconnected, PeerStatusRemoving:
		return true
	default:
		return false
	}
}

// generatePeerID generates a unique peer ID
func generatePeerID() string {
	return fmt.Sprintf("peer_%d", time.Now().UnixNano())
}

// dbPeerToPeerConfig converts a database peer to PeerConfig (package-level helper)
func dbPeerToPeerConfig(peer *db.Peer) *PeerConfig {
	config := &PeerConfig{
		ID:          peer.ID,
		NodeID:      peer.NodeID,
		PublicKey:   peer.PublicKey,
		AllocatedIP: peer.AllocatedIp,
		Status:      PeerStatus(peer.Status),
		CreatedAt:   peer.CreatedAt,
		UpdatedAt:   peer.UpdatedAt,
	}

	if peer.PresharedKey.Valid {
		config.PresharedKey = &peer.PresharedKey.String
	}

	return config
}
