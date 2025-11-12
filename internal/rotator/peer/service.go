package peer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// service implements the Service interface
type service struct {
	repo   Repository
	logger *applogger.Logger
}

// NewService creates a new peer service
func NewService(repo Repository, logger *applogger.Logger) Service {
	return &service{
		repo:   repo,
		logger: logger.WithComponent("peer.service"),
	}
}

// Create creates a new peer with validation
func (s *service) Create(ctx context.Context, req *CreateRequest) (*Peer, error) {
	op := s.logger.StartOp(ctx, "CreatePeer",
		slog.String("public_key", req.PublicKey))

	// Validate request
	if err := req.Validate(); err != nil {
		op.Fail(err, "validation failed")
		return nil, err
	}

	// Check for public key conflicts
	conflict, err := s.repo.CheckPublicKeyConflict(ctx, req.PublicKey)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to check public key conflict", true)
		op.Fail(domainErr, "failed to check public key conflict")
		return nil, domainErr
	}
	if conflict {
		domainErr := apperrors.NewPeerError(apperrors.ErrCodePeerKeyConflict, "public key already in use", false, nil).
			WithMetadata("public_key", req.PublicKey)
		s.logger.WarnContext(ctx, "public key conflict", slog.String("public_key", req.PublicKey))
		op.Fail(domainErr, "public key conflict")
		return nil, domainErr
	}

	// Check for IP conflicts
	ipConflict, err := s.repo.CheckIPConflict(ctx, req.AllocatedIP)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to check IP conflict", true)
		op.Fail(domainErr, "failed to check IP conflict")
		return nil, domainErr
	}
	if ipConflict {
		domainErr := apperrors.NewPeerError(apperrors.ErrCodePeerIPConflict, "IP address already allocated", false, nil).
			WithMetadata("ip_address", req.AllocatedIP)
		s.logger.WarnContext(ctx, "IP address conflict", slog.String("ip_address", req.AllocatedIP))
		op.Fail(domainErr, "IP address conflict")
		return nil, domainErr
	}

	// Create peer entity
	peer, err := NewPeer(req.NodeID, req.PublicKey, req.AllocatedIP, req.PresharedKey)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeInternal, "failed to create peer entity", false)
		op.Fail(domainErr, "failed to create peer entity")
		return nil, domainErr
	}

	// Generate ID
	peer.ID = generatePeerID()

	// Persist to repository
	if err := s.repo.Create(ctx, peer); err != nil {
		// Repo already returns a DomainError
		op.Fail(err, "failed to persist peer")
		return nil, err
	}

	op.Complete("peer created successfully", slog.String("peer_id", peer.ID))
	return peer, nil
}

// Get retrieves a peer by ID
func (s *service) Get(ctx context.Context, peerID string) (*Peer, error) {
	op := s.logger.StartOp(ctx, "GetPeer", slog.String("peer_id", peerID))

	if peerID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "peer_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return nil, err
	}

	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		// Repo already returns a DomainError
		op.Fail(err, "failed to get peer")
		return nil, err
	}

	op.Complete("peer retrieved successfully")
	return peer, nil
}

// GetByPublicKey retrieves a peer by its public key
func (s *service) GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error) {
	op := s.logger.StartOp(ctx, "GetPeerByPublicKey", slog.String("public_key", publicKey))

	if publicKey == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "public_key cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return nil, err
	}

	peer, err := s.repo.GetByPublicKey(ctx, publicKey)
	if err != nil {
		// Repo already returns a DomainError
		op.Fail(err, "failed to get peer by public key")
		return nil, err
	}

	op.Complete("peer retrieved by public key successfully")
	return peer, nil
}

// List retrieves peers based on filters
func (s *service) List(ctx context.Context, filters *Filters) ([]*Peer, error) {
	op := s.logger.StartOp(ctx, "ListPeers")

	peers, err := s.repo.List(ctx, filters)
	if err != nil {
		// Repo already returns a DomainError
		op.Fail(err, "failed to list peers")
		return nil, err
	}

	op.Complete("peers listed successfully", slog.Int("count", len(peers)))
	return peers, nil
}

// Remove removes a peer with proper cleanup
func (s *service) Remove(ctx context.Context, peerID string) error {
	op := s.logger.StartOp(ctx, "RemovePeer", slog.String("peer_id", peerID))

	if peerID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "peer_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return err
	}

	// Check if peer exists
	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer for removal")
		return err
	}

	// Mark as removing first (best effort)
	if err := s.repo.UpdateStatus(ctx, peerID, StatusRemoving); err != nil {
		s.logger.WarnContext(ctx, "failed to update peer status to removing (best effort), continuing deletion",
			"error", err, "peer_id", peerID)
	}

	// Delete the peer
	if err := s.repo.Delete(ctx, peerID); err != nil {
		op.Fail(err, "failed to delete peer")
		return err
	}

	// If we had additional cleanup (e.g., notify node, revoke certs), do it here
	_ = peer // Placeholder for future cleanup logic

	op.Complete("peer removed successfully")
	return nil
}

