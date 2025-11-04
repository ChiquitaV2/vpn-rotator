package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
)

// peerRepository implements peer.Repository using db.Store
type peerRepository struct {
	store db.Store
}

// NewPeerRepository creates a new peer repository
func NewPeerRepository(store db.Store) peer.Repository {
	return &peerRepository{
		store: store,
	}
}

// Create creates a new peer in the database
func (r *peerRepository) Create(ctx context.Context, p *peer.Peer) error {
	params := db.CreatePeerParams{
		ID:          p.ID,
		NodeID:      p.NodeID,
		PublicKey:   p.PublicKey,
		AllocatedIp: p.AllocatedIP,
		Status:      p.Status.String(),
	}

	if p.PresharedKey != nil {
		params.PresharedKey = sql.NullString{
			String: *p.PresharedKey,
			Valid:  true,
		}
	}

	_, err := r.store.CreatePeer(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create peer in database: %w", err)
	}

	return nil
}

// Get retrieves a peer by ID
func (r *peerRepository) Get(ctx context.Context, peerID string) (*peer.Peer, error) {
	dbPeer, err := r.store.GetPeer(ctx, peerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, peer.ErrPeerNotFound
		}
		return nil, fmt.Errorf("failed to get peer: %w", err)
	}

	return toDomainPeer(&dbPeer), nil
}

// GetByPublicKey retrieves a peer by public key
func (r *peerRepository) GetByPublicKey(ctx context.Context, publicKey string) (*peer.Peer, error) {
	dbPeer, err := r.store.GetPeerByPublicKey(ctx, publicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, peer.ErrPeerNotFound
		}
		return nil, fmt.Errorf("failed to get peer by public key: %w", err)
	}

	return toDomainPeer(&dbPeer), nil
}

// Delete deletes a peer by ID
func (r *peerRepository) Delete(ctx context.Context, peerID string) error {
	err := r.store.DeletePeer(ctx, peerID)
	if err != nil {
		return fmt.Errorf("failed to delete peer: %w", err)
	}

	return nil
}

// List retrieves peers based on filters
func (r *peerRepository) List(ctx context.Context, filters *peer.Filters) ([]*peer.Peer, error) {
	var dbPeers []db.Peer
	var err error

	// Apply filters
	if filters != nil && filters.NodeID != nil {
		dbPeers, err = r.store.GetPeersByNode(ctx, *filters.NodeID)
	} else if filters != nil && filters.Status != nil {
		dbPeers, err = r.store.GetPeersByStatus(ctx, filters.Status.String())
	} else {
		dbPeers, err = r.store.GetAllPeers(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}

	// Convert to domain peers
	peers := make([]*peer.Peer, 0, len(dbPeers))
	for i := range dbPeers {
		peers = append(peers, toDomainPeer(&dbPeers[i]))
	}

	// Apply pagination if specified
	if filters != nil {
		if filters.Offset != nil && *filters.Offset > 0 {
			if *filters.Offset >= len(peers) {
				return []*peer.Peer{}, nil
			}
			peers = peers[*filters.Offset:]
		}
		if filters.Limit != nil && *filters.Limit > 0 && *filters.Limit < len(peers) {
			peers = peers[:*filters.Limit]
		}
	}

	return peers, nil
}

// GetByNode retrieves all peers for a specific node
func (r *peerRepository) GetByNode(ctx context.Context, nodeID string) ([]*peer.Peer, error) {
	dbPeers, err := r.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peers by node: %w", err)
	}

	peers := make([]*peer.Peer, len(dbPeers))
	for i := range dbPeers {
		peers[i] = toDomainPeer(&dbPeers[i])
	}

	return peers, nil
}

// GetActiveByNode retrieves active peers for a specific node
func (r *peerRepository) GetActiveByNode(ctx context.Context, nodeID string) ([]*peer.Peer, error) {
	dbPeers, err := r.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peers by node: %w", err)
	}

	// Filter for active peers
	activePeers := make([]*peer.Peer, 0, len(dbPeers))
	for i := range dbPeers {
		if dbPeers[i].Status == string(peer.StatusActive) {
			activePeers = append(activePeers, toDomainPeer(&dbPeers[i]))
		}
	}

	return activePeers, nil
}

