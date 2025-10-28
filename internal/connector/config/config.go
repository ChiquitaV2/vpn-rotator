package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds the connector application configuration.
type Config struct {
	APIURL        string `mapstructure:"api_url"`
	KeyPath       string `mapstructure:"key_path"`
	Interface     string `mapstructure:"interface"`
	PollInterval  int    `mapstructure:"poll_interval"`
	AllowedIPs    string `mapstructure:"allowed_ips"`
	DNS           string `mapstructure:"dns"`
	Verbose       bool   `mapstructure:"verbose"`
	RetryAttempts int    `mapstructure:"retry_attempts"`
	RetryDelay    int    `mapstructure:"retry_delay"`
	LogLevel      string `mapstructure:"log_level"`
	LogFormat     string `mapstructure:"log_format"`
}

// LoadConfig loads the application configuration from various sources.
// Deprecated: Use NewLoader().Load() instead for better configuration management.
func LoadConfig() (*Config, error) {
	// Set default values
	viper.SetDefault("api_url", "http://localhost:8080")
	viper.SetDefault("key_path", "~/.vpn-rotator/client.key")
	viper.SetDefault("interface", "wg1")
	viper.SetDefault("poll_interval", 15) // minutes
	viper.SetDefault("allowed_ips", "0.0.0.0/0")
	viper.SetDefault("dns", "1.1.1.1,8.8.8.8")
	viper.SetDefault("verbose", false)
	viper.SetDefault("retry_attempts", 3)
	viper.SetDefault("retry_delay", 1) // seconds
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "text")

	// Read configuration from environment variables
	viper.AutomaticEnv()

	// Create and unmarshal config
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}
