package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoader_Load_Defaults(t *testing.T) {
	// Unset env vars to ensure a clean test
	os.Unsetenv("VPN_ROTATOR_API_URL")

	// Mock home directory to avoid polluting user's home
	tmpDir, err := os.MkdirTemp("", "home")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("HOME", tmpDir)

	loader := NewLoader()
	cfg, err := loader.Load()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.APIURL != "http://localhost:8080" {
		t.Errorf("wrong APIURL: got %s", cfg.APIURL)
	}
	if cfg.Interface != "wg0" {
		t.Errorf("wrong Interface: got %s", cfg.Interface)
	}
	if cfg.PollInterval != 15 {
		t.Errorf("wrong PollInterval: got %d", cfg.PollInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("wrong LogLevel: got %s", cfg.LogLevel)
	}
	if cfg.KeyPath == "" {
		t.Error("KeyPath should be auto-resolved, but is empty")
	}
}

func TestLoader_Load_FromFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, ".vpn-rotator.yaml")
	configContent := `
api_url: "http://test.com:9090"
interface: "wg-test"
poll_interval: 30
log_level: "debug"
generate_keys: true
`
	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer os.Chdir(originalWd)

	loader := NewLoader()
	cfg, err := loader.Load()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.APIURL != "http://test.com:9090" {
		t.Errorf("wrong APIURL: got %s", cfg.APIURL)
	}
	if cfg.Interface != "wg-test" {
		t.Errorf("wrong Interface: got %s", cfg.Interface)
	}
	if cfg.PollInterval != 30 {
		t.Errorf("wrong PollInterval: got %d", cfg.PollInterval)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("wrong LogLevel: got %s", cfg.LogLevel)
	}
	if !cfg.GenerateKeys {
		t.Error("expected GenerateKeys to be true")
	}
}

func TestLoader_Load_FromEnv(t *testing.T) {
	t.Setenv("VPN_ROTATOR_API_URL", "http://env.com")
	t.Setenv("VPN_ROTATOR_LOG_LEVEL", "warn")
	t.Setenv("VPN_ROTATOR_GENERATE_KEYS", "true")

	loader := NewLoader()
	cfg, err := loader.Load()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.APIURL != "http://env.com" {
		t.Errorf("wrong APIURL: got %s", cfg.APIURL)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("wrong LogLevel: got %s", cfg.LogLevel)
	}
	if !cfg.GenerateKeys {
		t.Error("expected GenerateKeys to be true")
	}
}

func TestLoader_Validation(t *testing.T) {
	t.Run("missing api_url", func(t *testing.T) {
		loader := NewLoader()
		loader.v.Set("api_url", "")
		_, err := loader.Load()
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "api_url is required") {
			t.Errorf("expected error to contain 'api_url is required', got '%v'", err)
		}
	})

	t.Run("invalid log_level", func(t *testing.T) {
		loader := NewLoader()
		loader.v.Set("log_level", "trace")
		_, err := loader.Load()
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid log_level") {
			t.Errorf("expected error to contain 'invalid log_level', got '%v'", err)
		}
	})
}