// GetInactive retrieves peers that have been inactive for the specified duration
func (r *peerRepository) GetInactive(ctx context.Context, inactiveDuration time.Duration) ([]*peer.Peer, error) {
	// Convert duration to minutes string for the query
	minutes := int(inactiveDuration.Minutes())
	dbPeers, err := r.store.GetInactivePeers(ctx, sql.NullString{
		String: fmt.Sprintf("%d", minutes),
		Valid:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get inactive peers: %w", err)
	}

	peers := make([]*peer.Peer, len(dbPeers))
	for i := range dbPeers {
		peers[i] = toDomainPeer(&dbPeers[i])
	}

	return peers, nil
}

// UpdateStatus updates a peer's status
func (r *peerRepository) UpdateStatus(ctx context.Context, peerID string, status peer.Status) error {
	err := r.store.UpdatePeerStatus(ctx, db.UpdatePeerStatusParams{
		ID:     peerID,
		Status: status.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to update peer status: %w", err)
	}

	return nil
}

// UpdateLastHandshake updates a peer's last handshake timestamp
func (r *peerRepository) UpdateLastHandshake(ctx context.Context, peerID string, timestamp time.Time) error {
	err := r.store.UpdatePeerLastHandshake(ctx, db.UpdatePeerLastHandshakeParams{
		ID: peerID,
		LastHandshakeAt: sql.NullTime{
			Time:  timestamp,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update peer last handshake: %w", err)
	}

	return nil
}

// UpdateNode updates a peer's node and IP address (for migration)
func (r *peerRepository) UpdateNode(ctx context.Context, peerID, nodeID, allocatedIP string) error {
	err := r.store.UpdatePeerNode(ctx, db.UpdatePeerNodeParams{
		ID:          peerID,
		NodeID:      nodeID,
		AllocatedIp: allocatedIP,
	})
	if err != nil {
		return fmt.Errorf("failed to update peer node: %w", err)
	}

	return nil
}

// CheckPublicKeyConflict checks if a public key is already in use
func (r *peerRepository) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	count, err := r.store.CheckPublicKeyConflict(ctx, publicKey)
	if err != nil {
		return false, fmt.Errorf("failed to check public key conflict: %w", err)
	}

	return count > 0, nil
}

// CheckIPConflict checks if an IP address is already allocated
func (r *peerRepository) CheckIPConflict(ctx context.Context, ipAddress string) (bool, error) {
	count, err := r.store.CheckIPConflict(ctx, ipAddress)
	if err != nil {
		return false, fmt.Errorf("failed to check IP conflict: %w", err)
	}

	return count > 0, nil
}

// DeleteByNode deletes all peers for a specific node
func (r *peerRepository) DeleteByNode(ctx context.Context, nodeID string) error {
	err := r.store.DeletePeersByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete peers by node: %w", err)
	}

	return nil
}

// CountByNode counts all peers for a specific node
func (r *peerRepository) CountByNode(ctx context.Context, nodeID string) (int64, error) {
	peers, err := r.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count peers by node: %w", err)
	}

	return int64(len(peers)), nil
}

// CountActiveByNode counts active peers for a specific node
func (r *peerRepository) CountActiveByNode(ctx context.Context, nodeID string) (int64, error) {
	peers, err := r.store.GetPeersByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to count active peers: %w", err)
	}

	var count int64
	for _, p := range peers {
		if p.Status == string(peer.StatusActive) {
			count++
		}
	}

	return count, nil
}

// GetStatistics retrieves peer statistics
func (r *peerRepository) GetStatistics(ctx context.Context) (*peer.Statistics, error) {
	dbStats, err := r.store.GetPeerStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer statistics: %w", err)
	}

	stats := &peer.Statistics{
		TotalPeers: dbStats.TotalPeers,
	}

	if dbStats.ActivePeers.Valid {
		stats.ActivePeers = int64(dbStats.ActivePeers.Float64)
	}
	if dbStats.DisconnectedPeers.Valid {
		stats.DisconnectedPeers = int64(dbStats.DisconnectedPeers.Float64)
	}
	if dbStats.RemovingPeers.Valid {
		stats.RemovingPeers = int64(dbStats.RemovingPeers.Float64)
	}

	return stats, nil
}

// toDomainPeer converts a database peer to a domain peer
func toDomainPeer(dbPeer *db.Peer) *peer.Peer {
	p := &peer.Peer{
		ID:          dbPeer.ID,
		NodeID:      dbPeer.NodeID,
		PublicKey:   dbPeer.PublicKey,
		AllocatedIP: dbPeer.AllocatedIp,
		Status:      peer.Status(dbPeer.Status),
		CreatedAt:   dbPeer.CreatedAt,
		UpdatedAt:   dbPeer.UpdatedAt,
	}

	if dbPeer.PresharedKey.Valid {
		p.PresharedKey = &dbPeer.PresharedKey.String
	}

	if dbPeer.LastHandshakeAt.Valid {
		p.LastHandshakeAt = &dbPeer.LastHandshakeAt.Time
	}

	return p
}
