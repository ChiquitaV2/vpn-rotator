package node

import (
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// Node represents a VPN server node domain entity
type Node struct {
	ID               string     `json:"id" db:"id"`
	ServerID         string     `json:"server_id" db:"server_id"`
	IPAddress        string     `json:"ip_address" db:"ip_address"`
	ServerPublicKey  string     `json:"server_public_key" db:"server_public_key"`
	Port             int        `json:"port" db:"port"`
	Status           Status     `json:"status" db:"status"`
	Version          int64      `json:"version" db:"version"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
	DestroyAt        *time.Time `json:"destroy_at,omitempty" db:"destroy_at"`
	LastHandshakeAt  *time.Time `json:"last_handshake_at,omitempty" db:"last_handshake_at"`
	ConnectedClients int        `json:"connected_clients" db:"connected_clients"`
}

// NewNode creates a new node with validation
func NewNode(nodeID, ipAddress, serverPublicKey string, port int) (*Node, error) {
	if nodeID == "" {
		return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "node_id cannot be empty", false, nil).
			WithMetadata("field", "node_id")
	}
	if ipAddress == "" {
		return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "ip_address cannot be empty", false, nil).
			WithMetadata("field", "ip_address")
	}
	if serverPublicKey == "" {
		return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "server_public_key cannot be empty", false, nil).
			WithMetadata("field", "server_public_key")
	}
	if port <= 0 || port > 65535 {
		return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "port must be between 1 and 65535", false, nil).
			WithMetadata("field", "port").WithMetadata("value", port)
	}

	now := time.Now()
	return &Node{
		ID:               nodeID,
		IPAddress:        ipAddress,
		ServerPublicKey:  serverPublicKey,
		Port:             port,
		Status:           StatusProvisioning,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
		ConnectedClients: 0,
	}, nil
}

// UpdateStatus changes the node status with validation
func (n *Node) UpdateStatus(newStatus Status) error {
	if !newStatus.IsValid() {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "invalid node status", false, nil).
			WithMetadata("status", newStatus)
	}

	if !n.Status.CanTransitionTo(newStatus) {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "invalid status transition", false, nil).
			WithMetadata("from", n.Status).WithMetadata("to", newStatus)
	}

	n.Status = newStatus
	n.UpdatedAt = time.Now()
	n.Version++
	return nil
}

// IsActive returns true if the node is active and ready to accept peers
func (n *Node) IsActive() bool {
	return n.Status == StatusActive
}

// IsHealthy returns true if the node is in a healthy state
func (n *Node) IsHealthy() bool {
	return n.Status == StatusActive || n.Status == StatusProvisioning
}

// CanAcceptPeers returns true if the node can accept new peers
func (n *Node) CanAcceptPeers() bool {
	return n.Status == StatusActive
}

// UpdateLastHandshake updates the last handshake timestamp
func (n *Node) UpdateLastHandshake(timestamp time.Time) {
	n.LastHandshakeAt = &timestamp
	n.UpdatedAt = time.Now()
	n.Version++
}

// UpdateConnectedClients updates the connected clients count
func (n *Node) UpdateConnectedClients(count int) error {
	if count < 0 {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "connected_clients cannot be negative", false, nil).
			WithMetadata("value", count)
	}

	n.ConnectedClients = count
	n.UpdatedAt = time.Now()
	n.Version++
	return nil
}

// ScheduleDestroy schedules the node for destruction at the specified time
func (n *Node) ScheduleDestroy(destroyAt time.Time) error {
	if destroyAt.Before(time.Now()) {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "destroy_at cannot be in the past", false, nil).
			WithMetadata("value", destroyAt)
	}

	n.DestroyAt = &destroyAt
	n.UpdatedAt = time.Now()
	n.Version++
	return nil
}

// ValidateCapacity validates if the node can accept additional peers
func (n *Node) ValidateCapacity(additionalPeers int, maxPeersPerNode int) error {
	if additionalPeers < 0 {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "additional_peers cannot be negative", false, nil).
			WithMetadata("value", additionalPeers)
	}

	if !n.CanAcceptPeers() {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeNotReady, "node not ready to accept peers", true, nil).
			WithMetadata("node_id", n.ID).WithMetadata("status", n.Status)
	}

	totalPeers := n.ConnectedClients + additionalPeers
	if totalPeers > maxPeersPerNode {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeAtCapacity, "node at capacity", true, nil).
			WithMetadata("node_id", n.ID).
			WithMetadata("current", n.ConnectedClients).
			WithMetadata("max", maxPeersPerNode).
			WithMetadata("requested", additionalPeers)
	}

	return nil
}

// ValidateForProvisioning validates node data before provisioning
func (n *Node) ValidateForProvisioning() error {
	if n.ID == "" {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "id cannot be empty", false, nil)
	}
	if n.Status != StatusProvisioning {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "status must be provisioning", false, nil).
			WithMetadata("status", n.Status)
	}
	return nil
}

// ValidateForDestruction validates node can be safely destroyed
func (n *Node) ValidateForDestruction() error {
	if n.ConnectedClients > 0 {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "cannot destroy node with active peers", false, nil).
			WithMetadata("node_id", n.ID).
			WithMetadata("connected_clients", n.ConnectedClients)
	}
	if n.Status == StatusDestroying {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeConflict, "node already being destroyed", false, nil).
			WithMetadata("node_id", n.ID)
	}
	return nil
}

// CalculateCapacityUsage calculates the capacity usage percentage
func (n *Node) CalculateCapacityUsage(maxPeersPerNode int) float64 {
	if maxPeersPerNode <= 0 {
		return 0.0
	}
	return float64(n.ConnectedClients) / float64(maxPeersPerNode) * 100.0
}

// IsOverCapacityThreshold checks if node is over the specified capacity threshold
func (n *Node) IsOverCapacityThreshold(maxPeersPerNode int, thresholdPercent float64) bool {
	usage := n.CalculateCapacityUsage(maxPeersPerNode)
	return usage > thresholdPercent
}

// CreateRequest represents a request to create a new node
type CreateRequest struct {
	ServerID        string `json:"server_id"`
	IPAddress       string `json:"ip_address"`
	ServerPublicKey string `json:"server_public_key"`
	Port            int    `json:"port"`
}

// Validate validates the create request
func (r *CreateRequest) Validate() error {
	if r.ServerID == "" {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "server_id cannot be empty", false, nil)
	}
	if r.IPAddress == "" {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "ip_address cannot be empty", false, nil)
	}
	if r.ServerPublicKey == "" {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "server_public_key cannot be empty", false, nil)
	}
	if r.Port <= 0 || r.Port > 65535 {
		return apperrors.NewNodeError(apperrors.ErrCodeNodeValidation, "port must be between 1 and 65535", false, nil)
	}
	return nil
}

// Filters represents filters for listing nodes
type Filters struct {
	Status    *Status `json:"status,omitempty"`
	ServerID  *string `json:"server_id,omitempty"`
	IPAddress *string `json:"ip_address,omitempty"`
	Limit     *int    `json:"limit,omitempty"`
	Offset    *int    `json:"offset,omitempty"`
}

// Statistics represents node statistics across the system
type Statistics struct {
	TotalNodes          int64   `json:"total_nodes"`
	ActiveNodes         int64   `json:"active_nodes"`
	ProvisioningNodes   int64   `json:"provisioning_nodes"`
	UnhealthyNodes      int64   `json:"unhealthy_nodes"`
	DestroyingNodes     int64   `json:"destroying_nodes"`
	TotalPeers          int64   `json:"total_peers"`
	AveragePeersPerNode float64 `json:"average_peers_per_node"`
}

// Health represents node health information
type Health struct {
	NodeID          string        `json:"node_id"`
	IsHealthy       bool          `json:"is_healthy"`
	ResponseTime    time.Duration `json:"response_time"`
	SystemLoad      float64       `json:"system_load"`
	MemoryUsage     float64       `json:"memory_usage"`
	DiskUsage       float64       `json:"disk_usage"`
	WireGuardStatus string        `json:"wireguard_status"`
	ConnectedPeers  int           `json:"connected_peers"`
	LastChecked     time.Time     `json:"last_checked"`
	Errors          []string      `json:"errors,omitempty"`
}

// Config represents node configuration for clients
type Config struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	ServerPort      int    `json:"server_port"`
}
