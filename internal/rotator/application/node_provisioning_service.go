package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

// NodeProvisioningService handles node provisioning operations at the application layer
type NodeProvisioningService struct {
	provisioningService *node.ProvisioningService
	nodeService         node.NodeService
	logger              *slog.Logger
}

// NewNodeProvisioningService creates a new node provisioning service
func NewNodeProvisioningService(
	provisioningService *node.ProvisioningService,
	nodeService node.NodeService,
	logger *slog.Logger,
) *NodeProvisioningService {
	return &NodeProvisioningService{
		provisioningService: provisioningService,
		nodeService:         nodeService,
		logger:              logger,
	}
}

// ProvisionNode provisions a new node with coordinated resource allocation
func (s *NodeProvisioningService) ProvisionNode(ctx context.Context) (*node.Node, error) {
	s.logger.Info("starting coordinated node provisioning")

	// Use the domain ProvisioningService for coordinated provisioning
	newNode, err := s.provisioningService.ProvisionNode(ctx)
	if err != nil {
		return nil, fmt.Errorf("coordinated provisioning failed: %w", err)
	}

	s.logger.Info("node provisioning completed successfully",
		slog.String("node_id", newNode.ID),
		slog.String("node_ip", newNode.IPAddress))

	return newNode, nil
}

// DestroyNode destroys a node with coordinated cleanup
func (s *NodeProvisioningService) DestroyNode(ctx context.Context, nodeID string) error {
	s.logger.Info("starting coordinated node destruction", slog.String("node_id", nodeID))

	// Use the domain ProvisioningService for coordinated destruction
	err := s.provisioningService.DestroyNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("coordinated destruction failed: %w", err)
	}

	s.logger.Info("node destruction completed successfully", slog.String("node_id", nodeID))
	return nil
}

// EnsureMinimumNodes ensures there's at least one active node available
func (s *NodeProvisioningService) EnsureMinimumNodes(ctx context.Context, minNodes int) error {
	s.logger.Debug("checking minimum node requirements", slog.Int("min_nodes", minNodes))

	// Get current active nodes
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return fmt.Errorf("failed to list active nodes: %w", err)
	}

	currentCount := len(activeNodes)
	if currentCount >= minNodes {
		s.logger.Debug("minimum node requirement satisfied",
			slog.Int("current", currentCount),
			slog.Int("required", minNodes))
		return nil
	}

	needed := minNodes - currentCount
	s.logger.Info("provisioning additional nodes to meet minimum requirement",
		slog.Int("current", currentCount),
		slog.Int("required", minNodes),
		slog.Int("needed", needed))

	// Provision needed nodes
	for i := 0; i < needed; i++ {
		_, err := s.ProvisionNode(ctx)
		if err != nil {
			s.logger.Error("failed to provision node for minimum requirement",
				slog.Int("attempt", i+1),
				slog.String("error", err.Error()))
			// Continue trying to provision other nodes
			continue
		}

		s.logger.Info("provisioned node for minimum requirement",
			slog.Int("provisioned", i+1),
			slog.Int("remaining", needed-i-1))
	}

	return nil
}

// GetProvisioningCapabilities returns information about provisioning capabilities
func (s *NodeProvisioningService) GetProvisioningCapabilities(ctx context.Context) (*ProvisioningCapabilities, error) {
	// Get current node statistics
	stats, err := s.nodeService.GetNodeStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node statistics: %w", err)
	}

	capabilities := &ProvisioningCapabilities{
		MaxNodesSupported:        100, // This should come from configuration
		CurrentActiveNodes:       int(stats.ActiveNodes),
		CurrentProvisioningNodes: int(stats.ProvisioningNodes),
		CanProvisionMore:         stats.ActiveNodes < 100, // Based on limits
		EstimatedProvisionTime:   "5-10 minutes",          // This could be calculated based on history
	}

	return capabilities, nil
}

// ProvisioningCapabilities represents the current provisioning capabilities
type ProvisioningCapabilities struct {
	MaxNodesSupported        int    `json:"max_nodes_supported"`
	CurrentActiveNodes       int    `json:"current_active_nodes"`
	CurrentProvisioningNodes int    `json:"current_provisioning_nodes"`
	CanProvisionMore         bool   `json:"can_provision_more"`
	EstimatedProvisionTime   string `json:"estimated_provision_time"`
}
