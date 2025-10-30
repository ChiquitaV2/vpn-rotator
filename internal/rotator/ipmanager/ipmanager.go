package ipmanager

import (
	"context"
	"fmt"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"log/slog"
	"net"
)

// IPManager defines the interface for IP address management
type IPManager interface {
	// Subnet management for nodes
	AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error)
	ReleaseNodeSubnet(ctx context.Context, nodeID string) error

	// Client IP allocation within node subnets
	AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error)
	ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error

	// Query operations
	GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error)
	GetAvailableIPCount(ctx context.Context, nodeID string) (int, error)
	GetNodeSubnet(ctx context.Context, nodeID string) (*NodeSubnetInfo, error)
}

// ipManager implements the IPManager interface
type ipManager struct {
	store  db.Store
	logger *slog.Logger

	// Base network configuration
	baseNetwork *net.IPNet // 10.8.0.0/16
	subnetMask  int        // /24 for node subnets
}

// DefaultConfig returns default IP manager configuration
func DefaultConfig() *Config {
	return &Config{
		BaseNetwork: "10.8.0.0/16",
		SubnetMask:  24,
	}
}

// NewIPManager creates a new IP manager instance
func NewIPManager(store db.Store, logger *slog.Logger, config *Config) (IPManager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Parse base network
	_, baseNet, err := net.ParseCIDR(config.BaseNetwork)
	if err != nil {
		return nil, fmt.Errorf("invalid base network %s: %w", config.BaseNetwork, err)
	}

	// Validate subnet mask
	if config.SubnetMask < 16 || config.SubnetMask > 30 {
		return nil, fmt.Errorf("invalid subnet mask %d: must be between 16 and 30", config.SubnetMask)
	}

	return &ipManager{
		store:       store,
		logger:      logger,
		baseNetwork: baseNet,
		subnetMask:  config.SubnetMask,
	}, nil
}

// AllocateNodeSubnet allocates a unique subnet for a node
func (m *ipManager) AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error) {
	// Check if node already has a subnet
	existing, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err == nil {
		// Node already has a subnet, parse and return it
		_, subnet, parseErr := net.ParseCIDR(existing.SubnetCidr)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid existing subnet CIDR %s for node %s: %w", existing.SubnetCidr, nodeID, parseErr)
		}
		return subnet, nil
	}

	// Get all used subnet CIDRs to find available one
	usedCIDRs, err := m.store.GetUsedSubnetCIDRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get used subnet CIDRs: %w", err)
	}

	// Find next available subnet
	subnet, err := m.findNextAvailableSubnet(usedCIDRs)
	if err != nil {
		return nil, fmt.Errorf("failed to find available subnet: %w", err)
	}

	// Calculate gateway and range IPs
	gatewayIP, rangeStart, rangeEnd := m.calculateSubnetIPs(subnet)

	// Store subnet allocation in database
	_, err = m.store.CreateNodeSubnet(ctx, db.CreateNodeSubnetParams{
		NodeID:       nodeID,
		SubnetCidr:   subnet.String(),
		GatewayIp:    gatewayIP.String(),
		IpRangeStart: rangeStart.String(),
		IpRangeEnd:   rangeEnd.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store node subnet: %w", err)
	}

	m.logger.Info("allocated subnet for node", slog.String("node_id", nodeID), slog.String("subnet", subnet.String()))
	return subnet, nil
}

// ReleaseNodeSubnet releases a node's subnet allocation
func (m *ipManager) ReleaseNodeSubnet(ctx context.Context, nodeID string) error {
	// Check if subnet exists
	_, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		// Subnet doesn't exist, nothing to release
		return nil
	}

	// Delete the subnet allocation
	err = m.store.DeleteNodeSubnet(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete node subnet: %w", err)
	}

	m.logger.Info("released subnet for node", slog.String("node_id", nodeID))
	return nil
}

