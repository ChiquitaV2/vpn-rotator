package peer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// Service defines the business logic interface for peer operations
// Contains only the methods actually used by consumers
type Service interface {
	// Core operations
	Create(ctx context.Context, req *CreateRequest) (*Peer, error)
	Get(ctx context.Context, peerID string) (*Peer, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error)
	List(ctx context.Context, filters *Filters) ([]*Peer, error)
	Remove(ctx context.Context, peerID string) error

	// Status operations
	UpdateStatus(ctx context.Context, peerID string, status Status) error

	// Migration operations
	Migrate(ctx context.Context, peerID, newNodeID, newIP string) error
	GetByNode(ctx context.Context, nodeID string) ([]*Peer, error)
	GetActiveByNode(ctx context.Context, nodeID string) ([]*Peer, error)

	// Query operations
	CountActiveByNode(ctx context.Context, nodeID string) (int64, error)
	GetInactive(ctx context.Context, inactiveMinutes int) ([]*Peer, error)
	GetStatistics(ctx context.Context) (*Statistics, error)

	// Batch operations for better performance
	CreateBatch(ctx context.Context, requests []*CreateRequest) ([]*Peer, error)
	UpdateStatusBatch(ctx context.Context, updates map[string]Status) error
}

// service implements the Service interface
type service struct {
	repo Repository
}

// NewService creates a new peer service
func NewService(repo Repository) Service {
	return &service{
		repo: repo,
	}
}

// Create creates a new peer with validation
func (s *service) Create(ctx context.Context, req *CreateRequest) (*Peer, error) {
	// Validate request
	if err := ValidateCreateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check for public key conflicts
	conflict, err := s.repo.CheckPublicKeyConflict(ctx, req.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check public key conflict: %w", err)
	}
	if conflict {
		return nil, NewConflictError("public_key", req.PublicKey, "public key already in use")
	}

	// Check for IP conflicts
	ipConflict, err := s.repo.CheckIPConflict(ctx, req.AllocatedIP)
	if err != nil {
		return nil, fmt.Errorf("failed to check IP conflict: %w", err)
	}
	if ipConflict {
		return nil, NewConflictError("ip_address", req.AllocatedIP, "IP address already allocated")
	}

	// Create peer entity
	peer, err := NewPeer(req.NodeID, req.PublicKey, req.AllocatedIP, req.PresharedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer entity: %w", err)
	}

	// Generate ID
	peer.ID = generatePeerID()

	// Persist to repository
	if err := s.repo.Create(ctx, peer); err != nil {
		return nil, fmt.Errorf("failed to persist peer: %w", err)
	}

	return peer, nil
}

// Get retrieves a peer by ID
func (s *service) Get(ctx context.Context, peerID string) (*Peer, error) {
	if peerID == "" {
		return nil, NewValidationError("peer_id", "cannot be empty", peerID)
	}

	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer: %w", err)
	}

	return peer, nil
}

// GetByPublicKey retrieves a peer by its public key
func (s *service) GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error) {
	if crypto.IsValidWireGuardKey(publicKey) {
		return nil, NewValidationError("public_key", "must be valid", publicKey)
	}

	peer, err := s.repo.GetByPublicKey(ctx, publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer by public key: %w", err)
	}

	return peer, nil
}

// List retrieves peers based on filters
func (s *service) List(ctx context.Context, filters *Filters) ([]*Peer, error) {
	peers, err := s.repo.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}

	return peers, nil
}

// Remove removes a peer with proper cleanup
func (s *service) Remove(ctx context.Context, peerID string) error {
	if peerID == "" {
		return NewValidationError("peer_id", "cannot be empty", peerID)
	}

	// Check if peer exists
	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		return fmt.Errorf("failed to get peer: %w", err)
	}

	// Mark as removing first (best effort)
	_ = s.repo.UpdateStatus(ctx, peerID, StatusRemoving)

	// Delete the peer
	if err := s.repo.Delete(ctx, peerID); err != nil {
		return fmt.Errorf("failed to delete peer: %w", err)
	}

	// If we had additional cleanup (e.g., notify node, revoke certs), do it here
	_ = peer // Placeholder for future cleanup logic

	return nil
}

// UpdateStatus updates a peer's status with validation
func (s *service) UpdateStatus(ctx context.Context, peerID string, status Status) error {
	if peerID == "" {
		return NewValidationError("peer_id", "cannot be empty", peerID)
	}

	if err := ValidateStatus(status); err != nil {
		return err
	}

	// Get current peer to validate transition
	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		return fmt.Errorf("failed to get peer: %w", err)
	}

	// Validate status transition
	if !peer.Status.CanTransitionTo(status) {
		return fmt.Errorf("invalid status transition from %s to %s", peer.Status, status)
	}

	// Update status
	if err := s.repo.UpdateStatus(ctx, peerID, status); err != nil {
		return fmt.Errorf("failed to update peer status: %w", err)
	}

	return nil
}

