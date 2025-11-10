package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// subnetRepository implements ip.Repository using db.Store
type subnetRepository struct {
	store  db.Store
	logger *applogger.Logger
}

// NewSubnetRepository creates a new subnet repository
func NewSubnetRepository(store db.Store, log *applogger.Logger) ip.Repository {
	return &subnetRepository{
		store:  store,
		logger: log.WithComponent("subnet.repository"),
	}
}

// CreateSubnet stores a subnet allocation
func (r *subnetRepository) CreateSubnet(ctx context.Context, subnet *ip.Subnet) error {
	start := time.Now()
	_, err := r.store.CreateNodeSubnet(ctx, db.CreateNodeSubnetParams{
		NodeID:       subnet.NodeID,
		SubnetCidr:   subnet.CIDR,
		GatewayIp:    subnet.Gateway.String(),
		IpRangeStart: subnet.RangeStart.String(),
		IpRangeEnd:   subnet.RangeEnd.String(),
	})
	r.logger.DBQuery(ctx, "CreateNodeSubnet", "node_subnets", time.Since(start), slog.String("node_id", subnet.NodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to create subnet in database", err, slog.String("node_id", subnet.NodeID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to create subnet in database", true, err)
	}
	return nil
}

// GetSubnet retrieves a subnet by node ID
func (r *subnetRepository) GetSubnet(ctx context.Context, nodeID string) (*ip.Subnet, error) {
	start := time.Now()
	dbSubnet, err := r.store.GetNodeSubnet(ctx, nodeID)
	r.logger.DBQuery(ctx, "GetNodeSubnet", "node_subnets", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NewIPError(apperrors.ErrCodeSubnetNotFound, fmt.Sprintf("subnet for node %s not found", nodeID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to get subnet from database", err, slog.String("node_id", nodeID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get subnet from database", true, err)
	}

	return r.toDomainSubnet(dbSubnet)
}

// GetAllSubnets retrieves all subnet allocations
func (r *subnetRepository) GetAllSubnets(ctx context.Context) ([]*ip.Subnet, error) {
	start := time.Now()
	dbSubnets, err := r.store.GetAllNodeSubnets(ctx)
	r.logger.DBQuery(ctx, "GetAllNodeSubnets", "node_subnets", time.Since(start))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get all subnets from database", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get all subnets from database", true, err)
	}

	subnets := make([]*ip.Subnet, 0, len(dbSubnets))
	for _, dbSubnet := range dbSubnets {
		subnet, err := r.toDomainSubnet(dbSubnet)
		if err != nil {
			return nil, apperrors.NewIPError(apperrors.ErrCodeInvalidCIDR, fmt.Sprintf("failed to convert subnet for node %s", dbSubnet.NodeID), false, err)
		}
		subnets = append(subnets, subnet)
	}

	return subnets, nil
}

// DeleteSubnet removes a subnet allocation
func (r *subnetRepository) DeleteSubnet(ctx context.Context, nodeID string) error {
	start := time.Now()
	err := r.store.DeleteNodeSubnet(ctx, nodeID)
	r.logger.DBQuery(ctx, "DeleteNodeSubnet", "node_subnets", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewIPError(apperrors.ErrCodeSubnetNotFound, fmt.Sprintf("subnet for node %s not found for deletion", nodeID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to delete subnet from database", err, slog.String("node_id", nodeID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to delete subnet from database", true, err)
	}
	return nil
}

// SubnetExists checks if a subnet exists for a node
func (r *subnetRepository) SubnetExists(ctx context.Context, nodeID string) (bool, error) {
	start := time.Now()
	_, err := r.store.GetNodeSubnet(ctx, nodeID)
	r.logger.DBQuery(ctx, "GetNodeSubnet", "node_subnets", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		r.logger.ErrorCtx(ctx, "failed to check subnet existence", err, slog.String("node_id", nodeID))
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check subnet existence", true, err)
	}
	return true, nil
}

// GetUsedSubnetCIDRs retrieves all currently used subnet CIDRs
func (r *subnetRepository) GetUsedSubnetCIDRs(ctx context.Context) ([]string, error) {
	start := time.Now()
	cidrs, err := r.store.GetUsedSubnetCIDRs(ctx)
	r.logger.DBQuery(ctx, "GetUsedSubnetCIDRs", "node_subnets", time.Since(start))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get used subnet CIDRs", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get used subnet CIDRs", true, err)
	}
	return cidrs, nil
}

