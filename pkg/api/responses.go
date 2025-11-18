package api

import "time"

// ConnectResponse represents the response for a successful peer connection
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

// DisconnectResponse represents the response for a successful peer disconnection
type DisconnectResponse struct {
	Message string `json:"message"`
	PeerID  string `json:"peer_id"`
}

// ConfigResponse represents the VPN configuration response
type ConfigResponse struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	Port            int    `json:"port"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status       string            `json:"status"`
	Version      string            `json:"version,omitempty"`
	Provisioning *ProvisioningInfo `json:"provisioning,omitempty"`
}

// ProvisioningInfo represents provisioning status information
type ProvisioningInfo struct {
	IsActive     bool       `json:"is_active"`
	Phase        string     `json:"phase,omitempty"`
	Progress     float64    `json:"progress"`
	EstimatedETA *time.Time `json:"estimated_eta,omitempty"`
}

// PeersList represents the response for listing peers
type PeersList struct {
	Peers      []Peer `json:"peers"`
	TotalCount int    `json:"total_count"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
}

// PeerConnectionStatus represents the status of a peer connection request
type PeerConnectionStatus struct {
	RequestID         string           `json:"request_id"`
	PeerID            string           `json:"peer_id,omitempty"`
	Phase             string           `json:"phase"`
	Progress          float64          `json:"progress"`
	IsActive          bool             `json:"is_active"`
	Message           string           `json:"message,omitempty"`
	ErrorMessage      string           `json:"error_message,omitempty"`
	StartedAt         time.Time        `json:"started_at"`
	LastUpdated       time.Time        `json:"last_updated"`
	EstimatedETA      *time.Time       `json:"estimated_eta,omitempty"`
	ConnectionDetails *ConnectResponse `json:"connection_details,omitempty"`
}

// PeerStatsResponse represents the peer statistics response
type PeerStatsResponse struct {
	TotalPeers   int            `json:"total_peers"`
	ActiveNodes  int            `json:"active_nodes"`
	Distribution map[string]int `json:"distribution"` // nodeID -> peer count
	Timestamp    time.Time      `json:"timestamp"`
}

// SystemStatus represents the overall system status
type SystemStatus struct {
	TotalNodes       int                   `json:"total_nodes"`
	ActiveNodes      int                   `json:"active_nodes"`
	TotalPeers       int                   `json:"total_peers"`
	ActivePeers      int                   `json:"active_peers"`
	NodeDistribution map[string]int        `json:"node_distribution"` // nodeID -> peer count
	SystemHealth     string                `json:"system_health"`     // healthy, degraded, unhealthy
	NodeStatuses     map[string]NodeStatus `json:"node_statuses"`     // nodeID -> status
	Provisioning     *ProvisioningInfo     `json:"provisioning,omitempty"`
	Timestamp        time.Time             `json:"timestamp"`
}

// NodeStatus represents the status and health of a single node (merged NodeStatusDetails + NodeHealthDetails)
type NodeStatus struct {
	NodeID       string    `json:"node_id"`
	Status       string    `json:"status"`
	PeerCount    int       `json:"peer_count"`
	IsHealthy    bool      `json:"is_healthy"`
	SystemLoad   float64   `json:"system_load"`
	MemoryUsage  float64   `json:"memory_usage"`
	DiskUsage    float64   `json:"disk_usage"`
	ResponseTime int64     `json:"response_time_ms,omitempty"`
	Issues       []string  `json:"issues,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
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
	Timestamp         time.Time      `json:"timestamp"`
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

// CleanupResult represents the result of a cleanup operation
type CleanupResult struct {
	PeersRemoved    int       `json:"peers_removed"`
	NodesDestroyed  int       `json:"nodes_destroyed"`
	SubnetsReleased int       `json:"subnets_released"`
	Errors          []string  `json:"errors,omitempty"`
	Duration        int64     `json:"duration_ms"`
	Timestamp       time.Time `json:"timestamp"`
}

// HealthReport represents a comprehensive system health report
type HealthReport struct {
	OverallHealth   string                `json:"overall_health"` // healthy, degraded, unhealthy
	NodeHealth      map[string]NodeStatus `json:"node_health"`    // nodeID -> health
	SystemMetrics   SystemMetrics         `json:"system_metrics"`
	Issues          []HealthIssue         `json:"issues,omitempty"`
	Recommendations []string              `json:"recommendations,omitempty"`
	Timestamp       time.Time             `json:"timestamp"`
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

// OrphanedResourcesReport represents a report of orphaned resources
type OrphanedResourcesReport struct {
	InactivePeers int       `json:"inactive_peers"`
	OrphanedNodes int       `json:"orphaned_nodes"`
	UnusedSubnets int       `json:"unused_subnets"`
	Timestamp     time.Time `json:"timestamp"`
}

// CapacityReport represents a comprehensive capacity report
type CapacityReport struct {
	TotalCapacity  int                     `json:"total_capacity"`
	TotalUsed      int                     `json:"total_used"`
	TotalAvailable int                     `json:"total_available"`
	OverallUsage   float64                 `json:"overall_usage"`
	Nodes          map[string]NodeCapacity `json:"nodes"`
	Timestamp      time.Time               `json:"timestamp"`
}

// NodeCapacity represents capacity information for a single node
type NodeCapacity struct {
	NodeID         string  `json:"node_id"`
	MaxPeers       int     `json:"max_peers"`
	CurrentPeers   int     `json:"current_peers"`
	AvailablePeers int     `json:"available_peers"`
	CapacityUsed   float64 `json:"capacity_used"`
}

// Peer represents detailed peer information (merged PeerInfo + PeerStatusResponse)
type Peer struct {
	ID              string     `json:"id"`
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	Status          string     `json:"status"`
	NodeID          string     `json:"node_id"`
	ServerIP        string     `json:"server_ip,omitempty"`
	ServerStatus    string     `json:"server_status,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
}
