package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

// nodeRepository implements node.NodeRepository using db.Store
type nodeRepository struct {
	store db.Store
}

// NewNodeRepository creates a new node repository
func NewNodeRepository(store db.Store) node.NodeRepository {
	return &nodeRepository{
		store: store,
	}
}

// Create creates a new node in the database
func (r *nodeRepository) Create(ctx context.Context, n *node.Node) error {
	params := db.CreateNodeParams{
		ID:              n.ID,
		IpAddress:       n.IPAddress,
		ServerPublicKey: n.ServerPublicKey,
		Port:            int64(n.Port),
		Status:          string(n.Status),
	}

	dbNode, err := r.store.CreateNode(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create node in database: %w", err)
	}

	// Update the node with database-generated fields
	n.CreatedAt = dbNode.CreatedAt
	n.UpdatedAt = dbNode.UpdatedAt
	n.Version = dbNode.Version

	return nil
}

// GetByID retrieves a node by ID
func (r *nodeRepository) GetByID(ctx context.Context, nodeID string) (*node.Node, error) {
	dbNode, err := r.store.GetNode(ctx, nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, node.ErrNodeNotFound
		}
		return nil, fmt.Errorf("failed to get node from database: %w", err)
	}

	return r.toDomainNode(&dbNode)
}

// Update updates an existing node
func (r *nodeRepository) Update(ctx context.Context, n *node.Node) error {
	// Update status with optimistic locking
	err := r.store.UpdateNodeStatus(ctx, db.UpdateNodeStatusParams{
		Status:  string(n.Status),
		ID:      n.ID,
		Version: n.Version - 1, // Version was already incremented in domain
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return node.ErrConcurrentModification
		}
		return fmt.Errorf("failed to update node: %w", err)
	}

	// Update activity if needed
	if n.LastHandshakeAt != nil || n.ConnectedClients > 0 {
		handshake := sql.NullTime{}
		if n.LastHandshakeAt != nil {
			handshake = sql.NullTime{
				Time:  *n.LastHandshakeAt,
				Valid: true,
			}
		}

		err = r.store.UpdateNodeActivity(ctx, db.UpdateNodeActivityParams{
			LastHandshakeAt:  handshake,
			ConnectedClients: int64(n.ConnectedClients),
			ID:               n.ID,
		})
		if err != nil {
			return fmt.Errorf("failed to update node activity: %w", err)
		}
	}

	return nil
}

// Delete deletes a node
func (r *nodeRepository) Delete(ctx context.Context, nodeID string) error {
	err := r.store.DeleteNode(ctx, nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return node.ErrNodeNotFound
		}
		return fmt.Errorf("failed to delete node: %w", err)
	}

	return nil
}

// List lists nodes with filters
func (r *nodeRepository) List(ctx context.Context, filters node.Filters) ([]*node.Node, error) {
	var dbNodes []db.Node
	var err error

	if filters.Status != nil {
		dbNodes, err = r.store.GetNodesByStatus(ctx, string(*filters.Status))
	} else {
		dbNodes, err = r.store.GetAllNodes(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Convert to domain nodes
	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert node: %w", err)
		}
		nodes = append(nodes, domainNode)
	}

	// Apply additional filters
	nodes = r.applyFilters(nodes, filters)

	return nodes, nil
}

// GetByServerID retrieves a node by server ID
func (r *nodeRepository) GetByServerID(ctx context.Context, serverID string) (*node.Node, error) {
	// Get all nodes and filter by server ID (no direct query available)
	dbNodes, err := r.store.GetAllNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}

	for i := range dbNodes {
		if dbNodes[i].ServerID.Valid && dbNodes[i].ServerID.String == serverID {
			return r.toDomainNode(&dbNodes[i])
		}
	}

	return nil, node.ErrNodeNotFound
}

// GetByIPAddress retrieves a node by IP address
func (r *nodeRepository) GetByIPAddress(ctx context.Context, ipAddress string) (*node.Node, error) {
	dbNode, err := r.store.GetNodeByIP(ctx, ipAddress)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, node.ErrNodeNotFound
		}
		return nil, fmt.Errorf("failed to get node by IP: %w", err)
	}

	return r.toDomainNode(&dbNode)
}

// UpdateStatus updates node status with optimistic locking
func (r *nodeRepository) UpdateStatus(ctx context.Context, nodeID string, status node.Status, version int64) error {
	err := r.store.UpdateNodeStatus(ctx, db.UpdateNodeStatusParams{
		Status:  string(status),
		ID:      nodeID,
		Version: version,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return node.ErrConcurrentModification
		}
		return fmt.Errorf("failed to update node status: %w", err)
	}

	return nil
}

