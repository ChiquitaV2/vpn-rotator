package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// VPNOrchestratorService orchestrates VPN operations by coordinating specialized services
type VPNOrchestratorService struct {
	peerConnectionService    *PeerConnectionService
	nodeRotationService      *NodeRotationService
	resourceCleanupService   *ResourceCleanupService
	provisioningOrchestrator *ProvisioningOrchestrator
	peerService              peer.Service
	logger                   *slog.Logger
}

// NewVPNOrchestratorService creates a new VPN orchestrator service with unified provisioning
// The orchestrator coordinates peer connection, node rotation, and resource cleanup services
func NewVPNOrchestratorService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	nodeInteractor nodeinteractor.NodeInteractor,
	provisioningOrchestrator *ProvisioningOrchestrator,
	logger *slog.Logger,
) VPNService {
	// NodeInteractor implements WireGuardManager, so we can pass it directly
	return &VPNOrchestratorService{
		peerConnectionService:    NewPeerConnectionService(nodeService, peerService, ipService, nodeInteractor, logger),
		nodeRotationService:      NewNodeRotationService(nodeService, peerService, ipService, nodeInteractor, provisioningOrchestrator, logger),
		resourceCleanupService:   NewResourceCleanupService(nodeService, peerService, ipService, logger),
		provisioningOrchestrator: provisioningOrchestrator,
		peerService:              peerService,
		logger:                   logger,
	}
}

// ConnectPeer connects a new peer by delegating to the peer connection service
// with automatic event-driven provisioning when needed
func (s *VPNOrchestratorService) ConnectPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	response, err := s.peerConnectionService.ConnectPeer(ctx, req)
	if err == nil {
		return response, nil
	}

	// Check if provisioning is required
	provErr, ok := err.(*ProvisioningRequiredError)
	if !ok {
		// Not a provisioning error, return as-is
		return nil, err
	}

	// Provisioning is required - trigger async provisioning via event bus
	return s.handleProvisioningRequired(ctx, provErr)
}

// handleProvisioningRequired handles provisioning requirements by triggering async provisioning
func (s *VPNOrchestratorService) handleProvisioningRequired(ctx context.Context, provErr *ProvisioningRequiredError) (*api.ConnectResponse, error) {
	s.logger.Info("provisioning required for peer connection")

	// Check if provisioning orchestrator is available
	if s.provisioningOrchestrator == nil {
		s.logger.Info("provisioning orchestrator not available, returning error")
		return nil, provErr
	}

	// Check if provisioning is already in progress
	if s.provisioningOrchestrator.IsProvisioning() {
		status := s.provisioningOrchestrator.GetCurrentStatus()
		estimatedWait := int(s.provisioningOrchestrator.GetEstimatedWaitTime().Seconds())

		s.logger.Info("provisioning already in progress",
			slog.String("phase", status.Phase),
			slog.Float64("progress", status.Progress*100))

		return nil, &ProvisioningRequiredError{
			Message:       fmt.Sprintf("Node provisioning in progress (phase: %s, progress: %.1f%%)", status.Phase, status.Progress*100),
			EstimatedWait: estimatedWait,
			RetryAfter:    estimatedWait / 2,
		}
	}

	// Trigger async provisioning
	if err := s.provisioningOrchestrator.ProvisionNodeAsync(ctx); err != nil {
		s.logger.Error("failed to trigger async provisioning",
			slog.String("error", err.Error()))
		return nil, provErr
	}

	s.logger.Info("async provisioning triggered")

	return nil, &ProvisioningRequiredError{
		Message:       "Node provisioning has been triggered, please retry in a few moments",
		EstimatedWait: 180, // 3 minutes default
		RetryAfter:    90,  // Suggest retry in 1.5 minutes
	}
}

// DisconnectPeer disconnects a peer by delegating to the peer connection service
func (s *VPNOrchestratorService) DisconnectPeer(ctx context.Context, peerID string) error {
	return s.peerConnectionService.DisconnectPeer(ctx, peerID)
}

// GetPeerStatus retrieves the status of a specific peer
func (s *VPNOrchestratorService) GetPeerStatus(ctx context.Context, peerID string) (*PeerStatus, error) {
	// Get peer from domain service
	existingPeer, err := s.peerService.Get(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer: %w", err)
	}

	// Build status response
	status := &PeerStatus{
		PeerID:      existingPeer.ID,
		PublicKey:   existingPeer.PublicKey,
		AllocatedIP: existingPeer.AllocatedIP,
		Status:      string(existingPeer.Status),
		NodeID:      existingPeer.NodeID,
		ConnectedAt: existingPeer.CreatedAt,
		LastSeen:    existingPeer.UpdatedAt,
	}

	return status, nil
}

// ListActivePeers retrieves all active peers in the system
func (s *VPNOrchestratorService) ListActivePeers(ctx context.Context) ([]*api.PeerInfo, error) {
	// Get all active peers
	activeStatus := peer.StatusActive
	filters := &peer.Filters{
		Status: &activeStatus,
	}

	peers, err := s.peerService.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list active peers: %w", err)
	}

	// Convert to API PeerInfo
	result := make([]*api.PeerInfo, 0, len(peers))
	for _, p := range peers {
		peerInfo := &api.PeerInfo{
			ID:          p.ID,
			NodeID:      p.NodeID,
			PublicKey:   p.PublicKey,
			AllocatedIP: p.AllocatedIP,
			Status:      string(p.Status),
			CreatedAt:   p.CreatedAt,
		}

		result = append(result, peerInfo)
	}

	s.logger.Debug("listed active peers", slog.Int("count", len(result)))
	return result, nil
}

// RotateNodes performs node rotation by delegating to the node rotation service
// The rotation service handles provisioning decisions and coordination internally
func (s *VPNOrchestratorService) RotateNodes(ctx context.Context) error {
	s.logger.Info("starting rotation cycle")

	// Check if provisioning is already in progress - if so, defer rotation
	if s.provisioningOrchestrator != nil && s.provisioningOrchestrator.IsProvisioning() {
		status := s.provisioningOrchestrator.GetCurrentStatus()
		waitTime := s.provisioningOrchestrator.GetEstimatedWaitTime()

		s.logger.Info("provisioning in progress, deferring rotation",
			slog.String("phase", status.Phase),
			slog.Float64("progress", status.Progress*100),
			slog.Duration("estimated_wait", waitTime))
		return nil
	}

	// Delegate to rotation service which handles all provisioning logic
	return s.nodeRotationService.RotateNodes(ctx)
}

// CleanupInactiveResources cleans up inactive peers and orphaned resources
func (s *VPNOrchestratorService) CleanupInactiveResources(ctx context.Context) error {
	return s.resourceCleanupService.CleanupInactiveResources(ctx)
}

// CleanupInactiveResourcesWithOptions performs comprehensive cleanup with configurable options
func (s *VPNOrchestratorService) CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error {
	return s.resourceCleanupService.CleanupInactiveResourcesWithOptions(ctx, options)
}

// MigratePeersFromNode migrates all peers from source node to target node
func (s *VPNOrchestratorService) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	return s.nodeRotationService.MigratePeersFromNode(ctx, sourceNodeID, targetNodeID)
}
