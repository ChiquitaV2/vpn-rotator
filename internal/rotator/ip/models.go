package ip

import (
	"net"

	sharedErrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// IPv4Address represents a validated IPv4 address
type IPv4Address struct {
	IP net.IP
}

// NewIPv4Address creates a new IPv4Address from a string
func NewIPv4Address(ipStr string) (*IPv4Address, error) {
	if err := ValidateIPv4Address(ipStr); err != nil {
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
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
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
	if err := ValidateNodeID(r.NodeID); err != nil {
		return err
	}
	if r.PeerID == "" {
		return sharedErrors.NewPeerError(sharedErrors.ErrCodePeerValidation, "peer ID cannot be empty", false, nil).WithMetadata("peer_id", r.PeerID)
	}
	if err := ValidateWireGuardPublicKey(r.PublicKey); err != nil {
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
	return ValidateNodeID(r.NodeID)
}
