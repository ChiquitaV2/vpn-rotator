package ipmanager

import (
	"fmt"
	"net"
	"strings"
)

// findNextAvailableSubnet finds the next available /24 subnet within the base network
func (m *ipManager) findNextAvailableSubnet(usedCIDRs []string) (*net.IPNet, error) {
	// Parse used subnets for quick lookup
	usedSubnets := make(map[string]bool)
	for _, cidr := range usedCIDRs {
		usedSubnets[cidr] = true
	}

	// Base network is 10.8.0.0/16, we allocate /24 subnets
	// This gives us 256 possible /24 subnets (10.8.0.0/24 through 10.8.255.0/24)
	baseIP := m.baseNetwork.IP.To4()
	if baseIP == nil {
		return nil, fmt.Errorf("base network is not IPv4")
	}

	// Start from 10.8.1.0/24 (skip 10.8.0.0/24 for management)
	for i := 1; i <= 255; i++ {
		// Construct subnet: 10.8.i.0/24
		subnetIP := net.IPv4(baseIP[0], baseIP[1], byte(i), 0)
		subnetCIDR := fmt.Sprintf("%s/%d", subnetIP.String(), m.subnetMask)

		// Check if this subnet is already used
		if !usedSubnets[subnetCIDR] {
			_, subnet, err := net.ParseCIDR(subnetCIDR)
			if err != nil {
				return nil, fmt.Errorf("failed to parse generated subnet %s: %w", subnetCIDR, err)
			}
			return subnet, nil
		}
	}

	return nil, fmt.Errorf("no available subnets in base network %s", m.baseNetwork.String())
}

// calculateSubnetIPs calculates gateway, range start, and range end IPs for a subnet
func (m *ipManager) calculateSubnetIPs(subnet *net.IPNet) (gateway, rangeStart, rangeEnd net.IP) {
	// For a /24 subnet like 10.8.1.0/24:
	// - Gateway: 10.8.1.1 (first usable IP)
	// - Range start: 10.8.1.2 (second usable IP)
	// - Range end: 10.8.1.254 (last usable IP, .255 is broadcast)

	subnetIP := subnet.IP.To4()
	if subnetIP == nil {
		return nil, nil, nil
	}

	// Gateway is first usable IP (.1)
	gateway = net.IPv4(subnetIP[0], subnetIP[1], subnetIP[2], 1)

	// Client range starts from .2
	rangeStart = net.IPv4(subnetIP[0], subnetIP[1], subnetIP[2], 2)

	// Client range ends at .254 (avoid broadcast .255)
	rangeEnd = net.IPv4(subnetIP[0], subnetIP[1], subnetIP[2], 254)

	return gateway, rangeStart, rangeEnd
}

// findNextAvailableIP finds the next available IP in the given range
func (m *ipManager) findNextAvailableIP(rangeStart, rangeEnd net.IP, allocatedMap map[string]bool) (net.IP, error) {
	start := ipToUint32(rangeStart)
	end := ipToUint32(rangeEnd)

	if start > end {
		return nil, fmt.Errorf("invalid IP range: start %s is after end %s", rangeStart.String(), rangeEnd.String())
	}

	// Sequential allocation: find first available IP
	for current := start; current <= end; current++ {
		ip := uint32ToIP(current)
		if !allocatedMap[ip.String()] {
			return ip, nil
		}
	}

	return nil, fmt.Errorf("no available IPs in range %s-%s", rangeStart.String(), rangeEnd.String())
}

// calculateIPRangeSize calculates the number of IPs in a range
func (m *ipManager) calculateIPRangeSize(rangeStart, rangeEnd net.IP) int {
	start := ipToUint32(rangeStart)
	end := ipToUint32(rangeEnd)

	if start > end {
		return 0
	}

	return int(end - start + 1)
}

// ipToUint32 converts an IPv4 address to uint32 for arithmetic operations
func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 + uint32(ip[1])<<16 + uint32(ip[2])<<8 + uint32(ip[3])
}

// uint32ToIP converts a uint32 back to IPv4 address
func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

// ValidateIPInSubnet validates that an IP address is within the node's allocated subnet
func (m *ipManager) ValidateIPInSubnet(ip net.IP, nodeSubnet *NodeSubnetInfo) error {
	_, subnet, err := net.ParseCIDR(nodeSubnet.SubnetCIDR)
	if err != nil {
		return fmt.Errorf("invalid subnet CIDR %s: %w", nodeSubnet.SubnetCIDR, err)
	}

	if !subnet.Contains(ip) {
		return fmt.Errorf("IP %s is not in subnet %s", ip.String(), nodeSubnet.SubnetCIDR)
	}

	// Check if IP is in the allowed client range
	start := ipToUint32(nodeSubnet.RangeStart)
	end := ipToUint32(nodeSubnet.RangeEnd)
	current := ipToUint32(ip)

	if current < start || current > end {
		return fmt.Errorf("IP %s is outside client range %s-%s", ip.String(), nodeSubnet.RangeStart.String(), nodeSubnet.RangeEnd.String())
	}

	return nil
}

// ParseSubnetFromCIDR extracts the subnet number from a CIDR string
// For example: "10.8.5.0/24" returns 5
func ParseSubnetFromCIDR(cidr string) (int, error) {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid CIDR format: %s", cidr)
	}

	ip := net.ParseIP(parts[0])
	if ip == nil {
		return 0, fmt.Errorf("invalid IP in CIDR: %s", parts[0])
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("not an IPv4 address: %s", parts[0])
	}

	// For 10.8.X.0/24, return X
	return int(ipv4[2]), nil
}

// GenerateSubnetCIDR generates a subnet CIDR for a given subnet number
// For example: subnet number 5 returns "10.8.5.0/24"
func (m *ipManager) GenerateSubnetCIDR(subnetNum int) string {
	baseIP := m.baseNetwork.IP.To4()
	subnetIP := net.IPv4(baseIP[0], baseIP[1], byte(subnetNum), 0)
	return fmt.Sprintf("%s/%d", subnetIP.String(), m.subnetMask)
}
