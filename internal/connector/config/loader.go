package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Loader handles configuration loading from multiple sources.
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new configuration loader.
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// Load loads configuration from files and environment variables.
func (l *Loader) Load() (*Config, error) {
	l.setDefaults()
	l.setupConfigPaths()
	l.setupEnvVars()

	// Try to read config file (it's optional)
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is OK, we'll use defaults + env vars
	}

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := l.validate(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Expand home directory in paths
	cfg.KeyPath = expandPath(cfg.KeyPath)

	return &cfg, nil
}

// setDefaults sets default configuration values.
func (l *Loader) setDefaults() {
	l.v.SetDefault("api_url", "http://localhost:8080")
	l.v.SetDefault("key_path", "~/.vpn-rotator/client.key")
	l.v.SetDefault("interface", "wg0")
	l.v.SetDefault("poll_interval", 15)
	l.v.SetDefault("allowed_ips", "0.0.0.0/0")
	l.v.SetDefault("dns", "1.1.1.1,8.8.8.8")
	l.v.SetDefault("verbose", false)
	l.v.SetDefault("retry_attempts", 3)
	l.v.SetDefault("retry_delay", 1)
	l.v.SetDefault("log_level", "info")
	l.v.SetDefault("log_format", "text")
}

// setupConfigPaths configures where to search for config files.
func (l *Loader) setupConfigPaths() {
	l.v.SetConfigName(".vpn-rotator")
	l.v.SetConfigType("yaml")

	// Search paths in priority order
	l.v.AddConfigPath("/etc/vpn-rotator")
	if home, err := os.UserHomeDir(); err == nil {
		l.v.AddConfigPath(home)
	}
	l.v.AddConfigPath(".")
}

// setupEnvVars configures environment variable handling.
func (l *Loader) setupEnvVars() {
	l.v.SetEnvPrefix("VPN_ROTATOR")
	l.v.AutomaticEnv()
}

// validate validates the configuration.
func (l *Loader) validate(cfg *Config) error {
	if cfg.APIURL == "" {
		return fmt.Errorf("api_url is required")
	}

	if cfg.Interface == "" {
		return fmt.Errorf("interface name is required")
	}

	if cfg.PollInterval < 1 {
		return fmt.Errorf("poll_interval must be at least 1 minute")
	}

	if cfg.RetryAttempts < 1 {
		return fmt.Errorf("retry_attempts must be at least 1")
	}

	if cfg.RetryDelay < 1 {
		return fmt.Errorf("retry_delay must be at least 1 second")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[cfg.LogLevel] {
		return fmt.Errorf("invalid log_level: %s (must be debug, info, warn, or error)", cfg.LogLevel)
	}

	// Validate log format
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return fmt.Errorf("invalid log_format: %s (must be text or json)", cfg.LogFormat)
	}

	return nil
}

// LoadWithPath loads configuration from a specific file path.
func LoadWithPath(path string) (*Config, error) {
	loader := NewLoader()
	loader.setDefaults()
	loader.setupEnvVars()

	loader.v.SetConfigFile(path)

	if err := loader.v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := loader.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := loader.validate(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	cfg.KeyPath = expandPath(cfg.KeyPath)

	return &cfg, nil
}

// LoadFromEnv loads configuration only from environment variables.
func LoadFromEnv() (*Config, error) {
	loader := NewLoader()
	loader.setDefaults()
	loader.setupEnvVars()

	var cfg Config
	if err := loader.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from environment: %w", err)
	}

	if err := loader.validate(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	cfg.KeyPath = expandPath(cfg.KeyPath)

	return &cfg, nil
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
