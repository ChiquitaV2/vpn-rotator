package ipmanager

import (
	"context"
	"fmt"
	"net"
	"sort"
)

// GetAllNodeSubnets returns subnet information for all nodes
func (m *ipManager) GetAllNodeSubnets(ctx context.Context) ([]NodeSubnetInfo, error) {
	subnets, err := m.store.GetAllNodeSubnets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all node subnets: %w", err)
	}

	result := make([]NodeSubnetInfo, len(subnets))
	for i, subnet := range subnets {
		gatewayIP := net.ParseIP(subnet.GatewayIp)
		rangeStart := net.ParseIP(subnet.IpRangeStart)
		rangeEnd := net.ParseIP(subnet.IpRangeEnd)

		if gatewayIP == nil || rangeStart == nil || rangeEnd == nil {
			return nil, fmt.Errorf("invalid IP addresses in subnet for node %s", subnet.NodeID)
		}

		result[i] = NodeSubnetInfo{
			NodeID:     subnet.NodeID,
			SubnetCIDR: subnet.SubnetCidr,
			GatewayIP:  gatewayIP,
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
		}
	}

	return result, nil
}

// GetAllSubnetStats returns usage statistics for all node subnets
func (m *ipManager) GetAllSubnetStats(ctx context.Context) ([]SubnetStats, error) {
	subnets, err := m.GetAllNodeSubnets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node subnets: %w", err)
	}

	stats := make([]SubnetStats, len(subnets))
	for i, subnet := range subnets {
		// Calculate total IPs in range
		totalIPs := m.calculateIPRangeSize(subnet.RangeStart, subnet.RangeEnd)

		// Get allocated count
		allocatedCount, err := m.store.CountPeersByNode(ctx, subnet.NodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to count peers for node %s: %w", subnet.NodeID, err)
		}

		availableIPs := totalIPs - int(allocatedCount)
		if availableIPs < 0 {
			availableIPs = 0
		}

		var utilizationPct float64
		if totalIPs > 0 {
			utilizationPct = float64(allocatedCount) / float64(totalIPs) * 100
		}

		stats[i] = SubnetStats{
			NodeID:         subnet.NodeID,
			SubnetCIDR:     subnet.SubnetCIDR,
			TotalIPs:       totalIPs,
			AllocatedIPs:   int(allocatedCount),
			AvailableIPs:   availableIPs,
			UtilizationPct: utilizationPct,
		}
	}

	return stats, nil
}

// FindLeastUtilizedNode finds the node with the lowest subnet utilization
func (m *ipManager) FindLeastUtilizedNode(ctx context.Context) (string, error) {
	stats, err := m.GetAllSubnetStats(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get subnet stats: %w", err)
	}

	if len(stats) == 0 {
		return "", fmt.Errorf("no nodes with allocated subnets found")
	}

	// Sort by utilization percentage (ascending)
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].UtilizationPct < stats[j].UtilizationPct
	})

	// Return the node with lowest utilization that has available IPs
	for _, stat := range stats {
		if stat.AvailableIPs > 0 {
			return stat.NodeID, nil
		}
	}

	return "", fmt.Errorf("no nodes with available IP capacity found")
}

// GetIPRangeForNode returns the IP range (start, end) for a node's subnet
func (m *ipManager) GetIPRangeForNode(ctx context.Context, nodeID string) (start, end net.IP, err error) {
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get subnet for node %s: %w", nodeID, err)
	}

	start = net.ParseIP(nodeSubnet.IpRangeStart)
	end = net.ParseIP(nodeSubnet.IpRangeEnd)

	if start == nil || end == nil {
		return nil, nil, fmt.Errorf("invalid IP range for node %s", nodeID)
	}

	return start, end, nil
}

// IsIPInNodeRange checks if an IP address is within a node's allocated range
func (m *ipManager) IsIPInNodeRange(ctx context.Context, nodeID string, ip net.IP) (bool, error) {
	start, end, err := m.GetIPRangeForNode(ctx, nodeID)
	if err != nil {
		return false, err
	}

	ipUint := ipToUint32(ip)
	startUint := ipToUint32(start)
	endUint := ipToUint32(end)

	return ipUint >= startUint && ipUint <= endUint, nil
}

