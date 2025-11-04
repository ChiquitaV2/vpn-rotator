package config

import (
	"fmt"
	"time"
)

// Config defines the configuration for the rotator service.
type Config struct {
	Service           ServiceConfig           `mapstructure:"service"`
	Log               LogConfig               `mapstructure:"log"`
	API               APIConfig               `mapstructure:"api"`
	DB                DBConfig                `mapstructure:"db"`
	Scheduler         SchedulerConfig         `mapstructure:"scheduler"`
	Hetzner           HetznerConfig           `mapstructure:"hetzner"`
	NodeService       NodeServiceConfig       `mapstructure:"node_service"`
	NodeInteractor    NodeInteractorConfig    `mapstructure:"node_interactor"`
	CircuitBreaker    CircuitBreakerConfig    `mapstructure:"circuit_breaker"`
	AsyncProvisioning AsyncProvisioningConfig `mapstructure:"async_provisioning"`
}

// LogConfig defines the logging configuration.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// APIConfig defines the API server configuration.
type APIConfig struct {
	ListenAddr string `mapstructure:"listen_addr"`
}

// DBConfig defines the database configuration.
type DBConfig struct {
	Path            string `mapstructure:"path"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // seconds
}

// SchedulerConfig defines the scheduler configuration.
type SchedulerConfig struct {
	RotationInterval time.Duration `mapstructure:"rotation_interval"`
	CleanupInterval  time.Duration `mapstructure:"cleanup_interval"`
	CleanupAge       time.Duration `mapstructure:"cleanup_age"`
}

// ServiceConfig defines service-level configuration options.
type ServiceConfig struct {
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// HetznerConfig defines the Hetzner provider configuration.
type HetznerConfig struct {
	APIToken          string   `mapstructure:"api_token"`
	ServerTypes       []string `mapstructure:"server_types"`
	Image             string   `mapstructure:"image"`
	Locations         []string `mapstructure:"locations"`
	SSHKey            string   `mapstructure:"ssh_key"`
	SSHPrivateKeyPath string   `mapstructure:"ssh_private_key_path"`
}

// NodeServiceConfig defines configuration for the Node domain service
type NodeServiceConfig struct {
	MaxPeersPerNode      int           `mapstructure:"max_peers_per_node"`
	CapacityThreshold    float64       `mapstructure:"capacity_threshold"`
	HealthCheckTimeout   time.Duration `mapstructure:"health_check_timeout"`
	ProvisioningTimeout  time.Duration `mapstructure:"provisioning_timeout"`
	DestructionTimeout   time.Duration `mapstructure:"destruction_timeout"`
	OptimalNodeSelection string        `mapstructure:"optimal_node_selection"`
}

// NodeInteractorConfig defines configuration for NodeInteractor operations
type NodeInteractorConfig struct {
	SSHTimeout          time.Duration      `mapstructure:"ssh_timeout"`
	CommandTimeout      time.Duration      `mapstructure:"command_timeout"`
	WireGuardConfigPath string             `mapstructure:"wireguard_config_path"`
	WireGuardInterface  string             `mapstructure:"wireguard_interface"`
	RetryAttempts       int                `mapstructure:"retry_attempts"`
	RetryBackoff        time.Duration      `mapstructure:"retry_backoff"`
	HealthCheckCommands []HealthCommand    `mapstructure:"health_check_commands"`
	SystemMetricsPath   SystemMetricsPaths `mapstructure:"system_metrics_paths"`
}

// HealthCommand defines a health check command configuration
type HealthCommand struct {
	Name         string        `mapstructure:"name"`
	Command      string        `mapstructure:"command"`
	Timeout      time.Duration `mapstructure:"timeout"`
	Critical     bool          `mapstructure:"critical"`
	ExpectedExit int           `mapstructure:"expected_exit"`
}

// SystemMetricsPaths defines paths for system metrics collection
type SystemMetricsPaths struct {
	LoadAvg   string `mapstructure:"load_avg"`
	MemInfo   string `mapstructure:"mem_info"`
	DiskUsage string `mapstructure:"disk_usage"`
	Uptime    string `mapstructure:"uptime"`
	Hostname  string `mapstructure:"hostname"`
	OSRelease string `mapstructure:"os_release"`
}

// CircuitBreakerConfig defines circuit breaker configuration
type CircuitBreakerConfig struct {
	FailureThreshold int           `mapstructure:"failure_threshold"`
	ResetTimeout     time.Duration `mapstructure:"reset_timeout"`
	MaxAttempts      int           `mapstructure:"max_attempts"`
}

// AsyncProvisioningConfig defines configuration for async provisioning service
type AsyncProvisioningConfig struct {
	EventBusMode           string        `mapstructure:"event_bus_mode"`
	WorkerTimeout          time.Duration `mapstructure:"worker_timeout"`
	ETAHistoryRetention    int           `mapstructure:"eta_history_retention"`
	DefaultProvisioningETA time.Duration `mapstructure:"default_provisioning_eta"`
	ProgressUpdateInterval time.Duration `mapstructure:"progress_update_interval"`
	MaxConcurrentJobs      int           `mapstructure:"max_concurrent_jobs"`
}

// Validate validates the configuration for correctness and completeness
func (c *Config) Validate() error {
	// Validate required Hetzner settings first
	if c.Hetzner.APIToken == "" {
		return fmt.Errorf("hetzner.api_token is required (set VPN_ROTATOR_HETZNER_API_TOKEN env var)")
	}
	if c.Hetzner.SSHPrivateKeyPath == "" {
		return fmt.Errorf("hetzner.ssh_private_key_path is required")
	}

	// Validate log level
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Log.Level != "" && !validLevels[c.Log.Level] {
		return fmt.Errorf("invalid log.level: %s (must be debug, info, warn, or error)", c.Log.Level)
	}

	// Validate log format
	if c.Log.Format != "" && c.Log.Format != "json" && c.Log.Format != "text" {
		return fmt.Errorf("invalid log.format: %s (must be json or text)", c.Log.Format)
	}

	// Validate service configuration with minimum requirements
	if c.Service.ShutdownTimeout > 0 && c.Service.ShutdownTimeout < time.Second {
		return fmt.Errorf("service.shutdown_timeout must be at least 1 second")
	}

	// Validate intervals with minimum requirements
	if c.Scheduler.RotationInterval > 0 && c.Scheduler.RotationInterval < time.Hour {
		return fmt.Errorf("scheduler.rotation_interval must be at least 1 hour")
	}
	if c.Scheduler.CleanupInterval > 0 && c.Scheduler.CleanupInterval < time.Minute {
		return fmt.Errorf("scheduler.cleanup_interval must be at least 1 minute")
	}
	if c.Scheduler.CleanupAge > 0 && c.Scheduler.CleanupAge < time.Hour {
		return fmt.Errorf("scheduler.cleanup_age must be at least 1 hour")
	}

	// Set defaults for missing values
	c.setDefaults()

	return nil
}

// setDefaults sets default values for configuration fields that are not set
func (c *Config) setDefaults() {
	// Service defaults
	if c.Service.ShutdownTimeout <= 0 {
		c.Service.ShutdownTimeout = 30 * time.Second
	}

	// Database defaults
	if c.DB.Path == "" {
		c.DB.Path = "./data/rotator.db"
	}
	if c.DB.MaxOpenConns <= 0 {
		c.DB.MaxOpenConns = 25
	}
	if c.DB.MaxIdleConns <= 0 {
		c.DB.MaxIdleConns = 5
	}
	if c.DB.ConnMaxLifetime <= 0 {
		c.DB.ConnMaxLifetime = 300
	}

	// API defaults
	if c.API.ListenAddr == "" {
		c.API.ListenAddr = ":8080"
	}

	// Log defaults
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}

	// Scheduler defaults
	if c.Scheduler.RotationInterval <= 0 {
		c.Scheduler.RotationInterval = 24 * time.Hour
	}
	if c.Scheduler.CleanupInterval <= 0 {
		c.Scheduler.CleanupInterval = 1 * time.Hour
	}
	if c.Scheduler.CleanupAge <= 0 {
		c.Scheduler.CleanupAge = 2 * time.Hour
	}

	// Hetzner defaults
	if len(c.Hetzner.ServerTypes) == 0 {
		c.Hetzner.ServerTypes = []string{"cx11"}
	}
	if c.Hetzner.Image == "" {
		c.Hetzner.Image = "ubuntu-22.04"
	}
	if len(c.Hetzner.Locations) == 0 {
		c.Hetzner.Locations = []string{"nbg1"}
	}

	// Node service defaults
	if c.NodeService.MaxPeersPerNode <= 0 {
		c.NodeService.MaxPeersPerNode = 50
	}
	if c.NodeService.CapacityThreshold <= 0 || c.NodeService.CapacityThreshold > 100 {
		c.NodeService.CapacityThreshold = 80.0
	}
	if c.NodeService.HealthCheckTimeout <= 0 {
		c.NodeService.HealthCheckTimeout = 30 * time.Second
	}
	if c.NodeService.ProvisioningTimeout <= 0 {
		c.NodeService.ProvisioningTimeout = 10 * time.Minute
	}
	if c.NodeService.DestructionTimeout <= 0 {
		c.NodeService.DestructionTimeout = 5 * time.Minute
	}
	if c.NodeService.OptimalNodeSelection == "" {
		c.NodeService.OptimalNodeSelection = "least_loaded"
	}

	// Node interactor defaults
	if c.NodeInteractor.SSHTimeout <= 0 {
		c.NodeInteractor.SSHTimeout = 30 * time.Second
	}
	if c.NodeInteractor.CommandTimeout <= 0 {
		c.NodeInteractor.CommandTimeout = 60 * time.Second
	}
	if c.NodeInteractor.WireGuardConfigPath == "" {
		c.NodeInteractor.WireGuardConfigPath = "/etc/wireguard/wg0.conf"
	}
	if c.NodeInteractor.WireGuardInterface == "" {
		c.NodeInteractor.WireGuardInterface = "wg0"
	}
	if c.NodeInteractor.RetryAttempts <= 0 {
		c.NodeInteractor.RetryAttempts = 3
	}
	if c.NodeInteractor.RetryBackoff <= 0 {
		c.NodeInteractor.RetryBackoff = 2 * time.Second
	}

	// Circuit breaker defaults
	if c.CircuitBreaker.FailureThreshold <= 0 {
		c.CircuitBreaker.FailureThreshold = 5
	}
	if c.CircuitBreaker.ResetTimeout <= 0 {
		c.CircuitBreaker.ResetTimeout = 60 * time.Second
	}
	if c.CircuitBreaker.MaxAttempts <= 0 {
		c.CircuitBreaker.MaxAttempts = 3
	}

	// Async provisioning defaults
	if c.AsyncProvisioning.EventBusMode == "" {
		c.AsyncProvisioning.EventBusMode = "simple"
	}
	if c.AsyncProvisioning.WorkerTimeout <= 0 {
		c.AsyncProvisioning.WorkerTimeout = 15 * time.Minute
	}
	if c.AsyncProvisioning.ETAHistoryRetention <= 0 {
		c.AsyncProvisioning.ETAHistoryRetention = 10
	}
	if c.AsyncProvisioning.DefaultProvisioningETA <= 0 {
		c.AsyncProvisioning.DefaultProvisioningETA = 3 * time.Minute
	}
	if c.AsyncProvisioning.ProgressUpdateInterval <= 0 {
		c.AsyncProvisioning.ProgressUpdateInterval = 5 * time.Second
	}
	if c.AsyncProvisioning.MaxConcurrentJobs <= 0 {
		c.AsyncProvisioning.MaxConcurrentJobs = 1
	}
}
