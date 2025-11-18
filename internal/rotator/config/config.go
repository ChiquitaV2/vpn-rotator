package config

import (
	"fmt"
	"time"
)

// Config defines the configuration for the rotator service.
// This focuses on essential user-configurable settings.
type Config struct {
	// Hetzner Cloud provider settings
	Hetzner HetznerConfig `mapstructure:"hetzner"`
	// VPN rotation behavior
	Rotation RotationConfig `mapstructure:"rotation"`
	// Peer connection limits
	Peers PeerConfig `mapstructure:"peers"`
	// Logging configuration
	Log LogConfig `mapstructure:"log"`
	// API server settings (optional)
	API APIConfig `mapstructure:"api"`
	// Database settings (optional)
	Database DatabaseConfig `mapstructure:"database"`
	// Service-level settings (optional)
	Service ServiceConfig `mapstructure:"service"`
}

// HetznerConfig defines the Hetzner Cloud provider configuration (REQUIRED)
type HetznerConfig struct {
	APIToken          string   `mapstructure:"api_token"`            // Required: Hetzner Cloud API token
	SSHPrivateKeyPath string   `mapstructure:"ssh_private_key_path"` // Required: Path to SSH private key
	ServerTypes       []string `mapstructure:"server_types"`         // Optional: Server types to use (default: ["cx11"])
	Locations         []string `mapstructure:"locations"`            // Optional: Datacenter locations (default: ["nbg1"])
	Image             string   `mapstructure:"image"`                // Optional: Server image (default: "ubuntu-22.04")
	SSHKey            string   `mapstructure:"ssh_key"`              // Optional: SSH key name in Hetzner
}

// RotationConfig defines VPN rotation behavior (ESSENTIAL)
type RotationConfig struct {
	Interval   time.Duration `mapstructure:"interval"`    // How often to rotate nodes (default: 24h)
	CleanupAge time.Duration `mapstructure:"cleanup_age"` // How long to keep old nodes (default: 2h)
}

// PeerConfig defines peer connection limits (IMPORTANT)
type PeerConfig struct {
	MaxPerNode        int     `mapstructure:"max_per_node"`       // Max peers per node (default: 50)
	CapacityThreshold float64 `mapstructure:"capacity_threshold"` // Capacity warning threshold 0-100 (default: 80.0)
}

// LogConfig defines logging configuration (BASIC)
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error (default: info)
	Format string `mapstructure:"format"` // json, text (default: json)

}

// APIConfig defines the API server configuration (ADVANCED)
type APIConfig struct {
	ListenAddr  string   `mapstructure:"listen_addr"`  // API server listen address (default: :8080)
	CORSOrigins []string `mapstructure:"cors_origins"` // List of allowed CORS origins (default: [http://localhost:3000])
}

// DatabaseConfig defines the database configuration (ADVANCED)
type DatabaseConfig struct {
	Path string `mapstructure:"path"` // Database file path (default: ./data/rotator.db)
}

// ServiceConfig defines service-level configuration (ADVANCED)
type ServiceConfig struct {
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"` // Graceful shutdown timeout (default: 30s)
}

// Validate validates the configuration for correctness and completeness
func (c *Config) Validate() error {
	// === REQUIRED SETTINGS ===
	if c.Hetzner.APIToken == "" {
		return fmt.Errorf("hetzner.api_token is required (set VPN_ROTATOR_HETZNER_API_TOKEN env var)")
	}
	if c.Hetzner.SSHPrivateKeyPath == "" {
		return fmt.Errorf("hetzner.ssh_private_key_path is required (set VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH env var)")
	}

	// === VALIDATION RULES ===

	// Log level validation
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Log.Level != "" && !validLevels[c.Log.Level] {
		return fmt.Errorf("invalid log.level: %s (must be debug, info, warn, or error)", c.Log.Level)
	}

	// Log format validation
	if c.Log.Format != "" && c.Log.Format != "json" && c.Log.Format != "text" {
		return fmt.Errorf("invalid log.format: %s (must be json or text)", c.Log.Format)
	}

	// Rotation interval validation (minimum 6 minutes)
	if c.Rotation.Interval > 0 && c.Rotation.Interval < 6*time.Minute {
		return fmt.Errorf("rotation.interval must be at least 6 minutes for safety")
	}

	// Cleanup age validation (minimum 10 minutes)
	if c.Rotation.CleanupAge > 0 && c.Rotation.CleanupAge < 10*time.Minute {
		return fmt.Errorf("rotation.cleanup_age must be at least 10 minutes")
	}

	// Peer capacity validation (0-100)
	if c.Peers.CapacityThreshold <= 0 || c.Peers.CapacityThreshold > 100 {
		return fmt.Errorf("peers.capacity_threshold must be between 0 and 100, got %.2f", c.Peers.CapacityThreshold)
	}

	// Service shutdown timeout validation
	if c.Service.ShutdownTimeout > 0 && c.Service.ShutdownTimeout < time.Second {
		return fmt.Errorf("service.shutdown_timeout must be at least 1 second")
	}

	// Set defaults after validation
	c.setDefaults()

	return nil
}

// setDefaults sets default values for configuration fields that are not set
func (c *Config) setDefaults() {
	// Hetzner provider defaults
	if len(c.Hetzner.ServerTypes) == 0 {
		c.Hetzner.ServerTypes = []string{"cx11"}
	}
	if c.Hetzner.Image == "" {
		c.Hetzner.Image = "ubuntu-22.04"
	}
	if len(c.Hetzner.Locations) == 0 {
		c.Hetzner.Locations = []string{"nbg1"}
	}

	// Rotation defaults
	if c.Rotation.Interval <= 0 {
		c.Rotation.Interval = 24 * time.Hour
	}
	if c.Rotation.CleanupAge <= 0 {
		c.Rotation.CleanupAge = 2 * time.Hour
	}

	// Peer defaults
	if c.Peers.MaxPerNode <= 0 {
		c.Peers.MaxPerNode = 50
	}
	if c.Peers.CapacityThreshold <= 0 {
		c.Peers.CapacityThreshold = 80.0
	}

	// Log defaults
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}

	// API defaults
	if c.API.ListenAddr == "" {
		c.API.ListenAddr = ":8080"
	}
	if len(c.API.CORSOrigins) == 0 {
		c.API.CORSOrigins = []string{"http://localhost:3000"}
	}
	// Database defaults
	if c.Database.Path == "" {
		c.Database.Path = "./data/rotator.db"
	}

	// Service defaults
	if c.Service.ShutdownTimeout <= 0 {
		c.Service.ShutdownTimeout = 30 * time.Second
	}
}
