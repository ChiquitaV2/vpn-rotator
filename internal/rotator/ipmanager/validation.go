package ipmanager

import (
	"context"
	"fmt"
	"net"
	"regexp"
)

// ValidatePublicKey validates a WireGuard public key format
func ValidatePublicKey(publicKey string) error {
	// WireGuard public keys are 44 characters, base64 encoded
	if len(publicKey) != 44 {
		return fmt.Errorf("invalid public key length: expected 44 characters, got %d", len(publicKey))
	}

	// Check if it's valid base64 (WireGuard keys end with '=')
	if publicKey[43] != '=' {
		return fmt.Errorf("invalid public key format: must end with '='")
	}

	// Basic regex check for base64 characters
	matched, err := regexp.MatchString(`^[A-Za-z0-9+/]{43}=$`, publicKey)
	if err != nil {
		return fmt.Errorf("failed to validate public key format: %w", err)
	}

	if !matched {
		return fmt.Errorf("invalid public key format: contains invalid characters")
	}

	return nil
}

// ValidateIPAddress validates that an IP address is a valid IPv4 address
func ValidateIPAddress(ip string) error {
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

// CheckIPConflict checks if an IP address is already allocated across all nodes
func (m *ipManager) CheckIPConflict(ctx context.Context, ip string) error {
	if err := ValidateIPAddress(ip); err != nil {
		return fmt.Errorf("invalid IP address: %w", err)
	}

	exists, err := m.store.CheckIPConflict(ctx, ip)
	if err != nil {
		return fmt.Errorf("failed to check IP conflict: %w", err)
	}

	if exists > 0 {
		return fmt.Errorf("IP address %s is already allocated", ip)
	}

	return nil
}

// CheckPublicKeyConflict checks if a public key is already in use
func (m *ipManager) CheckPublicKeyConflict(ctx context.Context, publicKey string) error {
	if err := ValidatePublicKey(publicKey); err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	exists, err := m.store.CheckPublicKeyConflict(ctx, publicKey)
	if err != nil {
		return fmt.Errorf("failed to check public key conflict: %w", err)
	}

	if exists > 0 {
		return fmt.Errorf("public key is already in use")
	}

	return nil
}

// ValidateNodeSubnetCapacity checks if a node has capacity for new peers
func (m *ipManager) ValidateNodeSubnetCapacity(ctx context.Context, nodeID string) error {
	availableCount, err := m.GetAvailableIPCount(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check node capacity: %w", err)
	}

	if availableCount <= 0 {
		return fmt.Errorf("node %s has no available IP addresses", nodeID)
	}

	return nil
}

// GetSubnetUtilization returns the utilization percentage of a node's subnet
func (m *ipManager) GetSubnetUtilization(ctx context.Context, nodeID string) (float64, error) {
	// Get node subnet information
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get node subnet: %w", err)
	}

	// Parse subnet range
	rangeStart := net.ParseIP(nodeSubnet.IpRangeStart)
	rangeEnd := net.ParseIP(nodeSubnet.IpRangeEnd)
	if rangeStart == nil || rangeEnd == nil {
		return 0, fmt.Errorf("invalid IP range for node %s", nodeID)
	}

	// Calculate total capacity
	totalCapacity := m.calculateIPRangeSize(rangeStart, rangeEnd)

	// Get allocated count
	allocatedCount, err := m.store.CountPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count allocated IPs: %w", err)
	}

	if totalCapacity == 0 {
		return 0, nil
	}

	utilization := float64(allocatedCount) / float64(totalCapacity) * 100
	return utilization, nil
}

// IsSubnetExhausted checks if a node's subnet is at or near capacity
func (m *ipManager) IsSubnetExhausted(ctx context.Context, nodeID string, threshold float64) (bool, error) {
	utilization, err := m.GetSubnetUtilization(ctx, nodeID)
	if err != nil {
		return false, err
	}

	return utilization >= threshold, nil
}

// ValidateSubnetAllocation validates that a subnet allocation is valid
func (m *ipManager) ValidateSubnetAllocation(subnet *net.IPNet) error {
	// Check if subnet is within base network
	if !m.baseNetwork.Contains(subnet.IP) {
		return fmt.Errorf("subnet %s is not within base network %s", subnet.String(), m.baseNetwork.String())
	}

	// Check subnet mask
	ones, _ := subnet.Mask.Size()
	if ones != m.subnetMask {
		return fmt.Errorf("subnet mask /%d does not match expected /%d", ones, m.subnetMask)
	}

	// Ensure subnet is properly aligned (e.g., 10.8.X.0/24, not 10.8.X.5/24)
	subnetIP := subnet.IP.To4()
	if subnetIP == nil {
		return fmt.Errorf("subnet IP is not IPv4")
	}

	// For /24 subnets, last octet should be 0
	if m.subnetMask == 24 && subnetIP[3] != 0 {
		return fmt.Errorf("subnet %s is not properly aligned for /%d", subnet.String(), m.subnetMask)
	}

	return nil
}
