// Package nodeinteractor provides a unified interface for SSH server operations and remote node management.
// It abstracts all SSH operations, WireGuard management, health checking, and system operations
// behind a clean interface with comprehensive error handling, retry logic, and circuit breaker patterns.
package nodeinteractor

import (
	"context"
	"log/slog"
	"time"
)

// Package constants
const (
	DefaultSSHTimeout     = 30 * time.Second
	DefaultCommandTimeout = 60 * time.Second
	DefaultRetryAttempts  = 3
	DefaultRetryBackoff   = 2 * time.Second
)

// Enhanced SSHNodeInteractor with full monitoring and error handling
type EnhancedSSHNodeInteractor struct {
	*SSHNodeInteractor
	metricsCollector *MetricsCollector
	healthMonitor    context.CancelFunc
}

// NewEnhancedSSHNodeInteractor creates a new enhanced SSH-based NodeInteractor with monitoring
func NewEnhancedSSHNodeInteractor(sshPrivateKey string, config NodeInteractorConfig, logger *slog.Logger) (*EnhancedSSHNodeInteractor, error) {
	baseInteractor, err := NewSSHNodeInteractor(sshPrivateKey, config, logger)
	if err != nil {
		return nil, err
	}

	metricsCollector := NewMetricsCollector(logger)

	enhanced := &EnhancedSSHNodeInteractor{
		SSHNodeInteractor: baseInteractor,
		metricsCollector:  metricsCollector,
	}

	// Start health monitoring
	ctx, cancel := context.WithCancel(context.Background())
	enhanced.healthMonitor = cancel
	go enhanced.monitorConnectionHealth(ctx)

	return enhanced, nil
}

// Override methods to add metrics collection

// CheckNodeHealth with metrics collection
func (e *EnhancedSSHNodeInteractor) CheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error) {
	start := time.Now()
	e.logOperationStart("check_health", nodeHost, map[string]interface{}{})

	result, err := e.SSHNodeInteractor.CheckNodeHealth(ctx, nodeHost)

	duration := time.Since(start)
	e.recordOperationMetrics("check_health", start, err)

	if err != nil {
		return nil, e.wrapError(err, "check_health", nodeHost, map[string]interface{}{
			"duration": duration,
		})
	}

	e.logOperationSuccess("check_health", nodeHost, duration, map[string]interface{}{
		"is_healthy":       result.IsHealthy,
		"connected_peers":  result.ConnectedPeers,
		"system_load":      result.SystemLoad,
		"memory_usage":     result.MemoryUsage,
		"disk_usage":       result.DiskUsage,
		"wireguard_status": result.WireGuardStatus,
	})

	return result, nil
}

// AddPeer with metrics collection
func (e *EnhancedSSHNodeInteractor) AddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	start := time.Now()
	e.logOperationStart("add_peer", nodeHost, map[string]interface{}{
		"public_key":  config.PublicKey[:8] + "...",
		"allowed_ips": config.AllowedIPs,
	})

	err := e.SSHNodeInteractor.AddPeer(ctx, nodeHost, config)

	duration := time.Since(start)
	e.recordOperationMetrics("add_peer", start, err)

	if err != nil {
		return e.wrapError(err, "add_peer", nodeHost, map[string]interface{}{
			"public_key":  config.PublicKey[:8] + "...",
			"allowed_ips": config.AllowedIPs,
			"duration":    duration,
		})
	}

	e.logOperationSuccess("add_peer", nodeHost, duration, map[string]interface{}{
		"public_key":  config.PublicKey[:8] + "...",
		"allowed_ips": config.AllowedIPs,
	})

	return nil
}

// RemovePeer with metrics collection
func (e *EnhancedSSHNodeInteractor) RemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	start := time.Now()
	e.logOperationStart("remove_peer", nodeHost, map[string]interface{}{
		"public_key": publicKey[:8] + "...",
	})

	err := e.SSHNodeInteractor.RemovePeer(ctx, nodeHost, publicKey)

	duration := time.Since(start)
	e.recordOperationMetrics("remove_peer", start, err)

	if err != nil {
		return e.wrapError(err, "remove_peer", nodeHost, map[string]interface{}{
			"public_key": publicKey[:8] + "...",
			"duration":   duration,
		})
	}

	e.logOperationSuccess("remove_peer", nodeHost, duration, map[string]interface{}{
		"public_key": publicKey[:8] + "...",
	})

	return nil
}

