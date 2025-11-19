package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// nodeRepository implements node.NodeRepository using db.Store
type nodeRepository struct {
	store  db.Store
	logger *applogger.Logger
}

// NewNodeRepository creates a new node repository
func NewNodeRepository(store db.Store, log *applogger.Logger) node.NodeRepository {
	return &nodeRepository{
		store:  store,
		logger: log.WithComponent("node.repository"), // Scope logger to this component
	}
}

// Create creates a new node in the database
func (r *nodeRepository) Create(ctx context.Context, n *node.Node) error {
	start := time.Now()
	params := db.CreateNodeParams{
		ID:              n.ID,
		IpAddress:       n.IPAddress,
		ServerPublicKey: n.ServerPublicKey,
		Port:            int64(n.Port),
		Status:          string(n.Status),
	}

	dbNode, err := r.store.CreateNode(ctx, params)
	r.logger.DBQuery(ctx, "CreateNode", "nodes", time.Since(start), slog.String("node_id", n.ID))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to create node in database", err, slog.String("node_id", n.ID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to create node in database", true, err)
	}

	// Update the node with database-generated fields
	n.CreatedAt = dbNode.CreatedAt
	n.UpdatedAt = dbNode.UpdatedAt
	n.Version = dbNode.Version

	return nil
}

// GetByID retrieves a node by ID
func (r *nodeRepository) GetByID(ctx context.Context, nodeID string) (*node.Node, error) {
	start := time.Now()
	dbNode, err := r.store.GetNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "GetNode", "nodes", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, fmt.Sprintf("node %s not found", nodeID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to get node from database", err, slog.String("node_id", nodeID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get node from database", true, err)
	}

	return r.toDomainNode(&dbNode)
}

// Update updates an existing node
func (r *nodeRepository) Update(ctx context.Context, n *node.Node) error {
	start := time.Now()
	err := r.store.UpdateNodeDetails(ctx, db.UpdateNodeDetailsParams{
		ServerID:        sql.NullString{String: n.ServerID, Valid: n.ServerID != ""},
		IpAddress:       n.IPAddress,
		ServerPublicKey: n.ServerPublicKey,
		Status:          string(n.Status),
		ID:              n.ID,
	})
	r.logger.DBQuery(ctx, "UpdateNodeDetails", "nodes", time.Since(start), slog.String("node_id", n.ID))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "failed to update node, concurrent modification or not found", false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to update node details", err, slog.String("node_id", n.ID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update node details", true, err)
	}

	if n.LastHandshakeAt != nil || n.ConnectedClients > 0 {
		handshake := sql.NullTime{}
		if n.LastHandshakeAt != nil {
			handshake = sql.NullTime{Time: *n.LastHandshakeAt, Valid: true}
		}
		start = time.Now()
		err = r.store.UpdateNodeActivity(ctx, db.UpdateNodeActivityParams{
			LastHandshakeAt:  handshake,
			ConnectedClients: int64(n.ConnectedClients),
			ID:               n.ID,
		})
		r.logger.DBQuery(ctx, "UpdateNodeActivity", "nodes", time.Since(start), slog.String("node_id", n.ID))
		if err != nil {
			r.logger.ErrorCtx(ctx, "failed to update node activity", err, slog.String("node_id", n.ID))
			return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update node activity", true, err)
		}
	}

	return nil
}

// Delete deletes a node
func (r *nodeRepository) Delete(ctx context.Context, nodeID string) error {
	start := time.Now()
	err := r.store.DeleteNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "DeleteNode", "nodes", time.Since(start), slog.String("node_id", nodeID))
	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, fmt.Sprintf("node %s not found for deletion", nodeID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to delete node", err, slog.String("node_id", nodeID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to delete node", true, err)
	}
	return nil
}

// List lists nodes with filters
func (r *nodeRepository) List(ctx context.Context, filters node.Filters) ([]*node.Node, error) {
	start := time.Now()
	var dbNodes []db.Node
	var err error

	if filters.Status != nil {
		dbNodes, err = r.store.GetNodesByStatus(ctx, string(*filters.Status))
	} else {
		dbNodes, err = r.store.GetAllNodes(ctx)
	}
	r.logger.DBQuery(ctx, "ListNodes", "nodes", time.Since(start), slog.Any("filters", filters))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to list nodes", err, slog.Any("filters", filters))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to list nodes", true, err)
	}

	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to convert db node to domain node", false, err)
		}
		nodes = append(nodes, domainNode)
	}

	nodes = r.applyFilters(nodes, filters)
	return nodes, nil
}

