package ip

import (
	"context"
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

// SubnetRepository defines storage operations for subnet management
type SubnetRepository interface {
	CreateSubnet(ctx context.Context, subnet *Subnet) error
	GetSubnet(ctx context.Context, nodeID string) (*Subnet, error)
	GetAllSubnets(ctx context.Context) ([]*Subnet, error)
	DeleteSubnet(ctx context.Context, nodeID string) error
	SubnetExists(ctx context.Context, nodeID string) (bool, error)
	GetUsedSubnetCIDRs(ctx context.Context) ([]string, error)
}

// IPRepository defines storage operations for IP allocation management
type IPRepository interface {
	GetAllocatedIPs(ctx context.Context, nodeID string) ([]*IPv4Address, error)
	CountAllocatedIPs(ctx context.Context, nodeID string) (int64, error)
	IPExists(ctx context.Context, ip string) (bool, error)
	CheckIPConflict(ctx context.Context, ip string) (bool, error)
	CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error)
}

// Repository combines both subnet and IP operations
type Repository interface {
	SubnetRepository
	IPRepository
}
