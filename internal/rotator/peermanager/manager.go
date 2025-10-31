package peermanager

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// Manager interface combines peer management with additional operations
type Manager interface {
	CreatePeer(ctx context.Context, req *CreatePeerRequest) (*PeerConfig, error)
	RemovePeer(ctx context.Context, peerID string) error
	GetPeer(ctx context.Context, peerID string) (*PeerConfig, error)
	ListPeers(ctx context.Context, filters *PeerFilters) ([]*PeerConfig, error)
	ValidatePeerConfig(peer *PeerConfig) error

	// Peer lifecycle operations
	GetPeerByPublicKey(ctx context.Context, publicKey string) (*PeerConfig, error)
	UpdatePeerStatus(ctx context.Context, peerID string, status string) error
	UpdatePeerLastHandshake(ctx context.Context, peerID string, timestamp time.Time) error

	// Peer migration operations
	MigratePeerToNode(ctx context.Context, peerID string, newNodeID string, newIP string) error
	GetPeersForMigration(ctx context.Context, nodeID string) ([]*PeerConfig, error)

	// Peer statistics and monitoring
	GetPeerStatistics(ctx context.Context) (*PeerStatistics, error)
	GetInactivePeers(ctx context.Context, inactiveMinutes int) ([]*PeerConfig, error)
	GetActivePeersByNode(ctx context.Context, nodeID string) ([]*PeerConfig, error)

	// Batch operations
	DeletePeersByNode(ctx context.Context, nodeID string) error
	CountPeersByNode(ctx context.Context, nodeID string) (int64, error)
	CountActivePeersByNode(ctx context.Context, nodeID string) (int64, error)

	// Additional high-level operations
	CreatePeerWithValidation(ctx context.Context, req *CreatePeerRequest) (*PeerConfig, error)
	RemovePeerWithCleanup(ctx context.Context, peerID string) error
	GetPeerWithDetails(ctx context.Context, peerID string) (*PeerInfo, error)
	ListPeersWithDetails(ctx context.Context, filters *PeerFilters) ([]*PeerInfo, error)
	GetNodePeerSummary(ctx context.Context, nodeID string) (*NodePeerSummary, error)
}

// manager implements the Manager interface
type manager struct {
	store  db.Store
	logger *logger.Logger
}

// NewManager creates a new comprehensive peer manager
func NewManager(store db.Store, log *logger.Logger) Manager {
	if log == nil {
		log = logger.New("info", "text")
	}

	// Create peer manager with the store

	return &manager{
		store:  store,
		logger: log,
	}
}

// Delegate PeerManager methods

// CreatePeer creates a new WireGuard peer
func (pm *manager) CreatePeer(ctx context.Context, req *CreatePeerRequest) (*PeerConfig, error) {
	pm.logger.Debug("creating new peer", "node_id", req.NodeID, "public_key", req.PublicKey[:8]+"...")

	// Validate the request
	if err := pm.validateCreatePeerRequest(req); err != nil {
		return nil, fmt.Errorf("invalid peer request: %w", err)
	}

	// Check for public key conflicts using store
	conflict, err := pm.store.CheckPublicKeyConflict(ctx, req.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check public key conflict: %w", err)
	}
	if conflict > 0 {
		return nil, fmt.Errorf("public key already in use")
	}

	// Check for IP conflicts using store
	ipConflict, err := pm.store.CheckIPConflict(ctx, req.AllocatedIP)
	if err != nil {
		return nil, fmt.Errorf("failed to check IP conflict: %w", err)
	}
	if ipConflict > 0 {
		return nil, fmt.Errorf("IP address %s already allocated", req.AllocatedIP)
	}

	// Generate peer ID
	peerID := generatePeerID()

	// Create peer in database using store
	createParams := db.CreatePeerParams{
		ID:          peerID,
		NodeID:      req.NodeID,
		PublicKey:   req.PublicKey,
		AllocatedIp: req.AllocatedIP,
		Status:      string(PeerStatusActive),
	}

	if req.PresharedKey != nil {
		createParams.PresharedKey = sql.NullString{
			String: *req.PresharedKey,
			Valid:  true,
		}
	}

	peer, err := pm.store.CreatePeer(ctx, createParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer in database: %w", err)
	}

	pm.logger.Info("created new peer", "peer_id", peerID, "node_id", req.NodeID, "allocated_ip", req.AllocatedIP)

	return dbPeerToPeerConfig(&peer), nil
}

