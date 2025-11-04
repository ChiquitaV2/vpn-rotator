package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// VPNOrchestratorService orchestrates VPN operations by coordinating specialized services
type VPNOrchestratorService struct {
	peerConnectionService   *PeerConnectionService
	nodeRotationService     *NodeRotationService
	resourceCleanupService  *ResourceCleanupService
	nodeProvisioningService *NodeProvisioningService
	peerService             peer.Service
	logger                  *slog.Logger
}

// NewVPNOrchestratorService creates a new VPN orchestrator service
func NewVPNOrchestratorService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	nodeProvisioningService *NodeProvisioningService,
	logger *slog.Logger,
) VPNService {
	return &VPNOrchestratorService{
		peerConnectionService:   NewPeerConnectionService(nodeService, peerService, ipService, nodeProvisioningService, logger),
		nodeRotationService:     NewNodeRotationService(nodeService, peerService, ipService, nodeProvisioningService, logger),
		resourceCleanupService:  NewResourceCleanupService(nodeService, peerService, ipService, logger),
		nodeProvisioningService: nodeProvisioningService,
		peerService:             peerService,
		logger:                  logger,
	}
}

// ConnectPeer connects a new peer by delegating to the peer connection service
func (s *VPNOrchestratorService) ConnectPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	return s.peerConnectionService.ConnectPeer(ctx, req)
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
func (s *VPNOrchestratorService) RotateNodes(ctx context.Context) error {
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