// AllocateClientIP allocates the next available IP address within a node's subnet
func (m *ipManager) AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error) {
	// Get node subnet information
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s has no allocated subnet: %w", nodeID, err)
	}

	// Parse subnet range
	rangeStart := net.ParseIP(nodeSubnet.IpRangeStart)
	rangeEnd := net.ParseIP(nodeSubnet.IpRangeEnd)
	if rangeStart == nil || rangeEnd == nil {
		return nil, fmt.Errorf("invalid IP range for node %s: start=%s, end=%s", nodeID, nodeSubnet.IpRangeStart, nodeSubnet.IpRangeEnd)
	}

	// Get currently allocated IPs for this node
	allocatedIPs, err := m.store.GetAllocatedIPsByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs for node %s: %w", nodeID, err)
	}

	// Convert allocated IPs to a map for fast lookup
	allocatedMap := make(map[string]bool)
	for _, ipStr := range allocatedIPs {
		allocatedMap[ipStr] = true
	}

	// Find next available IP in range
	nextIP, err := m.findNextAvailableIP(rangeStart, rangeEnd, allocatedMap)
	if err != nil {
		return nil, fmt.Errorf("failed to find available IP in node %s subnet: %w", nodeID, err)
	}

	return nextIP, nil
}

// ReleaseClientIP releases a client IP address (handled automatically by peer deletion)
func (m *ipManager) ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error {
	// IP is automatically released when peer is deleted from database
	// This method is provided for interface completeness and potential future use
	m.logger.Debug("client IP released", slog.String("node_id", nodeID), slog.String("ip", ip.String()))
	return nil
}

// GetAllocatedIPs returns all allocated IP addresses for a node
func (m *ipManager) GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error) {
	allocatedIPStrs, err := m.store.GetAllocatedIPsByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs for node %s: %w", nodeID, err)
	}

	ips := make([]net.IP, len(allocatedIPStrs))
	for i, ipStr := range allocatedIPStrs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address %s for node %s", ipStr, nodeID)
		}
		ips[i] = ip
	}

	return ips, nil
}

// GetAvailableIPCount returns the number of available IP addresses in a node's subnet
func (m *ipManager) GetAvailableIPCount(ctx context.Context, nodeID string) (int, error) {
	// Get node subnet information
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("node %s has no allocated subnet: %w", nodeID, err)
	}

	// Parse subnet range
	rangeStart := net.ParseIP(nodeSubnet.IpRangeStart)
	rangeEnd := net.ParseIP(nodeSubnet.IpRangeEnd)
	if rangeStart == nil || rangeEnd == nil {
		return 0, fmt.Errorf("invalid IP range for node %s", nodeID)
	}

	// Calculate total IPs in range
	totalIPs := m.calculateIPRangeSize(rangeStart, rangeEnd)

	// Get allocated count
	allocatedCount, err := m.store.CountPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count allocated IPs for node %s: %w", nodeID, err)
	}

	availableCount := totalIPs - int(allocatedCount)
	if availableCount < 0 {
		availableCount = 0
	}

	return availableCount, nil
}

// GetNodeSubnet returns subnet information for a node
func (m *ipManager) GetNodeSubnet(ctx context.Context, nodeID string) (*NodeSubnetInfo, error) {
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet for node %s: %w", nodeID, err)
	}

	gatewayIP := net.ParseIP(nodeSubnet.GatewayIp)
	rangeStart := net.ParseIP(nodeSubnet.IpRangeStart)
	rangeEnd := net.ParseIP(nodeSubnet.IpRangeEnd)

	if gatewayIP == nil || rangeStart == nil || rangeEnd == nil {
		return nil, fmt.Errorf("invalid IP addresses in subnet for node %s", nodeID)
	}

	return &NodeSubnetInfo{
		NodeID:     nodeSubnet.NodeID,
		SubnetCIDR: nodeSubnet.SubnetCidr,
		GatewayIP:  gatewayIP,
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
	}, nil
}
