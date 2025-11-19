package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// peerRepository implements peer.Repository using db.Store
type peerRepository struct {
	store  db.Store
	logger *applogger.Logger
}

// NewPeerRepository creates a new peer repository
func NewPeerRepository(store db.Store, log *applogger.Logger) peer.Repository {
	return &peerRepository{
		store:  store,
		logger: log.WithComponent("peer.repository"),
	}
}

// Create creates a new peer in the database
func (r *peerRepository) Create(ctx context.Context, p *peer.Peer) error {
	start := time.Now()
	params := db.CreatePeerParams{
		ID:             p.ID,
		NodeID:         p.NodeID,
		Protocol:       p.Protocol,
		Identifier:     p.Identifier,
		ProtocolConfig: sql.NullString{Valid: false},
		PublicKey:      sql.NullString{Valid: false},
		AllocatedIp:    p.AllocatedIP,
		PresharedKey:   sql.NullString{Valid: false},
		Status:         p.Status.String(),
	}

	if p.Protocol == "wireguard" {
		if p.PublicKey != "" {
			params.PublicKey = sql.NullString{String: p.PublicKey, Valid: true}
		}
		if p.PresharedKey != nil {
			params.PresharedKey = sql.NullString{String: *p.PresharedKey, Valid: true}
		}
	}

	_, err := r.store.CreatePeer(ctx, params)
	r.logger.DBQuery(ctx, "CreatePeer", "peers", time.Since(start), slog.String("peer_id", p.ID))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to create peer in database", err, slog.String("peer_id", p.ID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to create peer in database", true, err)
	}
	return nil
}

// Get retrieves a peer by ID
func (r *peerRepository) Get(ctx context.Context, peerID string) (*peer.Peer, error) {
	start := time.Now()
	dbPeer, err := r.store.GetPeer(ctx, peerID)
	r.logger.DBQuery(ctx, "GetPeer", "peers", time.Since(start), slog.String("peer_id", peerID))

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s not found", peerID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to get peer", err, slog.String("peer_id", peerID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get peer", true, err)
	}

	return toDomainPeer(&dbPeer), nil
}

// GetByPublicKey retrieves a peer by public key
func (r *peerRepository) GetByPublicKey(ctx context.Context, publicKey string) (*peer.Peer, error) {
	return r.GetByIdentifier(ctx, "wireguard", publicKey)
}

// GetByIdentifier retrieves a peer by protocol + identifier (protocol-agnostic API)
func (r *peerRepository) GetByIdentifier(ctx context.Context, protocolName, identifier string) (*peer.Peer, error) {
	start := time.Now()
	dbPeer, err := r.store.GetPeerByIdentifier(ctx, db.GetPeerByIdentifierParams{Protocol: protocolName, Identifier: identifier})
	r.logger.DBQuery(ctx, "GetPeerByIdentifier", "peers", time.Since(start), slog.String("protocol", protocolName), slog.String("identifier", identifier))

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s:%s not found", protocolName, identifier), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to get peer by identifier", err, slog.String("protocol", protocolName), slog.String("identifier", identifier))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get peer by identifier", true, err)
	}
	return toDomainPeer(&dbPeer), nil
}

// Delete deletes a peer by ID
func (r *peerRepository) Delete(ctx context.Context, peerID string) error {
	start := time.Now()
	err := r.store.DeletePeer(ctx, peerID)
	r.logger.DBQuery(ctx, "DeletePeer", "peers", time.Since(start), slog.String("peer_id", peerID))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s not found for deletion", peerID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to delete peer", err, slog.String("peer_id", peerID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to delete peer", true, err)
	}
	return nil
}

