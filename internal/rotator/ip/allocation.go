package ip

import (
	"context"
	"fmt"
	"net"

	sharedErrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// SubnetAllocator handles subnet allocation logic
type SubnetAllocator struct {
	config *NetworkConfig
}

// NewSubnetAllocator creates a new subnet allocator
func NewSubnetAllocator(config *NetworkConfig) *SubnetAllocator {
	return &SubnetAllocator{config: config}
}

// FindNextAvailableSubnet finds the next available subnet from the configured range
func (a *SubnetAllocator) FindNextAvailableSubnet(ctx context.Context, usedCIDRs []string) (*Subnet, error) {
	// Build map of used networks for quick lookup
	usedNets := make(map[string]bool)
	for _, cidr := range usedCIDRs {
		usedNets[cidr] = true
	}

	// Try each possible subnet in the range
	maxSubnets := a.config.MaxSubnets()
	for i := 0; i < maxSubnets; i++ {
		cidr, err := a.config.GenerateSubnetCIDR(i)
		if err != nil {
			return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "failed to generate subnet CIDR", false, err).WithMetadata("index", i)
		}

		// Skip if already used
		if usedNets[cidr] {
			continue
		}

		// Parse and create subnet
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("failed to parse generated CIDR %s", cidr), false, err).WithMetadata("cidr", cidr)
		}

		// Generate a temporary node ID for validation
		subnet, err := NewSubnet("temporary-id", network)
		if err != nil {
			return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "failed to create subnet", false, err).WithMetadata("cidr", cidr)
		}

		return subnet, nil
	}

	return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "no available subnets in network range", false, nil)
}

// ValidateSubnetAllocation validates that a subnet allocation is acceptable
func (a *SubnetAllocator) ValidateSubnetAllocation(subnet *Subnet) error {
	if subnet == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "subnet cannot be nil", false, nil)
	}

	// Validate alignment
	if err := validateSubnetAlignment(subnet.Network); err != nil {
		return err
	}

	// Validate within base network
	baseNet, err := a.config.ParsedBaseNetwork()
	if err != nil {
		return err
	}

	return validateSubnetInBaseNetwork(subnet.Network, baseNet)
}

// IPAllocator handles IP allocation within subnets
type IPAllocator struct{}

// NewIPAllocator creates a new IP allocator
func NewIPAllocator() *IPAllocator {
	return &IPAllocator{}
}

// FindNextAvailableIP finds the next available IP in a subnet
func (a *IPAllocator) FindNextAvailableIP(subnet *Subnet, allocatedIPs []*IPv4Address) (*IPv4Address, error) {
	if subnet == nil {
		return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "subnet cannot be nil", false, nil)
	}

	// Build map of allocated IPs for quick lookup
	allocated := make(map[string]bool)
	for _, ip := range allocatedIPs {
		allocated[ip.String()] = true
	}

	// Iterate through the usable range
	current := make(net.IP, len(subnet.RangeStart.IP))
	copy(current, subnet.RangeStart.IP)

	endIP := subnet.RangeEnd.IP

	for {
		// Check if current IP is available
		if !allocated[current.String()] {
			return &IPv4Address{IP: current.To4()}, nil
		}

		// Move to next IP
		for i := len(current) - 1; i >= 0; i-- {
			current[i]++
			if current[i] != 0 {
				break
			}
		}

		// Check if we've exceeded the range
		if !subnet.Network.Contains(current) || ipGreaterThan(current, endIP) {
			break
		}
	}

	return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "subnet has no available IPs", false, nil).WithMetadata("subnet_cidr", subnet.CIDR)
}

// FindLeastUtilizedSubnet finds the subnet with the lowest utilization
func FindLeastUtilizedSubnet(allocations []*SubnetAllocation) (string, error) {
	if len(allocations) == 0 {
		return "", sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "no subnet allocations available", false, nil)
	}

	var leastUtilized *SubnetAllocation
	lowestUtilization := 2.0 // Start above 100%

	for _, alloc := range allocations {
		capacity := CalculateCapacityInfo(alloc.Subnet, alloc.AllocatedCount)
		if capacity.UsagePercent < lowestUtilization {
			lowestUtilization = capacity.UsagePercent
			leastUtilized = alloc
		}
	}

	if leastUtilized == nil {
		return "", sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "could not find least utilized subnet", false, nil)
	}

	return leastUtilized.Subnet.NodeID, nil
}

// CalculateFragmentation calculates subnet fragmentation metrics
func CalculateFragmentation(allocations []*SubnetAllocation) float64 {
	if len(allocations) == 0 {
		return 0.0
	}

	var totalFragmentation float64
	for _, alloc := range allocations {
		capacity := CalculateCapacityInfo(alloc.Subnet, alloc.AllocatedCount)
		// Higher fragmentation when usage is between 25% and 75%
		if capacity.UsagePercent > 0.25 && capacity.UsagePercent < 0.75 {
			totalFragmentation += 1.0
		}
	}

	return totalFragmentation / float64(len(allocations))
}

// SuggestCompaction analyzes if subnet compaction would be beneficial
func SuggestCompaction(allocations []*SubnetAllocation) bool {
	if len(allocations) < 2 {
		return false
	}

	// Count underutilized subnets (< 50% usage)
	underutilized := 0
	for _, alloc := range allocations {
		capacity := CalculateCapacityInfo(alloc.Subnet, alloc.AllocatedCount)
		if capacity.UsagePercent < 0.50 {
			underutilized++
		}
	}

	// Suggest compaction if more than half are underutilized
	return underutilized > len(allocations)/2
}

// CalculateCapacityInfo calculates detailed capacity information
func CalculateCapacityInfo(subnet *Subnet, allocatedCount int) *SubnetCapacity {
	// Calculate total usable IPs (exclude network, gateway, broadcast)
	maskSize, _ := subnet.Network.Mask.Size()
	totalIPs := (1 << uint(32-maskSize)) - 3

	availableIPs := totalIPs - allocatedCount
	if availableIPs < 0 {
		availableIPs = 0
	}

	usagePercent := 0.0
	if totalIPs > 0 {
		usagePercent = float64(allocatedCount) / float64(totalIPs)
	}

	nearCapacity := usagePercent >= 0.80

	return &SubnetCapacity{
		TotalIPs:     totalIPs,
		AllocatedIPs: allocatedCount,
		AvailableIPs: availableIPs,
		UsagePercent: usagePercent,
		NearCapacity: nearCapacity,
		Threshold:    0.80,
	}
}
