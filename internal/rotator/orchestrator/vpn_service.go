package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// VPNService defines the application layer interface for VPN operations
type VPNService interface {
	// Peer connection operations (service-layer types)
	ConnectPeer(ctx context.Context, req services.ConnectRequest) (*services.ConnectResponse, error)
	DisconnectPeer(ctx context.Context, peerID string) error

	// Peer status and information operations
	GetPeerStatus(ctx context.Context, peerID string) (*services.PeerStatus, error)
	ListActivePeers(ctx context.Context) ([]services.PeerInfo, error)
	ListPeers(ctx context.Context, params services.PeerListParams) (*services.PeersListResponse, error)

	// Node rotation operations
	RotateNodes(ctx context.Context) error

	// Resource cleanup operations
	CleanupInactiveResources(ctx context.Context) error
	CleanupInactiveResourcesWithOptions(ctx context.Context, options services.CleanupOptions) error

	// Peer migration operations
	MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error

	// Provisioning status operations
	IsProvisioning() bool
	GetProvisioningStatus(ctx context.Context) (*events.ProvisioningNodeState, error)
}

// VPNServiceImpl orchestrates VPN operations by coordinating specialized services
type VPNServiceImpl struct {
	peerConnectionService  *services.PeerConnectionService
	nodeRotationService    *services.NodeRotationService
	resourceCleanupService *services.ResourceCleanupService
	provisioningService    *services.ProvisioningService
	peerService            peer.Service
	logger                 *applogger.Logger
}

// NewVPNService creates a new VPN orchestrator service with unified provisioning
func NewVPNService(
	peerConnectionService *services.PeerConnectionService,
	nodeRotationService *services.NodeRotationService,
	resourceCleanupService *services.ResourceCleanupService,
	provisioningService *services.ProvisioningService,
	peerService peer.Service,
	logger *applogger.Logger,
) VPNService {
	serviceLogger := logger.WithComponent("peer.lifecycle.orchestrator")
	return &VPNServiceImpl{
		peerConnectionService:  peerConnectionService,
		nodeRotationService:    nodeRotationService,
		resourceCleanupService: resourceCleanupService,
		provisioningService:    provisioningService,
		peerService:            peerService,
		logger:                 serviceLogger,
	}
}

// ConnectPeer connects a new peer by delegating to the peer connection service
func (s *VPNServiceImpl) ConnectPeer(ctx context.Context, req services.ConnectRequest) (*services.ConnectResponse, error) {
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
func (s *VPNServiceImpl) handleProvisioningRequired(ctx context.Context, provErr apperrors.DomainError) (*services.ConnectResponse, error) {
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
func (s *VPNServiceImpl) DisconnectPeer(ctx context.Context, peerID string) error {
	op := s.logger.StartOp(ctx, "DisconnectPeer", slog.String("peer_id", peerID))
	if err := s.peerConnectionService.DisconnectPeer(ctx, peerID); err != nil {
		op.Fail(err, "failed to disconnect peer")
		return err
	}
	op.Complete("peer disconnected successfully")
	return nil
}

// GetPeerStatus retrieves status for a specific peer
func (v *VPNServiceImpl) GetPeerStatus(ctx context.Context, peerID string) (*services.PeerStatus, error) {
	return v.peerConnectionService.GetPeerStatus(ctx, peerID)
}

// ListActivePeers retrieves all active peers in the system
func (s *VPNServiceImpl) ListActivePeers(ctx context.Context) ([]services.PeerInfo, error) {
	op := s.logger.StartOp(ctx, "ListActivePeers")
	activeStatus := peer.StatusActive
	filters := &peer.Filters{Status: &activeStatus}

	peers, err := s.peerService.List(ctx, filters)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to list active peers", true)
		op.Fail(err, "failed to list active peers")
		return nil, err
	}

	result := make([]services.PeerInfo, 0, len(peers))
	for _, p := range peers {
		result = append(result, services.PeerInfo{
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
func (s *VPNServiceImpl) RotateNodes(ctx context.Context) error {
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
func (s *VPNServiceImpl) CleanupInactiveResources(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "CleanupInactiveResources")
	if err := s.resourceCleanupService.CleanupInactiveResources(ctx); err != nil {
		op.Fail(err, "scheduled cleanup failed")
		return err
	}
	op.Complete("scheduled cleanup finished")
	return nil
}

// CleanupInactiveResourcesWithOptions performs comprehensive cleanup with configurable options
func (s *VPNServiceImpl) CleanupInactiveResourcesWithOptions(ctx context.Context, options services.CleanupOptions) error {
	return s.resourceCleanupService.CleanupInactiveResourcesWithOptions(ctx, options)
}

// MigratePeersFromNode migrates all peers from source node to target node
func (s *VPNServiceImpl) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	return s.nodeRotationService.MigratePeersFromNode(ctx, sourceNodeID, targetNodeID)
}

// ListPeers retrieves peers with filtering and pagination
func (s *VPNServiceImpl) ListPeers(ctx context.Context, params services.PeerListParams) (*services.PeersListResponse, error) {
	op := s.logger.StartOp(ctx, "ListPeers")
	peers, err := s.peerConnectionService.ListPeers(ctx, params)
	if err != nil {
		op.Fail(err, "failed to list peers")
		return nil, err
	}

	op.Complete("listed peers successfully", slog.Int("count", len(peers.Peers)))
	return peers, nil
}

// Health operations removed from VPNServiceImpl – handled by dedicated HealthService

// Provisioning status operations

// IsProvisioning returns whether provisioning is currently active
func (s *VPNServiceImpl) IsProvisioning() bool {
	if s.provisioningService == nil {
		return false
	}
	return s.provisioningService.IsProvisioning()
}

// GetProvisioningStatus retrieves the current provisioning status
func (s *VPNServiceImpl) GetProvisioningStatus(ctx context.Context) (*events.ProvisioningNodeState, error) {
	op := s.logger.StartOp(ctx, "GetProvisioningStatus")

	if s.provisioningService == nil {
		err := apperrors.NewSystemError(apperrors.ErrCodeInternal, "provisioning service is not available", false, nil)
		op.Fail(err, "provisioning service not available")
		return nil, err
	}

	status := s.provisioningService.GetCurrentStatus()
	if status == nil {
		err := apperrors.NewSystemError(apperrors.ErrCodeInternal, "no provisioning status available", false, nil)
		op.Fail(err, "no provisioning status available")
		return nil, err
	}

	op.Complete("retrieved provisioning status")
	return status, nil
}