// UpdateStatus updates a peer's status with validation
func (s *service) UpdateStatus(ctx context.Context, peerID string, status Status) error {
	op := s.logger.StartOp(ctx, "UpdateStatus",
		slog.String("peer_id", peerID),
		slog.String("new_status", status.String()))

	if peerID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "peer_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return err
	}

	// Get current peer to validate transition
	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer for status update")
		return err
	}

	// Validate status transition
	if err := peer.UpdateStatus(status); err != nil {
		op.Fail(err, "invalid status transition")
		return err
	}

	// Update status
	if err := s.repo.UpdateStatus(ctx, peerID, status); err != nil {
		op.Fail(err, "failed to update peer status in repo")
		return err
	}

	op.Complete("peer status updated successfully")
	return nil
}

// Migrate migrates a peer to a new node with a new IP
func (s *service) Migrate(ctx context.Context, peerID, newNodeID, newIP string) error {
	op := s.logger.StartOp(ctx, "MigratePeer",
		slog.String("peer_id", peerID),
		slog.String("new_node_id", newNodeID),
		slog.String("new_ip", newIP))

	if peerID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "peer_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return err
	}

	// Get peer to call domain logic
	peer, err := s.repo.Get(ctx, peerID)
	if err != nil {
		op.Fail(err, "failed to get peer for migration")
		return err
	}

	if err := peer.Migrate(newNodeID, newIP); err != nil {
		op.Fail(err, "migration validation failed")
		return err
	}

	// Check for IP conflicts (excluding current peer)
	ipConflict, err := s.repo.CheckIPConflict(ctx, newIP)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to check IP conflict", true)
		op.Fail(domainErr, "failed to check IP conflict")
		return domainErr
	}
	if ipConflict {
		// Double-check it's not the same peer (though migration implies it's a new IP)
		existingPeer, _ := s.repo.Get(ctx, peerID)
		if existingPeer != nil && existingPeer.AllocatedIP != newIP {
			domainErr := apperrors.NewPeerError(apperrors.ErrCodePeerIPConflict, "IP address already allocated", false, nil).
				WithMetadata("ip_address", newIP)
			s.logger.WarnContext(ctx, "IP conflict during migration", slog.String("ip_address", newIP))
			op.Fail(domainErr, "IP conflict during migration")
			return domainErr
		}
	}

	// Update node and IP
	if err := s.repo.UpdateNode(ctx, peerID, newNodeID, newIP); err != nil {
		op.Fail(err, "failed to migrate peer in repo")
		return err
	}

	op.Complete("peer migrated successfully")
	return nil
}

// GetByNode retrieves all peers for a specific node
func (s *service) GetByNode(ctx context.Context, nodeID string) ([]*Peer, error) {
	op := s.logger.StartOp(ctx, "GetByNode", slog.String("node_id", nodeID))

	if nodeID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "node_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return nil, err
	}

	peers, err := s.repo.GetByNode(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get peers by node from repo", err)
		op.Fail(err, "failed to get peers by node from repo")
		return nil, err
	}

	op.Complete("peers retrieved by node successfully", slog.Int("count", len(peers)))
	return peers, nil
}

// GetActiveByNode retrieves active peers for a specific node
func (s *service) GetActiveByNode(ctx context.Context, nodeID string) ([]*Peer, error) {
	op := s.logger.StartOp(ctx, "GetActiveByNode", slog.String("node_id", nodeID))

	if nodeID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "node_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return nil, err
	}

	peers, err := s.repo.GetActiveByNode(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get active peers by node from repo", err)
		op.Fail(err, "failed to get active peers by node from repo")
		return nil, err
	}

	op.Complete("active peers retrieved by node successfully", slog.Int("count", len(peers)))
	return peers, nil
}

// CountActiveByNode counts active peers for a specific node
func (s *service) CountActiveByNode(ctx context.Context, nodeID string) (int64, error) {
	op := s.logger.StartOp(ctx, "CountActiveByNode", slog.String("node_id", nodeID))

	if nodeID == "" {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "node_id cannot be empty", false, nil)
		op.Fail(err, "validation failed")
		return 0, err
	}

	count, err := s.repo.CountActiveByNode(ctx, nodeID)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to count active peers", true)
		op.Fail(domainErr, "failed to count active peers from repo")
		return 0, domainErr
	}

	op.Complete("active peers counted successfully", slog.Int64("count", count))
	return count, nil
}

