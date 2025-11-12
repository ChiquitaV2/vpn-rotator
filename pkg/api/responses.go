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

// PeerInfo represents peer information for listing operations
type PeerInfo struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"node_id"`
	PublicKey       string     `json:"public_key"`
	AllocatedIP     string     `json:"allocated_ip"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
}

// PeersListResponse represents the response for listing peers
type PeersListResponse struct {
	Peers      []PeerInfo `json:"peers"`
	TotalCount int        `json:"total_count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}

// PeerStatsResponse represents the peer statistics response
type PeerStatsResponse struct {
	TotalPeers   int            `json:"total_peers"`
	ActiveNodes  int            `json:"active_nodes"`
	Distribution map[string]int `json:"distribution"` // nodeID -> peer count
	LastUpdated  time.Time      `json:"last_updated"`
}

// ProvisioningResponse represents a response when provisioning is in progress
type ProvisioningResponse struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	EstimatedWait int    `json:"estimated_wait_seconds"`
	RetryAfter    int    `json:"retry_after_seconds"`
}

// SystemStatusResponse represents the overall system status
type SystemStatusResponse struct {
	TotalNodes       int                          `json:"total_nodes"`
	ActiveNodes      int                          `json:"active_nodes"`
	TotalPeers       int                          `json:"total_peers"`
	ActivePeers      int                          `json:"active_peers"`
	NodeDistribution map[string]int               `json:"node_distribution"` // nodeID -> peer count
	SystemHealth     string                       `json:"system_health"`     // healthy, degraded, unhealthy
	LastUpdated      time.Time                    `json:"last_updated"`
	NodeStatuses     map[string]NodeStatusDetails `json:"node_statuses"`          // nodeID -> status
	Provisioning     *ProvisioningInfo            `json:"provisioning,omitempty"` // current provisioning status
}

// NodeStatusDetails represents the status of a single node for API responses
type NodeStatusDetails struct {
	NodeID      string    `json:"node_id"`
	Status      string    `json:"status"`
	PeerCount   int       `json:"peer_count"`
	IsHealthy   bool      `json:"is_healthy"`
	SystemLoad  float64   `json:"system_load"`
	MemoryUsage float64   `json:"memory_usage"`
	DiskUsage   float64   `json:"disk_usage"`
	LastChecked time.Time `json:"last_checked"`
}

// NodeStatisticsResponse represents statistics about nodes for API responses
type NodeStatisticsResponse struct {
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

// RotationStatusResponse represents the status of node rotation operations for API responses
type RotationStatusResponse struct {
	InProgress        bool      `json:"in_progress"`
	LastRotation      time.Time `json:"last_rotation,omitempty"`
	NodesRotated      int       `json:"nodes_rotated"`
	PeersMigrated     int       `json:"peers_migrated"`
	RotationReason    string    `json:"rotation_reason,omitempty"`
	EstimatedComplete time.Time `json:"estimated_complete,omitempty"`
}

// CleanupResultResponse represents the result of a cleanup operation for API responses
type CleanupResultResponse struct {
	PeersRemoved    int       `json:"peers_removed"`
	NodesDestroyed  int       `json:"nodes_destroyed"`
	SubnetsReleased int       `json:"subnets_released"`
	Errors          []string  `json:"errors,omitempty"`
	Duration        int64     `json:"duration_ms"`
	Timestamp       time.Time `json:"timestamp"`
}

// HealthReportResponse represents a comprehensive system health report for API responses
type HealthReportResponse struct {
	OverallHealth   string                       `json:"overall_health"` // healthy, degraded, unhealthy
	NodeHealth      map[string]NodeHealthDetails `json:"node_health"`    // nodeID -> health
	SystemMetrics   SystemMetricsResponse        `json:"system_metrics"`
	Issues          []HealthIssueResponse        `json:"issues,omitempty"`
	Recommendations []string                     `json:"recommendations,omitempty"`
	LastChecked     time.Time                    `json:"last_checked"`
}

// NodeHealthDetails represents the health status of a single node for API responses
type NodeHealthDetails struct {
	NodeID       string    `json:"node_id"`
	IsHealthy    bool      `json:"is_healthy"`
	SystemLoad   float64   `json:"system_load"`
	MemoryUsage  float64   `json:"memory_usage"`
	DiskUsage    float64   `json:"disk_usage"`
	ResponseTime int64     `json:"response_time_ms"`
	Issues       []string  `json:"issues,omitempty"`
	LastChecked  time.Time `json:"last_checked"`
}

// SystemMetricsResponse represents overall system metrics for API responses
type SystemMetricsResponse struct {
	TotalCapacity   int     `json:"total_capacity"`
	UsedCapacity    int     `json:"used_capacity"`
	CapacityUsage   float64 `json:"capacity_usage"`
	AverageLoad     float64 `json:"average_load"`
	AverageMemory   float64 `json:"average_memory"`
	AverageDisk     float64 `json:"average_disk"`
	AverageResponse int64   `json:"average_response_ms"`
}

// HealthIssueResponse represents a specific health issue in the system for API responses
type HealthIssueResponse struct {
	Severity    string    `json:"severity"`  // critical, warning, info
	Component   string    `json:"component"` // node, peer, system
	ComponentID string    `json:"component_id,omitempty"`
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"timestamp"`
}

// OrphanedResourcesReportResponse represents a report of orphaned resources for API responses
type OrphanedResourcesReportResponse struct {
	InactivePeers int       `json:"inactive_peers"`
	OrphanedNodes int       `json:"orphaned_nodes"`
	UnusedSubnets int       `json:"unused_subnets"`
	Timestamp     time.Time `json:"timestamp"`
}

// CapacityReportResponse represents a comprehensive capacity report for API responses
type CapacityReportResponse struct {
	TotalCapacity  int                                 `json:"total_capacity"`
	TotalUsed      int                                 `json:"total_used"`
	TotalAvailable int                                 `json:"total_available"`
	OverallUsage   float64                             `json:"overall_usage"`
	Nodes          map[string]NodeCapacityInfoResponse `json:"nodes"`
	Timestamp      time.Time                           `json:"timestamp"`
}

// NodeCapacityInfoResponse represents capacity information for a single node for API responses
type NodeCapacityInfoResponse struct {
	NodeID         string  `json:"node_id"`
	MaxPeers       int     `json:"max_peers"`
	CurrentPeers   int     `json:"current_peers"`
	AvailablePeers int     `json:"available_peers"`
	CapacityUsed   float64 `json:"capacity_used"`
}

// PeerStatusResponse represents the status of a specific peer for API responses
type PeerStatusResponse struct {
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
