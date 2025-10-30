package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Loader provides a streamlined configuration loading experience
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new simplified configuration loader
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// Load loads configuration with intelligent defaults and automatic key discovery
func (l *Loader) Load() (*Config, error) {
	l.setIntelligentDefaults()
	l.setupAutoDiscovery()
	l.setupEnvironment()

	// Try to read config file (optional)
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is OK
	}

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply intelligent path resolution
	if err := l.resolveIntelligentPaths(&cfg); err != nil {
		return nil, fmt.Errorf("failed to resolve paths: %w", err)
	}

	// Basic validation
	if err := l.validateSimple(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// setIntelligentDefaults sets smart defaults based on common usage patterns
func (l *Loader) setIntelligentDefaults() {
	// API defaults
	l.v.SetDefault("api_url", "http://localhost:8080")

	// Network defaults
	l.v.SetDefault("interface", "wg0")
	l.v.SetDefault("allowed_ips", "0.0.0.0/0")
	l.v.SetDefault("dns", "1.1.1.1,8.8.8.8")

	// Timing defaults
	l.v.SetDefault("poll_interval", 15) // 15 minutes

	// Logging defaults
	l.v.SetDefault("log_level", "info")
	l.v.SetDefault("log_format", "text")

	// Key management - auto-discover or generate
	l.v.SetDefault("key_path", "") // Will be auto-resolved
	l.v.SetDefault("generate_keys", false)
}

// setupAutoDiscovery configures automatic discovery of config files and directories
func (l *Loader) setupAutoDiscovery() {
	l.v.SetConfigName(".vpn-rotator")
	l.v.SetConfigType("yaml")

	// Search in standard locations
	l.v.AddConfigPath("/etc/vpn-rotator")

	// User home directory
	if home, err := os.UserHomeDir(); err == nil {
		l.v.AddConfigPath(home)
		l.v.AddConfigPath(filepath.Join(home, ".vpn-rotator"))
		l.v.AddConfigPath(filepath.Join(home, ".config", "vpn-rotator"))
	}

	// Current directory
	l.v.AddConfigPath(".")
}

// setupEnvironment configures environment variable handling
func (l *Loader) setupEnvironment() {
	l.v.SetEnvPrefix("VPN_ROTATOR")
	l.v.AutomaticEnv()
}

// resolveIntelligentPaths automatically discovers and resolves file paths
func (l *Loader) resolveIntelligentPaths(cfg *Config) error {
	// Auto-discover key path if not specified
	if cfg.KeyPath == "" {
		keyPath, err := l.discoverKeyPath()
		if err != nil {
			return fmt.Errorf("failed to discover key path: %w", err)
		}
		cfg.KeyPath = keyPath
	} else {
		cfg.KeyPath = expandPath(cfg.KeyPath)
	}

	return nil
}

// discoverKeyPath intelligently discovers where to store/find the private key
func (l *Loader) discoverKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Preferred locations in order of preference
	candidates := []string{
		filepath.Join(home, ".vpn-rotator", "client.key"),
		filepath.Join(home, ".config", "vpn-rotator", "client.key"),
		filepath.Join(home, ".vpn-rotator.key"),
	}

	// Check if any existing key files exist
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// No existing key found, use the preferred location
	preferredPath := candidates[0]

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(preferredPath), 0700); err != nil {
		return "", fmt.Errorf("failed to create key directory: %w", err)
	}

	return preferredPath, nil
}

// validateSimple performs basic validation of the configuration
func (l *Loader) validateSimple(cfg *Config) error {
	if cfg.APIURL == "" {
		return fmt.Errorf("api_url is required")
	}

	if cfg.Interface == "" {
		return fmt.Errorf("interface name is required")
	}

	if cfg.PollInterval < 1 {
		return fmt.Errorf("poll_interval must be at least 1 minute")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[cfg.LogLevel] {
		return fmt.Errorf("invalid log_level: %s", cfg.LogLevel)
	}

	// Validate log format
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return fmt.Errorf("invalid log_format: %s", cfg.LogFormat)
	}

	return nil
}

// LoadFromFile loads configuration from a specific file with intelligent defaults
func LoadFromFile(configPath string) (*Config, error) {
	loader := NewLoader()
	loader.setIntelligentDefaults()
	loader.setupEnvironment()

	loader.v.SetConfigFile(configPath)

	if err := loader.v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", configPath, err)
	}

	var cfg Config
	if err := loader.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := loader.resolveIntelligentPaths(&cfg); err != nil {
		return nil, fmt.Errorf("failed to resolve paths: %w", err)
	}

	if err := loader.validateSimple(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// LoadFromEnvOnly loads configuration only from environment variables with intelligent defaults
func LoadFromEnvOnly() (*Config, error) {
	loader := NewLoader()
	loader.setIntelligentDefaults()
	loader.setupEnvironment()

	var cfg Config
	if err := loader.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from environment: %w", err)
	}

	if err := loader.resolveIntelligentPaths(&cfg); err != nil {
		return nil, fmt.Errorf("failed to resolve paths: %w", err)
	}

	if err := loader.validateSimple(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// CreateDefaultConfig creates a default configuration file in the user's home directory
func CreateDefaultConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".vpn-rotator")
	configFile := filepath.Join(configDir, ".vpn-rotator.yaml")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Don't overwrite existing config
	if _, err := os.Stat(configFile); err == nil {
		return fmt.Errorf("config file already exists: %s", configFile)
	}

	defaultConfig := `# VPN Rotator Client Configuration
# This file is automatically created with sensible defaults

# API Configuration
api_url: "http://localhost:8080"

# Network Configuration  
interface: "wg0"
allowed_ips: "0.0.0.0/0"
dns: "1.1.1.1,8.8.8.8"

# Timing Configuration
poll_interval: 15  # minutes between rotation checks

# Logging Configuration
log_level: "info"   # debug, info, warn, error
log_format: "text"  # text or json

# Key Management
# key_path: ""        # auto-discovered if not specified
generate_keys: false  # set to true to use server-generated keys
`

	if err := os.WriteFile(configFile, []byte(defaultConfig), 0600); err != nil {
		return fmt.Errorf("failed to write default config: %w", err)
	}

	fmt.Printf("Created default configuration file: %s\n", configFile)
	fmt.Printf("Edit this file to customize your VPN Rotator settings.\n")

	return nil
}

// expandPath expands ~ to home directory in file paths.
func expandPath(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if len(path) == 1 {
		return home
	}

	return filepath.Join(home, path[1:])
}
