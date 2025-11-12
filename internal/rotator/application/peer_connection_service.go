package application

import (
	"context"
	"log/slog"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// PeerConnectionService handles peer connection and disconnection operations
type PeerConnectionService struct {
	nodeService      node.NodeService
	peerService      peer.Service
	ipService        ip.Service
	wireguardManager node.WireGuardManager
	logger           *applogger.Logger
}

// NewPeerConnectionService creates a new peer connection service
func NewPeerConnectionService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	wireguardManager node.WireGuardManager,
	logger *applogger.Logger,
) *PeerConnectionService {
	return &PeerConnectionService{
		nodeService:      nodeService,
		peerService:      peerService,
		ipService:        ipService,
		wireguardManager: wireguardManager,
		logger:           logger.WithComponent("peerconnect.service"),
	}
}

// ConnectPeer connects a new peer by coordinating node selection, IP allocation, and peer creation
func (s *PeerConnectionService) ConnectPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	op := s.logger.StartOp(ctx, "ConnectPeer")
	if req.PublicKey != nil {
		op.With(slog.String("public_key", *req.PublicKey))
	}

	var privateKey *string
	if req.GenerateKeys {
		keyPair, err := crypto.GenerateKeyPair()
		if err != nil {
			err = apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to generate key pair", false, err)
			op.Fail(err, "key generation failed")
			return nil, err
		}
		req.PublicKey = &keyPair.PublicKey
		privateKey = &keyPair.PrivateKey
		op.Progress("keys generated")
	}

	if err := s.validateConnectRequest(req); err != nil {
		op.Fail(err, "invalid connect request")
		return nil, err
	}

	if response, err := s.connectExistingPeer(ctx, req); response != nil {
		op.Complete("existing peer connected")
		return response, nil
	} else if err != nil {
		op.Fail(err, "failed to check existing peer")
		return nil, err
	}

	selectedNode, err := s.selectNode(ctx)
	if err != nil {
		op.Fail(err, "failed to select or create node")
		return nil, err
	}
	op.With(slog.String("node_id", selectedNode.ID))
	op.Progress("node selected")

	if err := s.nodeService.ValidateNodeCapacity(ctx, selectedNode.ID, 1); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeAtCapacity, "node capacity validation failed", false)
		op.Fail(err, "node at capacity")
		return nil, err
	}

	allocatedIP, err := s.ipService.AllocateClientIP(ctx, selectedNode.ID)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainIP, apperrors.ErrCodeIPAllocation, "failed to allocate IP for peer", true)
		op.Fail(err, "ip allocation failed")
		return nil, err
	}
	op.With(slog.String("allocated_ip", allocatedIP.String()))
	op.Progress("ip allocated")

	peerReq := &peer.CreateRequest{
		NodeID:      selectedNode.ID,
		PublicKey:   *req.PublicKey,
		AllocatedIP: allocatedIP.String(),
	}

	createdPeer, err := s.peerService.Create(ctx, peerReq)
	if err != nil {
		if cleanupErr := s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to release IP after peer creation failure", cleanupErr)
		}
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerConflict, "failed to create peer", false)
		op.Fail(err, "peer creation failed")
		return nil, err
	}
	op.With(slog.String("peer_id", createdPeer.ID))

	wgConfig := node.PeerWireGuardConfig{
		PublicKey:  createdPeer.PublicKey,
		AllowedIPs: []string{createdPeer.AllocatedIP + "/32"},
	}

	if err := s.wireguardManager.AddPeer(ctx, selectedNode.IPAddress, wgConfig); err != nil {
		if cleanupErr := s.peerService.Remove(ctx, createdPeer.ID); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to remove peer after infra add failure", cleanupErr)
		}
		if cleanupErr := s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to release IP after infra add failure", cleanupErr)
		}
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "failed to add peer to node infrastructure", true, err)
		op.Fail(err, "failed to add peer to node")
		return nil, err
	}

	nodePublicKey := selectedNode.ServerPublicKey
	if nodePublicKey == "" {
		wgStatus, err := s.wireguardManager.GetWireGuardStatus(ctx, selectedNode.IPAddress)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to get node public key from infrastructure, client config may be incomplete", "error", err.Error())
		} else {
			nodePublicKey = wgStatus.PublicKey
		}
	}

	response := &api.ConnectResponse{
		PeerID:           createdPeer.ID,
		ServerPublicKey:  nodePublicKey,
		ServerIP:         selectedNode.IPAddress,
		ServerPort:       51820,
		ClientIP:         createdPeer.AllocatedIP,
		ClientPrivateKey: privateKey,

		DNS:        []string{"1.1.1.1", "8.8.8.8"},
		AllowedIPs: []string{"0.0.0.0/0"},
	}

	op.Complete("peer connected successfully")
	return response, nil
}

// DisconnectPeer disconnects a peer by removing it from the node and cleaning up resources
func (s *PeerConnectionService) DisconnectPeer(ctx context.Context, peerID string) error {
	op := s.logger.StartOp(ctx, "DisconnectPeer", slog.String("peer_id", peerID))

	existingPeer, err := s.peerService.Get(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer for disconnection")
		return err
	}
	op.With(slog.String("node_id", existingPeer.NodeID))

	selectedNode, err := s.nodeService.GetNode(ctx, existingPeer.NodeID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to get node for peer removal, infrastructure cleanup will be skipped", "error", err.Error())
	} else {
		if err := s.wireguardManager.RemovePeer(ctx, selectedNode.IPAddress, existingPeer.PublicKey); err != nil {
			s.logger.WarnContext(ctx, "failed to remove peer from node infrastructure, continuing", "error", err.Error())
		}
	}

	ip := net.ParseIP(existingPeer.AllocatedIP)
	if ip != nil {
		if err := s.ipService.ReleaseClientIP(ctx, existingPeer.NodeID, ip); err != nil {
			s.logger.WarnContext(ctx, "failed to release IP, continuing", "error", err.Error())
		}
	}

	if err := s.peerService.Remove(ctx, peerID); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerConflict, "failed to remove peer from domain", false)
		op.Fail(err, "failed to remove peer from domain")
		return err
	}

	op.Complete("peer disconnected successfully")
	return nil
}

