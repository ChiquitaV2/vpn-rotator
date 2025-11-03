package ip

import (
	"context"
	"fmt"
	"log/slog"
	"net"
)

// Service provides IP and subnet management operations
type Service interface {
	// Subnet operations - return types that consumers expect
	AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error)
	ReleaseNodeSubnet(ctx context.Context, nodeID string) error
	GetNodeSubnet(ctx context.Context, nodeID string) (*Subnet, error)

	// IP operations - return types that consumers expect
	AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error)
	ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error
	GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error)
	GetAvailableIPCount(ctx context.Context, nodeID string) (int, error)

	// Validation and conflict checking
	CheckIPConflict(ctx context.Context, ip string) (bool, error)
	CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error)
}

type service struct {
	repository      Repository
	subnetAllocator *SubnetAllocator
	ipAllocator     *IPAllocator
	config          *NetworkConfig
	logger          *slog.Logger
}

// NewService creates a new IP service
func NewService(repo Repository, config *NetworkConfig, logger *slog.Logger) (Service, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid network config: %w", err)
	}

	return &service{
		repository:      repo,
		subnetAllocator: NewSubnetAllocator(config),
		ipAllocator:     NewIPAllocator(),
		config:          config,
		logger:          logger,
	}, nil
}

// AllocateNodeSubnet allocates a subnet for a node and returns *net.IPNet
func (s *service) AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	// Check if node already has a subnet
	existing, err := s.repository.GetSubnet(ctx, nodeID)
	if err == nil {
		s.logger.Debug("node already has subnet",
			slog.String("node_id", nodeID),
			slog.String("subnet", existing.CIDR))
		return existing.Network, nil
	}

	// Get all used subnet CIDRs
	usedCIDRs, err := s.repository.GetUsedSubnetCIDRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get used subnets: %w", err)
	}

	// Find next available subnet
	subnet, err := s.subnetAllocator.FindNextAvailableSubnet(ctx, usedCIDRs)
	if err != nil {
		return nil, &SubnetAllocationError{
			NodeID: nodeID,
			Err:    err,
		}
	}

	// Assign node ID
	subnet.NodeID = nodeID

	// Validate allocation
	if err := s.subnetAllocator.ValidateSubnetAllocation(subnet); err != nil {
		return nil, &SubnetAllocationError{
			NodeID: nodeID,
			Err:    err,
		}
	}

	// Store subnet
	if err := s.repository.CreateSubnet(ctx, subnet); err != nil {
		return nil, fmt.Errorf("failed to store subnet: %w", err)
	}

	s.logger.Info("allocated subnet",
		slog.String("node_id", nodeID),
		slog.String("subnet", subnet.CIDR),
		slog.String("gateway", subnet.Gateway.String()))

	return subnet.Network, nil
}

// ReleaseNodeSubnet releases a node's subnet allocation
func (s *service) ReleaseNodeSubnet(ctx context.Context, nodeID string) error {
	if err := ValidateNodeID(nodeID); err != nil {
		return err
	}

	// Check if subnet exists
	exists, err := s.repository.SubnetExists(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check subnet existence: %w", err)
	}

	if !exists {
		s.logger.Debug("subnet does not exist, nothing to release",
			slog.String("node_id", nodeID))
		return nil
	}

	// Delete subnet
	if err := s.repository.DeleteSubnet(ctx, nodeID); err != nil {
		return fmt.Errorf("failed to delete subnet: %w", err)
	}

	s.logger.Info("released subnet", slog.String("node_id", nodeID))
	return nil
}

// GetNodeSubnet retrieves a subnet for a node
func (s *service) GetNodeSubnet(ctx context.Context, nodeID string) (*Subnet, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet for node %s: %w", nodeID, err)
	}

	return subnet, nil
}

// AllocateClientIP allocates an IP address within a node's subnet and returns net.IP
func (s *service) AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	// Get subnet
	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		return nil, &IPAllocationError{
			NodeID: nodeID,
			Err:    fmt.Errorf("node has no allocated subnet: %w", err),
		}
	}

	// Get allocated IPs
	allocatedIPs, err := s.repository.GetAllocatedIPs(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs: %w", err)
	}

	// Find next available IP
	nextIP, err := s.ipAllocator.FindNextAvailableIP(subnet, allocatedIPs)
	if err != nil {
		return nil, &IPAllocationError{
			NodeID: nodeID,
			Err:    err,
		}
	}

	s.logger.Debug("allocated IP",
		slog.String("node_id", nodeID),
		slog.String("ip", nextIP.String()))

	return nextIP.IP, nil
}

// ReleaseClientIP releases an IP address allocation
func (s *service) ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error {
	if err := ValidateNodeID(nodeID); err != nil {
		return err
	}

	s.logger.Debug("IP released",
		slog.String("node_id", nodeID),
		slog.String("ip", ip.String()))

	return nil
}

// GetAllocatedIPs returns all allocated IPs for a node as []net.IP
func (s *service) GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	ips, err := s.repository.GetAllocatedIPs(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs for node %s: %w", nodeID, err)
	}

	// Convert to []net.IP
	result := make([]net.IP, len(ips))
	for i, ip := range ips {
		result[i] = ip.IP
	}

	return result, nil
}

// GetAvailableIPCount returns the number of available IPs for a node
func (s *service) GetAvailableIPCount(ctx context.Context, nodeID string) (int, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return 0, err
	}

	// Get subnet
	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get subnet: %w", err)
	}

	// Get allocated count
	count, err := s.repository.CountAllocatedIPs(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count allocated IPs: %w", err)
	}

	// Calculate capacity
	capacity := CalculateCapacityInfo(subnet, int(count))
	return capacity.AvailableIPs, nil
}

// CheckIPConflict checks if an IP is already allocated
func (s *service) CheckIPConflict(ctx context.Context, ip string) (bool, error) {
	if err := ValidateIPv4Address(ip); err != nil {
		return false, err
	}

	return s.repository.CheckIPConflict(ctx, ip)
}

// CheckPublicKeyConflict checks if a public key is already in use
func (s *service) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	if err := ValidateWireGuardPublicKey(publicKey); err != nil {
		return false, err
	}

	return s.repository.CheckPublicKeyConflict(ctx, publicKey)
}
