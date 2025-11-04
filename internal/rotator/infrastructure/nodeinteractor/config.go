package nodeinteractor

import "time"

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
		return NewValidationError("ssh_timeout", config.SSHTimeout, "positive", "must be greater than 0")
	}

	if config.CommandTimeout <= 0 {
		return NewValidationError("command_timeout", config.CommandTimeout, "positive", "must be greater than 0")
	}

	if config.WireGuardInterface == "" {
		return NewValidationError("wireguard_interface", config.WireGuardInterface, "non-empty", "cannot be empty")
	}

	if config.WireGuardConfigPath == "" {
		return NewValidationError("wireguard_config_path", config.WireGuardConfigPath, "non-empty", "cannot be empty")
	}

	if config.RetryAttempts < 1 {
		return NewValidationError("retry_attempts", config.RetryAttempts, "minimum", "must be at least 1")
	}

	if config.RetryBackoff <= 0 {
		return NewValidationError("retry_backoff", config.RetryBackoff, "positive", "must be greater than 0")
	}

	// Validate health check commands
	for i, cmd := range config.HealthCheckCommands {
		if cmd.Name == "" {
			return NewValidationError("health_check_commands["+string(rune(i))+"].name", cmd.Name, "non-empty", "cannot be empty")
		}
		if cmd.Command == "" {
			return NewValidationError("health_check_commands["+string(rune(i))+"].command", cmd.Command, "non-empty", "cannot be empty")
		}
		if cmd.Timeout <= 0 {
			return NewValidationError("health_check_commands["+string(rune(i))+"].timeout", cmd.Timeout, "positive", "must be greater than 0")
		}
	}

	// Validate circuit breaker config
	if config.CircuitBreaker.FailureThreshold < 1 {
		return NewValidationError("circuit_breaker.failure_threshold", config.CircuitBreaker.FailureThreshold, "minimum", "must be at least 1")
	}

	if config.CircuitBreaker.ResetTimeout <= 0 {
		return NewValidationError("circuit_breaker.reset_timeout", config.CircuitBreaker.ResetTimeout, "positive", "must be greater than 0")
	}

	if config.CircuitBreaker.MaxAttempts < 1 {
		return NewValidationError("circuit_breaker.max_attempts", config.CircuitBreaker.MaxAttempts, "minimum", "must be at least 1")
	}

	return nil
}
