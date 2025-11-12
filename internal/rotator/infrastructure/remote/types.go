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