// GetNextSequentialIP returns the next IP that would be allocated in a node's subnet
func (m *ipManager) GetNextSequentialIP(ctx context.Context, nodeID string) (net.IP, error) {
	// Get node subnet information
	nodeSubnet, err := m.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s has no allocated subnet: %w", nodeID, err)
	}

	// Parse subnet range
	rangeStart := net.ParseIP(nodeSubnet.IpRangeStart)
	rangeEnd := net.ParseIP(nodeSubnet.IpRangeEnd)
	if rangeStart == nil || rangeEnd == nil {
		return nil, fmt.Errorf("invalid IP range for node %s", nodeID)
	}

	// Get currently allocated IPs for this node
	allocatedIPs, err := m.store.GetAllocatedIPsByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs for node %s: %w", nodeID, err)
	}

	// Convert to map for fast lookup
	allocatedMap := make(map[string]bool)
	for _, ipStr := range allocatedIPs {
		allocatedMap[ipStr] = true
	}

	// Find next available IP
	return m.findNextAvailableIP(rangeStart, rangeEnd, allocatedMap)
}

// CompactIPAllocation suggests IP addresses that could be freed to optimize allocation
// This is useful for defragmentation when many IPs have been allocated and freed
func (m *ipManager) CompactIPAllocation(ctx context.Context, nodeID string) ([]net.IP, error) {
	// Get allocated IPs
	allocatedIPStrs, err := m.store.GetAllocatedIPsByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs: %w", err)
	}

	if len(allocatedIPStrs) == 0 {
		return []net.IP{}, nil
	}

	// Convert to IPs and sort
	allocatedIPs := make([]net.IP, len(allocatedIPStrs))
	for i, ipStr := range allocatedIPStrs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address %s", ipStr)
		}
		allocatedIPs[i] = ip
	}

	// Sort IPs by their numeric value
	sort.Slice(allocatedIPs, func(i, j int) bool {
		return ipToUint32(allocatedIPs[i]) < ipToUint32(allocatedIPs[j])
	})

	// Find gaps in allocation that could be compacted
	var suggestions []net.IP
	start, _, err := m.GetIPRangeForNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	startUint := ipToUint32(start)

	// Suggest moving IPs to fill gaps from the beginning
	expectedNext := startUint
	for _, ip := range allocatedIPs {
		currentUint := ipToUint32(ip)
		if currentUint != expectedNext {
			// There's a gap, suggest moving this IP to fill it
			suggestions = append(suggestions, uint32ToIP(expectedNext))
		}
		expectedNext++
	}

	return suggestions, nil
}

// GetSubnetCapacityInfo returns detailed capacity information for a node
func (m *ipManager) GetSubnetCapacityInfo(ctx context.Context, nodeID string) (*SubnetCapacityInfo, error) {
	// Get subnet info
	subnetInfo, err := m.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node subnet: %w", err)
	}

	// Calculate capacity
	totalCapacity := m.calculateIPRangeSize(subnetInfo.RangeStart, subnetInfo.RangeEnd)

	// Get used capacity
	usedCount, err := m.store.CountPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to count peers: %w", err)
	}

	usedCapacity := int(usedCount)
	availableCapacity := totalCapacity - usedCapacity
	if availableCapacity < 0 {
		availableCapacity = 0
	}

	var utilizationPct float64
	if totalCapacity > 0 {
		utilizationPct = float64(usedCapacity) / float64(totalCapacity) * 100
	}

	// Get next available IP
	var nextIP net.IP
	if availableCapacity > 0 {
		nextIP, _ = m.GetNextSequentialIP(ctx, nodeID) // Ignore error if no IPs available
	}

	return &SubnetCapacityInfo{
		NodeID:            nodeID,
		SubnetCIDR:        subnetInfo.SubnetCIDR,
		TotalCapacity:     totalCapacity,
		UsedCapacity:      usedCapacity,
		AvailableCapacity: availableCapacity,
		UtilizationPct:    utilizationPct,
		NextAvailableIP:   nextIP,
		IsNearCapacity:    utilizationPct >= 90.0,
		IsFull:            availableCapacity == 0,
	}, nil
}
