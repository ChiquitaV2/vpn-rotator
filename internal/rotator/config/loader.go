package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Loader handles configuration loading from YAML files and environment variables
type Loader struct {
	v           *viper.Viper
	configPaths []string
	envPrefix   string
}

// NewLoader creates a new configuration loader with enhanced Viper features
func NewLoader() *Loader {
	v := viper.New()

	// Enable case-insensitive key matching
	v.SetTypeByDefaultValue(true)

	return &Loader{
		v:           v,
		configPaths: getDefaultConfigPaths(),
		envPrefix:   "VPN_ROTATOR",
	}
}

// NewLoaderWithOptions creates a loader with custom options
func NewLoaderWithOptions(configPaths []string, envPrefix string) *Loader {
	v := viper.New()
	v.SetTypeByDefaultValue(true)

	return &Loader{
		v:           v,
		configPaths: configPaths,
		envPrefix:   envPrefix,
	}
}

// getDefaultConfigPaths returns the default configuration search paths
func getDefaultConfigPaths() []string {
	paths := []string{
		"/etc/vpn-rotator",   // System-wide config
		"$HOME/.vpn-rotator", // User config
		".",                  // Current directory
	}

	// Add current working directory explicitly
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, wd)
	}

	// Add binary directory if available
	if execPath, err := os.Executable(); err == nil {
		if dir := strings.TrimSuffix(execPath, "/rotator"); dir != execPath {
			paths = append(paths, dir)
		}
	}

	return paths
}

// Load loads configuration from files and environment variables
// Priority order: ENV variables > YAML files > defaults
func (l *Loader) Load() (*Config, error) {
	// Configure file settings
	l.v.SetConfigName("config")
	l.v.SetConfigType("yaml")

	// Add search paths
	for _, path := range l.configPaths {
		l.v.AddConfigPath(path)
	}

	// Configure environment variable support with enhanced mapping
	l.setupEnvironmentVariables()

	// Set all defaults first
	l.setDefaults()

	// Try to read config file (optional)
	configFileUsed := ""
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found is acceptable
	} else {
		configFileUsed = l.v.ConfigFileUsed()
	}

	// Create config with enhanced unmarshaling
	var cfg Config
	if err := l.unmarshalConfig(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Log configuration source for debugging
	if configFileUsed != "" {
		// Note: In production, you might want to log this via your logger
		// fmt.Printf("Loaded configuration from: %s\n", configFileUsed)
	}

	return &cfg, nil
}

// setupEnvironmentVariables configures enhanced environment variable mapping
func (l *Loader) setupEnvironmentVariables() {
	l.v.SetEnvPrefix(l.envPrefix)
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	l.v.AutomaticEnv()

	// Bind specific known environment variables for better discoverability
	envMappings := map[string]string{
		"hetzner.api_token":            "HETZNER_API_TOKEN",
		"hetzner.ssh_private_key_path": "SSH_PRIVATE_KEY_PATH",
		"hetzner.ssh_key":              "SSH_KEY_NAME",
		"database.path":                "DATABASE_PATH",
		"api.listen_addr":              "API_LISTEN_ADDR",
		"log.level":                    "LOG_LEVEL",
		"log.format":                   "LOG_FORMAT",
		"rotation.interval":            "ROTATION_INTERVAL",
		"rotation.cleanup_age":         "ROTATION_CLEANUP_AGE",
		"peers.max_per_node":           "PEERS_MAX_PER_NODE",
		"peers.capacity_threshold":     "PEERS_CAPACITY_THRESHOLD",
		"service.shutdown_timeout":     "SERVICE_SHUTDOWN_TIMEOUT",
	}

	// Bind environment variables with and without prefix
	for viperKey, envKey := range envMappings {
		// Bind with prefix (e.g., VPN_ROTATOR_HETZNER_API_TOKEN)
		l.v.BindEnv(viperKey, l.envPrefix+"_"+envKey)
		// Also bind common environment variables without prefix
		if envKey == "HETZNER_API_TOKEN" || envKey == "DATABASE_PATH" {
			l.v.BindEnv(viperKey, envKey)
		}
	}

	// Enable automatic comma-separated string to slice conversion
	// This works with Viper's built-in StringSlice support
	l.v.SetTypeByDefaultValue(true)
}

// unmarshalConfig unmarshals configuration with enhanced validation
func (l *Loader) unmarshalConfig(cfg *Config) error {
	// Viper handles time.Duration and slice parsing automatically with mapstructure tags
	return l.v.Unmarshal(cfg)
}

