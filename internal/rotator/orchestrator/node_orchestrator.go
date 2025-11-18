package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// RotationStatus represents the current state of node rotation
type RotationStatus struct {
	InProgress        bool
	LastRotation      time.Time
	NodesRotated      int
	PeersMigrated     int
	RotationReason    string
	EstimatedComplete time.Time
}

// NodeOrchestrator handles node rotation and migration operations
type NodeOrchestrator interface {
	// Node rotation operations
	RotateNodes(ctx context.Context) error
	ForceRotation(ctx context.Context) error
	GetRotationStatus(ctx context.Context) (*RotationStatus, error)

	// Peer migration operations
	MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error

	// Provisioning status operations
	IsProvisioning() bool
	GetProvisioningStatus(ctx context.Context) (*node.ProvisioningState, error)
}

// NodeOrchestratorImpl orchestrates node rotation and migration
type NodeOrchestratorImpl struct {
	nodeRotationService *services.NodeRotationService
	nodeService         node.NodeService
	nodeStateTracker    *node.ProvisioningStateTracker
	logger              *applogger.Logger
}

// NewNodeOrchestrator creates a new node orchestrator
func NewNodeOrchestrator(
	nodeRotationService *services.NodeRotationService,
	nodeService node.NodeService,
	nodeStateTracker *node.ProvisioningStateTracker,
	logger *applogger.Logger,
) NodeOrchestrator {
	serviceLogger := logger.WithComponent("node.orchestrator")
	return &NodeOrchestratorImpl{
		nodeRotationService: nodeRotationService,
		nodeService:         nodeService,
		nodeStateTracker:    nodeStateTracker,
		logger:              serviceLogger,
	}
}

// RotateNodes performs node rotation by delegating to the node rotation service
func (n *NodeOrchestratorImpl) RotateNodes(ctx context.Context) error {
	op := n.logger.StartOp(ctx, "RotateNodes")

	if err := n.nodeRotationService.RotateNodes(ctx); err != nil {
		op.Fail(err, "rotation cycle failed")
		return err
	}

	op.Complete("rotation cycle finished")
	return nil
}

// ForceRotation forces an immediate node rotation (manual trigger)
func (n *NodeOrchestratorImpl) ForceRotation(ctx context.Context) error {
	op := n.logger.StartOp(ctx, "ForceRotation", slog.String("trigger", "manual"))

	if err := n.nodeRotationService.RotateNodes(ctx); err != nil {
		op.Fail(err, "forced rotation failed")
		return err
	}

	op.Complete("forced rotation initiated")
	return nil
}

// GetRotationStatus retrieves the current rotation status
func (n *NodeOrchestratorImpl) GetRotationStatus(ctx context.Context) (*RotationStatus, error) {
	op := n.logger.StartOp(ctx, "GetRotationStatus")

	provisioningStatus := node.StatusProvisioning
	provisioningNodes, err := n.nodeService.ListNodes(ctx, node.Filters{Status: &provisioningStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to check provisioning nodes", true)
		op.Fail(err, "failed to check provisioning nodes")
		return nil, err
	}

	status := &RotationStatus{
		InProgress:    len(provisioningNodes) > 0,
		LastRotation:  time.Now().Add(-24 * time.Hour),
		NodesRotated:  0,
		PeersMigrated: 0,
	}

	if status.InProgress {
		status.RotationReason = "rotation in progress"
		status.EstimatedComplete = time.Now().Add(10 * time.Minute)
	}

	op.Complete("retrieved rotation status")
	return status, nil
}

// MigratePeersFromNode migrates all peers from source node to target node
func (n *NodeOrchestratorImpl) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	return n.nodeRotationService.MigratePeersFromNode(ctx, sourceNodeID, targetNodeID)
}

// IsProvisioning returns whether provisioning is currently active
func (n *NodeOrchestratorImpl) IsProvisioning() bool {
	return n.nodeStateTracker.IsProvisioning()
}

// GetProvisioningStatus retrieves the current provisioning status
func (n *NodeOrchestratorImpl) GetProvisioningStatus(ctx context.Context) (*node.ProvisioningState, error) {
	op := n.logger.StartOp(ctx, "GetProvisioningStatus")

	status := n.nodeStateTracker.GetActiveProvisioning()
	if status == nil {
		op.Complete("no active provisioning")
		return nil, nil
	}

	op.Complete("retrieved provisioning status")
	return status, nil
}