// RemovePeer removes a WireGuard peer
func (pm *manager) RemovePeer(ctx context.Context, peerID string) error {
	pm.logger.Debug("removing peer", "peer_id", peerID)

	// Check if peer exists using store
	peer, err := pm.store.GetPeer(ctx, peerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("peer not found: %s", peerID)
		}
		return fmt.Errorf("failed to get peer: %w", err)
	}

	// Update peer status to removing first
	err = pm.store.UpdatePeerStatus(ctx, db.UpdatePeerStatusParams{
		ID:     peerID,
		Status: string(PeerStatusRemoving),
	})
	if err != nil {
		pm.logger.Error("failed to update peer status to removing", "peer_id", peerID, "error", err)
		// Continue with deletion even if status update fails
	}

	// Delete peer from database using store
	err = pm.store.DeletePeer(ctx, peerID)
	if err != nil {
		return fmt.Errorf("failed to delete peer from database: %w", err)
	}

	pm.logger.Info("removed peer", "peer_id", peerID, "node_id", peer.NodeID, "allocated_ip", peer.AllocatedIp)
	return nil
}

// GetPeer retrieves a specific peer by ID
func (pm *manager) GetPeer(ctx context.Context, peerID string) (*PeerConfig, error) {
	pm.logger.Debug("getting peer", "peer_id", peerID)

	peer, err := pm.store.GetPeer(ctx, peerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("peer not found: %s", peerID)
		}
		return nil, fmt.Errorf("failed to get peer: %w", err)
	}

	return dbPeerToPeerConfig(&peer), nil
}

// ListPeers lists peers based on filters
func (pm *manager) ListPeers(ctx context.Context, filters *PeerFilters) ([]*PeerConfig, error) {
	pm.logger.Debug("listing peers", "filters", filters)

	var peers []db.Peer
	var err error

	// Apply filters and get peers using store
	if filters != nil && filters.NodeID != nil {
		peers, err = pm.store.GetPeersByNode(ctx, *filters.NodeID)
	} else if filters != nil && filters.Status != nil {
		peers, err = pm.store.GetPeersByStatus(ctx, string(*filters.Status))
	} else {
		// List all peers
		peers, err = pm.store.GetAllPeers(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}

	// Convert to PeerConfig slice
	result := make([]*PeerConfig, len(peers))
	for i, peer := range peers {
		result[i] = dbPeerToPeerConfig(&peer)
	}

	// Apply limit and offset if specified
	if filters != nil {
		if filters.Offset != nil && *filters.Offset > 0 {
			if *filters.Offset >= len(result) {
				return []*PeerConfig{}, nil
			}
			result = result[*filters.Offset:]
		}
		if filters.Limit != nil && *filters.Limit > 0 && *filters.Limit < len(result) {
			result = result[:*filters.Limit]
		}
	}

	pm.logger.Debug("listed peers", "count", len(result))
	return result, nil
}

// ValidatePeerConfig validates a peer configuration
func (pm *manager) ValidatePeerConfig(peer *PeerConfig) error {
	if peer == nil {
		return fmt.Errorf("peer config cannot be nil")
	}

	// Validate public key format
	if !crypto.IsValidWireGuardKey(*peer.PresharedKey) {
		return fmt.Errorf("invalid preshared key")
	}

	// Validate IP address format
	if err := validateIPAddress(peer.AllocatedIP); err != nil {
		return fmt.Errorf("invalid allocated IP: %w", err)
	}

	// Validate node ID
	if strings.TrimSpace(peer.NodeID) == "" {
		return fmt.Errorf("node ID cannot be empty")
	}

	// Validate status
	if !isValidPeerStatus(peer.Status) {
		return fmt.Errorf("invalid peer status: %s", peer.Status)
	}

	// Validate preshared key if provided
	if peer.PresharedKey != nil && *peer.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*peer.PresharedKey) {
			return fmt.Errorf("invalid preshared key")
		}
	}

	return nil
}

