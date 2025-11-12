package peer

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

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
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}
	if err := validatePublicKey(publicKey); err != nil {
		return nil, err
	}
	if err := validateIPAddress(allocatedIP); err != nil {
		return nil, err
	}
	if presharedKey != nil {
		if err := validatePresharedKey(*presharedKey); err != nil {
			return nil, err
		}
	}

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
	if err := validateNodeID(newNodeID); err != nil {
		return err
	}
	if err := validateIPAddress(newIP); err != nil {
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

// Validate validates the create request
func (r *CreateRequest) Validate() error {
	if r == nil {
		return apperrors.NewSystemError(apperrors.ErrCodeValidation, "create request cannot be nil", false, nil)
	}

	if err := validateNodeID(r.NodeID); err != nil {
		return err
	}

	if err := validatePublicKey(r.PublicKey); err != nil {
		return err
	}

	if err := validateIPAddress(r.AllocatedIP); err != nil {
		return err
	}

	if r.PresharedKey != nil {
		if err := validatePresharedKey(*r.PresharedKey); err != nil {
			return err
		}
	}

	return nil
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

// validation functions

func validatePublicKey(publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "public_key cannot be empty", false, nil).
			WithMetadata("public_key", publicKey)
	}

	if !crypto.IsValidWireGuardKey(publicKey) {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "invalid WireGuard key format", false, nil).
			WithMetadata("public_key", publicKey)
	}

	return nil
}

func validatePresharedKey(presharedKey string) error {
	if strings.TrimSpace(presharedKey) == "" {
		return nil // Empty is valid (optional)
	}

	if !crypto.IsValidWireGuardKey(presharedKey) {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "invalid WireGuard key format", false, nil).
			WithMetadata("preshared_key", "REDACTED")
	}

	return nil
}

var (
	// Cache for validated IPs to avoid repeated parsing
	ipCache     = sync.Map{}
	cacheConfig = DefaultConfig()
)

func validateIPAddress(ipAddr string) error {
	if ipAddr == "" {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "ip_address cannot be empty", false, nil).
			WithMetadata("ip_address", ipAddr)
	}

	// Check cache first
	if _, exists := ipCache.Load(ipAddr); exists {
		return nil
	}

	parsedIP := net.ParseIP(ipAddr)
	if parsedIP == nil {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "invalid IP address format", false, nil).
			WithMetadata("ip_address", ipAddr)
	}

	// Ensure it's IPv4
	if parsedIP.To4() == nil {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "must be IPv4", false, nil).
			WithMetadata("ip_address", ipAddr)
	}

	// Cache valid IP if caching is enabled and under limit
	if cacheConfig.EnableIPCache && GetCacheSize() < cacheConfig.MaxCacheSize {
		ipCache.Store(ipAddr, struct{}{})
	}
	return nil
}

func validateNodeID(nodeID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return apperrors.NewPeerError(apperrors.ErrCodePeerValidation, "node_id cannot be empty", false, nil).
			WithMetadata("node_id", nodeID)
	}

	return nil
}

// Object Pool for Peers

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

// ClearIPCache clears the IP validation cache
func ClearIPCache() {
	ipCache = sync.Map{}
}

// GetCacheSize returns the approximate size of the IP cache
func GetCacheSize() int {
	count := 0
	ipCache.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}