// GetByServerID retrieves a node by server ID
func (r *nodeRepository) GetByServerID(ctx context.Context, serverID string) (*node.Node, error) {
	start := time.Now()
	dbNodes, err := r.store.GetAllNodes(ctx)
	r.logger.DBQuery(ctx, "GetAllNodes", "nodes", time.Since(start), slog.String("lookup_server_id", serverID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get nodes for server ID lookup", err, slog.String("server_id", serverID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get nodes for server ID lookup", true, err)
	}

	for i := range dbNodes {
		if dbNodes[i].ServerID.Valid && dbNodes[i].ServerID.String == serverID {
			return r.toDomainNode(&dbNodes[i])
		}
	}

	return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, fmt.Sprintf("node with server ID %s not found", serverID), false, nil)
}

// GetByIPAddress retrieves a node by IP address
func (r *nodeRepository) GetByIPAddress(ctx context.Context, ipAddress string) (*node.Node, error) {
	start := time.Now()
	dbNode, err := r.store.GetNodeByIP(ctx, ipAddress)
	r.logger.DBQuery(ctx, "GetNodeByIP", "nodes", time.Since(start), slog.String("ip_address", ipAddress))

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, fmt.Sprintf("node with IP %s not found", ipAddress), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to get node by IP", err, slog.String("ip_address", ipAddress))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get node by IP", true, err)
	}

	return r.toDomainNode(&dbNode)
}

// UpdateStatus updates node status with optimistic locking
func (r *nodeRepository) UpdateStatus(ctx context.Context, nodeID string, status node.Status, version int64) error {
	start := time.Now()
	err := r.store.UpdateNodeStatus(ctx, db.UpdateNodeStatusParams{
		Status:  string(status),
		ID:      nodeID,
		Version: version,
	})
	r.logger.DBQuery(ctx, "UpdateNodeStatus", "nodes", time.Since(start), slog.String("node_id", nodeID), slog.String("status", string(status)))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "failed to update node status, concurrent modification or not found", false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to update node status", err, slog.String("node_id", nodeID), slog.Int64("version", version))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update node status", true, err)
	}
	return nil
}

// UpdateHealth updates node health information
func (r *nodeRepository) UpdateHealth(ctx context.Context, nodeID string, health *node.NodeHealthStatus) error {
	handshake := sql.NullTime{Time: health.LastChecked, Valid: true}
	start := time.Now()
	err := r.store.UpdateNodeActivity(ctx, db.UpdateNodeActivityParams{
		LastHandshakeAt:  handshake,
		ConnectedClients: int64(health.ConnectedPeers),
		ID:               nodeID,
	})
	r.logger.DBQuery(ctx, "UpdateNodeActivity", "nodes", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to update node health activity", err, slog.String("node_id", nodeID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update node health activity", true, err)
	}
	return nil
}

// GetUnhealthyNodes retrieves all unhealthy nodes
func (r *nodeRepository) GetUnhealthyNodes(ctx context.Context) ([]*node.Node, error) {
	start := time.Now()
	dbNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusUnhealthy))
	r.logger.DBQuery(ctx, "GetNodesByStatus", "nodes", time.Since(start), slog.String("status", string(node.StatusUnhealthy)))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get unhealthy nodes", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get unhealthy nodes", true, err)
	}

	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to convert db node to domain node", false, err)
		}
		nodes = append(nodes, domainNode)
	}
	return nodes, nil
}

// GetActiveNodes retrieves all active nodes
func (r *nodeRepository) GetActiveNodes(ctx context.Context) ([]*node.Node, error) {
	start := time.Now()
	dbNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusActive))
	r.logger.DBQuery(ctx, "GetNodesByStatus", "nodes", time.Since(start), slog.String("status", string(node.StatusActive)))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get active nodes", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get active nodes", true, err)
	}

	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to convert db node to domain node", false, err)
		}
		nodes = append(nodes, domainNode)
	}
	return nodes, nil
}