// UpdateHealth updates node health information
func (r *nodeRepository) UpdateHealth(ctx context.Context, nodeID string, health *node.Health) error {
	// Update activity with connected peers count
	handshake := sql.NullTime{
		Time:  health.LastChecked,
		Valid: true,
	}

	err := r.store.UpdateNodeActivity(ctx, db.UpdateNodeActivityParams{
		LastHandshakeAt:  handshake,
		ConnectedClients: int64(health.ConnectedPeers),
		ID:               nodeID,
	})
	if err != nil {
		return fmt.Errorf("failed to update node health: %w", err)
	}

	return nil
}

// GetUnhealthyNodes retrieves all unhealthy nodes
func (r *nodeRepository) GetUnhealthyNodes(ctx context.Context) ([]*node.Node, error) {
	// Get nodes by unhealthy status
	dbNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusUnhealthy))
	if err != nil {
		return nil, fmt.Errorf("failed to get unhealthy nodes: %w", err)
	}

	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert node: %w", err)
		}
		nodes = append(nodes, domainNode)
	}

	return nodes, nil
}

// GetActiveNodes retrieves all active nodes
func (r *nodeRepository) GetActiveNodes(ctx context.Context) ([]*node.Node, error) {
	// Get nodes by active status
	dbNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusActive))
	if err != nil {
		return nil, fmt.Errorf("failed to get active nodes: %w", err)
	}

	nodes := make([]*node.Node, 0, len(dbNodes))
	for i := range dbNodes {
		domainNode, err := r.toDomainNode(&dbNodes[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert node: %w", err)
		}
		nodes = append(nodes, domainNode)
	}

	return nodes, nil
}

// GetNodeCapacity retrieves node capacity information
func (r *nodeRepository) GetNodeCapacity(ctx context.Context, nodeID string) (*node.NodeCapacity, error) {
	// Get node to extract capacity info
	n, err := r.GetByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// Assume max 253 peers per node (based on /24 subnet minus server IP)
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
	// Get node count
	nodeCount, err := r.store.GetNodeCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node count: %w", err)
	}

	// Get total connected clients
	totalClients, err := r.store.GetTotalConnectedClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get total clients: %w", err)
	}

	// Get unhealthy nodes count
	unhealthyNodes, err := r.store.GetNodesByStatus(ctx, string(node.StatusUnhealthy))
	if err != nil {
		return nil, fmt.Errorf("failed to get unhealthy nodes: %w", err)
	}

	stats := &node.Statistics{
		TotalNodes:        nodeCount.Total,
		ActiveNodes:       int64(nodeCount.Active.Float64),
		ProvisioningNodes: int64(nodeCount.Provisioning.Float64),
		UnhealthyNodes:    int64(len(unhealthyNodes)),
		DestroyingNodes:   int64(nodeCount.Destroying.Float64),
		TotalPeers:        totalClients.(int64),
	}

	// Calculate average peers per node
	if stats.TotalNodes > 0 {
		stats.AveragePeersPerNode = float64(stats.TotalPeers) / float64(stats.TotalNodes)
	}

	return stats, nil
}

// IncrementVersion increments the node version for optimistic locking
func (r *nodeRepository) IncrementVersion(ctx context.Context, nodeID string) (int64, error) {
	// Get current node
	n, err := r.GetByID(ctx, nodeID)
	if err != nil {
		return 0, err
	}

	return n.Version + 1, nil
}

// WithTx executes a function within a transaction
func (r *nodeRepository) WithTx(ctx context.Context, fn func(node.NodeRepository) error) error {
	// Check if store supports transactions
	txStore, ok := r.store.(interface {
		BeginTx(ctx context.Context) (db.Store, error)
		Commit() error
		Rollback() error
	})
	if !ok {
		// If no transaction support, execute directly
		return fn(r)
	}

	// Begin transaction
	tx, err := txStore.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create repository with transaction store
	txRepo := &nodeRepository{store: tx}

	// Execute function
	if err := fn(txRepo); err != nil {
		if rbErr := txStore.Rollback(); rbErr != nil {
			return fmt.Errorf("failed to rollback transaction after error %v: %w", err, rbErr)
		}
		return err
	}

	// Commit transaction
	if err := txStore.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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
	// Apply server ID filter
	if filters.ServerID != nil {
		var filtered []*node.Node
		for _, n := range nodes {
			if n.ServerID == *filters.ServerID {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	// Apply IP address filter
	if filters.IPAddress != nil {
		var filtered []*node.Node
		for _, n := range nodes {
			if n.IPAddress == *filters.IPAddress {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	// Apply offset
	if filters.Offset != nil && *filters.Offset > 0 {
		if *filters.Offset >= len(nodes) {
			return []*node.Node{}
		}
		nodes = nodes[*filters.Offset:]
	}

	// Apply limit
	if filters.Limit != nil && *filters.Limit > 0 && *filters.Limit < len(nodes) {
		nodes = nodes[:*filters.Limit]
	}

	return nodes
}