// List retrieves peers based on filters
func (r *peerRepository) List(ctx context.Context, filters *peer.Filters) ([]*peer.Peer, error) {
	start := time.Now()
	var dbPeers []db.Peer
	var err error

	if filters != nil && filters.NodeID != nil {
		dbPeers, err = r.store.GetPeersByNode(ctx, *filters.NodeID)
	} else if filters != nil && filters.Status != nil {
		dbPeers, err = r.store.GetPeersByStatus(ctx, filters.Status.String())
	} else {
		dbPeers, err = r.store.GetAllPeers(ctx)
	}
	r.logger.DBQuery(ctx, "ListPeers", "peers", time.Since(start), slog.Any("filters", filters))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to list peers", err, slog.Any("filters", filters))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to list peers", true, err)
	}

	peers := make([]*peer.Peer, 0, len(dbPeers))
	for i := range dbPeers {
		peers = append(peers, toDomainPeer(&dbPeers[i]))
	}

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
	start := time.Now()
	dbPeers, err := r.store.GetPeersByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "GetPeersByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get peers by node", err, slog.String("node_id", nodeID))
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get peers by node", true, err)
	}

	peers := make([]*peer.Peer, len(dbPeers))
	for i := range dbPeers {
		peers[i] = toDomainPeer(&dbPeers[i])
	}
	return peers, nil
}

// GetActiveByNode retrieves active peers for a specific node
func (r *peerRepository) GetActiveByNode(ctx context.Context, nodeID string) ([]*peer.Peer, error) {
	peers, err := r.GetByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	activePeers := make([]*peer.Peer, 0)
	for _, p := range peers {
		if p.Status == peer.StatusActive {
			activePeers = append(activePeers, p)
		}
	}
	return activePeers, nil
}

// GetInactive retrieves peers that have been inactive for the specified duration
func (r *peerRepository) GetInactive(ctx context.Context, inactiveDuration time.Duration) ([]*peer.Peer, error) {
	minutes := int(inactiveDuration.Minutes())
	start := time.Now()
	dbPeers, err := r.store.GetInactivePeers(ctx, sql.NullString{
		String: fmt.Sprintf("%d", minutes),
		Valid:  true,
	})
	r.logger.DBQuery(ctx, "GetInactivePeers", "peers", time.Since(start), slog.Int("inactive_minutes", minutes))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get inactive peers", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get inactive peers", true, err)
	}

	peers := make([]*peer.Peer, len(dbPeers))
	for i := range dbPeers {
		peers[i] = toDomainPeer(&dbPeers[i])
	}
	return peers, nil
}

// UpdateStatus updates a peer's status
func (r *peerRepository) UpdateStatus(ctx context.Context, peerID string, status peer.Status) error {
	start := time.Now()
	err := r.store.UpdatePeerStatus(ctx, db.UpdatePeerStatusParams{
		ID:     peerID,
		Status: status.String(),
	})
	r.logger.DBQuery(ctx, "UpdatePeerStatus", "peers", time.Since(start), slog.String("peer_id", peerID), slog.String("status", status.String()))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s not found for status update", peerID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to update peer status", err, slog.String("peer_id", peerID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update peer status", true, err)
	}
	return nil
}

// UpdateLastHandshake updates a peer's last handshake timestamp
func (r *peerRepository) UpdateLastHandshake(ctx context.Context, peerID string, timestamp time.Time) error {
	start := time.Now()
	err := r.store.UpdatePeerLastHandshake(ctx, db.UpdatePeerLastHandshakeParams{
		ID:              peerID,
		LastHandshakeAt: sql.NullTime{Time: timestamp, Valid: true},
	})
	r.logger.DBQuery(ctx, "UpdatePeerLastHandshake", "peers", time.Since(start), slog.String("peer_id", peerID))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s not found for handshake update", peerID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to update peer last handshake", err, slog.String("peer_id", peerID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update peer last handshake", true, err)
	}
	return nil
}

// UpdateNode updates a peer's node and IP address (for migration)
func (r *peerRepository) UpdateNode(ctx context.Context, peerID, nodeID, allocatedIP string) error {
	start := time.Now()
	err := r.store.UpdatePeerNode(ctx, db.UpdatePeerNodeParams{
		ID:          peerID,
		NodeID:      nodeID,
		AllocatedIp: allocatedIP,
	})
	r.logger.DBQuery(ctx, "UpdatePeerNode", "peers", time.Since(start), slog.String("peer_id", peerID), slog.String("new_node_id", nodeID))

	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NewPeerError(apperrors.ErrCodePeerNotFound, fmt.Sprintf("peer %s not found for node migration", peerID), false, err)
		}
		r.logger.ErrorCtx(ctx, "failed to update peer node", err, slog.String("peer_id", peerID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to update peer node", true, err)
	}
	return nil
}