// GetNodeCapacity retrieves node capacity information
func (r *nodeRepository) GetNodeCapacity(ctx context.Context, nodeID string) (*node.NodeCapacity, error) {
	n, err := r.GetByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	const maxPeers = 253
	capacity := &node.NodeCapacity{
		NodeID:         n.ID,
		MaxPeers:       maxPeers,
		CurrentPeers:   n.ConnectedClients,
		AvailablePeers: maxPeers - n.ConnectedClients,
		CapacityUsed:   n.CalculateCapacityUsage(maxPeers),
	}
	return capacity, nil
}

// GetStatistics retrieves node statistics
func (r *nodeRepository) GetStatistics(ctx context.Context) (*node.Statistics, error) {
	start := time.Now()
	nodeCount, err := r.store.GetNodeCount(ctx)
	r.logger.DBQuery(ctx, "GetNodeCount", "nodes", time.Since(start))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get node count", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get node count", true, err)
	}

	start = time.Now()
	totalClients, err := r.store.GetTotalConnectedClients(ctx)
	r.logger.DBQuery(ctx, "GetTotalConnectedClients", "nodes", time.Since(start))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get total clients", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get total clients", true, err)
	}

	start = time.Now()
	unhealthyNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusUnhealthy))
	r.logger.DBQuery(ctx, "GetNodesByStatus", "nodes", time.Since(start), slog.String("status", string(node.StatusUnhealthy)))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get unhealthy nodes", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get unhealthy nodes", true, err)
	}

	stats := &node.Statistics{
		TotalNodes:        nodeCount.Total,
		ActiveNodes:       int64(nodeCount.Active.Float64),
		ProvisioningNodes: int64(nodeCount.Provisioning.Float64),
		UnhealthyNodes:    int64(len(unhealthyNodes)),
		DestroyingNodes:   int64(nodeCount.Destroying.Float64),
		TotalPeers:        totalClients.(int64),
	}

	if stats.TotalNodes > 0 {
		stats.AveragePeersPerNode = float64(stats.TotalPeers) / float64(stats.TotalNodes)
	}

	return stats, nil
}

// IncrementVersion increments the node version for optimistic locking
func (r *nodeRepository) IncrementVersion(ctx context.Context, nodeID string) (int64, error) {
	n, err := r.GetByID(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	return n.Version + 1, nil
}

// Helper methods

// toDomainNode converts a database node to a domain node
func (r *nodeRepository) toDomainNode(dbNode *db.Node) (*node.Node, error) {
	n := &node.Node{
		ID:               dbNode.ID,
		ServerID:         dbNode.ServerID.String,
		IPAddress:        dbNode.IpAddress,
		ServerPublicKey:  dbNode.ServerPublicKey,
		Port:             int(dbNode.Port),
		Status:           node.Status(dbNode.Status),
		Version:          dbNode.Version,
		CreatedAt:        dbNode.CreatedAt,
		UpdatedAt:        dbNode.UpdatedAt,
		ConnectedClients: int(dbNode.ConnectedClients),
	}

	if dbNode.DestroyAt.Valid {
		n.DestroyAt = &dbNode.DestroyAt.Time
	}

	if dbNode.LastHandshakeAt.Valid {
		n.LastHandshakeAt = &dbNode.LastHandshakeAt.Time
	}

	return n, nil
}

// applyFilters applies additional filters to nodes
func (r *nodeRepository) applyFilters(nodes []*node.Node, filters node.Filters) []*node.Node {
	if filters.ServerID != nil {
		var filtered []*node.Node
		for _, n := range nodes {
			if n.ServerID == *filters.ServerID {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	if filters.IPAddress != nil {
		var filtered []*node.Node
		for _, n := range nodes {
			if n.IPAddress == *filters.IPAddress {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	if filters.Offset != nil && *filters.Offset > 0 {
		if *filters.Offset >= len(nodes) {
			return []*node.Node{}
		}
		nodes = nodes[*filters.Offset:]
	}

	if filters.Limit != nil && *filters.Limit > 0 && *filters.Limit < len(nodes) {
		nodes = nodes[:*filters.Limit]
	}

	return nodes
}
