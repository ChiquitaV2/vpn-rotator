package remote

import (
	"fmt"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// DefaultConfig returns a NodeInteractorConfig with sensible defaults
func DefaultConfig() NodeInteractorConfig {
	return NodeInteractorConfig{
		SSHTimeout:          30 * time.Second,
		CommandTimeout:      60 * time.Second,
		WireGuardConfigPath: "/etc/wireguard/wg0.conf",
		WireGuardInterface:  "wg0",
		RetryAttempts:       3,
		RetryBackoff:        2 * time.Second,
		HealthCheckCommands: []HealthCommand{
			{
				Name:         "connectivity",
				Command:      "echo 'health_check'",
				Timeout:      5 * time.Second,
				Critical:     true,
				ExpectedExit: 0,
			},
			{
				Name:         "wireguard_status",
				Command:      "wg show wg0",
				Timeout:      10 * time.Second,
				Critical:     true,
				ExpectedExit: 0,
			},
			{
				Name:         "system_load",
				Command:      "uptime",
				Timeout:      5 * time.Second,
				Critical:     false,
				ExpectedExit: 0,
			},
			{
				Name:         "disk_space",
				Command:      "df -kP /",
				Timeout:      5 * time.Second,
				Critical:     false,
				ExpectedExit: 0,
			},
			{
				Name:         "memory_usage",
				Command:      "free -m",
				Timeout:      5 * time.Second,
				Critical:     false,
				ExpectedExit: 0,
			},
		},
		SystemMetricsPath: SystemMetricsPaths{
			LoadAvg:   "/proc/loadavg",
			MemInfo:   "/proc/meminfo",
			DiskUsage: "df -kP /",
			Uptime:    "/proc/uptime",
			Hostname:  "/etc/hostname",
			OSRelease: "/etc/os-release",
		},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold: 5,
			ResetTimeout:     60 * time.Second,
			MaxAttempts:      3,
		},
	}
}

// ValidateConfig validates the NodeInteractorConfig
func ValidateConfig(config NodeInteractorConfig) error {
	if config.SSHTimeout <= 0 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "ssh_timeout must be greater than 0", false, nil)
	}

	if config.CommandTimeout <= 0 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "command_timeout must be greater than 0", false, nil)
	}

	if config.WireGuardInterface == "" {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "wireguard_interface cannot be empty", false, nil)
	}

	if config.WireGuardConfigPath == "" {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "wireguard_config_path cannot be empty", false, nil)
	}

	if config.RetryAttempts < 1 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "retry_attempts must be at least 1", false, nil)
	}

	if config.RetryBackoff <= 0 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "retry_backoff must be greater than 0", false, nil)
	}

	// Validate health check commands
	for i, cmd := range config.HealthCheckCommands {
		if cmd.Name == "" {
			return errors.NewInfrastructureError(errors.ErrCodeConfiguration, fmt.Sprintf("health_check_commands[%d].name cannot be empty", i), false, nil)
		}
		if cmd.Command == "" {
			return errors.NewInfrastructureError(errors.ErrCodeConfiguration, fmt.Sprintf("health_check_commands[%d].command cannot be empty", i), false, nil)
		}
		if cmd.Timeout <= 0 {
			return errors.NewInfrastructureError(errors.ErrCodeConfiguration, fmt.Sprintf("health_check_commands[%d].timeout must be greater than 0", i), false, nil)
		}
	}

	// Validate circuit breaker config
	if config.CircuitBreaker.FailureThreshold < 1 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "circuit_breaker.failure_threshold must be at least 1", false, nil)
	}

	if config.CircuitBreaker.ResetTimeout <= 0 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "circuit_breaker.reset_timeout must be greater than 0", false, nil)
	}

	if config.CircuitBreaker.MaxAttempts < 1 {
		return errors.NewInfrastructureError(errors.ErrCodeConfiguration, "circuit_breaker.max_attempts must be at least 1", false, nil)
	}

	return nil
}
