package application

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// PeerConnectionService handles peer connection and disconnection operations
type PeerConnectionService struct {
	nodeService      node.NodeService
	peerService      peer.Service
	ipService        ip.Service
	wireguardManager nodeinteractor.WireGuardManager
	logger           *slog.Logger
}

// NewPeerConnectionService creates a new peer connection service
func NewPeerConnectionService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	wireguardManager nodeinteractor.WireGuardManager,
	logger *slog.Logger,
) *PeerConnectionService {
	return &PeerConnectionService{
		nodeService:      nodeService,
		peerService:      peerService,
		ipService:        ipService,
		wireguardManager: wireguardManager,
		logger:           logger,
	}
}

// ConnectPeer connects a new peer by coordinating node selection, IP allocation, and peer creation
func (s *PeerConnectionService) ConnectPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	publicKey := ""
	if req.PublicKey != nil {
		publicKey = *req.PublicKey
	}

	s.logger.Info("connecting peer", slog.String("public_key", publicKey))

	// Validate request
	if err := s.validateConnectRequest(req); err != nil {
		return nil, fmt.Errorf("invalid connect request: %w", err)
	}

	if response, err := s.connectExistingPeer(ctx, req); err == nil && response != nil {
		s.logger.Info("peer already exists, returning existing connection",
			slog.String("peer_id", response.PeerID),
			slog.String("server_ip", response.ServerIP),
			slog.String("client_ip", response.ClientIP))
		return response, nil
	}

	// Select or create optimal node for the peer
	selectedNode, err := s.selectOrCreateNode(ctx)
	if err != nil {
		// Check if this is a provisioning required error
		if provErr, ok := err.(*ProvisioningRequiredError); ok {
			s.logger.Info("node provisioning required for peer connection",
				slog.String("public_key", publicKey),
				slog.String("message", provErr.Message),
				slog.Int("estimated_wait", provErr.EstimatedWait))

			// Return the provisioning error directly - the caller should handle this
			return nil, provErr
		}
		return nil, fmt.Errorf("failed to select or create node: %w", err)
	}

	s.logger.Debug("selected node for peer",
		slog.String("node_id", selectedNode.ID),
		slog.String("node_ip", selectedNode.IPAddress))

	// Validate node capacity
	if err := s.nodeService.ValidateNodeCapacity(ctx, selectedNode.ID, 1); err != nil {
		return nil, fmt.Errorf("node capacity validation failed: %w", err)
	}

	// Allocate IP address for the peer
	allocatedIP, err := s.ipService.AllocateClientIP(ctx, selectedNode.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate IP for peer: %w", err)
	}

	s.logger.Debug("allocated IP for peer",
		slog.String("ip", allocatedIP.String()),
		slog.String("node_id", selectedNode.ID))

	// Create peer in domain
	peerReq := &peer.CreateRequest{
		NodeID:      selectedNode.ID,
		PublicKey:   publicKey,
		AllocatedIP: allocatedIP.String(),
	}

	createdPeer, err := s.peerService.Create(ctx, peerReq)
	if err != nil {
		// Cleanup allocated IP on failure
		_ = s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP)
		return nil, fmt.Errorf("failed to create peer: %w", err)
	}

	// Add peer to node via nodeInteractor (direct infrastructure call)
	wgConfig := nodeinteractor.PeerWireGuardConfig{
		PublicKey:  createdPeer.PublicKey,
		AllowedIPs: []string{createdPeer.AllocatedIP + "/32"},
	}

	if err := s.wireguardManager.AddPeer(ctx, selectedNode.IPAddress, wgConfig); err != nil {
		// Cleanup peer and IP on failure
		_ = s.peerService.Remove(ctx, createdPeer.ID)
		_ = s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP)
		return nil, fmt.Errorf("failed to add peer to node infrastructure: %w", err)
	}

	// Get node public key for client configuration via nodeInteractor
	nodePublicKey := selectedNode.ServerPublicKey // From database
	if nodePublicKey == "" {
		// Fallback: query from node if not in database
		wgStatus, err := s.wireguardManager.GetWireGuardStatus(ctx, selectedNode.IPAddress)
		if err != nil {
			s.logger.Warn("failed to get node public key from infrastructure",
				slog.String("node_id", selectedNode.ID),
				slog.String("error", err.Error()))
		} else {
			nodePublicKey = wgStatus.PublicKey
		}
	}

	// Build response
	response := &api.ConnectResponse{
		PeerID:          createdPeer.ID,
		ServerPublicKey: nodePublicKey,
		ServerIP:        selectedNode.IPAddress,
		ServerPort:      51820, // Default WireGuard port
		ClientIP:        createdPeer.AllocatedIP,
		DNS:             []string{"1.1.1.1", "8.8.8.8"},
		AllowedIPs:      []string{"0.0.0.0/0"},
	}

	s.logger.Info("peer connected successfully",
		slog.String("peer_id", createdPeer.ID),
		slog.String("node_id", selectedNode.ID),
		slog.String("allocated_ip", createdPeer.AllocatedIP))

	return response, nil
}