// CountSubnets returns the total number of allocated subnets
func (r *subnetRepository) CountSubnets(ctx context.Context) (int, error) {
	start := time.Now()
	subnets, err := r.store.GetAllNodeSubnets(ctx)
	r.logger.DBQuery(ctx, "GetAllNodeSubnets", "node_subnets", time.Since(start))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to count subnets", err)
		return 0, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to count subnets", true, err)
	}
	return len(subnets), nil
}

// GetAllocatedIPs retrieves all allocated IP addresses for a node
func (r *subnetRepository) GetAllocatedIPs(ctx context.Context, nodeID string) ([]*ip.IPv4Address, error) {
	start := time.Now()
	ipStrings, err := r.store.GetAllocatedIPsByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "GetAllocatedIPsByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get allocated IPs from database", err, slog.String("node_id", nodeID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get allocated IPs from database", true, err)
	}

	ips := make([]*ip.IPv4Address, 0, len(ipStrings))
	for _, ipStr := range ipStrings {
		ipaddr, err := ip.NewIPv4Address(ipStr)
		if err != nil {
			return nil, apperrors.NewIPError(apperrors.ErrCodeInvalidIPAddress, fmt.Sprintf("invalid IP address in database %s", ipStr), false, err)
		}
		ips = append(ips, ipaddr)
	}

	return ips, nil
}

// CountAllocatedIPs returns the number of allocated IPs for a node
func (r *subnetRepository) CountAllocatedIPs(ctx context.Context, nodeID string) (int64, error) {
	start := time.Now()
	count, err := r.store.CountPeersByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "CountPeersByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to count allocated IPs", err, slog.String("node_id", nodeID))
		return 0, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to count allocated IPs", true, err)
	}
	return count, nil
}

// IPExists checks if an IP address is allocated
func (r *subnetRepository) IPExists(ctx context.Context, ip string) (bool, error) {
	start := time.Now()
	count, err := r.store.CheckIPConflict(ctx, ip)
	r.logger.DBQuery(ctx, "CheckIPConflict", "peers", time.Since(start), slog.String("ip_address", ip))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to check IP existence", err, slog.String("ip_address", ip))
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check IP existence", true, err)
	}
	return count > 0, nil
}

// CheckIPConflict checks if an IP is already allocated
func (r *subnetRepository) CheckIPConflict(ctx context.Context, ipAddress string) (bool, error) {
	start := time.Now()
	count, err := r.store.CheckIPConflict(ctx, ipAddress)
	r.logger.DBQuery(ctx, "CheckIPConflict", "peers", time.Since(start), slog.String("ip_address", ipAddress))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to check IP conflict", err)
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check IP conflict", true, err)
	}
	return count > 0, nil
}

// CheckPublicKeyConflict checks if a public key is already in use
func (r *subnetRepository) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	start := time.Now()
	count, err := r.store.CheckPublicKeyConflict(ctx, publicKey)
	r.logger.DBQuery(ctx, "CheckPublicKeyConflict", "peers", time.Since(start), slog.String("public_key", publicKey))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to check public key conflict", err)
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check public key conflict", true, err)
	}
	return count > 0, nil
}

// toDomainSubnet converts a database subnet to a domain subnet
func (r *subnetRepository) toDomainSubnet(dbSubnet db.NodeSubnet) (*ip.Subnet, error) {
	_, network, err := net.ParseCIDR(dbSubnet.SubnetCidr)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR %s: %w", dbSubnet.SubnetCidr, err)
	}

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
		},
		nil
}
