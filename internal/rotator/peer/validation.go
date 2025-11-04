package peer

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// ValidatePublicKey validates a WireGuard public key
func ValidatePublicKey(publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return NewValidationError("public_key", "cannot be empty", publicKey)
	}

	if !crypto.IsValidWireGuardKey(publicKey) {
		return NewValidationError("public_key", "invalid WireGuard key format", publicKey)
	}

	return nil
}

// ValidatePresharedKey validates a WireGuard preshared key
func ValidatePresharedKey(presharedKey string) error {
	if strings.TrimSpace(presharedKey) == "" {
		return nil // Empty is valid (optional)
	}

	if !crypto.IsValidWireGuardKey(presharedKey) {
		return NewValidationError("preshared_key", "invalid WireGuard key format", presharedKey)
	}

	return nil
}

var (
	// Cache for validated IPs to avoid repeated parsing
	ipCache     = sync.Map{}
	cacheConfig = DefaultConfig()
)

// ClearIPCache clears the IP validation cache
func ClearIPCache() {
	ipCache = sync.Map{}
}

// GetCacheSize returns the approximate size of the IP cache
func GetCacheSize() int {
	count := 0
	ipCache.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// ValidateIPAddress validates an IPv4 address with caching
func ValidateIPAddress(ipAddr string) error {
	if ipAddr == "" {
		return NewValidationError("ip_address", "cannot be empty", ipAddr)
	}

	// Check cache first
	if _, exists := ipCache.Load(ipAddr); exists {
		return nil
	}

	parsedIP := net.ParseIP(ipAddr)
	if parsedIP == nil {
		return NewValidationError("ip_address", "invalid IP address format", ipAddr)
	}

	// Ensure it's IPv4
	if parsedIP.To4() == nil {
		return NewValidationError("ip_address", "must be IPv4", ipAddr)
	}

	// Cache valid IP if caching is enabled and under limit
	if cacheConfig.EnableIPCache && GetCacheSize() < cacheConfig.MaxCacheSize {
		ipCache.Store(ipAddr, struct{}{})
	}
	return nil
}

// ValidateNodeID validates a node ID
func ValidateNodeID(nodeID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return NewValidationError("node_id", "cannot be empty", nodeID)
	}

	return nil
}

// ValidateStatus validates a peer status
func ValidateStatus(status Status) error {
	if !status.IsValid() {
		return NewValidationError("status", "invalid status", status)
	}

	return nil
}

// ValidatePeer validates a complete peer entity
func ValidatePeer(p *Peer) error {
	if p == nil {
		return fmt.Errorf("peer cannot be nil")
	}

	if err := ValidateNodeID(p.NodeID); err != nil {
		return err
	}

	if err := ValidatePublicKey(p.PublicKey); err != nil {
		return err
	}

	if err := ValidateIPAddress(p.AllocatedIP); err != nil {
		return err
	}

	if err := ValidateStatus(p.Status); err != nil {
		return err
	}

	if p.PresharedKey != nil {
		if err := ValidatePresharedKey(*p.PresharedKey); err != nil {
			return err
		}
	}

	return nil
}

// ValidateCreateRequest validates a create request
func ValidateCreateRequest(req *CreateRequest) error {
	if req == nil {
		return fmt.Errorf("create request cannot be nil")
	}

	if err := ValidateNodeID(req.NodeID); err != nil {
		return err
	}

	if err := ValidatePublicKey(req.PublicKey); err != nil {
		return err
	}

	if err := ValidateIPAddress(req.AllocatedIP); err != nil {
		return err
	}

	if req.PresharedKey != nil {
		if err := ValidatePresharedKey(*req.PresharedKey); err != nil {
			return err
		}
	}

	return nil
}
