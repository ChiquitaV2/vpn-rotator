package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// VPNOrchestratorService orchestrates VPN operations by coordinating specialized services
type VPNOrchestratorService struct {
	peerConnectionService  *PeerConnectionService
	nodeRotationService    *NodeRotationService
	resourceCleanupService *ResourceCleanupService
	provisioningService    *ProvisioningService
	peerService            peer.Service
	logger                 *applogger.Logger
}

// NewVPNOrchestratorService creates a new VPN orchestrator service with unified provisioning
func NewVPNOrchestratorService(
	peerConnectionService *PeerConnectionService,
	nodeRotationService *NodeRotationService,
	resourceCleanupService *ResourceCleanupService,
	provisioningService *ProvisioningService,
	peerService peer.Service,
	logger *applogger.Logger,
) VPNService {
	serviceLogger := logger.WithComponent("vpn.orchestrator")
	return &VPNOrchestratorService{
		peerConnectionService:  peerConnectionService,
		nodeRotationService:    nodeRotationService,
		resourceCleanupService: resourceCleanupService,
		provisioningService:    provisioningService,
		peerService:            peerService,
		logger:                 serviceLogger,
	}
}

// ConnectPeer connects a new peer by delegating to the peer connection service
func (s *VPNOrchestratorService) ConnectPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	op := s.logger.StartOp(ctx, "ConnectPeer")
	response, err := s.peerConnectionService.ConnectPeer(ctx, req)
	if err != nil {
		if apperrors.IsErrorCode(err, apperrors.ErrCodeNodeNotReady) {
			op.Fail(err, "provisioning required")
			return s.handleProvisioningRequired(ctx, err.(apperrors.DomainError))
		}
		op.Fail(err, "unhandled error in connect peer")
		return nil, err
	}
	op.Complete("connect peer successful")
	return response, nil
}

// handleProvisioningRequired handles provisioning requirements by triggering async provisioning
func (s *VPNOrchestratorService) handleProvisioningRequired(ctx context.Context, provErr apperrors.DomainError) (*api.ConnectResponse, error) {
	s.logger.InfoContext(ctx, "provisioning required for peer connection")

	if s.provisioningService == nil {
		s.logger.InfoContext(ctx, "provisioning service not available, returning error")
		return nil, provErr
	}

	if s.provisioningService.IsProvisioning() {
		status := s.provisioningService.GetCurrentStatus()
		estimatedWait := int(s.provisioningService.GetEstimatedWaitTime().Seconds())
		s.logger.InfoContext(ctx, "provisioning already in progress", "phase", status.Phase, "progress", status.Progress*100)
		return nil, apperrors.NewSystemError(apperrors.ErrCodeNodeNotReady, fmt.Sprintf("Node provisioning in progress (phase: %s, progress: %.1f%%)", status.Phase, status.Progress*100), true, provErr).WithMetadata("estimated_wait_sec", estimatedWait).WithMetadata("retry_after_sec", estimatedWait/2)
	}

	if err := s.provisioningService.ProvisionNodeAsync(ctx); err != nil {
		s.logger.ErrorCtx(ctx, "failed to trigger async provisioning", err)
		return nil, provErr
	}

	s.logger.InfoContext(ctx, "async provisioning triggered")
	return nil, apperrors.NewSystemError(apperrors.ErrCodeNodeNotReady, "Node provisioning has been triggered, please retry in a few moments", true, provErr).WithMetadata("estimated_wait_sec", 180).WithMetadata("retry_after_sec", 90)
}

// DisconnectPeer disconnects a peer by delegating to the peer connection service
func (s *VPNOrchestratorService) DisconnectPeer(ctx context.Context, peerID string) error {
	op := s.logger.StartOp(ctx, "DisconnectPeer", slog.String("peer_id", peerID))
	if err := s.peerConnectionService.DisconnectPeer(ctx, peerID); err != nil {
		op.Fail(err, "failed to disconnect peer")
		return err
	}
	op.Complete("peer disconnected successfully")
	return nil
}

// GetPeerStatus retrieves the status of a specific peer
func (s *VPNOrchestratorService) GetPeerStatus(ctx context.Context, peerID string) (*PeerStatus, error) {
	op := s.logger.StartOp(ctx, "GetPeerStatus", slog.String("peer_id", peerID))
	status, err := s.peerConnectionService.GetPeerStatus(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer")
		return nil, err
	}
	op.Complete("retrieved peer status")
	return status, nil
}

// ListActivePeers retrieves all active peers in the system
func (s *VPNOrchestratorService) ListActivePeers(ctx context.Context) ([]*api.PeerInfo, error) {
	op := s.logger.StartOp(ctx, "ListActivePeers")
	activeStatus := peer.StatusActive
	filters := &peer.Filters{Status: &activeStatus}

	peers, err := s.peerService.List(ctx, filters)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to list active peers", true)
		op.Fail(err, "failed to list active peers")
		return nil, err
	}

	result := make([]*api.PeerInfo, 0, len(peers))
	for _, p := range peers {
		result = append(result, &api.PeerInfo{
			ID:          p.ID,
			NodeID:      p.NodeID,
			PublicKey:   p.PublicKey,
			AllocatedIP: p.AllocatedIP,
			Status:      string(p.Status),
			CreatedAt:   p.CreatedAt,
		})
	}

	op.Complete("listed active peers", slog.Int("count", len(result)))
	return result, nil
}

// RotateNodes performs node rotation by delegating to the node rotation service
func (s *VPNOrchestratorService) RotateNodes(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "RotateNodes")
	if s.provisioningService != nil && s.provisioningService.IsProvisioning() {
		op.Complete("rotation deferred due to active provisioning")
		return nil
	}

	if err := s.nodeRotationService.RotateNodes(ctx); err != nil {
		op.Fail(err, "rotation cycle failed")
		return err
	}

	op.Complete("rotation cycle finished")
	return nil
}

// CleanupInactiveResources cleans up inactive peers and orphaned resources
func (s *VPNOrchestratorService) CleanupInactiveResources(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "CleanupInactiveResources")
	if err := s.resourceCleanupService.CleanupInactiveResources(ctx); err != nil {
		op.Fail(err, "scheduled cleanup failed")
		return err
	}
	op.Complete("scheduled cleanup finished")
	return nil
}

// CleanupInactiveResourcesWithOptions performs comprehensive cleanup with configurable options
func (s *VPNOrchestratorService) CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error {
	return s.resourceCleanupService.CleanupInactiveResourcesWithOptions(ctx, options)
}

// MigratePeersFromNode migrates all peers from source node to target node
func (s *VPNOrchestratorService) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	return s.nodeRotationService.MigratePeersFromNode(ctx, sourceNodeID, targetNodeID)
}
