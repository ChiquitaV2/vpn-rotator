package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Loader handles configuration loading from YAML files and environment variables
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// Load loads configuration from files and environment variables
// YAML files take precedence, then ENV variables override
func (l *Loader) Load() (*Config, error) {
	// Set config file settings
	l.v.SetConfigName("config")
	l.v.SetConfigType("yaml")

	// Add multiple search paths (in order of priority)
	l.v.AddConfigPath("/etc/vpn-rotator")   // System-wide config
	l.v.AddConfigPath("$HOME/.vpn-rotator") // User config
	l.v.AddConfigPath(".")                  // Current directory

	// Enable environment variable support
	l.v.SetEnvPrefix("VPN_ROTATOR")
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()

	// Set defaults
	l.setDefaults()

	// Read config file (optional - will use defaults and ENV if not found)
	if err := l.v.ReadInConfig(); err != nil {
		// Config file not found is OK - we'll use defaults and ENV
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Unmarshal into Config struct
	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func (l *Loader) setDefaults() {
	// Log defaults
	l.v.SetDefault("log.level", "info")
	l.v.SetDefault("log.format", "json")

	// API defaults
	l.v.SetDefault("api.listen_addr", ":8080")

	// Database defaults
	l.v.SetDefault("db.path", "./data/rotator.db")
	l.v.SetDefault("db.max_open_conns", 25)
	l.v.SetDefault("db.max_idle_conns", 5)
	l.v.SetDefault("db.conn_max_lifetime", 300) // 5 minutes

	// Service defaults
	l.v.SetDefault("service.shutdown_timeout", "30s")

	// Scheduler defaults
	l.v.SetDefault("scheduler.rotation_interval", "24h")
	l.v.SetDefault("scheduler.cleanup_interval", "1h")
	l.v.SetDefault("scheduler.cleanup_age", "2h")

	// Hetzner defaults
	l.v.SetDefault("hetzner.server_types", []string{"cx11"})
	l.v.SetDefault("hetzner.image", "ubuntu-22.04")
	l.v.SetDefault("hetzner.locations", []string{"nbg1"})

	// Node service defaults
	l.v.SetDefault("node_service.max_peers_per_node", 50)
	l.v.SetDefault("node_service.capacity_threshold", 80.0)
	l.v.SetDefault("node_service.health_check_timeout", "30s")
	l.v.SetDefault("node_service.provisioning_timeout", "10m")
	l.v.SetDefault("node_service.destruction_timeout", "5m")
	l.v.SetDefault("node_service.optimal_node_selection", "least_loaded")

	// Node interactor defaults
	l.v.SetDefault("node_interactor.ssh_timeout", "30s")
	l.v.SetDefault("node_interactor.command_timeout", "60s")
	l.v.SetDefault("node_interactor.wireguard_config_path", "/etc/wireguard/wg0.conf")
	l.v.SetDefault("node_interactor.wireguard_interface", "wg0")
	l.v.SetDefault("node_interactor.retry_attempts", 3)
	l.v.SetDefault("node_interactor.retry_backoff", "2s")

	// Circuit breaker defaults
	l.v.SetDefault("circuit_breaker.failure_threshold", 5)
	l.v.SetDefault("circuit_breaker.reset_timeout", "60s")
	l.v.SetDefault("circuit_breaker.max_attempts", 3)
}

// LoadWithPath loads configuration from a specific file path
func LoadWithPath(configPath string) (*Config, error) {
	loader := NewLoader()
	loader.v.SetConfigFile(configPath)
	return loader.Load()
}

// LoadFromEnv loads configuration only from environment variables
func LoadFromEnv() (*Config, error) {
	loader := NewLoader()
	loader.setDefaults()

	// Skip file reading, only use ENV vars
	loader.v.SetEnvPrefix("VPN_ROTATOR")
	loader.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	loader.v.AutomaticEnv()

	var cfg Config
	if err := loader.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from env: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}
