package node

import (
	"context"
	"log/slog"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// Service implements the NodeService interface with domain business logic
type Service struct {
	repository       NodeRepository
	cloudProvisioner CloudProvisioner
	healthChecker    HealthChecker
	wireguardManager WireGuardManager
	logger           *applogger.Logger // <-- Changed
	config           ServiceConfig
}

// ServiceConfig contains configuration for the node service
type ServiceConfig struct {
	MaxPeersPerNode      int           `json:"max_peers_per_node"`
	CapacityThreshold    float64       `json:"capacity_threshold"` // Percentage (e.g., 80.0)
	HealthCheckTimeout   time.Duration `json:"health_check_timeout"`
	ProvisioningTimeout  time.Duration `json:"provisioning_timeout"`
	DestructionTimeout   time.Duration `json:"destruction_timeout"`
	OptimalNodeSelection string        `json:"optimal_node_selection"` // "least_loaded", "round_robin", "random"
}

// NewService creates a new node domain service
func NewService(
	repository NodeRepository,
	cloudProvisioner CloudProvisioner,
	healthChecker HealthChecker,
	wireguardManager WireGuardManager,
	logger *applogger.Logger, // <-- Changed
	config ServiceConfig,
) *Service {
	return &Service{
		repository:       repository,
		cloudProvisioner: cloudProvisioner,
		healthChecker:    healthChecker,
		wireguardManager: wireguardManager,
		logger:           logger.WithComponent("node.service"), // <-- Scoped logger
		config:           config,
	}
}

// DestroyNode destroys a node with proper cleanup and validation
func (s *Service) DestroyNode(ctx context.Context, nodeID string) error {
	op := s.logger.StartOp(ctx, "DestroyNode", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return err // Repo returns DomainError
	}

	// Validate node can be destroyed
	if err := node.ValidateForDestruction(); err != nil {
		s.logger.ErrorCtx(ctx, "node validation failed", err)
		op.Fail(err, "node validation failed")
		return err // Model returns DomainError
	}

	// Update status to destroying to prevent race conditions
	if err := node.UpdateStatus(StatusDestroying); err != nil {
		s.logger.ErrorCtx(ctx, "failed to update node status", err)
		op.Fail(err, "failed to update node status")
		return err // Model returns DomainError
	}

	if err := s.repository.Update(ctx, node); err != nil {
		s.logger.ErrorCtx(ctx, "failed to save destroying status", err)
		op.Fail(err, "failed to save destroying status")
		return err // Repo returns DomainError
	}

	// Destroy cloud infrastructure if server ID exists
	if node.ServerID != "" {
		if err := s.cloudProvisioner.DestroyNode(ctx, node.ServerID); err != nil {
			// This is a major error, but we must continue to delete the DB record.
			s.logger.ErrorCtx(ctx, "failed to destroy cloud infrastructure, continuing with db delete", err,
				slog.String("server_id", node.ServerID))
			// Do not return yet, must delete DB record.
		}
	}

	// Remove from repository
	if err := s.repository.Delete(ctx, nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "failed to delete node from repository", err)
		op.Fail(err, "failed to delete node from repository")
		return err // Repo returns DomainError
	}

	op.Complete("node destroyed successfully")
	return nil
}

// CheckNodeHealth checks node health using NodeInteractor
func (s *Service) CheckNodeHealth(ctx context.Context, nodeID string) (*NodeHealthStatus, error) {
	op := s.logger.StartOp(ctx, "CheckNodeHealth", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return nil, err
	}

	// Use NodeInteractor for health check
	healthStatus, err := s.healthChecker.CheckNodeHealth(ctx, node.IPAddress)
	if err != nil {
		domainErr := apperrors.NewInfrastructureError(apperrors.ErrCodeHealthCheckFailed, "comprehensive health check failed", true, err).
			WithMetadata("node_id", nodeID)
		s.logger.ErrorCtx(ctx, "health check interactor failed", domainErr)
		op.Fail(domainErr, "health check interactor failed")
		return nil, domainErr
	}

	// Convert NodeInteractor health status to domain NodeHealthStatus
	health := &NodeHealthStatus{
		IsHealthy:       healthStatus.IsHealthy,
		ResponseTime:    healthStatus.ResponseTime,
		SystemLoad:      healthStatus.SystemLoad,
		MemoryUsage:     healthStatus.MemoryUsage,
		DiskUsage:       healthStatus.DiskUsage,
		WireGuardStatus: healthStatus.WireGuardStatus,
		ConnectedPeers:  healthStatus.ConnectedPeers,
		LastChecked:     healthStatus.LastChecked,
		Errors:          healthStatus.Errors,
	}

	// Update node status based on health
	var newStatus Status
	if health.IsHealthy {
		newStatus = StatusActive
	} else {
		newStatus = StatusUnhealthy
	}

	if node.Status != newStatus {
		if err := s.UpdateNodeStatus(ctx, nodeID, newStatus); err != nil {
			// Log as error, but don't fail the health check itself
			s.logger.ErrorCtx(ctx, "failed to update node status based on health", err,
				slog.String("new_status", string(newStatus)))
		}
	}

	op.Complete("node health check completed", slog.Bool("is_healthy", health.IsHealthy))
	return health, nil
}

