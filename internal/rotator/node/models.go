package node

import (
	"time"
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
		return nil, NewValidationError("node_id", "cannot be empty", nodeID)
	}
	if ipAddress == "" {
		return nil, NewValidationError("ip_address", "cannot be empty", ipAddress)
	}
	if serverPublicKey == "" {
		return nil, NewValidationError("server_public_key", "cannot be empty", serverPublicKey)
	}
	if port <= 0 || port > 65535 {
		return nil, NewValidationError("port", "must be between 1 and 65535", port)
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
		return ErrInvalidStatus
	}

	if !n.Status.CanTransitionTo(newStatus) {
		return ErrInvalidStatusTransition
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
		return NewValidationError("connected_clients", "cannot be negative", count)
	}

	n.ConnectedClients = count
	n.UpdatedAt = time.Now()
	n.Version++
	return nil
}

// ScheduleDestroy schedules the node for destruction at the specified time
func (n *Node) ScheduleDestroy(destroyAt time.Time) error {
	if destroyAt.Before(time.Now()) {
		return NewValidationError("destroy_at", "cannot be in the past", destroyAt)
	}

	n.DestroyAt = &destroyAt
	n.UpdatedAt = time.Now()
	n.Version++
	return nil
}

// ValidateCapacity validates if the node can accept additional peers
func (n *Node) ValidateCapacity(additionalPeers int, maxPeersPerNode int) error {
	if additionalPeers < 0 {
		return NewValidationError("additional_peers", "cannot be negative", additionalPeers)
	}

	if !n.CanAcceptPeers() {
		return ErrNodeNotReady
	}

	totalPeers := n.ConnectedClients + additionalPeers
	if totalPeers > maxPeersPerNode {
		return NewCapacityError(n.ID, n.ConnectedClients, maxPeersPerNode, additionalPeers, true, ErrNodeAtCapacity)
	}

	return nil
}

// ValidateForProvisioning validates node data before provisioning
func (n *Node) ValidateForProvisioning() error {
	if n.ID == "" {
		return NewValidationError("id", "cannot be empty", n.ID)
	}
	if n.Status != StatusProvisioning {
		return NewValidationError("status", "must be provisioning", n.Status)
	}
	return nil
}

// ValidateForDestruction validates node can be safely destroyed
func (n *Node) ValidateForDestruction() error {
	if n.ConnectedClients > 0 {
		return NewValidationError("connected_clients", "cannot destroy node with active peers", n.ConnectedClients)
	}
	if n.Status == StatusDestroying {
		return NewValidationError("status", "node already being destroyed", n.Status)
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
		return NewValidationError("server_id", "cannot be empty", r.ServerID)
	}
	if r.IPAddress == "" {
		return NewValidationError("ip_address", "cannot be empty", r.IPAddress)
	}
	if r.ServerPublicKey == "" {
		return NewValidationError("server_public_key", "cannot be empty", r.ServerPublicKey)
	}
	if r.Port <= 0 || r.Port > 65535 {
		return NewValidationError("port", "must be between 1 and 65535", r.Port)
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
