package peer

import (
	"context"
	"time"
)

// Repository defines the interface for peer data access
type Repository interface {
	// Core CRUD operations
	Create(ctx context.Context, peer *Peer) error
	Get(ctx context.Context, peerID string) (*Peer, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error)
	Delete(ctx context.Context, peerID string) error

	// Query operations
	List(ctx context.Context, filters *Filters) ([]*Peer, error)
	GetByNode(ctx context.Context, nodeID string) ([]*Peer, error)
	GetActiveByNode(ctx context.Context, nodeID string) ([]*Peer, error)
	GetInactive(ctx context.Context, inactiveDuration time.Duration) ([]*Peer, error)

	// Update operations
	UpdateStatus(ctx context.Context, peerID string, status Status) error
	UpdateLastHandshake(ctx context.Context, peerID string, timestamp time.Time) error
	UpdateNode(ctx context.Context, peerID, nodeID, allocatedIP string) error

	// Conflict checking
	CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error)
	CheckIPConflict(ctx context.Context, ipAddress string) (bool, error)

	// Batch operations
	DeleteByNode(ctx context.Context, nodeID string) error
	CountByNode(ctx context.Context, nodeID string) (int64, error)
	CountActiveByNode(ctx context.Context, nodeID string) (int64, error)

	// Statistics
	GetStatistics(ctx context.Context) (*Statistics, error)
}
