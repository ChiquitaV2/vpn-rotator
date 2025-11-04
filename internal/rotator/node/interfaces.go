package node

import (
	"context"
	"net"
	"time"
)

// NodeService defines the core node domain operations
type NodeService interface {
	DestroyNode(ctx context.Context, nodeID string) error

	// Node health and status operations
	CheckNodeHealth(ctx context.Context, nodeID string) (*Health, error)
	UpdateNodeStatus(ctx context.Context, nodeID string, status Status) error

	// Node capacity and selection operations
	ValidateNodeCapacity(ctx context.Context, nodeID string, additionalPeers int) error
	SelectOptimalNode(ctx context.Context, filters Filters) (*Node, error)

	// Peer management operations on nodes
	AddPeerToNode(ctx context.Context, nodeID string, peerConfig PeerConfig) error
	RemovePeerFromNode(ctx context.Context, nodeID string, publicKey string) error
	ListNodePeers(ctx context.Context, nodeID string) ([]*PeerInfo, error)
	UpdateNodePeerConfig(ctx context.Context, nodeID string, peerConfig PeerConfig) error

	// Node information operations
	GetNodePublicKey(ctx context.Context, nodeID string) (string, error)
	GetNodeStatistics(ctx context.Context) (*Statistics, error)
	ListNodes(ctx context.Context, filters Filters) ([]*Node, error)
	GetNode(ctx context.Context, nodeID string) (*Node, error)
}

// NodeRepository defines the data persistence interface for nodes
type NodeRepository interface {
	// Basic CRUD operations
	Create(ctx context.Context, node *Node) error
	GetByID(ctx context.Context, nodeID string) (*Node, error)
	Update(ctx context.Context, node *Node) error
	Delete(ctx context.Context, nodeID string) error

	// Query operations
	List(ctx context.Context, filters Filters) ([]*Node, error)
	GetByServerID(ctx context.Context, serverID string) (*Node, error)
	GetByIPAddress(ctx context.Context, ipAddress string) (*Node, error)

	// Status and health operations
	UpdateStatus(ctx context.Context, nodeID string, status Status, version int64) error
	UpdateHealth(ctx context.Context, nodeID string, health *Health) error
	GetUnhealthyNodes(ctx context.Context) ([]*Node, error)
	GetActiveNodes(ctx context.Context) ([]*Node, error)

	// Capacity and statistics operations
	GetNodeCapacity(ctx context.Context, nodeID string) (*NodeCapacity, error)
	GetStatistics(ctx context.Context) (*Statistics, error)

	// Optimistic locking support
	IncrementVersion(ctx context.Context, nodeID string) (int64, error)

	// Transaction support
	WithTx(ctx context.Context, fn func(NodeRepository) error) error
}

// CloudProvisioner defines the interface for cloud infrastructure provisioning
// This is an alias to the infrastructure CloudProvisioner interface
type CloudProvisioner interface {
	ProvisionNode(ctx context.Context, config ProvisioningConfig) (*ProvisionedNode, error)
	DestroyNode(ctx context.Context, serverID string) error
	GetNodeStatus(ctx context.Context, serverID string) (*CloudNodeStatus, error)
}

// NodeInteractor defines the interface for remote node operations (already exists in infrastructure)
// This is a reference to the existing interface in infrastructure/nodeinteractor
type NodeInteractor interface {
	CheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error)
	AddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error
	RemovePeer(ctx context.Context, nodeHost string, publicKey string) error
	ListPeers(ctx context.Context, nodeHost string) ([]*WireGuardPeerStatus, error)
	UpdatePeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error
	GetWireGuardStatus(ctx context.Context, nodeHost string) (*WireGuardStatus, error)
}

// PeerConfig represents peer configuration for node operations
type PeerConfig struct {
	ID           string   `json:"id"`
	PublicKey    string   `json:"public_key"`
	PresharedKey *string  `json:"preshared_key,omitempty"`
	AllocatedIP  string   `json:"allocated_ip"`
	AllowedIPs   []string `json:"allowed_ips"`
}

// PeerInfo represents peer information from a node
type PeerInfo struct {
	PublicKey     string     `json:"public_key"`
	AllocatedIP   string     `json:"allocated_ip"`
	AllowedIPs    []string   `json:"allowed_ips"`
	LastHandshake *time.Time `json:"last_handshake,omitempty"`
	TransferRx    int64      `json:"transfer_rx"`
	TransferTx    int64      `json:"transfer_tx"`
}

// NodeCapacity represents node capacity information
type NodeCapacity struct {
	NodeID         string  `json:"node_id"`
	MaxPeers       int     `json:"max_peers"`
	CurrentPeers   int     `json:"current_peers"`
	AvailablePeers int     `json:"available_peers"`
	CapacityUsed   float64 `json:"capacity_used"` // Percentage
}

// ProvisioningConfig represents configuration for cloud provisioning
type ProvisioningConfig struct {
	Region       string            `json:"region"`
	InstanceType string            `json:"instance_type"`
	ImageID      string            `json:"image_id"`
	SSHKeyName   string            `json:"ssh_key_name"`
	Tags         map[string]string `json:"tags"`
	UserData     string            `json:"user_data"`
	Subnet       *net.IPNet        `json:"subnet"`
	SSHPublicKey string            `json:"ssh_public_key"`
}

// ProvisionedNode represents a provisioned node from cloud provider
type ProvisionedNode struct {
	ServerID  string `json:"server_id"`
	IPAddress string `json:"ip_address"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
	Region    string `json:"region"`
}

// CloudNodeStatus represents cloud provider node status
type CloudNodeStatus struct {
	ServerID  string `json:"server_id"`
	Status    string `json:"status"`
	IPAddress string `json:"ip_address"`
}

// NodeHealthStatus represents health status from NodeInteractor
type NodeHealthStatus struct {
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

// WireGuardStatus represents WireGuard interface status
type WireGuardStatus struct {
	InterfaceName string    `json:"interface_name"`
	PublicKey     string    `json:"public_key"`
	ListenPort    int       `json:"listen_port"`
	IsRunning     bool      `json:"is_running"`
	PeerCount     int       `json:"peer_count"`
	LastUpdated   time.Time `json:"last_updated"`
}

// WireGuardPeerStatus represents a WireGuard peer's status
type WireGuardPeerStatus struct {
	PublicKey           string     `json:"public_key"`
	Endpoint            *string    `json:"endpoint,omitempty"`
	AllowedIPs          []string   `json:"allowed_ips"`
	LastHandshake       *time.Time `json:"last_handshake,omitempty"`
	TransferRx          int64      `json:"transfer_rx"`
	TransferTx          int64      `json:"transfer_tx"`
	PersistentKeepalive int        `json:"persistent_keepalive"`
}

// PeerWireGuardConfig represents a peer's WireGuard configuration
type PeerWireGuardConfig struct {
	PublicKey    string   `json:"public_key"`
	PresharedKey *string  `json:"preshared_key,omitempty"`
	AllowedIPs   []string `json:"allowed_ips"`
	Endpoint     *string  `json:"endpoint,omitempty"`
}