// Migrate migrates a peer to a new node with a new IP
func (s *service) Migrate(ctx context.Context, peerID, newNodeID, newIP string) error {
	if peerID == "" {
		return NewValidationError("peer_id", "cannot be empty", peerID)
	}

	if err := ValidateNodeID(newNodeID); err != nil {
		return err
	}

	if err := ValidateIPAddress(newIP); err != nil {
		return err
	}

	// Check for IP conflicts (excluding current peer)
	ipConflict, err := s.repo.CheckIPConflict(ctx, newIP)
	if err != nil {
		return fmt.Errorf("failed to check IP conflict: %w", err)
	}
	if ipConflict {
		// Double-check it's not the same peer
		existingPeer, _ := s.repo.Get(ctx, peerID)
		if existingPeer != nil && existingPeer.AllocatedIP != newIP {
			return NewConflictError("ip_address", newIP, "IP address already allocated")
		}
	}

	// Update node and IP
	if err := s.repo.UpdateNode(ctx, peerID, newNodeID, newIP); err != nil {
		return fmt.Errorf("failed to migrate peer: %w", err)
	}

	return nil
}

// GetByNode retrieves all peers for a specific node
func (s *service) GetByNode(ctx context.Context, nodeID string) ([]*Peer, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	peers, err := s.repo.GetByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peers by node: %w", err)
	}

	return peers, nil
}

// GetActiveByNode retrieves active peers for a specific node
func (s *service) GetActiveByNode(ctx context.Context, nodeID string) ([]*Peer, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return nil, err
	}

	peers, err := s.repo.GetActiveByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active peers by node: %w", err)
	}

	return peers, nil
}

// CountActiveByNode counts active peers for a specific node
func (s *service) CountActiveByNode(ctx context.Context, nodeID string) (int64, error) {
	if err := ValidateNodeID(nodeID); err != nil {
		return 0, err
	}

	count, err := s.repo.CountActiveByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count active peers: %w", err)
	}

	return count, nil
}

// GetInactive retrieves peers that have been inactive for the specified duration
func (s *service) GetInactive(ctx context.Context, inactiveMinutes int) ([]*Peer, error) {
	if inactiveMinutes <= 0 {
		return nil, NewValidationError("inactive_minutes", "must be positive", inactiveMinutes)
	}

	duration := time.Duration(inactiveMinutes) * time.Minute
	peers, err := s.repo.GetInactive(ctx, duration)
	if err != nil {
		return nil, fmt.Errorf("failed to get inactive peers: %w", err)
	}

	return peers, nil
}

// GetStatistics retrieves peer statistics
func (s *service) GetStatistics(ctx context.Context) (*Statistics, error) {
	stats, err := s.repo.GetStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer statistics: %w", err)
	}

	return stats, nil
}

// CreateBatch creates multiple peers in a single transaction for better performance
func (s *service) CreateBatch(ctx context.Context, requests []*CreateRequest) ([]*Peer, error) {
	if len(requests) == 0 {
		return []*Peer{}, nil
	}

	// Pre-validate all requests
	for i, req := range requests {
		if err := ValidateCreateRequest(req); err != nil {
			return nil, fmt.Errorf("validation failed for request %d: %w", i, err)
		}
	}

	// Check for conflicts in batch
	publicKeys := make([]string, len(requests))
	ips := make([]string, len(requests))
	for i, req := range requests {
		publicKeys[i] = req.PublicKey
		ips[i] = req.AllocatedIP
	}

	// TODO: Add batch conflict checking to repository interface
	// For now, fall back to individual checks
	peers := make([]*Peer, 0, len(requests))
	for _, req := range requests {
		peer, err := s.Create(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create peer in batch: %w", err)
		}
		peers = append(peers, peer)
	}

	return peers, nil
}

// UpdateStatusBatch updates multiple peer statuses efficiently
func (s *service) UpdateStatusBatch(ctx context.Context, updates map[string]Status) error {
	if len(updates) == 0 {
		return nil
	}

	// Validate all statuses first
	for peerID, status := range updates {
		if peerID == "" {
			return NewValidationError("peer_id", "cannot be empty", peerID)
		}
		if err := ValidateStatus(status); err != nil {
			return fmt.Errorf("invalid status for peer %s: %w", peerID, err)
		}
	}

	// TODO: Add batch status update to repository interface
	// For now, fall back to individual updates
	for peerID, status := range updates {
		if err := s.UpdateStatus(ctx, peerID, status); err != nil {
			return fmt.Errorf("failed to update status for peer %s: %w", peerID, err)
		}
	}

	return nil
}

// generatePeerID generates a unique peer ID with minimal allocations
func generatePeerID() string {
	// Pre-allocate buffer to avoid multiple allocations
	const prefix = "peer_"
	nano := time.Now().UnixNano()

	// Use strings.Builder for efficient string building
	var builder strings.Builder
	builder.Grow(len(prefix) + 20) // Pre-allocate capacity
	builder.WriteString(prefix)
	builder.WriteString(fmt.Sprintf("%d", nano))
	return builder.String()
}
