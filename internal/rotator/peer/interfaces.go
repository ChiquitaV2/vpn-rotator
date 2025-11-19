package peer

import (
	"context"
	"time"
)

// Service defines the business logic interface for peer operations
type Service interface {
	// Core operations
	Create(ctx context.Context, req *CreateRequest) (*Peer, error)
	Get(ctx context.Context, peerID string) (*Peer, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error)
	// Protocol-agnostic lookup
	GetByIdentifier(ctx context.Context, protocol, identifier string) (*Peer, error)
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

// Repository defines the interface for peer data access
type Repository interface {
	// Core CRUD operations
	Create(ctx context.Context, peer *Peer) error
	Get(ctx context.Context, peerID string) (*Peer, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*Peer, error)
	// Protocol-agnostic lookup
	GetByIdentifier(ctx context.Context, protocol, identifier string) (*Peer, error)
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
	// Protocol-agnostic conflict
	CheckIdentifierConflict(ctx context.Context, protocol, identifier string) (bool, error)
	CheckIPConflict(ctx context.Context, ipAddress string) (bool, error)

	// Batch operations
	DeleteByNode(ctx context.Context, nodeID string) error
	CountByNode(ctx context.Context, nodeID string) (int64, error)
	CountActiveByNode(ctx context.Context, nodeID string) (int64, error)

	// Statistics
	GetStatistics(ctx context.Context) (*Statistics, error)
}