// GetInactive retrieves peers that have been inactive for the specified duration
func (s *service) GetInactive(ctx context.Context, inactiveMinutes int) ([]*Peer, error) {
	op := s.logger.StartOp(ctx, "GetInactive", slog.Int("inactive_minutes", inactiveMinutes))

	if inactiveMinutes <= 0 {
		err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "inactive_minutes must be positive", false, nil).
			WithMetadata("inactive_minutes", inactiveMinutes)
		op.Fail(err, "validation failed")
		return nil, err
	}

	duration := time.Duration(inactiveMinutes) * time.Minute
	peers, err := s.repo.GetInactive(ctx, duration)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to get inactive peers", true)
		op.Fail(domainErr, "failed to get inactive peers from repo")
		return nil, domainErr
	}

	op.Complete("inactive peers retrieved successfully", slog.Int("count", len(peers)))
	return peers, nil
}

// GetStatistics retrieves peer statistics
func (s *service) GetStatistics(ctx context.Context) (*Statistics, error) {
	op := s.logger.StartOp(ctx, "GetStatistics")

	stats, err := s.repo.GetStatistics(ctx)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeDatabase, "failed to get peer statistics", true)
		op.Fail(domainErr, "failed to get peer statistics from repo")
		return nil, domainErr
	}

	op.Complete("statistics retrieved successfully", slog.Int64("total_peers", stats.TotalPeers))
	return stats, nil
}

// CreateBatch creates multiple peers in a single transaction for better performance
func (s *service) CreateBatch(ctx context.Context, requests []*CreateRequest) ([]*Peer, error) {
	op := s.logger.StartOp(ctx, "CreateBatch", slog.Int("batch_size", len(requests)))

	if len(requests) == 0 {
		op.Complete("empty batch")
		return []*Peer{}, nil
	}

	// Pre-validate all requests
	for i, req := range requests {
		if err := req.Validate(); err != nil {
			if domainErr, ok := err.(apperrors.DomainError); ok {
				err = domainErr.WithMetadata("batch_index", i)
			} else {
				err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeValidation, "failed to validate create request in batch", false).WithMetadata("batch_index", i)
			}
			op.Fail(err, "validation failed for batch request")
			return nil, err
		}
	}

	// TODO: Add batch conflict checking to repository interface
	// For now, fall back to individual checks
	peers := make([]*Peer, 0, len(requests))
	for _, req := range requests {
		peer, err := s.Create(ctx, req)
		if err != nil {
			// Create already logs and wraps, just bubble it up
			op.Fail(err, "failed to create peer in batch")
			return nil, apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeInternal, "failed to create peer in batch", false)
		}
		peers = append(peers, peer)
	}

	op.Complete("batch creation completed successfully", slog.Int("count", len(peers)))
	return peers, nil
}

// UpdateStatusBatch updates multiple peer statuses efficiently
func (s *service) UpdateStatusBatch(ctx context.Context, updates map[string]Status) error {
	op := s.logger.StartOp(ctx, "UpdateStatusBatch", slog.Int("batch_size", len(updates)))

	if len(updates) == 0 {
		op.Complete("empty batch")
		return nil
	}

	// Validate all statuses first
	for peerID, status := range updates {
		if peerID == "" {
			err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "peer_id cannot be empty", false, nil)
			op.Fail(err, "validation failed")
			return err
		}
		if !status.IsValid() {
			err := apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "invalid status", false, nil).
				WithMetadata("status", status)
			if domainErr, ok := err.(apperrors.DomainError); ok {
				err = domainErr.WithMetadata("peer_id", peerID)
			} else {
				err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeValidation, "failed to validate status in batch", false).WithMetadata("peer_id", peerID)
			}
			op.Fail(err, "validation failed for batch status update")
			return err
		}
	}

	// TODO: Add batch status update to repository interface
	// For now, fall back to individual updates
	for peerID, status := range updates {
		if err := s.UpdateStatus(ctx, peerID, status); err != nil {
			if domainErr, ok := err.(apperrors.DomainError); ok {
				err = domainErr.WithMetadata("peer_id", peerID)
			} else {
				err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeInternal, "failed to update status for peer in batch", false).WithMetadata("peer_id", peerID)
			}
			op.Fail(err, "failed to update status for peer in batch")
			return apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodeInternal, "failed to update status for peer in batch", false)
		}
	}

	op.Complete("batch status update completed successfully")
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