// validateCreatePeerRequest validates a create peer request
func (pm *manager) validateCreatePeerRequest(req *CreatePeerRequest) error {
	if req == nil {
		return fmt.Errorf("create peer request cannot be nil")
	}

	// Validate public key format
	if !crypto.IsValidWireGuardKey(req.PublicKey) {
		return fmt.Errorf("invalid public key")
	}

	// Validate IP address format
	if err := validateIPAddress(req.AllocatedIP); err != nil {
		return fmt.Errorf("invalid allocated IP: %w", err)
	}

	// Validate node ID
	if strings.TrimSpace(req.NodeID) == "" {
		return fmt.Errorf("node ID cannot be empty")
	}

	// Validate preshared key if provided
	if req.PresharedKey != nil && *req.PresharedKey != "" {
		if !crypto.IsValidWireGuardKey(*req.PresharedKey) {
			return fmt.Errorf("invalid preshared key")
		}
	}

	return nil
}

// Peer lifecycle operations using repository
func (m *manager) GetPeerByPublicKey(ctx context.Context, publicKey string) (*PeerConfig, error) {
	m.logger.Debug("getting peer by public key", "public_key", publicKey[:8]+"...")

	peer, err := m.store.GetPeerByPublicKey(ctx, publicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("peer not found with public key")
		}
		return nil, fmt.Errorf("failed to get peer by public key: %w", err)
	}
	return dbPeerToPeerConfig(&peer), nil
}

func (m *manager) UpdatePeerStatus(ctx context.Context, peerID string, status string) error {
	m.logger.Debug("updating peer status", "peer_id", peerID, "status", status)

	// Validate status
	if !isValidPeerStatus(PeerStatus(status)) {
		return fmt.Errorf("invalid peer status: %s", status)
	}

	err := m.store.UpdatePeerStatus(ctx, db.UpdatePeerStatusParams{
		ID:     peerID,
		Status: status,
	})
	if err != nil {
		return fmt.Errorf("failed to update peer status: %w", err)
	}

	m.logger.Info("updated peer status", "peer_id", peerID, "status", status)
	return nil
}

func (m *manager) UpdatePeerLastHandshake(ctx context.Context, peerID string, timestamp time.Time) error {
	m.logger.Debug("updating peer last handshake", "peer_id", peerID, "timestamp", timestamp)

	err := m.store.UpdatePeerLastHandshake(ctx, db.UpdatePeerLastHandshakeParams{
		ID: peerID,
		LastHandshakeAt: sql.NullTime{
			Time:  timestamp,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update peer last handshake: %w", err)
	}

	m.logger.Debug("updated peer last handshake", "peer_id", peerID)
	return nil
}

func (m *manager) MigratePeerToNode(ctx context.Context, peerID string, newNodeID string, newIP string) error {
	// Validate IP address format first
	if err := validateIPAddress(newIP); err != nil {
		return fmt.Errorf("invalid new IP address: %w", err)
	}

	// Check for IP conflicts using store
	ipConflict, err := m.store.CheckIPConflict(ctx, newIP)
	if err != nil {
		return fmt.Errorf("failed to check IP conflict: %w", err)
	}
	if ipConflict > 0 {
		return fmt.Errorf("IP address %s already allocated", newIP)
	}

	// Update peer node using store
	return m.store.UpdatePeerNode(ctx, db.UpdatePeerNodeParams{
		ID:          peerID,
		NodeID:      newNodeID,
		AllocatedIp: newIP,
	})
}

func (m *manager) GetPeersForMigration(ctx context.Context, nodeID string) ([]*PeerConfig, error) {
	m.logger.Debug("getting peers for migration", "node_id", nodeID)

	peers, err := m.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peers for migration: %w", err)
	}

	result := make([]*PeerConfig, len(peers))
	for i, peer := range peers {
		result[i] = dbPeerToPeerConfig(&peer)
	}

	m.logger.Debug("got peers for migration", "node_id", nodeID, "count", len(result))
	return result, nil
}

func (m *manager) DeletePeersByNode(ctx context.Context, nodeID string) error {
	m.logger.Debug("deleting peers by node", "node_id", nodeID)

	err := m.store.DeletePeersByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete peers by node: %w", err)
	}

	m.logger.Info("deleted peers by node", "node_id", nodeID)
	return nil
}

func (m *manager) GetPeerStatistics(ctx context.Context) (*PeerStatistics, error) {
	m.logger.Debug("getting peer statistics")

	// Use the store directly for peer statistics
	stats, err := m.store.GetPeerStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer statistics: %w", err)
	}

	result := &PeerStatistics{
		TotalPeers: stats.TotalPeers,
	}

	if stats.ActivePeers.Valid {
		result.ActivePeers = int64(stats.ActivePeers.Float64)
	}
	if stats.DisconnectedPeers.Valid {
		result.DisconnectedPeers = int64(stats.DisconnectedPeers.Float64)
	}
	if stats.RemovingPeers.Valid {
		result.RemovingPeers = int64(stats.RemovingPeers.Float64)
	}

	m.logger.Debug("got peer statistics", "total", result.TotalPeers, "active", result.ActivePeers)
	return result, nil
}

