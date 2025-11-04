package node

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// PeerManagementService provides comprehensive peer management operations using NodeInteractor
type PeerManagementService struct {
	nodeService    NodeService
	repository     NodeRepository
	nodeInteractor nodeinteractor.NodeInteractor
	logger         *slog.Logger
	config         PeerManagementConfig

	// Synchronization for concurrent operations
	peerMutex sync.RWMutex
}

// PeerManagementConfig contains configuration for peer management
type PeerManagementConfig struct {
	MaxPeersPerNode       int  `json:"max_peers_per_node"`
	ValidateBeforeAdd     bool `json:"validate_before_add"`
	ValidateAfterAdd      bool `json:"validate_after_add"`
	ValidateAfterRemove   bool `json:"validate_after_remove"`
	SyncConfigAfterChange bool `json:"sync_config_after_change"`
	AllowDuplicateIPs     bool `json:"allow_duplicate_ips"`
	RequirePresharedKey   bool `json:"require_preshared_key"`
}

// PeerOperation represents a peer management operation
type PeerOperation struct {
	Type      string      `json:"type"` // "add", "remove", "update", "sync"
	NodeID    string      `json:"node_id"`
	PeerID    string      `json:"peer_id"`
	PublicKey string      `json:"public_key"`
	Config    *PeerConfig `json:"config,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// PeerSyncResult represents the result of a peer synchronization operation
type PeerSyncResult struct {
	NodeID       string   `json:"node_id"`
	TotalPeers   int      `json:"total_peers"`
	AddedPeers   []string `json:"added_peers"`
	RemovedPeers []string `json:"removed_peers"`
	UpdatedPeers []string `json:"updated_peers"`
	Errors       []string `json:"errors"`
	Success      bool     `json:"success"`
}

// NewPeerManagementService creates a new peer management service
func NewPeerManagementService(
	nodeService NodeService,
	repository NodeRepository,
	nodeInteractor nodeinteractor.NodeInteractor,
	logger *slog.Logger,
	config PeerManagementConfig,
) *PeerManagementService {
	return &PeerManagementService{
		nodeService:    nodeService,
		repository:     repository,
		nodeInteractor: nodeInteractor,
		logger:         logger,
		config:         config,
	}
}

// AddPeerToNode adds a peer to a node with comprehensive validation and error handling
func (pms *PeerManagementService) AddPeerToNode(ctx context.Context, nodeID string, peerConfig PeerConfig) error {
	pms.peerMutex.Lock()
	defer pms.peerMutex.Unlock()

	pms.logger.Debug("adding peer to node with validation",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerConfig.ID))

	// Validate peer configuration
	if err := pms.validatePeerConfig(&peerConfig); err != nil {
		return fmt.Errorf("invalid peer config: %w", err)
	}

	// Get node from repository
	node, err := pms.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Validate node can accept peers
	if err := pms.nodeService.ValidateNodeCapacity(ctx, nodeID, 1); err != nil {
		return err
	}

	// Pre-add validation
	if pms.config.ValidateBeforeAdd {
		if err := pms.validatePeerAddition(ctx, node, &peerConfig); err != nil {
			return fmt.Errorf("pre-add validation failed: %w", err)
		}
	}

	// Convert to NodeInteractor format
	wireGuardConfig := nodeinteractor.PeerWireGuardConfig{
		PublicKey:    peerConfig.PublicKey,
		PresharedKey: peerConfig.PresharedKey,
		AllowedIPs:   peerConfig.AllowedIPs,
	}

	// Add peer using NodeInteractor
	if err := pms.nodeInteractor.AddPeer(ctx, node.IPAddress, wireGuardConfig); err != nil {
		return NewPeerOperationError(node.IPAddress, peerConfig.ID, peerConfig.PublicKey, "add", err)
	}

	// Post-add validation
	if pms.config.ValidateAfterAdd {
		if err := pms.validatePeerExists(ctx, node.IPAddress, peerConfig.PublicKey); err != nil {
			// Attempt rollback
			pms.logger.Warn("post-add validation failed, attempting rollback",
				slog.String("node_id", nodeID),
				slog.String("peer_id", peerConfig.ID))

			if rollbackErr := pms.nodeInteractor.RemovePeer(ctx, node.IPAddress, peerConfig.PublicKey); rollbackErr != nil {
				pms.logger.Error("rollback failed",
					slog.String("node_id", nodeID),
					slog.String("peer_id", peerConfig.ID),
					slog.String("error", rollbackErr.Error()))
			}

			return fmt.Errorf("post-add validation failed: %w", err)
		}
	}

	// Sync configuration if enabled
	if pms.config.SyncConfigAfterChange {
		if err := pms.syncNodeConfiguration(ctx, node.IPAddress); err != nil {
			pms.logger.Warn("failed to sync configuration after peer addition",
				slog.String("node_id", nodeID),
				slog.String("peer_id", peerConfig.ID),
				slog.String("error", err.Error()))
		}
	}

	pms.logger.Info("successfully added peer to node",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerConfig.ID),
		slog.String("allocated_ip", peerConfig.AllocatedIP))

	return nil
}

// RemovePeerFromNode removes a peer from a node with validation
func (pms *PeerManagementService) RemovePeerFromNode(ctx context.Context, nodeID string, publicKey string) error {
	pms.peerMutex.Lock()
	defer pms.peerMutex.Unlock()

	pms.logger.Debug("removing peer from node with validation",
		slog.String("node_id", nodeID),
		slog.String("public_key", publicKey[:8]+"..."))

	// Validate public key
	if !crypto.IsValidWireGuardKey(publicKey) {
		return fmt.Errorf("invalid public key format")
	}

	// Get node from repository
	node, err := pms.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Check if peer exists before removal
	exists, err := pms.checkPeerExists(ctx, node.IPAddress, publicKey)
	if err != nil {
		pms.logger.Warn("failed to check peer existence before removal",
			slog.String("node_id", nodeID),
			slog.String("public_key", publicKey[:8]+"..."),
			slog.String("error", err.Error()))
	} else if !exists {
		pms.logger.Debug("peer not found on node, skipping removal",
			slog.String("node_id", nodeID),
			slog.String("public_key", publicKey[:8]+"..."))
		return nil // Not an error if peer doesn't exist
	}

	// Remove peer using NodeInteractor
	if err := pms.nodeInteractor.RemovePeer(ctx, node.IPAddress, publicKey); err != nil {
		return NewPeerOperationError(node.IPAddress, "", publicKey, "remove", err)
	}

	// Post-removal validation
	if pms.config.ValidateAfterRemove {
		if err := pms.validatePeerRemoved(ctx, node.IPAddress, publicKey); err != nil {
			pms.logger.Warn("post-removal validation failed",
				slog.String("node_id", nodeID),
				slog.String("public_key", publicKey[:8]+"..."),
				slog.String("error", err.Error()))
		}
	}

	// Sync configuration if enabled
	if pms.config.SyncConfigAfterChange {
		if err := pms.syncNodeConfiguration(ctx, node.IPAddress); err != nil {
			pms.logger.Warn("failed to sync configuration after peer removal",
				slog.String("node_id", nodeID),
				slog.String("public_key", publicKey[:8]+"..."),
				slog.String("error", err.Error()))
		}
	}

	pms.logger.Info("successfully removed peer from node",
		slog.String("node_id", nodeID),
		slog.String("public_key", publicKey[:8]+"..."))

	return nil
}

// ListNodePeers lists all peers on a node with detailed information
func (pms *PeerManagementService) ListNodePeers(ctx context.Context, nodeID string) ([]*PeerInfo, error) {
	pms.peerMutex.RLock()
	defer pms.peerMutex.RUnlock()

	pms.logger.Debug("listing peers on node", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := pms.repository.GetByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// List peers using NodeInteractor
	wireGuardPeers, err := pms.nodeInteractor.ListPeers(ctx, node.IPAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}

	// Convert to domain PeerInfo
	var peers []*PeerInfo
	for _, wgPeer := range wireGuardPeers {
		peer := &PeerInfo{
			PublicKey:     wgPeer.PublicKey,
			AllowedIPs:    wgPeer.AllowedIPs,
			LastHandshake: wgPeer.LastHandshake,
			TransferRx:    wgPeer.TransferRx,
			TransferTx:    wgPeer.TransferTx,
		}

		// Extract allocated IP from allowed IPs
		if len(wgPeer.AllowedIPs) > 0 {
			allowedIP := wgPeer.AllowedIPs[0]
			if idx := strings.Index(allowedIP, "/"); idx > 0 {
				peer.AllocatedIP = allowedIP[:idx]
			} else {
				peer.AllocatedIP = allowedIP
			}
		}

		peers = append(peers, peer)
	}

	pms.logger.Debug("listed peers on node",
		slog.String("node_id", nodeID),
		slog.Int("peer_count", len(peers)))

	return peers, nil
}

// UpdateNodePeerConfig updates a peer's configuration with validation
func (pms *PeerManagementService) UpdateNodePeerConfig(ctx context.Context, nodeID string, peerConfig PeerConfig) error {
	pms.peerMutex.Lock()
	defer pms.peerMutex.Unlock()

	pms.logger.Debug("updating peer config on node",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerConfig.ID))

	// Validate peer configuration
	if err := pms.validatePeerConfig(&peerConfig); err != nil {
		return fmt.Errorf("invalid peer config: %w", err)
	}

	// Get node from repository
	node, err := pms.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Convert to NodeInteractor format
	wireGuardConfig := nodeinteractor.PeerWireGuardConfig{
		PublicKey:    peerConfig.PublicKey,
		PresharedKey: peerConfig.PresharedKey,
		AllowedIPs:   peerConfig.AllowedIPs,
	}

	// Update peer using NodeInteractor
	if err := pms.nodeInteractor.UpdatePeer(ctx, node.IPAddress, wireGuardConfig); err != nil {
		return NewPeerOperationError(node.IPAddress, peerConfig.ID, peerConfig.PublicKey, "update", err)
	}

	// Sync configuration if enabled
	if pms.config.SyncConfigAfterChange {
		if err := pms.syncNodeConfiguration(ctx, node.IPAddress); err != nil {
			pms.logger.Warn("failed to sync configuration after peer update",
				slog.String("node_id", nodeID),
				slog.String("peer_id", peerConfig.ID),
				slog.String("error", err.Error()))
		}
	}

	pms.logger.Info("successfully updated peer config on node",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerConfig.ID))

	return nil
}

// SyncNodePeers synchronizes peers on a node with a desired configuration
func (pms *PeerManagementService) SyncNodePeers(ctx context.Context, nodeID string, desiredPeers []PeerConfig) (*PeerSyncResult, error) {
	pms.peerMutex.Lock()
	defer pms.peerMutex.Unlock()

	pms.logger.Info("synchronizing peers on node",
		slog.String("node_id", nodeID),
		slog.Int("desired_peers", len(desiredPeers)))

	result := &PeerSyncResult{
		NodeID:       nodeID,
		AddedPeers:   []string{},
		RemovedPeers: []string{},
		UpdatedPeers: []string{},
		Errors:       []string{},
		Success:      true,
	}

	// Get node from repository
	node, err := pms.repository.GetByID(ctx, nodeID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get node: %v", err))
		result.Success = false
		return result, fmt.Errorf("failed to get node: %w", err)
	}

	// Get current peers
	currentPeers, err := pms.ListNodePeers(ctx, nodeID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to list current peers: %v", err))
		result.Success = false
		return result, fmt.Errorf("failed to list current peers: %w", err)
	}

	// Create maps for easier comparison
	currentPeerMap := make(map[string]*PeerInfo)
	for _, peer := range currentPeers {
		currentPeerMap[peer.PublicKey] = peer
	}

	desiredPeerMap := make(map[string]*PeerConfig)
	for i := range desiredPeers {
		desiredPeerMap[desiredPeers[i].PublicKey] = &desiredPeers[i]
	}

	// Convert desired peers to NodeInteractor format
	var wireGuardConfigs []nodeinteractor.PeerWireGuardConfig
	for _, peerConfig := range desiredPeers {
		wireGuardConfigs = append(wireGuardConfigs, nodeinteractor.PeerWireGuardConfig{
			PublicKey:    peerConfig.PublicKey,
			PresharedKey: peerConfig.PresharedKey,
			AllowedIPs:   peerConfig.AllowedIPs,
		})
	}

	// Use NodeInteractor sync operation
	if err := pms.nodeInteractor.SyncPeers(ctx, node.IPAddress, wireGuardConfigs); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("sync operation failed: %v", err))
		result.Success = false
		return result, fmt.Errorf("sync operation failed: %w", err)
	}

	// Determine what changed by comparing before and after
	// Find peers to remove (in current but not in desired)
	for publicKey := range currentPeerMap {
		if _, exists := desiredPeerMap[publicKey]; !exists {
			result.RemovedPeers = append(result.RemovedPeers, publicKey[:8]+"...")
		}
	}

	// Find peers to add (in desired but not in current)
	for publicKey, peerConfig := range desiredPeerMap {
		if _, exists := currentPeerMap[publicKey]; !exists {
			result.AddedPeers = append(result.AddedPeers, peerConfig.ID)
		} else {
			// Peer exists, might be updated
			result.UpdatedPeers = append(result.UpdatedPeers, peerConfig.ID)
		}
	}

	result.TotalPeers = len(desiredPeers)

	pms.logger.Info("peer synchronization completed",
		slog.String("node_id", nodeID),
		slog.Int("total_peers", result.TotalPeers),
		slog.Int("added", len(result.AddedPeers)),
		slog.Int("removed", len(result.RemovedPeers)),
		slog.Int("updated", len(result.UpdatedPeers)))

	return result, nil
}

// Private helper methods

func (pms *PeerManagementService) validatePeerConfig(peer *PeerConfig) error {
	if peer == nil {
		return fmt.Errorf("peer config cannot be nil")
	}

	if peer.ID == "" {
		return fmt.Errorf("peer ID cannot be empty")
	}

	if !crypto.IsValidWireGuardKey(peer.PublicKey) {
		return fmt.Errorf("invalid public key")
	}

	if peer.AllocatedIP == "" {
		return fmt.Errorf("allocated IP cannot be empty")
	}

	if pms.config.RequirePresharedKey && (peer.PresharedKey == nil || *peer.PresharedKey == "") {
		return fmt.Errorf("preshared key is required")
	}

	if peer.PresharedKey != nil && *peer.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*peer.PresharedKey) {
			return fmt.Errorf("invalid preshared key")
		}
	}

	return nil
}

func (pms *PeerManagementService) validatePeerAddition(ctx context.Context, node *Node, peerConfig *PeerConfig) error {
	// Check for duplicate public key
	existingPeers, err := pms.ListNodePeers(ctx, node.ID)
	if err != nil {
		return fmt.Errorf("failed to check existing peers: %w", err)
	}

	for _, existingPeer := range existingPeers {
		if existingPeer.PublicKey == peerConfig.PublicKey {
			return fmt.Errorf("peer with public key %s already exists", peerConfig.PublicKey[:8]+"...")
		}

		if !pms.config.AllowDuplicateIPs && existingPeer.AllocatedIP == peerConfig.AllocatedIP {
			return fmt.Errorf("IP address %s already allocated to another peer", peerConfig.AllocatedIP)
		}
	}

	// Check capacity
	if len(existingPeers) >= pms.config.MaxPeersPerNode {
		return fmt.Errorf("node at maximum capacity (%d peers)", pms.config.MaxPeersPerNode)
	}

	return nil
}

func (pms *PeerManagementService) validatePeerExists(ctx context.Context, nodeIP, publicKey string) error {
	exists, err := pms.checkPeerExists(ctx, nodeIP, publicKey)
	if err != nil {
		return err
	}

	if !exists {
		return fmt.Errorf("peer with public key %s not found after addition", publicKey[:8]+"...")
	}

	return nil
}

func (pms *PeerManagementService) validatePeerRemoved(ctx context.Context, nodeIP, publicKey string) error {
	exists, err := pms.checkPeerExists(ctx, nodeIP, publicKey)
	if err != nil {
		return err
	}

	if exists {
		return fmt.Errorf("peer with public key %s still exists after removal", publicKey[:8]+"...")
	}

	return nil
}

func (pms *PeerManagementService) checkPeerExists(ctx context.Context, nodeIP, publicKey string) (bool, error) {
	wireGuardPeers, err := pms.nodeInteractor.ListPeers(ctx, nodeIP)
	if err != nil {
		return false, fmt.Errorf("failed to list peers: %w", err)
	}

	for _, peer := range wireGuardPeers {
		if peer.PublicKey == publicKey {
			return true, nil
		}
	}

	return false, nil
}

func (pms *PeerManagementService) syncNodeConfiguration(ctx context.Context, nodeIP string) error {
	// Save WireGuard configuration to persist changes
	// This uses NodeInteractor's SaveWireGuardConfig method
	return pms.nodeInteractor.SaveWireGuardConfig(ctx, nodeIP)
}
