package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnhancedViperFeatures(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		testFunc func(t *testing.T, loader *Loader)
	}{
		{
			name: "Environment variable mapping with prefix",
			envVars: map[string]string{
				"VPN_ROTATOR_HETZNER_API_TOKEN":            "test-token-prefixed",
				"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH": "/test/key/path",
				"VPN_ROTATOR_ROTATION_INTERVAL":            "12h",
				"VPN_ROTATOR_PEERS_MAX_PER_NODE":           "75",
				"VPN_ROTATOR_PEERS_CAPACITY_THRESHOLD":     "90.5",
				"VPN_ROTATOR_LOG_LEVEL":                    "debug",
			},
			testFunc: func(t *testing.T, loader *Loader) {
				assert.Equal(t, "test-token-prefixed", loader.GetString("hetzner.api_token"))
				assert.Equal(t, "/test/key/path", loader.GetString("hetzner.ssh_private_key_path"))
				assert.Equal(t, "12h", loader.GetString("rotation.interval"))
				assert.Equal(t, 75, loader.GetInt("peers.max_per_node"))
				assert.Equal(t, 90.5, loader.v.GetFloat64("peers.capacity_threshold"))
				assert.Equal(t, "debug", loader.GetString("log.level"))
			},
		},
		{
			name: "Alternative environment variable names",
			envVars: map[string]string{
				"HETZNER_API_TOKEN":                        "test-token-alt",
				"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH": "/test/key/path",
				"DATABASE_PATH":                            "/custom/db/path.db",
			},
			testFunc: func(t *testing.T, loader *Loader) {
				assert.Equal(t, "test-token-alt", loader.GetString("hetzner.api_token"))
				assert.Equal(t, "/custom/db/path.db", loader.GetString("database.path"))
			},
		},
		{
			name: "Duration parsing and validation",
			envVars: map[string]string{
				"VPN_ROTATOR_HETZNER_API_TOKEN":            "test-token",
				"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH": "/test/key/path",
				"VPN_ROTATOR_ROTATION_INTERVAL":            "6h30m",
				"VPN_ROTATOR_ROTATION_CLEANUP_AGE":         "45m",
				"VPN_ROTATOR_SERVICE_SHUTDOWN_TIMEOUT":     "60s",
			},
			testFunc: func(t *testing.T, loader *Loader) {
				cfg, err := loader.Load()
				require.NoError(t, err)

				assert.Equal(t, 6*time.Hour+30*time.Minute, cfg.Rotation.Interval)
				assert.Equal(t, 45*time.Minute, cfg.Rotation.CleanupAge)
				assert.Equal(t, 60*time.Second, cfg.Service.ShutdownTimeout)
			},
		},
		{
			name: "IsSet functionality",
			envVars: map[string]string{
				"VPN_ROTATOR_HETZNER_API_TOKEN": "test-token",
				"VPN_ROTATOR_LOG_LEVEL":         "warn",
			},
			testFunc: func(t *testing.T, loader *Loader) {
				assert.True(t, loader.IsSet("hetzner.api_token"))
				assert.True(t, loader.IsSet("log.level"))
				// Note: IsSet returns true for keys with defaults set by setDefaults()
				// This is expected Viper behavior
				assert.False(t, loader.IsSet("nonexistent.key"))
			},
		},
		{
			name: "AllSettings functionality",
			envVars: map[string]string{
				"VPN_ROTATOR_HETZNER_API_TOKEN":            "test-token",
				"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH": "/test/key/path",
				"VPN_ROTATOR_LOG_LEVEL":                    "info",
			},
			testFunc: func(t *testing.T, loader *Loader) {
				settings := loader.AllSettings()
				assert.NotEmpty(t, settings)

				// Should contain our basic structure
				assert.Contains(t, settings, "hetzner")
				assert.Contains(t, settings, "rotation")
				assert.Contains(t, settings, "peers")
				assert.Contains(t, settings, "log")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment before each test
			clearTestEnv(t)

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Create loader and set it up
			loader := NewLoader()
			loader.setupEnvironmentVariables()
			loader.setDefaults()

			// Run the test
			tt.testFunc(t, loader)

			// Clean up environment after test
			clearTestEnv(t)
		})
	}
}

func TestEnvironmentVariableValidation(t *testing.T) {
	// Clear environment
	clearTestEnv(t)

	// Test missing required variables
	missing := ValidateEnvironment()
	assert.Len(t, missing, 2) // Should be missing API token and SSH key path
	assert.Contains(t, missing, "VPN_ROTATOR_HETZNER_API_TOKEN")
	assert.Contains(t, missing, "VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH")

	// Set required variables
	os.Setenv("VPN_ROTATOR_HETZNER_API_TOKEN", "test-token")
	os.Setenv("VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH", "/test/key")

	missing = ValidateEnvironment()
	assert.Empty(t, missing)

	// Test alternative API token name
	os.Unsetenv("VPN_ROTATOR_HETZNER_API_TOKEN")
	os.Setenv("HETZNER_API_TOKEN", "alt-token")

	missing = ValidateEnvironment()
	assert.Empty(t, missing) // Both should be satisfied now with alternative token name

	clearTestEnv(t)
}

func TestGetEnvironmentVariableExamples(t *testing.T) {
	examples := GetEnvironmentVariableExamples()

	// Should contain required variables
	assert.Contains(t, examples, "VPN_ROTATOR_HETZNER_API_TOKEN")
	assert.Contains(t, examples, "VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH")

	// Should contain alternative names
	assert.Contains(t, examples, "HETZNER_API_TOKEN")
	assert.Contains(t, examples, "DATABASE_PATH")

	// Should have reasonable example values
	assert.NotEmpty(t, examples["VPN_ROTATOR_HETZNER_API_TOKEN"])
	assert.NotEmpty(t, examples["VPN_ROTATOR_ROTATION_INTERVAL"])
}

func TestConfigSearchPaths(t *testing.T) {
	paths := getDefaultConfigPaths()

	// Should include standard paths
	assert.Contains(t, paths, "/etc/vpn-rotator")
	assert.Contains(t, paths, "$HOME/.vpn-rotator")
	assert.Contains(t, paths, ".")

	// Should include current working directory
	wd, err := os.Getwd()
	if err == nil {
		assert.Contains(t, paths, wd)
	}
}

// clearTestEnv clears all VPN_ROTATOR_* environment variables for testing
func clearTestEnv(t *testing.T) {
	envVars := []string{
		"VPN_ROTATOR_HETZNER_API_TOKEN",
		"VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH",
		"VPN_ROTATOR_HETZNER_SSH_KEY",
		"VPN_ROTATOR_HETZNER_SERVER_TYPES",
		"VPN_ROTATOR_HETZNER_LOCATIONS",
		"VPN_ROTATOR_HETZNER_IMAGE",
		"VPN_ROTATOR_ROTATION_INTERVAL",
		"VPN_ROTATOR_ROTATION_CLEANUP_AGE",
		"VPN_ROTATOR_PEERS_MAX_PER_NODE",
		"VPN_ROTATOR_PEERS_CAPACITY_THRESHOLD",
		"VPN_ROTATOR_LOG_LEVEL",
		"VPN_ROTATOR_LOG_FORMAT",
		"VPN_ROTATOR_API_LISTEN_ADDR",
		"VPN_ROTATOR_DATABASE_PATH",
		"VPN_ROTATOR_SERVICE_SHUTDOWN_TIMEOUT",
		"HETZNER_API_TOKEN",
		"DATABASE_PATH",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}
}
