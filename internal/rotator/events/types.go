package events

import "time"

// Event type constants for provisioning lifecycle
const (
	EventProvisionRequested = "provision.requested"
	EventProvisionProgress  = "provision.progress"
	EventProvisionCompleted = "provision.completed"
	EventProvisionFailed    = "provision.failed"
)

// Event type constants for node lifecycle
const (
	EventNodeStatusChanged = "node.status.changed"
	EventNodeCreated       = "node.created"
	EventNodeDestroyed     = "node.destroyed"
)

// Event type constants for WireGuard operations
const (
	EventWireGuardPeerAdded   = "node.wireguard.peer_added"
	EventWireGuardPeerRemoved = "node.wireguard.peer_removed"
	EventWireGuardConfigSaved = "node.wireguard.config_saved"
)

// Provisioning phase constants - these match actual provisioning steps
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
