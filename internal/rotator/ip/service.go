package ip

import (
	"context"
	"fmt"
	"net"

	sharedErrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	sharedLogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
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
	logger          *sharedLogger.Logger
}

// NewService creates a new IP service
func NewService(repo Repository, config *NetworkConfig, logger *sharedLogger.Logger) (Service, error) {
	if err := config.Validate(); err != nil {
		return nil, sharedErrors.NewSystemError(sharedErrors.ErrCodeConfiguration, "invalid network configuration", false, err)
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
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return nil, err
	}

	// Check if node already has a subnet
	existing, err := s.repository.GetSubnet(ctx, nodeID)
	if err == nil {
		s.logger.DebugContext(ctx, "node already has subnet",
			"node_id", nodeID,
			"subnet", existing.CIDR)
		return existing.Network, nil
	}

	// Get all used subnet CIDRs
	usedCIDRs, err := s.repository.GetUsedSubnetCIDRs(ctx)
	if err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to get used subnets", true, err)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return nil, dbErr
	}

	// Find next available subnet
	subnet, err := s.subnetAllocator.FindNextAvailableSubnet(ctx, usedCIDRs)
	if err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "failed to find next available subnet", false, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "subnet allocation failed", ipErr)
		return nil, ipErr
	}

	// Assign node ID
	subnet.NodeID = nodeID

	// Validate allocation
	if err := s.subnetAllocator.ValidateSubnetAllocation(subnet); err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeInvalidCIDR, "invalid subnet allocation", false, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "subnet validation failed", ipErr)
		return nil, ipErr
	}

	// Store subnet
	if err := s.repository.CreateSubnet(ctx, subnet); err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to store subnet", true, err).WithMetadata("node_id", nodeID).WithMetadata("subnet_cidr", subnet.CIDR)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return nil, dbErr
	}

	s.logger.DebugContext(ctx, "allocated subnet",
		"node_id", nodeID,
		"subnet", subnet.CIDR,
		"gateway", subnet.Gateway.String())

	return subnet.Network, nil
}

// ReleaseNodeSubnet releases a node's subnet allocation
func (s *service) ReleaseNodeSubnet(ctx context.Context, nodeID string) error {
	if err := ValidateNodeID(nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return err
	}

	// Check if subnet exists
	exists, err := s.repository.SubnetExists(ctx, nodeID)
	if err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to check subnet existence", true, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return dbErr
	}

	if !exists {
		s.logger.DebugContext(ctx, "subnet does not exist, nothing to release",
			"node_id", nodeID)
		return nil
	}

	// Delete subnet
	if err := s.repository.DeleteSubnet(ctx, nodeID); err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to delete subnet", true, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return dbErr
	}

	s.logger.DebugContext(ctx, "released subnet", "node_id", nodeID)
	return nil
}

// GetNodeSubnet retrieves a subnet for a node
func (s *service) GetNodeSubnet(ctx context.Context, nodeID string) (*Subnet, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return nil, err
	}

	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, fmt.Sprintf("failed to get subnet for node %s", nodeID), false, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "subnet lookup failed", ipErr)
		return nil, ipErr
	}

	return subnet, nil
}

// AllocateClientIP allocates an IP address within a node's subnet and returns net.IP
func (s *service) AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return nil, err
	}

	// Get subnet
	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "node has no allocated subnet", false, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "subnet lookup failed", ipErr)
		return nil, ipErr
	}

	// Get allocated IPs
	allocatedIPs, err := s.repository.GetAllocatedIPs(ctx, nodeID)
	if err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to get allocated IPs", true, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return nil, dbErr
	}

	// Find next available IP
	nextIP, err := s.ipAllocator.FindNextAvailableIP(subnet, allocatedIPs)
	if err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "failed to find next available IP", false, err).WithMetadata("node_id", nodeID).WithMetadata("subnet_cidr", subnet.CIDR)
		s.logger.ErrorCtx(ctx, "IP allocation failed", ipErr)
		return nil, ipErr
	}

	s.logger.DebugContext(ctx, "allocated IP",
		"node_id", nodeID,
		"ip", nextIP.String())

	return nextIP.IP, nil
}

// ReleaseClientIP releases an IP address allocation
func (s *service) ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error {
	if err := ValidateNodeID(nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return err
	}

	s.logger.DebugContext(ctx, "IP released",
		"node_id", nodeID,
		"ip", ip.String())

	return nil
}

// GetAllocatedIPs returns all allocated IPs for a node as []net.IP
func (s *service) GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return nil, err
	}

	ips, err := s.repository.GetAllocatedIPs(ctx, nodeID)
	if err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, fmt.Sprintf("failed to get allocated IPs for node %s", nodeID), true, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return nil, dbErr
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
		s.logger.ErrorCtx(ctx, "validation failed for node ID", err)
		return 0, err
	}

	// Get subnet
	subnet, err := s.repository.GetSubnet(ctx, nodeID)
	if err != nil {
		ipErr := sharedErrors.NewIPError(sharedErrors.ErrCodeSubnetExhausted, "failed to get subnet", false, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "subnet lookup failed", ipErr)
		return 0, ipErr
	}

	// Get allocated count
	count, err := s.repository.CountAllocatedIPs(ctx, nodeID)
	if err != nil {
		dbErr := sharedErrors.NewDatabaseError(sharedErrors.ErrCodeDatabase, "failed to count allocated IPs", true, err).WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "database operation failed", dbErr)
		return 0, dbErr
	}

	// Calculate capacity
	capacity := CalculateCapacityInfo(subnet, int(count))
	return capacity.AvailableIPs, nil
}

// CheckIPConflict checks if an IP is already allocated
func (s *service) CheckIPConflict(ctx context.Context, ip string) (bool, error) {
	if err := ValidateIPv4Address(ip); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for IP address", err)
		return false, err
	}

	return s.repository.CheckIPConflict(ctx, ip)
}

// CheckPublicKeyConflict checks if a public key is already in use
func (s *service) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	if err := ValidateWireGuardPublicKey(publicKey); err != nil {
		s.logger.ErrorCtx(ctx, "validation failed for public key", err)
		return false, err
	}

	return s.repository.CheckPublicKeyConflict(ctx, publicKey)
}
