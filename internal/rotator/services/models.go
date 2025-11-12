package services

import (
	"time"
)

// ProvisioningRequiredError indicates that async provisioning is required
type ProvisioningRequiredError struct {
	Message       string
	EstimatedWait int
	RetryAfter    int
}

func (e *ProvisioningRequiredError) Error() string {
	return e.Message
}

// PeerStatus represents the current status of a peer (extended from API models)
type PeerStatus struct {
	PeerID       string    `json:"peer_id"`
	PublicKey    string    `json:"public_key"`
	AllocatedIP  string    `json:"allocated_ip"`
	Status       string    `json:"status"`
	NodeID       string    `json:"node_id"`
	ServerIP     string    `json:"server_ip,omitempty"`
	ServerStatus string    `json:"server_status,omitempty"`
	ConnectedAt  time.Time `json:"connected_at"`
	LastSeen     time.Time `json:"last_seen"`
}

// SystemStatus represents the overall system status
type SystemStatus struct {
	TotalNodes       int                   `json:"total_nodes"`
	ActiveNodes      int                   `json:"active_nodes"`
	TotalPeers       int                   `json:"total_peers"`
	ActivePeers      int                   `json:"active_peers"`
	NodeDistribution map[string]int        `json:"node_distribution"` // nodeID -> peer count
	SystemHealth     string                `json:"system_health"`     // healthy, degraded, unhealthy
	LastUpdated      time.Time             `json:"last_updated"`
	NodeStatuses     map[string]NodeStatus `json:"node_statuses"`          // nodeID -> status
	Provisioning     *ProvisioningInfo     `json:"provisioning,omitempty"` // current provisioning status
}

// ProvisioningInfo represents provisioning status information for system status
type ProvisioningInfo struct {
	IsActive     bool       `json:"is_active"`
	Phase        string     `json:"phase,omitempty"`
	Progress     float64    `json:"progress"`
	EstimatedETA *time.Time `json:"estimated_eta,omitempty"`
}

// NodeStatus represents the status of a single node
type NodeStatus struct {
	NodeID      string    `json:"node_id"`
	Status      string    `json:"status"`
	PeerCount   int       `json:"peer_count"`
	IsHealthy   bool      `json:"is_healthy"`
	SystemLoad  float64   `json:"system_load"`
	MemoryUsage float64   `json:"memory_usage"`
	DiskUsage   float64   `json:"disk_usage"`
	LastChecked time.Time `json:"last_checked"`
}

// NodeStatistics represents statistics about nodes
type NodeStatistics struct {
	TotalNodes        int            `json:"total_nodes"`
	ActiveNodes       int            `json:"active_nodes"`
	ProvisioningNodes int            `json:"provisioning_nodes"`
	UnhealthyNodes    int            `json:"unhealthy_nodes"`
	AverageLoad       float64        `json:"average_load"`
	AverageMemory     float64        `json:"average_memory"`
	AverageDisk       float64        `json:"average_disk"`
	NodeDistribution  map[string]int `json:"node_distribution"` // region -> count
	LastUpdated       time.Time      `json:"last_updated"`
}

// RotationStatus represents the status of node rotation operations
type RotationStatus struct {
	InProgress        bool      `json:"in_progress"`
	LastRotation      time.Time `json:"last_rotation,omitempty"`
	NodesRotated      int       `json:"nodes_rotated"`
	PeersMigrated     int       `json:"peers_migrated"`
	RotationReason    string    `json:"rotation_reason,omitempty"`
	EstimatedComplete time.Time `json:"estimated_complete,omitempty"`
}

// HealthReport represents a comprehensive system health report
type HealthReport struct {
	OverallHealth   string                `json:"overall_health"` // healthy, degraded, unhealthy
	NodeHealth      map[string]NodeHealth `json:"node_health"`    // nodeID -> health
	SystemMetrics   SystemMetrics         `json:"system_metrics"`
	Issues          []HealthIssue         `json:"issues,omitempty"`
	Recommendations []string              `json:"recommendations,omitempty"`
	LastChecked     time.Time             `json:"last_checked"`
}

// NodeHealth represents the health status of a single node
type NodeHealth struct {
	NodeID       string    `json:"node_id"`
	IsHealthy    bool      `json:"is_healthy"`
	SystemLoad   float64   `json:"system_load"`
	MemoryUsage  float64   `json:"memory_usage"`
	DiskUsage    float64   `json:"disk_usage"`
	ResponseTime int64     `json:"response_time_ms"`
	Issues       []string  `json:"issues,omitempty"`
	LastChecked  time.Time `json:"last_checked"`
}

// SystemMetrics represents overall system metrics
type SystemMetrics struct {
	TotalCapacity   int     `json:"total_capacity"`
	UsedCapacity    int     `json:"used_capacity"`
	CapacityUsage   float64 `json:"capacity_usage"`
	AverageLoad     float64 `json:"average_load"`
	AverageMemory   float64 `json:"average_memory"`
	AverageDisk     float64 `json:"average_disk"`
	AverageResponse int64   `json:"average_response_ms"`
}

