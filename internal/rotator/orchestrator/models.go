package orchestrator

import "time"

// NodeInfo represents detailed node information for internal operations
type NodeInfo struct {
	ID              string `json:"id"`
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	Status          string `json:"status"`
	PeerCount       int    `json:"peer_count"`
	AvailableIPs    int    `json:"available_ips"`
}

// RotationStatus represents the current status of any ongoing rotation (consolidated from rotation.go)
type RotationStatus struct {

	// Enhanced fields for detailed status
	HasActiveNode       bool   `json:"has_active_node"`
	ActiveNodeID        string `json:"active_node_id,omitempty"`
	ActiveNodeIP        string `json:"active_node_ip,omitempty"`
	ActiveNodePeerCount int    `json:"active_node_peer_count"`
	ProvisioningNodes   int    `json:"provisioning_nodes"`
	ScheduledNodes      int    `json:"scheduled_nodes"`
	RotationInProgress  bool   `json:"rotation_in_progress"`
}

// PeerStats represents peer distribution statistics across nodes (consolidated from shared models)
type PeerStats struct {
	TotalPeers   int            `json:"total_peers"`
	ActiveNodes  int            `json:"active_nodes"`
	Distribution map[string]int `json:"distribution"` // nodeID -> peer count
	LastUpdated  time.Time      `json:"last_updated"`
}

// ClientConfig represents a complete client configuration for VPN connection (consolidated from shared models)
type ClientConfig struct {
	ServerPublicKey  string   `json:"server_public_key"`
	ServerIP         string   `json:"server_ip"`
	ServerPort       int      `json:"server_port"`
	ClientPrivateKey string   `json:"client_private_key"`
	ClientPublicKey  string   `json:"client_public_key"`
	ClientIP         string   `json:"client_ip"`
	DNS              []string `json:"dns"`
	AllowedIPs       []string `json:"allowed_ips"`
}