func (m *manager) GetInactivePeers(ctx context.Context, inactiveMinutes int) ([]*PeerConfig, error) {
	m.logger.Debug("getting inactive peers", "inactive_minutes", inactiveMinutes)

	// Use the store directly for inactive peers
	peers, err := m.store.GetInactivePeers(ctx, sql.NullString{
		String: fmt.Sprintf("%d", inactiveMinutes),
		Valid:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get inactive peers: %w", err)
	}

	result := make([]*PeerConfig, len(peers))
	for i, peer := range peers {
		result[i] = dbPeerToPeerConfig(&peer)
	}

	m.logger.Debug("got inactive peers", "count", len(result))
	return result, nil
}

func (m *manager) GetActivePeersByNode(ctx context.Context, nodeID string) ([]*PeerConfig, error) {
	m.logger.Debug("getting active peers by node", "node_id", nodeID)

	peers, err := m.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peers by node: %w", err)
	}

	// Filter for active peers only
	var activePeers []*PeerConfig
	for _, peer := range peers {
		if peer.Status == string(PeerStatusActive) {
			activePeers = append(activePeers, dbPeerToPeerConfig(&peer))
		}
	}

	m.logger.Debug("got active peers by node", "node_id", nodeID, "count", len(activePeers))
	return activePeers, nil
}

func (m *manager) CountPeersByNode(ctx context.Context, nodeID string) (int64, error) {
	m.logger.Debug("counting peers by node", "node_id", nodeID)

	// Use GetPeersByNode and count
	peers, err := m.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count peers by node: %w", err)
	}
	count := int64(len(peers))

	m.logger.Debug("counted peers by node", "node_id", nodeID, "count", count)
	return count, nil
}

func (m *manager) CountActivePeersByNode(ctx context.Context, nodeID string) (int64, error) {
	m.logger.Debug("counting active peers by node", "node_id", nodeID)

	peers, err := m.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get peers by node: %w", err)
	}

	// Count only active peers
	var count int64
	for _, peer := range peers {
		if peer.Status == string(PeerStatusActive) {
			count++
		}
	}

	m.logger.Debug("counted active peers by node", "node_id", nodeID, "count", count)
	return count, nil
}

// Enhanced high-level operations

// CreatePeerWithValidation creates a peer with comprehensive validation
func (m *manager) CreatePeerWithValidation(ctx context.Context, req *CreatePeerRequest) (*PeerConfig, error) {
	m.logger.Debug("creating peer with validation", "node_id", req.NodeID)

	// Validate that the node exists using store
	_, err := m.store.GetNode(ctx, req.NodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid node ID: %s", req.NodeID)
		}
		return nil, fmt.Errorf("failed to validate node: %w", err)
	}

	// Create the peer using the standard method
	peer, err := m.CreatePeer(ctx, req)
	if err != nil {
		return nil, err
	}

	m.logger.Info("created peer with validation", "peer_id", peer.ID, "node_id", req.NodeID)
	return peer, nil
}

// RemovePeerWithCleanup removes a peer with comprehensive cleanup
func (m *manager) RemovePeerWithCleanup(ctx context.Context, peerID string) error {
	m.logger.Debug("removing peer with cleanup", "peer_id", peerID)

	// First mark the peer for removal
	err := m.UpdatePeerStatus(ctx, peerID, string(PeerStatusRemoving))
	if err != nil {
		m.logger.Warn("failed to mark peer for removal", "peer_id", peerID, "error", err)
		// Continue with removal even if status update fails
	}

	// Remove the peer
	err = m.RemovePeer(ctx, peerID)
	if err != nil {
		return err
	}

	m.logger.Info("removed peer with cleanup", "peer_id", peerID)
	return nil
}