// UpdateNodeStatus updates node status with validation
func (s *Service) UpdateNodeStatus(ctx context.Context, nodeID string, status Status) error {
	op := s.logger.StartOp(ctx, "UpdateNodeStatus",
		slog.String("node_id", nodeID),
		slog.String("new_status", status.String()))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return err
	}

	// Update status with validation
	if err := node.UpdateStatus(status); err != nil {
		s.logger.ErrorCtx(ctx, "failed to update node status", err)
		op.Fail(err, "failed to update node status")
		return err
	}

	// Save to repository
	if err := s.repository.Update(ctx, node); err != nil {
		s.logger.ErrorCtx(ctx, "failed to save node", err)
		op.Fail(err, "failed to save node")
		return err
	}

	op.Complete("node status updated successfully")
	return nil
}

// ValidateNodeCapacity validates if node can accept additional peers
func (s *Service) ValidateNodeCapacity(ctx context.Context, nodeID string, additionalPeers int) error {
	op := s.logger.StartOp(ctx, "ValidateNodeCapacity",
		slog.String("node_id", nodeID),
		slog.Int("additional_peers", additionalPeers))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return err
	}

	// Validate capacity using node entity
	if err := node.ValidateCapacity(additionalPeers, s.config.MaxPeersPerNode); err != nil {
		s.logger.ErrorCtx(ctx, "node capacity validation failed", err)
		op.Fail(err, "node capacity validation failed")
		return err // Returns DomainError
	}

	// Check capacity threshold
	if node.IsOverCapacityThreshold(s.config.MaxPeersPerNode, s.config.CapacityThreshold) {
		err := apperrors.NewNodeError(apperrors.ErrCodeNodeAtCapacity, "node is over capacity threshold", true, nil).
			WithMetadata("node_id", nodeID).
			WithMetadata("current", node.ConnectedClients).
			WithMetadata("threshold_pct", s.config.CapacityThreshold)
		s.logger.WarnContext(ctx, "node over capacity threshold", slog.String("node_id", nodeID))
		op.Fail(err, "node over capacity threshold")
		return err
	}

	op.Complete("node capacity validation completed")
	return nil
}

// SelectOptimalNode selects the best node based on configured strategy
func (s *Service) SelectOptimalNode(ctx context.Context, filters Filters) (*Node, error) {
	op := s.logger.StartOp(ctx, "SelectOptimalNode",
		slog.String("strategy", s.config.OptimalNodeSelection))

	// Get active nodes
	activeFilters := filters
	activeStatus := StatusActive
	activeFilters.Status = &activeStatus

	nodes, err := s.repository.List(ctx, activeFilters)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to list nodes", err)
		op.Fail(err, "failed to list nodes")
		return nil, err
	}

	if len(nodes) == 0 {
		err := apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, "no active nodes found", true, nil)
		s.logger.WarnContext(ctx, "no active nodes found to select from")
		op.Fail(err, "no active nodes found to select from")
		return nil, err
	}

	// Filter nodes that can accept peers
	var availableNodes []*Node
	for _, node := range nodes {
		if err := s.ValidateNodeCapacity(ctx, node.ID, 1); err == nil {
			availableNodes = append(availableNodes, node)
		} else {
			s.logger.WarnContext(ctx, "node skipped (at capacity)", slog.String("node_id", node.ID))
		}
	}

	if len(availableNodes) == 0 {
		err := apperrors.NewNodeError(apperrors.ErrCodeNodeAtCapacity, "no available nodes with capacity", true, nil)
		s.logger.WarnContext(ctx, "no active nodes found with available capacity")
		op.Fail(err, "no available nodes found with available capacity")
		return nil, err
	}

	// Select based on strategy
	var selectedNode *Node
	switch s.config.OptimalNodeSelection {
	case "least_loaded":
		selectedNode = s.selectLeastLoadedNode(availableNodes)
	case "round_robin":
		selectedNode = s.selectRoundRobinNode(availableNodes)
	case "random":
		selectedNode = s.selectRandomNode(availableNodes)
	default:
		selectedNode = s.selectLeastLoadedNode(availableNodes)
	}

	op.Complete("node selected successfully", slog.String("selected_node_id", selectedNode.ID))
	return selectedNode, nil
}