// ListPeers lists, filters, and paginates active peers
func (s *PeerConnectionService) ListPeers(ctx context.Context, params api.PeerListParams) (*api.PeersListResponse, error) {
	op := s.logger.StartOp(ctx, "ListPeers")

	activeStatus := peer.StatusActive
	filters := &peer.Filters{Status: &activeStatus}

	peers, err := s.peerService.List(ctx, filters)
	if err != nil {
		op.Fail(err, "failed to list active peers")
		return nil, err
	}

	// Filter peers
	filteredPeers := peers
	if params.NodeID != nil {
		var filtered []*peer.Peer
		for _, peer := range peers {
			if peer.NodeID == *params.NodeID {
				filtered = append(filtered, peer)
			}
		}
		filteredPeers = filtered
	}
	// NOTE: Add filtering by status if needed in the future

	// Paginate peers
	offset, limit := 0, len(filteredPeers)
	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = *params.Limit
	}

	start, end := offset, offset+limit
	if start > len(filteredPeers) {
		start = len(filteredPeers)
	}
	if end > len(filteredPeers) {
		end = len(filteredPeers)
	}
	paginatedPeers := filteredPeers[start:end]

	peerInfos := make([]api.PeerInfo, len(paginatedPeers))
	for i, peer := range paginatedPeers {
		peerInfos[i] = api.PeerInfo{
			ID:          peer.ID,
			NodeID:      peer.NodeID,
			PublicKey:   peer.PublicKey,
			AllocatedIP: peer.AllocatedIP,
			Status:      string(peer.Status),
			CreatedAt:   peer.CreatedAt,
		}
	}

	response := &api.PeersListResponse{
		Peers:      peerInfos,
		TotalCount: len(filteredPeers),
		Offset:     offset,
		Limit:      limit,
	}

	op.Complete("listed peers successfully", slog.Int("count", len(peerInfos)))
	return response, nil
}

// GetPeerStatus retrieves the status of a specific peer
func (s *PeerConnectionService) GetPeerStatus(ctx context.Context, peerID string) (*PeerStatus, error) {
	op := s.logger.StartOp(ctx, "GetPeerStatus", slog.String("peer_id", peerID))
	existingPeer, err := s.peerService.Get(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer")
		return nil, err
	}

	status := &PeerStatus{
		PeerID:      existingPeer.ID,
		PublicKey:   existingPeer.PublicKey,
		AllocatedIP: existingPeer.AllocatedIP,
		Status:      string(existingPeer.Status),
		NodeID:      existingPeer.NodeID,
		ConnectedAt: existingPeer.CreatedAt,
		LastSeen:    existingPeer.UpdatedAt,
	}

	op.Complete("retrieved peer status")
	return status, nil
}

// selectNode selects an optimal node or returns error if none are available
func (s *PeerConnectionService) selectNode(ctx context.Context) (*node.Node, error) {
	activeStatus := node.StatusActive
	selectedNode, err := s.nodeService.SelectOptimalNode(ctx, node.Filters{Status: &activeStatus})
	if err == nil {
		return selectedNode, nil
	}

	if apperrors.IsErrorCode(err, apperrors.ErrCodeNodeNotFound) || apperrors.IsErrorCode(err, apperrors.ErrCodeNodeAtCapacity) {
		s.logger.InfoContext(ctx, "no active nodes available, provisioning required")
		return nil, apperrors.NewSystemError(apperrors.ErrCodeNodeNotReady, "No active nodes available, provisioning required. Please try again.", true, err).WithMetadata("estimated_wait_sec", 180).WithMetadata("retry_after_sec", 90)
	}

	return nil, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeInternal, "failed to select node", false)
}

// validateConnectRequest validates the connect request
func (s *PeerConnectionService) validateConnectRequest(req api.ConnectRequest) error {
	if req.PublicKey == nil || *req.PublicKey == "" {
		if !req.GenerateKeys {
			return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "public key is required or generate_keys must be true", false, nil)
		}
	}

	if req.PublicKey != nil && !crypto.IsValidWireGuardKey(*req.PublicKey) {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "invalid WireGuard public key format", false, nil)
	}

	return nil
}

func (s *PeerConnectionService) connectExistingPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	if req.PublicKey == nil {
		return nil, nil
	}
	existingPeer, err := s.peerService.GetByPublicKey(ctx, *req.PublicKey)
	if err != nil {
		if apperrors.IsErrorCode(err, apperrors.ErrCodePeerNotFound) {
			return nil, nil
		}
		return nil, apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to check existing peer", true)
	}

	n, err := s.nodeService.GetNode(ctx, existingPeer.NodeID)
	if err != nil {
		return nil, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to get node for existing peer", false)
	}

	return &api.ConnectResponse{
		PeerID:          existingPeer.ID,
		ServerPublicKey: n.ServerPublicKey,
		ServerIP:        n.IPAddress,
		ServerPort:      n.Port,
		ClientIP:        existingPeer.AllocatedIP,
		DNS:             []string{"1.1.1.1", "8.8.8.8"},
		AllowedIPs:      []string{"0.0.0.0/0"},
	}, nil
}