// setDefaults sets default configuration values using Viper's hierarchical defaults
func (l *Loader) setDefaults() {
	// === BASIC CONFIGURATION DEFAULTS ===

	// Log defaults
	l.v.SetDefault("log.level", "info")
	l.v.SetDefault("log.format", "json")

	// Hetzner defaults (using Viper's slice support)
	l.v.SetDefault("hetzner.server_types", []string{"cx11"})
	l.v.SetDefault("hetzner.image", "ubuntu-22.04")
	l.v.SetDefault("hetzner.locations", []string{"nbg1"})
	l.v.SetDefault("hetzner.ssh_key", "vpn-rotator-key")

	// Rotation defaults (using Viper's duration support)
	l.v.SetDefault("rotation.interval", "24h")
	l.v.SetDefault("rotation.cleanup_age", "2h")

	// Peer defaults
	l.v.SetDefault("peers.max_per_node", 50)
	l.v.SetDefault("peers.capacity_threshold", 80.0)

	// === ADVANCED CONFIGURATION DEFAULTS ===

	// API defaults
	l.v.SetDefault("api.listen_addr", ":8080")

	// Database defaults
	l.v.SetDefault("database.path", "./data/rotator.db")

	// Service defaults (using Viper's duration support)
	l.v.SetDefault("service.shutdown_timeout", "30s")
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
	loader.setupEnvironmentVariables()

	var cfg Config
	if err := loader.unmarshalConfig(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from env: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// LoadWithWatcher loads configuration and sets up file watching for hot reloads
func LoadWithWatcher(onConfigChange func(*Config)) (*Config, func(), error) {
	loader := NewLoader()

	// Initial load
	cfg, err := loader.Load()
	if err != nil {
		return nil, nil, err
	}

	// Setup file watcher
	loader.v.WatchConfig()
	loader.v.OnConfigChange(func(e fsnotify.Event) {
		// Reload configuration
		var newCfg Config
		if err := loader.unmarshalConfig(&newCfg); err == nil {
			if err := newCfg.Validate(); err == nil {
				onConfigChange(&newCfg)
			}
		}
	})

	// Return config and stop function
	stopWatcher := func() {
		// Viper doesn't provide a direct way to stop watching
		// This is a placeholder for potential future implementation
	}

	return cfg, stopWatcher, nil
}

// GetViperInstance returns the underlying Viper instance for advanced usage
func (l *Loader) GetViperInstance() *viper.Viper {
	return l.v
}

// IsSet checks if a configuration key is set (useful for conditional logic)
func (l *Loader) IsSet(key string) bool {
	return l.v.IsSet(key)
}

// GetString gets a string value with fallback handling
func (l *Loader) GetString(key string) string {
	return l.v.GetString(key)
}

// GetBool gets a boolean value with fallback handling
func (l *Loader) GetBool(key string) bool {
	return l.v.GetBool(key)
}

// GetInt gets an integer value with fallback handling
func (l *Loader) GetInt(key string) int {
	return l.v.GetInt(key)
}

// GetStringSlice gets a string slice value with fallback handling
func (l *Loader) GetStringSlice(key string) []string {
	return l.v.GetStringSlice(key)
}

// AllSettings returns all configuration settings as a map
func (l *Loader) AllSettings() map[string]interface{} {
	return l.v.AllSettings()
}

// ConfigFileUsed returns the path of the config file used
func (l *Loader) ConfigFileUsed() string {
	return l.v.ConfigFileUsed()
}

// GetEnvironmentVariableExamples returns example environment variables for documentation
func GetEnvironmentVariableExamples() map[string]string {
	return map[string]string{
		"VPN_ROTATOR_HETZNER_API_TOKEN":            "your-hetzner-api-token-here",
		"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH": "/path/to/your/ssh/private/key",
		"VPN_ROTATOR_HETZNER_SSH_KEY":              "your-ssh-key-name-in-hetzner",
		"VPN_ROTATOR_HETZNER_SERVER_TYPES":         "cx11,cx21",
		"VPN_ROTATOR_HETZNER_LOCATIONS":            "nbg1,fsn1,hel1",
		"VPN_ROTATOR_HETZNER_IMAGE":                "ubuntu-22.04",
		"VPN_ROTATOR_ROTATION_INTERVAL":            "24h",
		"VPN_ROTATOR_ROTATION_CLEANUP_AGE":         "2h",
		"VPN_ROTATOR_PEERS_MAX_PER_NODE":           "50",
		"VPN_ROTATOR_PEERS_CAPACITY_THRESHOLD":     "80.0",
		"VPN_ROTATOR_LOG_LEVEL":                    "info",
		"VPN_ROTATOR_LOG_FORMAT":                   "json",
		"VPN_ROTATOR_API_LISTEN_ADDR":              ":8080",
		"VPN_ROTATOR_DATABASE_PATH":                "./data/rotator.db",
		"VPN_ROTATOR_SERVICE_SHUTDOWN_TIMEOUT":     "30s",
		// Common alternatives without prefix
		"HETZNER_API_TOKEN": "your-hetzner-api-token-here",
		"DATABASE_PATH":     "./data/rotator.db",
	}
}

// ValidateEnvironment checks if required environment variables are present
func ValidateEnvironment() []string {
	var missing []string
	required := []string{
		"VPN_ROTATOR_HETZNER_API_TOKEN",
		"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH",
	}

	for _, envVar := range required {
		if os.Getenv(envVar) == "" {
			// Check alternative names
			if envVar == "VPN_ROTATOR_HETZNER_API_TOKEN" && os.Getenv("HETZNER_API_TOKEN") != "" {
				continue
			}
			missing = append(missing, envVar)
		}
	}

	return missing
}