// GetNodePublicKey retrieves node's WireGuard public key using NodeInteractor
func (s *Service) GetNodePublicKey(ctx context.Context, nodeID string) (string, error) {
	op := s.logger.StartOp(ctx, "GetNodePublicKey", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return "", err
	}

	// Get WireGuard status using NodeInteractor
	wgStatus, err := s.wireguardManager.GetWireGuardStatus(ctx, node.IPAddress)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainInfrastructure, "wg_status_failed", "failed to get WireGuard status", true)
		s.logger.ErrorCtx(ctx, "failed to get wg status", domainErr)
		op.Fail(domainErr, "failed to get wg status")
		return "", domainErr
	}

	op.Complete("node public key retrieved successfully")
	return wgStatus.PublicKey, nil
}

// GetNodeStatistics retrieves node statistics from repository
func (s *Service) GetNodeStatistics(ctx context.Context) (*Statistics, error) {
	op := s.logger.StartOp(ctx, "GetNodeStatistics")

	stats, err := s.repository.GetStatistics(ctx)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get statistics", err)
		op.Fail(err, "failed to get statistics")
		return nil, err // Repo returns DomainError
	}

	op.Complete("node statistics retrieved successfully")
	return stats, nil
}

// ListNodes lists nodes with filters
func (s *Service) ListNodes(ctx context.Context, filters Filters) ([]*Node, error) {
	op := s.logger.StartOp(ctx, "ListNodes")

	nodes, err := s.repository.List(ctx, filters)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to list nodes", err)
		op.Fail(err, "failed to list nodes")
		return nil, err // Repo returns DomainError
	}

	op.Complete("nodes listed successfully", slog.Int("count", len(nodes)))
	return nodes, nil
}

// GetNode retrieves a single node by ID
func (s *Service) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	op := s.logger.StartOp(ctx, "GetNode", slog.String("node_id", nodeID))

	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return nil, err // Repo returns DomainError
	}

	op.Complete("node retrieved successfully")
	return node, nil
}

// Helper methods

// checkNodeConflicts checks for server ID and IP address conflicts
func (s *Service) checkNodeConflicts(ctx context.Context, node *Node) error {
	// Check server ID conflict
	if existingNode, err := s.repository.GetByServerID(ctx, node.ServerID); err == nil && existingNode != nil {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "server ID already in use", false, nil).
			WithMetadata("server_id", node.ServerID)
	}

	// Check IP address conflict
	if existingNode, err := s.repository.GetByIPAddress(ctx, node.IPAddress); err == nil && existingNode != nil {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "IP address already in use", false, nil).
			WithMetadata("ip_address", node.IPAddress)
	}

	return nil
}

// selectLeastLoadedNode selects the node with the lowest peer count
func (s *Service) selectLeastLoadedNode(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	leastLoaded := nodes[0]
	for _, node := range nodes[1:] {
		if node.ConnectedClients < leastLoaded.ConnectedClients {
			leastLoaded = node
		}
	}

	return leastLoaded
}

// selectRoundRobinNode selects node using round-robin (simplified implementation)
func (s *Service) selectRoundRobinNode(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	// Simple round-robin based on current time
	index := int(time.Now().UnixNano()) % len(nodes)
	return nodes[index]
}

// selectRandomNode selects a random node
func (s *Service) selectRandomNode(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	// Simple random selection based on current time
	index := int(time.Now().UnixNano()) % len(nodes)
	return nodes[index]
}
