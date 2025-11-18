// Package events defines all event types, constants, and data structures used throughout
// the VPN rotator system. This is the single source of truth for event definitions.
package events

import "time"

// =============================================================================
// EVENT TYPE CONSTANTS
// =============================================================================
// All event types are defined here to ensure consistency across the application

// Provisioning Lifecycle Events
const (
	EventProvisionRequested = "provision.requested"
	EventProvisionProgress  = "provision.progress"
	EventProvisionCompleted = "provision.completed"
	EventProvisionFailed    = "provision.failed"
)

// Node Lifecycle Events
const (
	EventNodeStatusChanged = "node.status.changed"
	EventNodeCreated       = "node.created"
	EventNodeDestroyed     = "node.destroyed"
)

// Peer Connection Events
const (
	EventPeerConnectRequested    = "peer.connect.requested"
	EventPeerConnectProgress     = "peer.connect.progress"
	EventPeerConnected           = "peer.connected"
	EventPeerConnectFailed       = "peer.connect.failed"
	EventPeerDisconnectRequested = "peer.disconnect.requested"
	EventPeerDisconnected        = "peer.disconnected"
)

// WireGuard Operation Events
const (
	EventWireGuardPeerAdded   = "node.wireguard.peer_added"
	EventWireGuardPeerRemoved = "node.wireguard.peer_removed"
	EventWireGuardConfigSaved = "node.wireguard.config_saved"
)

// Generic Infrastructure Events
const (
	EventResourceCreated = "resource.created"
	EventResourceUpdated = "resource.updated"
	EventResourceDeleted = "resource.deleted"
	EventHealthCheck     = "health.check"
	EventError           = "error.occurred"
)

// =============================================================================
// PROVISIONING PHASE CONSTANTS
// =============================================================================
// These constants match actual provisioning steps in the system

const (
	PhaseInitialization   = "initialization"
	PhaseSubnetAllocation = "subnet_allocation"
	PhaseKeyGeneration    = "key_generation"
	PhaseCloudProvision   = "cloud_provision"
	PhaseWaitCloudInit    = "wait_cloud_init"
	PhaseSSHConnection    = "ssh_connection"
	PhaseHealthCheck      = "health_check"
	PhaseActivation       = "activation"
	PhaseCompleted        = "completed"
)

// =============================================================================
// EVENT DATA STRUCTURES
// =============================================================================
// All event payloads are defined below, organized by functional area

// -----------------------------------------------------------------------------
// Provisioning Events
// -----------------------------------------------------------------------------

// ProvisionRequestedEvent represents a request to start provisioning
type ProvisionRequestedEvent struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // "rotator", "scheduler", "admin"
}

// ProvisionProgressEvent represents progress updates during provisioning
type ProvisionProgressEvent struct {
	NodeID    string                 `json:"node_id,omitempty"`
	Phase     string                 `json:"phase"`
	Progress  float64                `json:"progress"` // 0.0 to 1.0
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // Additional context-specific data
	Timestamp time.Time              `json:"timestamp"`
}

