package remote

import (
	"time"
)

// NodeInteractorConfig contains configuration for NodeInteractor operations
type NodeInteractorConfig struct {
	SSHTimeout          time.Duration        `json:"ssh_timeout"`
	CommandTimeout      time.Duration        `json:"command_timeout"`
	WireGuardConfigPath string               `json:"wireguard_config_path"`
	WireGuardInterface  string               `json:"wireguard_interface"`
	HealthCheckCommands []HealthCommand      `json:"health_check_commands"`
	RetryAttempts       int                  `json:"retry_attempts"`
	RetryBackoff        time.Duration        `json:"retry_backoff"`
	SystemMetricsPath   SystemMetricsPaths   `json:"system_metrics_paths"`
	CircuitBreaker      CircuitBreakerConfig `json:"circuit_breaker"`
}

// HealthCommand defines a health check command configuration
type HealthCommand struct {
	Name         string        `json:"name"`
	Command      string        `json:"command"`
	Timeout      time.Duration `json:"timeout"`
	Critical     bool          `json:"critical"`
	ExpectedExit int           `json:"expected_exit"`
}

// SystemMetricsPaths defines paths for system metrics collection
type SystemMetricsPaths struct {
	LoadAvg   string `json:"load_avg"`   // /proc/loadavg
	MemInfo   string `json:"mem_info"`   // /proc/meminfo
	DiskUsage string `json:"disk_usage"` // df command
	Uptime    string `json:"uptime"`     // /proc/uptime
	Hostname  string `json:"hostname"`   // /etc/hostname
	OSRelease string `json:"os_release"` // /etc/os-release
}

// CircuitBreakerConfig defines circuit breaker configuration
type CircuitBreakerConfig struct {
	FailureThreshold int           `json:"failure_threshold"`
	ResetTimeout     time.Duration `json:"reset_timeout"`
	MaxAttempts      int           `json:"max_attempts"`
}

// NodeHealthStatus represents comprehensive node health information
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

// NodeSystemInfo represents detailed system information
type NodeSystemInfo struct {
	Hostname          string             `json:"hostname"`
	OS                string             `json:"os"`
	Kernel            string             `json:"kernel"`
	Uptime            time.Duration      `json:"uptime"`
	CPUCores          int                `json:"cpu_cores"`
	TotalMemory       int64              `json:"total_memory"`
	TotalDisk         int64              `json:"total_disk"`
	NetworkInterfaces []NetworkInterface `json:"network_interfaces"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Status    string `json:"status"`
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

// WireGuardConfig represents complete WireGuard interface configuration
type WireGuardConfig struct {
	InterfaceName string                `json:"interface_name"`
	PrivateKey    string                `json:"private_key"`
	ListenPort    int                   `json:"listen_port"`
	Address       []string              `json:"address"`
	DNS           []string              `json:"dns,omitempty"`
	Peers         []PeerWireGuardConfig `json:"peers"`
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Duration time.Duration `json:"duration"`
}

// NodeInteractorMetrics contains comprehensive metrics
type NodeInteractorMetrics struct {
	Operations      OperationMetrics                   `json:"operations"`
	Connections     map[string]ConnectionHealthMetrics `json:"connections"`
	CircuitBreakers map[string]CircuitBreakerStats     `json:"circuit_breakers"`
}