// ListPeers with metrics collection
func (e *EnhancedSSHNodeInteractor) ListPeers(ctx context.Context, nodeHost string) ([]*WireGuardPeerStatus, error) {
	start := time.Now()
	e.logOperationStart("list_peers", nodeHost, map[string]interface{}{})

	result, err := e.SSHNodeInteractor.ListPeers(ctx, nodeHost)

	duration := time.Since(start)
	e.recordOperationMetrics("list_peers", start, err)

	if err != nil {
		return nil, e.wrapError(err, "list_peers", nodeHost, map[string]interface{}{
			"duration": duration,
		})
	}

	e.logOperationSuccess("list_peers", nodeHost, duration, map[string]interface{}{
		"peer_count": len(result),
	})

	return result, nil
}

// ExecuteCommand with metrics collection
func (e *EnhancedSSHNodeInteractor) ExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
	start := time.Now()
	e.logOperationStart("execute_command", nodeHost, map[string]interface{}{
		"command": command,
	})

	result, err := e.SSHNodeInteractor.ExecuteCommand(ctx, nodeHost, command)

	duration := time.Since(start)
	e.recordOperationMetrics("execute_command", start, err)

	if err != nil {
		return nil, e.wrapError(err, "execute_command", nodeHost, map[string]interface{}{
			"command":  command,
			"duration": duration,
		})
	}

	e.logOperationSuccess("execute_command", nodeHost, duration, map[string]interface{}{
		"command":   command,
		"exit_code": result.ExitCode,
	})

	return result, nil
}

// GetMetrics returns comprehensive metrics about NodeInteractor operations
func (e *EnhancedSSHNodeInteractor) GetMetrics() NodeInteractorMetrics {
	return NodeInteractorMetrics{
		Operations:      e.metricsCollector.GetOperationMetrics(),
		Connections:     e.metricsCollector.GetConnectionMetrics(),
		CircuitBreakers: e.GetCircuitBreakerStats(),
	}
}

// NodeInteractorMetrics contains comprehensive metrics
type NodeInteractorMetrics struct {
	Operations      OperationMetrics                   `json:"operations"`
	Connections     map[string]ConnectionHealthMetrics `json:"connections"`
	CircuitBreakers map[string]CircuitBreakerStats     `json:"circuit_breakers"`
}

// Close closes the enhanced interactor and stops monitoring
func (e *EnhancedSSHNodeInteractor) Close() {
	if e.healthMonitor != nil {
		e.healthMonitor()
	}
	e.SSHNodeInteractor.Close()
	e.logger.Info("enhanced SSH node interactor closed")
}

// Utility functions for creating NodeInteractor instances

// NewNodeInteractorWithDefaults creates a NodeInteractor with default configuration
func NewNodeInteractorWithDefaults(sshPrivateKey string, logger *slog.Logger) (NodeInteractor, error) {
	config := DefaultConfig()
	return NewEnhancedSSHNodeInteractor(sshPrivateKey, config, logger)
}

// NewNodeInteractorForTesting creates a NodeInteractor suitable for testing
func NewNodeInteractorForTesting(sshPrivateKey string, logger *slog.Logger) (NodeInteractor, error) {
	config := DefaultConfig()
	// Reduce timeouts for faster tests
	config.SSHTimeout = 5 * time.Second
	config.CommandTimeout = 10 * time.Second
	config.RetryAttempts = 1
	config.RetryBackoff = 100 * time.Millisecond

	// Reduce circuit breaker thresholds for testing
	config.CircuitBreaker.FailureThreshold = 2
	config.CircuitBreaker.ResetTimeout = 5 * time.Second

	return NewEnhancedSSHNodeInteractor(sshPrivateKey, config, logger)
}
