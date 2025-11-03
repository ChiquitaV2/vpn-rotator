package ip

import "context"

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
