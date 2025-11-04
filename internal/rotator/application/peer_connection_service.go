package application

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// PeerConnectionService handles peer connection and disconnection operations
type PeerConnectionService struct {
	nodeService             node.NodeService
	peerService             peer.Service
	ipService               ip.Service
	nodeProvisioningService *NodeProvisioningService
	logger                  *slog.Logger
}

// NewPeerConnectionService creates a new peer connection service
func NewPeerConnectionService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	nodeProvisioningService *NodeProvisioningService,
	logger *slog.Logger,
) *PeerConnectionService {
	return &PeerConnectionService{
		nodeService:             nodeService,
		peerService:             peerService,
		ipService:               ipService,
		nodeProvisioningService: nodeProvisioningService,
		logger:                  logger,
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

	// Select or create optimal node for the peer
	selectedNode, err := s.selectOrCreateNode(ctx)
	if err != nil {
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

	// Add peer to node via node service
	peerConfig := node.PeerConfig{
		ID:          createdPeer.ID,
		PublicKey:   createdPeer.PublicKey,
		AllocatedIP: createdPeer.AllocatedIP,
		AllowedIPs:  []string{createdPeer.AllocatedIP + "/32"},
	}

	if err := s.nodeService.AddPeerToNode(ctx, selectedNode.ID, peerConfig); err != nil {
		// Cleanup peer and IP on failure
		_ = s.peerService.Remove(ctx, createdPeer.ID)
		_ = s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP)
		return nil, fmt.Errorf("failed to add peer to node: %w", err)
	}

	// Get node public key for client configuration
	nodePublicKey, err := s.nodeService.GetNodePublicKey(ctx, selectedNode.ID)
	if err != nil {
		s.logger.Warn("failed to get node public key",
			slog.String("node_id", selectedNode.ID),
			slog.String("error", err.Error()))
		nodePublicKey = ""
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

	// Remove peer from node
	if err := s.nodeService.RemovePeerFromNode(ctx, existingPeer.NodeID, existingPeer.PublicKey); err != nil {
		s.logger.Warn("failed to remove peer from node",
			slog.String("peer_id", peerID),
			slog.String("node_id", existingPeer.NodeID),
			slog.String("error", err.Error()))
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

// selectOrCreateNode selects an optimal node or creates one if none are available
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

	// If no active nodes available, create a new one using coordinated provisioning
	if err == node.ErrNodeNotFound || err == node.ErrNodeAtCapacity {
		s.logger.Info("no active nodes available, provisioning new node")

		// Use the NodeProvisioningService for coordinated provisioning
		newNode, err := s.nodeProvisioningService.ProvisionNode(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to provision new node: %w", err)
		}

		s.logger.Info("provisioned new node for peer connection",
			slog.String("node_id", newNode.ID),
			slog.String("node_ip", newNode.IPAddress))

		return newNode, nil
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
