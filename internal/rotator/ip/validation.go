package ip

import (
	"fmt"
	"net"
	"strings"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// ValidateIPv4Address validates an IPv4 address string
func ValidateIPv4Address(ipStr string) error {
	if ipStr == "" {
		return &ValidationError{
			Field: "ip_address",
			Value: ipStr,
			Err:   ErrInvalidIPAddress,
		}
	}

	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return &ValidationError{
			Field: "ip_address",
			Value: ipStr,
			Err:   ErrInvalidIPAddress,
		}
	}

	return nil
}

// ValidateCIDR validates a CIDR notation string
func ValidateCIDR(cidr string) error {
	if cidr == "" {
		return &ValidationError{
			Field: "cidr",
			Value: cidr,
			Err:   ErrInvalidCIDR,
		}
	}

	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return &ValidationError{
			Field: "cidr",
			Value: cidr,
			Err:   fmt.Errorf("%w: %v", ErrInvalidCIDR, err),
		}
	}

	return nil
}

// ValidateNodeID validates a node ID
func ValidateNodeID(nodeID string) error {
	if nodeID == "" {
		return &ValidationError{
			Field: "node_id",
			Value: nodeID,
			Err:   ErrInvalidNodeID,
		}
	}

	// Additional validation: node IDs should be reasonable length
	if len(nodeID) > 255 {
		return &ValidationError{
			Field: "node_id",
			Value: nodeID,
			Err:   fmt.Errorf("node ID too long (max 255 characters)"),
		}
	}

	return nil
}

// ValidateSubnetAlignment checks if a subnet is properly aligned
func ValidateSubnetAlignment(network *net.IPNet) error {
	if network == nil {
		return fmt.Errorf("network cannot be nil")
	}

	// Check if the IP is the network address (all host bits are zero)
	networkIP := network.IP.Mask(network.Mask)
	if !network.IP.Equal(networkIP) {
		return fmt.Errorf("subnet %s is not properly aligned to its mask", network.String())
	}

	return nil
}

// ValidateSubnetInBaseNetwork checks if a subnet is within the base network
func ValidateSubnetInBaseNetwork(subnet, baseNetwork *net.IPNet) error {
	if subnet == nil || baseNetwork == nil {
		return fmt.Errorf("subnet and base network cannot be nil")
	}

	if !baseNetwork.Contains(subnet.IP) {
		return fmt.Errorf("subnet %s is not within base network %s", subnet.String(), baseNetwork.String())
	}

	return nil
}

// ValidateIPInSubnet checks if an IP is within a subnet
func ValidateIPInSubnet(ip net.IP, subnet *net.IPNet) error {
	if ip == nil || subnet == nil {
		return fmt.Errorf("IP and subnet cannot be nil")
	}

	if !subnet.Contains(ip) {
		return fmt.Errorf("IP %s is not in subnet %s", ip.String(), subnet.String())
	}

	return nil
}

// ValidateCapacity checks if there's sufficient capacity
func ValidateCapacity(available, required int) error {
	if available < required {
		return &CapacityError{
			Available: available,
			Required:  required,
			Err:       fmt.Errorf("insufficient capacity"),
		}
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
		return &CapacityError{
			Available:       total - allocated,
			Required:        1,
			ThresholdFailed: true,
			Err:             fmt.Errorf("capacity threshold %.2f%% exceeded (current: %.2f%%)", threshold*100, usage*100),
		}
	}

	return nil
}

// ValidateWireGuardPublicKey validates a WireGuard public key format
func ValidateWireGuardPublicKey(publicKey string) error {
	if publicKey == "" {
		return &ValidationError{
			Field: "public_key",
			Value: publicKey,
			Err:   fmt.Errorf("public key cannot be empty"),
		}
	}

	publicKey = strings.TrimSpace(publicKey)
	if !crypto.IsValidWireGuardKey(publicKey) {
		return &ValidationError{
			Field: "public_key",
			Value: publicKey,
			Err:   fmt.Errorf("invalid WireGuard public key format"),
		}
	}

	return nil
}