// ProvisionCompletedEvent represents successful completion of provisioning
type ProvisionCompletedEvent struct {
	NodeID    string        `json:"node_id"`
	ServerID  string        `json:"server_id,omitempty"`
	IPAddress string        `json:"ip_address,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// ProvisionFailedEvent represents a provisioning failure
type ProvisionFailedEvent struct {
	NodeID    string    `json:"node_id,omitempty"`
	Phase     string    `json:"phase"`
	Error     string    `json:"error"`
	Retryable bool      `json:"retryable"`
	Timestamp time.Time `json:"timestamp"`
}

// -----------------------------------------------------------------------------
// Node Lifecycle Events
// -----------------------------------------------------------------------------

// NodeStatusChangedEvent represents a node status transition
type NodeStatusChangedEvent struct {
	NodeID         string    `json:"node_id"`
	PreviousStatus string    `json:"previous_status"`
	NewStatus      string    `json:"new_status"`
	Reason         string    `json:"reason,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// NodeCreatedEvent represents a new node being created in the database
type NodeCreatedEvent struct {
	NodeID    string    `json:"node_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// NodeDestroyedEvent represents a node being destroyed
type NodeDestroyedEvent struct {
	NodeID    string    `json:"node_id"`
	ServerID  string    `json:"server_id,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// -----------------------------------------------------------------------------
// Peer Connection Events
// -----------------------------------------------------------------------------

// PeerConnectRequestedEvent represents a request to connect a peer
type PeerConnectRequestedEvent struct {
	RequestID    string    `json:"request_id"`
	PublicKey    *string   `json:"public_key,omitempty"`
	GenerateKeys bool      `json:"generate_keys"`
	Timestamp    time.Time `json:"timestamp"`
}

// PeerConnectProgressEvent represents progress updates during peer connection
type PeerConnectProgressEvent struct {
	RequestID string                 `json:"request_id"`
	PeerID    string                 `json:"peer_id,omitempty"`
	Phase     string                 `json:"phase"`    // "node_selection", "ip_allocation", "peer_creation", "wireguard_config"
	Progress  float64                `json:"progress"` // 0.0 to 1.0
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// PeerConnectedEvent represents successful peer connection
type PeerConnectedEvent struct {
	RequestID        string        `json:"request_id"`
	PeerID           string        `json:"peer_id"`
	NodeID           string        `json:"node_id"`
	PublicKey        string        `json:"public_key"`
	AllocatedIP      string        `json:"allocated_ip"`
	ServerPublicKey  string        `json:"server_public_key"`
	ServerIP         string        `json:"server_ip"`
	ServerPort       int           `json:"server_port"`
	ClientPrivateKey *string       `json:"client_private_key,omitempty"`
	Duration         time.Duration `json:"duration"`
	Timestamp        time.Time     `json:"timestamp"`
}

// PeerConnectFailedEvent represents a peer connection failure
type PeerConnectFailedEvent struct {
	RequestID string    `json:"request_id"`
	Phase     string    `json:"phase"`
	Error     string    `json:"error"`
	Retryable bool      `json:"retryable"`
	Timestamp time.Time `json:"timestamp"`
}

// PeerDisconnectRequestedEvent represents a request to disconnect a peer
type PeerDisconnectRequestedEvent struct {
	RequestID string    `json:"request_id"`
	PeerID    string    `json:"peer_id"`
	Timestamp time.Time `json:"timestamp"`
}

// PeerDisconnectedEvent represents successful peer disconnection
type PeerDisconnectedEvent struct {
	RequestID string        `json:"request_id"`
	PeerID    string        `json:"peer_id"`
	NodeID    string        `json:"node_id"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// -----------------------------------------------------------------------------
// WireGuard Operation Events
// -----------------------------------------------------------------------------

// WireGuardPeerAddedEvent represents a peer being added to a node
type WireGuardPeerAddedEvent struct {
	NodeID     string    `json:"node_id"`
	NodeIP     string    `json:"node_ip"`
	PeerID     string    `json:"peer_id"`
	PublicKey  string    `json:"public_key"`
	AllowedIPs []string  `json:"allowed_ips"`
	Timestamp  time.Time `json:"timestamp"`
}

// WireGuardPeerRemovedEvent represents a peer being removed from a node
type WireGuardPeerRemovedEvent struct {
	NodeID    string    `json:"node_id"`
	NodeIP    string    `json:"node_ip"`
	PeerID    string    `json:"peer_id,omitempty"`
	PublicKey string    `json:"public_key"`
	Timestamp time.Time `json:"timestamp"`
}

// WireGuardConfigSavedEvent represents WireGuard configuration being saved
type WireGuardConfigSavedEvent struct {
	NodeID     string    `json:"node_id"`
	NodeIP     string    `json:"node_ip"`
	PublicKey  string    `json:"public_key"`
	ConfigPath string    `json:"config_path,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// -----------------------------------------------------------------------------
// Generic Infrastructure Events
// -----------------------------------------------------------------------------

// ResourceCreatedEvent represents a generic resource creation event
type ResourceCreatedEvent struct {
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// ResourceUpdatedEvent represents a generic resource update event
type ResourceUpdatedEvent struct {
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Changes      map[string]interface{} `json:"changes"`
	Timestamp    time.Time              `json:"timestamp"`
}

// ResourceDeletedEvent represents a generic resource deletion event
type ResourceDeletedEvent struct {
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Reason       string                 `json:"reason,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// HealthCheckEvent represents a system health check event
type HealthCheckEvent struct {
	Component string                 `json:"component"`
	Status    string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// ErrorEvent represents a generic error event
type ErrorEvent struct {
	Component string                 `json:"component"`
	Operation string                 `json:"operation"`
	Error     string                 `json:"error"`
	Retryable bool                   `json:"retryable"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}
