package ip

import (
	"fmt"
	"math"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	sharedErrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// NetworkConfig defines the network configuration for IP allocation
// This now uses the centralized internal configuration
type NetworkConfig struct {
	// BaseNetwork is the overall network range (e.g., "10.8.0.0/16")
	BaseNetwork string

	// SubnetMask is the mask for individual node subnets (e.g., 24 for /24)
	SubnetMask int

	// CapacityThreshold is the percentage (0.0-1.0) at which capacity warnings trigger
	CapacityThreshold float64
}

// DefaultNetworkConfig returns the hardcoded network configuration from centralized config
// Network settings are intentionally not user-configurable to ensure consistency
func DefaultNetworkConfig() *NetworkConfig {
	defaults := config.NewInternalDefaults().NetworkDefaults()
	return &NetworkConfig{
		BaseNetwork:       defaults.BaseNetwork,
		SubnetMask:        defaults.SubnetMask,
		CapacityThreshold: defaults.CapacityThreshold,
	}
}

// Validate checks if the network configuration is valid
func (c *NetworkConfig) Validate() error {
	// Validate base network
	if err := ValidateCIDR(c.BaseNetwork); err != nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "invalid base network", false, err).WithMetadata("base_network", c.BaseNetwork)
	}

	// Parse base network to validate structure
	_, baseNet, err := net.ParseCIDR(c.BaseNetwork)
	if err != nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "failed to parse base network", false, err).WithMetadata("base_network", c.BaseNetwork)
	}

	// Validate subnet mask
	baseOnes, _ := baseNet.Mask.Size()
	if c.SubnetMask <= baseOnes {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("subnet mask /%d must be larger than base network mask /%d", c.SubnetMask, baseOnes), false, nil).WithMetadata("subnet_mask", c.SubnetMask).WithMetadata("base_network_mask", baseOnes)
	}

	if c.SubnetMask > 30 {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("subnet mask /%d is too restrictive (max /30)", c.SubnetMask), false, nil).WithMetadata("subnet_mask", c.SubnetMask)
	}

	// Validate capacity threshold
	if c.CapacityThreshold <= 0 || c.CapacityThreshold > 1.0 {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeConfiguration, fmt.Sprintf("capacity threshold must be between 0.0 and 1.0, got %.2f", c.CapacityThreshold), false, nil).WithMetadata("capacity_threshold", c.CapacityThreshold)
	}

	return nil
}

// ParsedBaseNetwork returns the parsed base network
func (c *NetworkConfig) ParsedBaseNetwork() (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(c.BaseNetwork)
	if err != nil {
		return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "failed to parse base network", false, err).WithMetadata("base_network", c.BaseNetwork)
	}
	return network, nil
}

// MaxSubnets returns the maximum number of subnets that can be allocated
func (c *NetworkConfig) MaxSubnets() int {
	_, baseNet, _ := net.ParseCIDR(c.BaseNetwork)
	baseOnes, _ := baseNet.Mask.Size()
	subnetBits := c.SubnetMask - baseOnes
	return int(math.Pow(2, float64(subnetBits)))
}

// GenerateSubnetCIDR generates a subnet CIDR for the given index
func (c *NetworkConfig) GenerateSubnetCIDR(index int) (string, error) {
	baseIP, baseNet, err := net.ParseCIDR(c.BaseNetwork)
	if err != nil {
		return "", sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "invalid base network", false, err).WithMetadata("base_network", c.BaseNetwork)
	}

	baseOnes, _ := baseNet.Mask.Size()
	subnetBits := c.SubnetMask - baseOnes

	if index >= int(math.Pow(2, float64(subnetBits))) {
		return "", sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, fmt.Sprintf("subnet index %d exceeds maximum %d", index, int(math.Pow(2, float64(subnetBits)))-1), false, nil).WithMetadata("index", index).WithMetadata("max_subnets", int(math.Pow(2, float64(subnetBits)))-1)
	}

	// Calculate subnet IP
	ip := make(net.IP, len(baseIP.To4()))
	copy(ip, baseIP.To4())

	// Apply subnet index
	shift := 32 - c.SubnetMask
	ipInt := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	ipInt += uint32(index << shift)

	ip[0] = byte(ipInt >> 24)
	ip[1] = byte(ipInt >> 16)
	ip[2] = byte(ipInt >> 8)
	ip[3] = byte(ipInt)

	return fmt.Sprintf("%s/%d", ip.String(), c.SubnetMask), nil
}
