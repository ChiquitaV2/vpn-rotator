package services

import (
	"context"
	"log/slog"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/protocol"
)

// PeerConnectionService handles peer connection and disconnection operations
type PeerConnectionService struct {
	nodeService      node.NodeService
	peerService      peer.Service
	ipService        ip.Service
	protoManager     protocol.Manager
	eventPublisher   *events.EventPublisher
	progressReporter events.PeerConnectionProgressReporter
	stateTracker     *peer.PeerConnectionStateTracker
	logger           *applogger.Logger
}

// NewPeerConnectionService creates a new peer connection service
func NewPeerConnectionService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	protoManager protocol.Manager,
	eventPublisher *events.EventPublisher,
	stateTracker *peer.PeerConnectionStateTracker,
	logger *applogger.Logger,
) *PeerConnectionService {
	progressReporter := events.NewEventBasedPeerConnectionProgressReporter(eventPublisher.Peer, logger)
	return &PeerConnectionService{
		nodeService:      nodeService,
		peerService:      peerService,
		ipService:        ipService,
		protoManager:     protoManager,
		eventPublisher:   eventPublisher,
		progressReporter: progressReporter,
		stateTracker:     stateTracker,
		logger:           logger.WithComponent("peerconnect.service"),
	}
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
		identifier := existingPeer.Identifier
		if identifier == "" {
			identifier = existingPeer.PublicKey
		}
		if err := s.protoManager.RemovePeer(ctx, selectedNode.IPAddress, identifier); err != nil {
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
func (s *PeerConnectionService) ListPeers(ctx context.Context, params PeerListParams) (*PeersListResponse, error) {
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

	peerInfos := make([]PeerInfo, len(paginatedPeers))
	for i, peer := range paginatedPeers {
		peerInfos[i] = PeerInfo{
			ID:          peer.ID,
			NodeID:      peer.NodeID,
			PublicKey:   peer.PublicKey,
			AllocatedIP: peer.AllocatedIP,
			Status:      string(peer.Status),
			CreatedAt:   peer.CreatedAt,
		}
	}

	response := &PeersListResponse{
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

// GetConnectionStatus retrieves the status of an async peer connection request
func (s *PeerConnectionService) GetConnectionStatus(ctx context.Context, requestID string) (*peer.PeerConnectionState, error) {
	op := s.logger.StartOp(ctx, "GetConnectionStatus", slog.String("request_id", requestID))

	state := s.stateTracker.GetConnectionState(requestID)
	if state == nil {
		err := apperrors.NewDomainAPIError(apperrors.ErrCodePeerNotFound, "connection request not found", false, nil).
			WithMetadata("request_id", requestID)
		op.Fail(err, "connection request not found")
		return nil, err
	}

	op.Complete("connection status retrieved successfully")
	return state, nil
}

// validateConnectRequest validates the connect request
func (s *PeerConnectionService) validateConnectRequest(req ConnectRequest) error {
	// Default protocol
	protocolName := "wireguard"
	if req.Protocol != nil && *req.Protocol != "" {
		protocolName = *req.Protocol
	}

	// Currently only WireGuard is supported in async flow
	if protocolName != "wireguard" && protocolName != "wg" && protocolName != "WireGuard" {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "unsupported protocol (only wireguard supported currently)", false, nil).
			WithMetadata("protocol", protocolName)
	}

	// Derive identifier for WG (public key)
	var id string
	if req.Identifier != nil && *req.Identifier != "" {
		id = *req.Identifier
	} else if req.PublicKey != nil && *req.PublicKey != "" {
		id = *req.PublicKey
	}

	// Either we have a key/identifier or we must generate
	if id == "" && !req.GenerateKeys {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "identifier/public_key is required or generate_keys must be true", false, nil)
	}

	// Validate WireGuard key format if present
	if id != "" && !crypto.IsValidWireGuardKey(id) {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "invalid WireGuard public key format", false, nil)
	}
	return nil
}