// GetPeerWithDetails retrieves a peer with additional details
func (m *manager) GetPeerWithDetails(ctx context.Context, peerID string) (*PeerInfo, error) {
	m.logger.Debug("getting peer with details", "peer_id", peerID)

	peer, err := m.GetPeer(ctx, peerID)
	if err != nil {
		return nil, err
	}

	// Convert to PeerInfo with additional details
	info := &PeerInfo{
		ID:          peer.ID,
		NodeID:      peer.NodeID,
		PublicKey:   peer.PublicKey,
		AllocatedIP: peer.AllocatedIP,
		Status:      string(peer.Status),
		CreatedAt:   peer.CreatedAt,
	}

	// Get the database peer to access last handshake using store
	dbPeer, err := m.store.GetPeer(ctx, peerID)
	if err == nil && dbPeer.LastHandshakeAt.Valid {
		info.LastHandshakeAt = &dbPeer.LastHandshakeAt.Time
	}

	return info, nil
}

// ListPeersWithDetails lists peers with enhanced filtering and details
func (m *manager) ListPeersWithDetails(ctx context.Context, filters *PeerFilters) ([]*PeerInfo, error) {
	m.logger.Debug("listing peers with details", "filters", filters)

	peers, err := m.ListPeers(ctx, filters)
	if err != nil {
		return nil, err
	}

	// Convert to PeerInfo slice with additional details
	result := make([]*PeerInfo, len(peers))
	for i, peer := range peers {
		info := &PeerInfo{
			ID:          peer.ID,
			NodeID:      peer.NodeID,
			PublicKey:   peer.PublicKey,
			AllocatedIP: peer.AllocatedIP,
			Status:      string(peer.Status),
			CreatedAt:   peer.CreatedAt,
		}

		// Get additional details from database using store
		dbPeer, err := m.store.GetPeer(ctx, peer.ID)
		if err == nil && dbPeer.LastHandshakeAt.Valid {
			info.LastHandshakeAt = &dbPeer.LastHandshakeAt.Time
		}

		result[i] = info
	}

	return result, nil
}

// ActivatePeer activates a peer
func (m *manager) ActivatePeer(ctx context.Context, peerID string) error {
	m.logger.Debug("activating peer", "peer_id", peerID)
	return m.UpdatePeerStatus(ctx, peerID, string(PeerStatusActive))
}

// DeactivatePeer deactivates a peer
func (m *manager) DeactivatePeer(ctx context.Context, peerID string) error {
	m.logger.Debug("deactivating peer", "peer_id", peerID)
	return m.UpdatePeerStatus(ctx, peerID, string(PeerStatusDisconnected))
}

// MarkPeerForRemoval marks a peer for removal
func (m *manager) MarkPeerForRemoval(ctx context.Context, peerID string) error {
	m.logger.Debug("marking peer for removal", "peer_id", peerID)
	return m.UpdatePeerStatus(ctx, peerID, string(PeerStatusRemoving))
}

// GetNodePeerSummary provides a summary of peers for a specific node
func (m *manager) GetNodePeerSummary(ctx context.Context, nodeID string) (*NodePeerSummary, error) {
	m.logger.Debug("getting node peer summary", "node_id", nodeID)

	totalPeers, err := m.CountPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to count total peers: %w", err)
	}

	activePeers, err := m.CountActivePeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to count active peers: %w", err)
	}

	// Calculate usage (assuming max 253 peers per node based on /24 subnet)
	const maxPeersPerNode = 253
	usage := fmt.Sprintf("%d/%d", activePeers, maxPeersPerNode)

	summary := &NodePeerSummary{
		NodeID:      nodeID,
		TotalPeers:  totalPeers,
		ActivePeers: activePeers,
		Capacity:    maxPeersPerNode,
		Usage:       usage,
	}

	m.logger.Debug("got node peer summary", "node_id", nodeID, "total", totalPeers, "active", activePeers)
	return summary, nil
}

// ValidateNodeCapacity validates that a node has capacity for additional peers
func (m *manager) ValidateNodeCapacity(ctx context.Context, nodeID string, maxPeers int) error {
	m.logger.Debug("validating node capacity", "node_id", nodeID, "max_peers", maxPeers)

	activePeers, err := m.CountActivePeersByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to count active peers: %w", err)
	}

	if activePeers >= int64(maxPeers) {
		return fmt.Errorf("node %s is at capacity: %d/%d peers", nodeID, activePeers, maxPeers)
	}

	m.logger.Debug("node has capacity", "node_id", nodeID, "active_peers", activePeers, "max_peers", maxPeers)
	return nil
}