// DisconnectPeer disconnects a peer by removing it from the node and cleaning up resources
func (s *PeerConnectionService) DisconnectPeer(ctx context.Context, peerID string) error {
	s.logger.Info("disconnecting peer", slog.String("peer_id", peerID))

	// Get peer information
	existingPeer, err := s.peerService.Get(ctx, peerID)
	if err != nil {
		return fmt.Errorf("failed to get peer: %w", err)
	}

	// Get node to access its IP address
	selectedNode, err := s.nodeService.GetNode(ctx, existingPeer.NodeID)
	if err != nil {
		s.logger.Warn("failed to get node for peer removal",
			slog.String("peer_id", peerID),
			slog.String("node_id", existingPeer.NodeID),
			slog.String("error", err.Error()))
	} else {
		// Remove peer from node via nodeInteractor (direct infrastructure call)
		if err := s.wireguardManager.RemovePeer(ctx, selectedNode.IPAddress, existingPeer.PublicKey); err != nil {
			s.logger.Warn("failed to remove peer from node infrastructure",
				slog.String("peer_id", peerID),
				slog.String("node_id", existingPeer.NodeID),
				slog.String("error", err.Error()))
		}
	}

	// Release IP address
	ip := net.ParseIP(existingPeer.AllocatedIP)
	if ip != nil {
		if err := s.ipService.ReleaseClientIP(ctx, existingPeer.NodeID, ip); err != nil {
			s.logger.Warn("failed to release IP",
				slog.String("peer_id", peerID),
				slog.String("ip", existingPeer.AllocatedIP),
				slog.String("error", err.Error()))
		}
	}

	// Remove peer from domain
	if err := s.peerService.Remove(ctx, peerID); err != nil {
		return fmt.Errorf("failed to remove peer: %w", err)
	}

	s.logger.Info("peer disconnected successfully", slog.String("peer_id", peerID))
	return nil
}

// selectOrCreateNode selects an optimal node or triggers async provisioning if none are available
func (s *PeerConnectionService) selectOrCreateNode(ctx context.Context) (*node.Node, error) {
	// Try to select an existing active node
	activeStatus := node.StatusActive
	selectedNode, err := s.nodeService.SelectOptimalNode(ctx, node.Filters{
		Status: &activeStatus,
	})

	// If we have an active node, use it
	if err == nil {
		return selectedNode, nil
	}

	// If no active nodes available, return a special error that indicates provisioning is needed
	if err == node.ErrNodeNotFound || err == node.ErrNodeAtCapacity {
		s.logger.Info("no active nodes available, provisioning required")

		// Return a special error that the VPN orchestrator can handle
		// The VPN orchestrator will decide whether to use event-driven or synchronous provisioning
		return nil, &ProvisioningRequiredError{
			Message:       "No active nodes available, provisioning required",
			EstimatedWait: 180, // 3 minutes default estimate
			RetryAfter:    90,  // Suggest retry in 1.5 minutes
		}
	}

	return nil, fmt.Errorf("failed to select node: %w", err)
}

// validateConnectRequest validates the connect request
func (s *PeerConnectionService) validateConnectRequest(req api.ConnectRequest) error {
	if req.PublicKey == nil || *req.PublicKey == "" {
		if !req.GenerateKeys {
			return fmt.Errorf("public key is required or generate_keys must be true")
		}
	}

	if !crypto.IsValidWireGuardKey(*req.PublicKey) {
		return fmt.Errorf("invalid WireGuard public key format")
	}

	// TODO: Add WireGuard public key format validation
	// TODO: Add key generation logic if req.GenerateKeys is true

	return nil
}

func (s *PeerConnectionService) connectExistingPeer(ctx context.Context, req api.ConnectRequest) (*api.ConnectResponse, error) {
	// Check if a peer with the given public key already exists
	existingPeer, err := s.peerService.GetByPublicKey(ctx, *req.PublicKey)
	if err != nil {
		if err == peer.ErrPeerNotFound {
			return nil, nil // Peer does not exist
		}
		return nil, fmt.Errorf("failed to check existing peer: %w", err)
	}

	// Get node information
	n, err := s.nodeService.GetNode(ctx, existingPeer.NodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node for existing peer: %w", err)
	}
	// Build response for existing peer
	response := &api.ConnectResponse{
		PeerID:          existingPeer.ID,
		ServerPublicKey: n.ServerPublicKey,
		ServerIP:        n.IPAddress,
		ServerPort:      n.Port, // Default WireGuard port
		ClientIP:        existingPeer.AllocatedIP,
		DNS:             []string{"1.1.1.1", "8.8.8.8"},
		AllowedIPs:      []string{"0.0.0.0/0"},
	}
	return response, nil
}