// CheckPublicKeyConflict checks if a public key is already in use
func (r *peerRepository) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	return r.CheckIdentifierConflict(ctx, "wireguard", publicKey)
}

// CheckIdentifierConflict checks if an identifier is already in use for a protocol
func (r *peerRepository) CheckIdentifierConflict(ctx context.Context, protocolName, identifier string) (bool, error) {
	start := time.Now()
	count, err := r.store.CheckIdentifierConflict(ctx, db.CheckIdentifierConflictParams{Protocol: protocolName, Identifier: identifier})
	r.logger.DBQuery(ctx, "CheckIdentifierConflict", "peers", time.Since(start), slog.String("protocol", protocolName), slog.String("identifier", identifier))
	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to check identifier conflict", err)
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check identifier conflict", true, err)
	}
	return count > 0, nil
}

// CheckIPConflict checks if an IP address is already allocated
func (r *peerRepository) CheckIPConflict(ctx context.Context, ipAddress string) (bool, error) {
	start := time.Now()
	count, err := r.store.CheckIPConflict(ctx, ipAddress)
	r.logger.DBQuery(ctx, "CheckIPConflict", "peers", time.Since(start), slog.String("ip_address", ipAddress))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to check IP conflict", err)
		return false, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to check IP conflict", true, err)
	}
	return count > 0, nil
}

// DeleteByNode deletes all peers for a specific node
func (r *peerRepository) DeleteByNode(ctx context.Context, nodeID string) error {
	start := time.Now()
	err := r.store.DeletePeersByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "DeletePeersByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to delete peers by node", err, slog.String("node_id", nodeID))
		return apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to delete peers by node", true, err)
	}
	return nil
}

// CountByNode counts all peers for a specific node
func (r *peerRepository) CountByNode(ctx context.Context, nodeID string) (int64, error) {
	start := time.Now()
	count, err := r.store.CountPeersByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "CountPeersByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to count peers by node", err, slog.String("node_id", nodeID))
		return 0, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to count peers by node", true, err)
	}
	return count, nil
}

// CountActiveByNode counts active peers for a specific node
func (r *peerRepository) CountActiveByNode(ctx context.Context, nodeID string) (int64, error) {
	start := time.Now()
	count, err := r.store.CountActivePeersByNode(ctx, nodeID)
	r.logger.DBQuery(ctx, "CountActivePeersByNode", "peers", time.Since(start), slog.String("node_id", nodeID))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to count active peers by node", err, slog.String("node_id", nodeID))
		return 0, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to count active peers by node", true, err)
	}
	return count, nil
}

// GetStatistics retrieves peer statistics
func (r *peerRepository) GetStatistics(ctx context.Context) (*peer.Statistics, error) {
	start := time.Now()
	dbStats, err := r.store.GetPeerStatistics(ctx)
	r.logger.DBQuery(ctx, "GetPeerStatistics", "peers", time.Since(start))

	if err != nil {
		r.logger.ErrorCtx(ctx, "failed to get peer statistics", err)
		return nil, apperrors.NewDatabaseError(apperrors.ErrCodeDatabase, "failed to get peer statistics", true, err)
	}

	stats := &peer.Statistics{TotalPeers: dbStats.TotalPeers}
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
		Protocol:    dbPeer.Protocol,
		Identifier:  dbPeer.Identifier,
		AllocatedIP: dbPeer.AllocatedIp,
		Status:      peer.Status(dbPeer.Status),
		CreatedAt:   dbPeer.CreatedAt,
		UpdatedAt:   dbPeer.UpdatedAt,
	}

	if dbPeer.PublicKey.Valid && dbPeer.Protocol == "wireguard" {
		p.PublicKey = dbPeer.PublicKey.String
	}
	if dbPeer.PresharedKey.Valid && dbPeer.Protocol == "wireguard" {
		p.PresharedKey = &dbPeer.PresharedKey.String
	}
	if dbPeer.LastHandshakeAt.Valid {
		p.LastHandshakeAt = &dbPeer.LastHandshakeAt.Time
	}
	return p
}
