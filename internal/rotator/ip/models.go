package ip

import (
	"fmt"
	"net"
	"strings"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	sharedErrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
)

// IPv4Address represents a validated IPv4 address
type IPv4Address struct {
	IP net.IP
}

// NewIPv4Address creates a new IPv4Address from a string
func NewIPv4Address(ipStr string) (*IPv4Address, error) {
	if err := validateIPv4Address(ipStr); err != nil {
		return nil, err
	}
	ip := net.ParseIP(ipStr)
	return &IPv4Address{IP: ip.To4()}, nil
}

// String returns the string representation of the IP
func (a *IPv4Address) String() string {
	return a.IP.String()
}

// Subnet represents a network subnet allocated to a VPN node
type Subnet struct {
	NodeID     string
	CIDR       string
	Network    *net.IPNet
	Gateway    *IPv4Address
	RangeStart *IPv4Address
	RangeEnd   *IPv4Address
}

// NewSubnet creates a new Subnet from a network
func NewSubnet(nodeID string, network *net.IPNet) (*Subnet, error) {
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}
	if network == nil {
		return nil, sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "network cannot be nil", false, nil)
	}

	// Calculate gateway (first usable IP)
	gateway := make(net.IP, len(network.IP))
	copy(gateway, network.IP)
	gateway[len(gateway)-1]++

	// Calculate range start (after gateway)
	rangeStart := make(net.IP, len(gateway))
	copy(rangeStart, gateway)
	rangeStart[len(rangeStart)-1]++

	// Calculate range end (last usable IP before broadcast)
	rangeEnd := make(net.IP, len(network.IP))
	copy(rangeEnd, network.IP)
	mask := network.Mask
	for i := range rangeEnd {
		rangeEnd[i] |= ^mask[i]
	}
	rangeEnd[len(rangeEnd)-1]--

	gatewayAddr := &IPv4Address{IP: gateway.To4()}
	rangeStartAddr := &IPv4Address{IP: rangeStart.To4()}
	rangeEndAddr := &IPv4Address{IP: rangeEnd.To4()}

	return &Subnet{
		NodeID:     nodeID,
		CIDR:       network.String(),
		Network:    network,
		Gateway:    gatewayAddr,
		RangeStart: rangeStartAddr,
		RangeEnd:   rangeEndAddr,
	}, nil
}

// SubnetAllocation represents a subnet with its current allocations
type SubnetAllocation struct {
	Subnet         *Subnet
	AllocatedIPs   []*IPv4Address
	AllocatedCount int
}

// IPAllocation represents an allocated IP address
type IPAllocation struct {
	NodeID    string
	IP        *IPv4Address
	PeerID    string
	PublicKey string
}

// SubnetCapacity represents capacity information for a subnet
type SubnetCapacity struct {
	TotalIPs     int
	AllocatedIPs int
	AvailableIPs int
	UsagePercent float64
	NearCapacity bool
	Threshold    float64
}

// AllocationRequest represents a request to allocate an IP
type AllocationRequest struct {
	NodeID    string
	PeerID    string
	PublicKey string
}

// Validate checks if the allocation request is valid
func (r *AllocationRequest) Validate() error {
	if err := validateNodeID(r.NodeID); err != nil {
		return err
	}
	if r.PeerID == "" {
		return sharedErrors.NewPeerError(sharedErrors.ErrCodePeerValidation, "peer ID cannot be empty", false, nil).WithMetadata("peer_id", r.PeerID)
	}
	if err := validateWireGuardPublicKey(r.PublicKey); err != nil {
		return err
	}
	return nil
}

// SubnetRequest represents a request to allocate a subnet
type SubnetRequest struct {
	NodeID string
}

// Validate checks if the subnet request is valid
func (r *SubnetRequest) Validate() error {
	return validateNodeID(r.NodeID)
}

// --- Private Validation Functions ---

func validateIPv4Address(ipStr string) error {
	if ipStr == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "IP address cannot be empty", false, nil).WithMetadata("ip_address", ipStr)
	}

	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "invalid IP address format", false, nil).WithMetadata("ip_address", ipStr)
	}

	return nil
}

func validateCIDR(cidr string) error {
	if cidr == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "CIDR cannot be empty", false, nil).WithMetadata("cidr", cidr)
	}

	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("invalid CIDR notation: %v", err), false, err).WithMetadata("cidr", cidr)
	}

	return nil
}

func validateNodeID(nodeID string) error {
	if nodeID == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "node ID cannot be empty", false, nil).WithMetadata("node_id", nodeID)
	}

	if len(nodeID) > 255 {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidIPAddress, "node ID too long (max 255 characters)", false, nil).WithMetadata("node_id", nodeID)
	}

	return nil
}

func validateWireGuardPublicKey(publicKey string) error {
	if publicKey == "" {
		return sharedErrors.NewIPError(sharedErrors.ErrCodePeerValidation, "public key cannot be empty", false, nil).WithMetadata("public_key", publicKey)
	}

	publicKey = strings.TrimSpace(publicKey)
	if !crypto.IsValidWireGuardKey(publicKey) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodePeerValidation, "invalid WireGuard public key format", false, nil).WithMetadata("public_key", publicKey)
	}

	return nil
}

// validateSubnetAlignment checks if a subnet is properly aligned
func validateSubnetAlignment(network *net.IPNet) error {
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

// validateSubnetInBaseNetwork checks if a subnet is within the base network
func validateSubnetInBaseNetwork(subnet, baseNetwork *net.IPNet) error {
	if subnet == nil || baseNetwork == nil {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "subnet and base network cannot be nil", false, nil)
	}

	if !baseNetwork.Contains(subnet.IP) {
		return sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, fmt.Sprintf("subnet %s is not within base network %s", subnet.String(), baseNetwork.String()), false, nil).WithMetadata("subnet", subnet.String()).WithMetadata("base_network", baseNetwork.String())
	}

	return nil
}

// ipGreaterThan checks if ip1 > ip2
func ipGreaterThan(ip1, ip2 net.IP) bool {
	ip1 = ip1.To4()
	ip2 = ip2.To4()
	for i := 0; i < len(ip1); i++ {
		if ip1[i] > ip2[i] {
			return true
		} else if ip1[i] < ip2[i] {
			return false
		}
	}
	return false
}
