package ip

import (
	"fmt"
	"net"
	"strings"

	sharedErrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// ValidateIPv4Address validates an IPv4 address string
func ValidateIPv4Address(ipStr string) error {
	if ipStr == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "IP address cannot be empty", false, nil).WithMetadata("ip_address", ipStr)
	}

	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "invalid IP address format", false, nil).WithMetadata("ip_address", ipStr)
	}

	return nil
}

// ValidateCIDR validates a CIDR notation string
func ValidateCIDR(cidr string) error {
	if cidr == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "CIDR cannot be empty", false, nil).WithMetadata("cidr", cidr)
	}

	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("invalid CIDR notation: %v", err), false, err).WithMetadata("cidr", cidr)
	}

	return nil
}

// ValidateNodeID validates a node ID
func ValidateNodeID(nodeID string) error {
	if nodeID == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "node ID cannot be empty", false, nil).WithMetadata("node_id", nodeID)
	}

	// Additional validation: node IDs should be reasonable length
	if len(nodeID) > 255 {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "node ID too long (max 255 characters)", false, nil).WithMetadata("node_id", nodeID)
	}

	return nil
}

// ValidateSubnetAlignment checks if a subnet is properly aligned
func ValidateSubnetAlignment(network *net.IPNet) error {
	if network == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "network cannot be nil", false, nil)
	}

	// Check if the IP is the network address (all host bits are zero)
	networkIP := network.IP.Mask(network.Mask)
	if !network.IP.Equal(networkIP) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("subnet %s is not properly aligned to its mask", network.String()), false, nil).WithMetadata("subnet", network.String())
	}

	return nil
}

// ValidateSubnetInBaseNetwork checks if a subnet is within the base network
func ValidateSubnetInBaseNetwork(subnet, baseNetwork *net.IPNet) error {
	if subnet == nil || baseNetwork == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "subnet and base network cannot be nil", false, nil)
	}

	if !baseNetwork.Contains(subnet.IP) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("subnet %s is not within base network %s", subnet.String(), baseNetwork.String()), false, nil).WithMetadata("subnet", subnet.String()).WithMetadata("base_network", baseNetwork.String())
	}

	return nil
}

// ValidateIPInSubnet checks if an IP is within a subnet
func ValidateIPInSubnet(ip net.IP, subnet *net.IPNet) error {
	if ip == nil || subnet == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "IP and subnet cannot be nil", false, nil)
	}

	if !subnet.Contains(ip) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, fmt.Sprintf("IP %s is not in subnet %s", ip.String(), subnet.String()), false, nil).WithMetadata("ip", ip.String()).WithMetadata("subnet", subnet.String())
	}

	return nil
}

// ValidateCapacity checks if there's sufficient capacity
func ValidateCapacity(available, required int) error {
	if available < required {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeCapacityExceeded, fmt.Sprintf("insufficient capacity: available=%d, required=%d", available, required), false, nil).WithMetadata("available", available).WithMetadata("required", required)
	}
	return nil
}

// ValidateNotNearCapacity checks if capacity is below threshold
func ValidateNotNearCapacity(allocated, total int, threshold float64) error {
	if total == 0 {
		return nil
	}

	usage := float64(allocated) / float64(total)
	if usage >= threshold {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeCapacityExceeded, fmt.Sprintf("capacity threshold %.2f%% exceeded (current: %.2f%%)", threshold*100, usage*100), false, nil).WithMetadata("allocated", allocated).WithMetadata("total", total).WithMetadata("threshold", threshold).WithMetadata("usage", usage)
	}

	return nil
}

// ValidateWireGuardPublicKey validates a WireGuard public key format
func ValidateWireGuardPublicKey(publicKey string) error {
	if publicKey == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodePeerValidation, "public key cannot be empty", false, nil).WithMetadata("public_key", publicKey)
	}

	publicKey = strings.TrimSpace(publicKey)
	if !crypto.IsValidWireGuardKey(publicKey) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodePeerValidation, "invalid WireGuard public key format", false, nil).WithMetadata("public_key", publicKey)
	}

	return nil
}
