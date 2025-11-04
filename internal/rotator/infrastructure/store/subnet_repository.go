package store

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
)

// subnetRepository implements ip.Repository using db.Store
type subnetRepository struct {
	store db.Store
}

// NewSubnetRepository creates a new subnet repository
func NewSubnetRepository(store db.Store) ip.Repository {
	return &subnetRepository{
		store: store,
	}
}

// CreateSubnet stores a subnet allocation
func (r *subnetRepository) CreateSubnet(ctx context.Context, subnet *ip.Subnet) error {
	_, err := r.store.CreateNodeSubnet(ctx, db.CreateNodeSubnetParams{
		NodeID:       subnet.NodeID,
		SubnetCidr:   subnet.CIDR,
		GatewayIp:    subnet.Gateway.String(),
		IpRangeStart: subnet.RangeStart.String(),
		IpRangeEnd:   subnet.RangeEnd.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to create subnet in database: %w", err)
	}
	return nil
}

// GetSubnet retrieves a subnet by node ID
func (r *subnetRepository) GetSubnet(ctx context.Context, nodeID string) (*ip.Subnet, error) {
	dbSubnet, err := r.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ip.ErrSubnetNotFound
		}
		return nil, fmt.Errorf("failed to get subnet from database: %w", err)
	}

	return r.toDomainSubnet(dbSubnet)
}

// GetAllSubnets retrieves all subnet allocations
func (r *subnetRepository) GetAllSubnets(ctx context.Context) ([]*ip.Subnet, error) {
	dbSubnets, err := r.store.GetAllNodeSubnets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all subnets from database: %w", err)
	}

	subnets := make([]*ip.Subnet, 0, len(dbSubnets))
	for _, dbSubnet := range dbSubnets {
		subnet, err := r.toDomainSubnet(dbSubnet)
		if err != nil {
			return nil, fmt.Errorf("failed to convert subnet for node %s: %w", dbSubnet.NodeID, err)
		}
		subnets = append(subnets, subnet)
	}

	return subnets, nil
}

// DeleteSubnet removes a subnet allocation
func (r *subnetRepository) DeleteSubnet(ctx context.Context, nodeID string) error {
	err := r.store.DeleteNodeSubnet(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete subnet from database: %w", err)
	}
	return nil
}

// SubnetExists checks if a subnet exists for a node
func (r *subnetRepository) SubnetExists(ctx context.Context, nodeID string) (bool, error) {
	_, err := r.store.GetNodeSubnet(ctx, nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check subnet existence: %w", err)
	}
	return true, nil
}

// GetUsedSubnetCIDRs retrieves all currently used subnet CIDRs
func (r *subnetRepository) GetUsedSubnetCIDRs(ctx context.Context) ([]string, error) {
	cidrs, err := r.store.GetUsedSubnetCIDRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get used subnet CIDRs: %w", err)
	}
	return cidrs, nil
}

// CountSubnets returns the total number of allocated subnets
func (r *subnetRepository) CountSubnets(ctx context.Context) (int, error) {
	subnets, err := r.store.GetAllNodeSubnets(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count subnets: %w", err)
	}
	return len(subnets), nil
}

// GetAllocatedIPs retrieves all allocated IP addresses for a node
func (r *subnetRepository) GetAllocatedIPs(ctx context.Context, nodeID string) ([]*ip.IPv4Address, error) {
	ipStrings, err := r.store.GetAllocatedIPsByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocated IPs from database: %w", err)
	}

	ips := make([]*ip.IPv4Address, 0, len(ipStrings))
	for _, ipStr := range ipStrings {
		ipaddr, err := ip.NewIPv4Address(ipStr)
		if err != nil {
			return nil, fmt.Errorf("invalid IP address in database %s: %w", ipStr, err)
		}
		ips = append(ips, ipaddr)
	}

	return ips, nil
}

// CountAllocatedIPs returns the number of allocated IPs for a node
func (r *subnetRepository) CountAllocatedIPs(ctx context.Context, nodeID string) (int64, error) {
	count, err := r.store.CountPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count allocated IPs: %w", err)
	}
	return count, nil
}

// IPExists checks if an IP address is allocated
func (r *subnetRepository) IPExists(ctx context.Context, ip string) (bool, error) {
	count, err := r.store.CheckIPConflict(ctx, ip)
	if err != nil {
		return false, fmt.Errorf("failed to check IP existence: %w", err)
	}
	return count > 0, nil
}

// CheckIPConflict checks if an IP is already allocated
func (r *subnetRepository) CheckIPConflict(ctx context.Context, ip string) (bool, error) {
	count, err := r.store.CheckIPConflict(ctx, ip)
	if err != nil {
		return false, fmt.Errorf("failed to check IP conflict: %w", err)
	}
	return count > 0, nil
}

// CheckPublicKeyConflict checks if a public key is already in use
func (r *subnetRepository) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	count, err := r.store.CheckPublicKeyConflict(ctx, publicKey)
	if err != nil {
		return false, fmt.Errorf("failed to check public key conflict: %w", err)
	}
	return count > 0, nil
}

// toDomainSubnet converts a database subnet to a domain subnet
func (r *subnetRepository) toDomainSubnet(dbSubnet db.NodeSubnet) (*ip.Subnet, error) {
	// Parse CIDR
	_, network, err := net.ParseCIDR(dbSubnet.SubnetCidr)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR %s: %w", dbSubnet.SubnetCidr, err)
	}

	// Parse IPs
	gateway, err := ip.NewIPv4Address(dbSubnet.GatewayIp)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway IP %s: %w", dbSubnet.GatewayIp, err)
	}

	rangeStart, err := ip.NewIPv4Address(dbSubnet.IpRangeStart)
	if err != nil {
		return nil, fmt.Errorf("invalid range start IP %s: %w", dbSubnet.IpRangeStart, err)
	}

	rangeEnd, err := ip.NewIPv4Address(dbSubnet.IpRangeEnd)
	if err != nil {
		return nil, fmt.Errorf("invalid range end IP %s: %w", dbSubnet.IpRangeEnd, err)
	}

	return &ip.Subnet{
		NodeID:     dbSubnet.NodeID,
		CIDR:       dbSubnet.SubnetCidr,
		Network:    network,
		Gateway:    gateway,
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
	}, nil
}
