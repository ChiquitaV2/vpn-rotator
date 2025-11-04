package node

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
)

// Service implements the NodeService interface with domain business logic
type Service struct {
	repository       NodeRepository
	cloudProvisioner CloudProvisioner
	healthChecker    nodeinteractor.HealthChecker
	wireguardManager nodeinteractor.WireGuardManager
	logger           *slog.Logger
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
	healthChecker nodeinteractor.HealthChecker,
	wireguardManager nodeinteractor.WireGuardManager,
	logger *slog.Logger,
	config ServiceConfig,
) *Service {
	return &Service{
		repository:       repository,
		cloudProvisioner: cloudProvisioner,
		healthChecker:    healthChecker,
		wireguardManager: wireguardManager,
		logger:           logger,
		config:           config,
	}
}

// DestroyNode destroys a node with proper cleanup and validation
func (s *Service) DestroyNode(ctx context.Context, nodeID string) error {
	s.logger.Info("destroying node", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Validate node can be destroyed
	if err := node.ValidateForDestruction(); err != nil {
		return fmt.Errorf("node validation failed: %w", err)
	}

	// Update status to destroying to prevent race conditions
	if err := node.UpdateStatus(StatusDestroying); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	if err := s.repository.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	// Note: The application layer is responsible for ensuring peers are migrated
	// before calling DestroyNode. The domain service assumes this precondition is met.

	// Destroy cloud infrastructure if server ID exists
	if node.ServerID != "" {
		if err := s.cloudProvisioner.DestroyNode(ctx, node.ServerID); err != nil {
			s.logger.Error("failed to destroy cloud infrastructure",
				slog.String("node_id", nodeID),
				slog.String("server_id", node.ServerID),
				slog.String("error", err.Error()))
			// Continue with database cleanup even if cloud destruction fails
		}
	}

	// Remove from repository
	if err := s.repository.Delete(ctx, nodeID); err != nil {
		return fmt.Errorf("failed to delete node from repository: %w", err)
	}

	s.logger.Info("successfully destroyed node", slog.String("node_id", nodeID))
	return nil
}

// CheckNodeHealth checks node health using NodeInteractor
func (s *Service) CheckNodeHealth(ctx context.Context, nodeID string) (*Health, error) {
	s.logger.Debug("checking node health", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Use NodeInteractor for health check
	healthStatus, err := s.healthChecker.CheckNodeHealth(ctx, node.IPAddress)
	if err != nil {
		return nil, NewHealthCheckError(nodeID, "comprehensive", err)
	}

	// Convert NodeInteractor health status to domain Health
	health := &Health{
		NodeID:          nodeID,
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
			s.logger.Warn("failed to update node status based on health",
				slog.String("node_id", nodeID),
				slog.String("new_status", string(newStatus)),
				slog.String("error", err.Error()))
		}
	}

	return health, nil
}

// UpdateNodeStatus updates node status with validation
func (s *Service) UpdateNodeStatus(ctx context.Context, nodeID string, status Status) error {
	s.logger.Debug("updating node status",
		slog.String("node_id", nodeID),
		slog.String("status", string(status)))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Update status with validation
	if err := node.UpdateStatus(status); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	// Save to repository
	if err := s.repository.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to save node: %w", err)
	}

	s.logger.Info("successfully updated node status",
		slog.String("node_id", nodeID),
		slog.String("status", string(status)))

	return nil
}

// ValidateNodeCapacity validates if node can accept additional peers
func (s *Service) ValidateNodeCapacity(ctx context.Context, nodeID string, additionalPeers int) error {
	s.logger.Debug("validating node capacity",
		slog.String("node_id", nodeID),
		slog.Int("additional_peers", additionalPeers))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Validate capacity using node entity
	if err := node.ValidateCapacity(additionalPeers, s.config.MaxPeersPerNode); err != nil {
		return err
	}

	// Check capacity threshold
	if node.IsOverCapacityThreshold(s.config.MaxPeersPerNode, s.config.CapacityThreshold) {
		return NewCapacityError(nodeID, node.ConnectedClients, s.config.MaxPeersPerNode, additionalPeers, true, ErrNodeAtCapacity)
	}

	return nil
}

// SelectOptimalNode selects the best node based on configured strategy
func (s *Service) SelectOptimalNode(ctx context.Context, filters Filters) (*Node, error) {
	s.logger.Debug("selecting optimal node", slog.String("strategy", s.config.OptimalNodeSelection))

	// Get active nodes
	activeFilters := filters
	activeStatus := StatusActive
	activeFilters.Status = &activeStatus

	nodes, err := s.repository.List(ctx, activeFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, ErrNodeNotFound
	}

	// Filter nodes that can accept peers
	var availableNodes []*Node
	for _, node := range nodes {
		if err := s.ValidateNodeCapacity(ctx, node.ID, 1); err == nil {
			availableNodes = append(availableNodes, node)
		}
	}

	if len(availableNodes) == 0 {
		return nil, ErrNodeAtCapacity
	}

	// Select based on strategy
	switch s.config.OptimalNodeSelection {
	case "least_loaded":
		return s.selectLeastLoadedNode(availableNodes), nil
	case "round_robin":
		return s.selectRoundRobinNode(availableNodes), nil
	case "random":
		return s.selectRandomNode(availableNodes), nil
	default:
		return s.selectLeastLoadedNode(availableNodes), nil
	}
}

// GetNodePublicKey retrieves node's WireGuard public key using NodeInteractor
func (s *Service) GetNodePublicKey(ctx context.Context, nodeID string) (string, error) {
	s.logger.Debug("getting node public key", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to get node: %w", err)
	}

	// Get WireGuard status using NodeInteractor
	wgStatus, err := s.wireguardManager.GetWireGuardStatus(ctx, node.IPAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get WireGuard status: %w", err)
	}

	return wgStatus.PublicKey, nil
}

// GetNodeStatistics retrieves node statistics from repository
func (s *Service) GetNodeStatistics(ctx context.Context) (*Statistics, error) {
	s.logger.Debug("getting node statistics")

	stats, err := s.repository.GetStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}

	return stats, nil
}

// ListNodes lists nodes with filters
func (s *Service) ListNodes(ctx context.Context, filters Filters) ([]*Node, error) {
	s.logger.Debug("listing nodes")

	nodes, err := s.repository.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodes, nil
}

// GetNode retrieves a single node by ID
func (s *Service) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	s.logger.Debug("getting node", slog.String("node_id", nodeID))

	node, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	return node, nil
}

// Helper methods

// checkNodeConflicts checks for server ID and IP address conflicts
func (s *Service) checkNodeConflicts(ctx context.Context, node *Node) error {
	// Check server ID conflict
	if existingNode, err := s.repository.GetByServerID(ctx, node.ServerID); err == nil && existingNode != nil {
		return NewConflictError("server_id", node.ServerID, "server ID already in use")
	}

	// Check IP address conflict
	if existingNode, err := s.repository.GetByIPAddress(ctx, node.IPAddress); err == nil && existingNode != nil {
		return NewConflictError("ip_address", node.IPAddress, "IP address already in use")
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
