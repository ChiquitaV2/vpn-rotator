package peer

import (
	"fmt"
	"sync"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// Pool for reusing Peer objects to reduce GC pressure
var peerPool = sync.Pool{
	New: func() interface{} {
		return &Peer{}
	},
}

// GetPeerFromPool gets a peer from the pool
func GetPeerFromPool() *Peer {
	return peerPool.Get().(*Peer)
}

// ReturnPeerToPool returns a peer to the pool after resetting it
func ReturnPeerToPool(p *Peer) {
	// Reset the peer
	*p = Peer{}
	peerPool.Put(p)
}

// Peer represents a WireGuard peer domain entity
type Peer struct {
	ID              string
	NodeID          string
	PublicKey       string
	AllocatedIP     string
	PresharedKey    *string
	Status          Status
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastHandshakeAt *time.Time
}

// NewPeer creates a new peer with validation using object pool
func NewPeer(nodeID, publicKey, allocatedIP string, presharedKey *string) (*Peer, error) {
	// Get peer from pool to reduce allocations
	peer := GetPeerFromPool()

	// Initialize with provided values
	peer.NodeID = nodeID
	peer.PublicKey = publicKey
	peer.AllocatedIP = allocatedIP
	peer.PresharedKey = presharedKey
	peer.Status = StatusActive
	peer.CreatedAt = time.Now()
	peer.UpdatedAt = time.Now()

	return peer, nil
}

// UpdateStatus changes the peer status with validation
func (p *Peer) UpdateStatus(newStatus Status) error {
	if !newStatus.IsValid() {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "invalid status", false, nil).
			WithMetadata("status", newStatus)
	}

	if !p.Status.CanTransitionTo(newStatus) {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation,
			fmt.Sprintf("invalid status transition from %s to %s", p.Status, newStatus), false, nil)
	}

	p.Status = newStatus
	p.UpdatedAt = time.Now()
	return nil
}

// IsActive returns true if the peer is active
func (p *Peer) IsActive() bool {
	return p.Status == StatusActive
}

// Migrate updates the peer's node and IP address
func (p *Peer) Migrate(newNodeID, newIP string) error {
	if err := ValidateNodeID(newNodeID); err != nil {
		return err
	}
	if err := ValidateIPAddress(newIP); err != nil {
		return err
	}

	p.NodeID = newNodeID
	p.AllocatedIP = newIP
	p.UpdatedAt = time.Now()
	return nil
}

// UpdateLastHandshake updates the last handshake timestamp
func (p *Peer) UpdateLastHandshake(timestamp time.Time) {
	p.LastHandshakeAt = &timestamp
	p.UpdatedAt = time.Now()
}

// CreateRequest represents a request to create a new peer
type CreateRequest struct {
	NodeID       string
	PublicKey    string
	AllocatedIP  string
	PresharedKey *string
}

// Validate validates the create request by delegating to the validation function
func (r *CreateRequest) Validate() error {
	return ValidateCreateRequest(r)
}

// Filters represents filters for listing peers
type Filters struct {
	NodeID    *string
	Status    *Status
	PublicKey *string
	Limit     *int
	Offset    *int
}

// Statistics represents peer statistics across the system
type Statistics struct {
	TotalPeers        int64
	ActivePeers       int64
	DisconnectedPeers int64
	RemovingPeers     int64
}