// HealthIssue represents a specific health issue in the system
type HealthIssue struct {
	Severity    string    `json:"severity"`  // critical, warning, info
	Component   string    `json:"component"` // node, peer, system
	ComponentID string    `json:"component_id,omitempty"`
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"timestamp"`
}

// CleanupOptions represents options for manual cleanup operations
type CleanupOptions struct {
	InactivePeers   bool `json:"inactive_peers"`
	OrphanedNodes   bool `json:"orphaned_nodes"`
	UnusedSubnets   bool `json:"unused_subnets"`
	InactiveMinutes int  `json:"inactive_minutes"`
	DryRun          bool `json:"dry_run"`
}

// CleanupResult represents the result of a cleanup operation
type CleanupResult struct {
	PeersRemoved    int       `json:"peers_removed"`
	NodesDestroyed  int       `json:"nodes_destroyed"`
	SubnetsReleased int       `json:"subnets_released"`
	Errors          []string  `json:"errors,omitempty"`
	Duration        int64     `json:"duration_ms"`
	Timestamp       time.Time `json:"timestamp"`
}

// RotationDecision represents the decision about which nodes to rotate
type RotationDecision struct {
	NodesToRotate []NodeRotationInfo `json:"nodes_to_rotate"`
	Reason        string             `json:"reason"`
	Timestamp     time.Time          `json:"timestamp"`
}

// NodeRotationInfo contains information about a node that needs rotation
type NodeRotationInfo struct {
	NodeID      string  `json:"node_id"`
	Reason      string  `json:"reason"`
	Priority    int     `json:"priority"` // 1=critical, 2=high, 3=medium, 4=low
	SystemLoad  float64 `json:"system_load"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
	IsHealthy   bool    `json:"is_healthy"`
	PeerCount   int     `json:"peer_count"`
}

// OrphanedResourcesReport represents a report of orphaned resources
type OrphanedResourcesReport struct {
	InactivePeers int       `json:"inactive_peers"`
	OrphanedNodes int       `json:"orphaned_nodes"`
	UnusedSubnets int       `json:"unused_subnets"`
	Timestamp     time.Time `json:"timestamp"`
}

// CapacityReport represents a comprehensive capacity report (service layer)
type CapacityReport struct {
	TotalCapacity  int                         `json:"total_capacity"`
	TotalUsed      int                         `json:"total_used"`
	TotalAvailable int                         `json:"total_available"`
	OverallUsage   float64                     `json:"overall_usage"`
	Nodes          map[string]NodeCapacityInfo `json:"nodes"`
	Timestamp      time.Time                   `json:"timestamp"`
}

// NodeCapacityInfo represents capacity information for a single node (service layer)
type NodeCapacityInfo struct {
	NodeID         string  `json:"node_id"`
	MaxPeers       int     `json:"max_peers"`
	CurrentPeers   int     `json:"current_peers"`
	AvailablePeers int     `json:"available_peers"`
	CapacityUsed   float64 `json:"capacity_used"`
}

// ConnectRequest represents a request to connect a peer to the VPN (service layer)
type ConnectRequest struct {
	PublicKey    *string `json:"public_key,omitempty"`    // User-provided public key
	GenerateKeys bool    `json:"generate_keys,omitempty"` // Request server-side key generation
}

// ConnectResponse represents the response for a successful peer connection (service layer)
type ConnectResponse struct {
	PeerID           string   `json:"peer_id"`
	ServerPublicKey  string   `json:"server_public_key"`
	ServerIP         string   `json:"server_ip"`
	ServerPort       int      `json:"server_port"`
	ClientIP         string   `json:"client_ip"`
	ClientPrivateKey *string  `json:"client_private_key,omitempty"` // Only if server-generated
	DNS              []string `json:"dns"`
	AllowedIPs       []string `json:"allowed_ips"`
}

// PeerListParams represents query parameters for listing peers (service layer)
type PeerListParams struct {
	NodeID *string `json:"node_id,omitempty"`
	Status *string `json:"status,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Offset *int    `json:"offset,omitempty"`
}

// PeerInfo represents peer information for listing operations (service layer)
type PeerInfo struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"node_id"`
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
}

// PeersListResponse represents the response for listing peers (service layer)
type PeersListResponse struct {
	Peers      []PeerInfo `json:"peers"`
	TotalCount int        `json:"total_count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}

// PeerStatsResponse represents the peer statistics response (service layer)
type PeerStatsResponse struct {
	TotalPeers   int            `json:"total_peers"`
	ActiveNodes  int            `json:"active_nodes"`
	Distribution map[string]int `json:"distribution"` // nodeID -> peer count
	LastUpdated  time.Time      `json:"last_updated"`
}

// HealthResponse represents the health check response (service layer)
type HealthResponse struct {
	Status       string            `json:"status"`
	Version      string            `json:"version,omitempty"`
	Provisioning *ProvisioningInfo `json:"provisioning,omitempty"`
}
