package nodeinteractor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/remote/ssh"
)

// SSHNodeInteractor implements NodeInteractor using SSH connections with pooling
type SSHNodeInteractor struct {
	sshPool          *ssh.Pool
	config           NodeInteractorConfig
	logger           *slog.Logger
	circuitBreakers  map[string]*CircuitBreaker
	cbMutex          sync.RWMutex
	metricsCollector *MetricsCollector
	healthMonitor    context.CancelFunc
}

// NewSSHNodeInteractor creates a new SSH-based NodeInteractor
func NewSSHNodeInteractor(sshPrivateKey string, config NodeInteractorConfig, logger *slog.Logger) (*SSHNodeInteractor, error) {
	if err := ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create SSH connection pool
	sshPool := ssh.NewPool(sshPrivateKey, logger, 5*time.Minute)
	if sshPool == nil {
		return nil, fmt.Errorf("failed to create SSH connection pool")
	}

	metricsCollector := NewMetricsCollector(logger)

	interactor := &SSHNodeInteractor{
		sshPool:          sshPool,
		config:           config,
		logger:           logger,
		circuitBreakers:  make(map[string]*CircuitBreaker),
		metricsCollector: metricsCollector,
	}

	// Start health monitoring
	ctx, cancel := context.WithCancel(context.Background())
	interactor.healthMonitor = cancel
	go interactor.monitorConnectionHealth(ctx)

	return interactor, nil
}

// getCircuitBreaker gets or creates a circuit breaker for a host
func (s *SSHNodeInteractor) getCircuitBreaker(host string) *CircuitBreaker {
	s.cbMutex.RLock()
	if cb, exists := s.circuitBreakers[host]; exists {
		s.cbMutex.RUnlock()
		return cb
	}
	s.cbMutex.RUnlock()

	s.cbMutex.Lock()
	defer s.cbMutex.Unlock()

	// Double-check after acquiring write lock
	if cb, exists := s.circuitBreakers[host]; exists {
		return cb
	}

	cb := NewCircuitBreaker(s.config.CircuitBreaker)
	s.circuitBreakers[host] = cb
	return cb
}

// executeWithRetry executes a command with retry logic and circuit breaker protection
func (s *SSHNodeInteractor) executeWithRetry(ctx context.Context, host, command string) (*CommandResult, error) {
	cb := s.getCircuitBreaker(host)

	var lastErr error
	var result *CommandResult

	for attempt := 1; attempt <= s.config.RetryAttempts; attempt++ {
		err := cb.Execute(ctx, func() error {
			var execErr error
			result, execErr = s.executeCommandOnce(ctx, host, command)
			return execErr
		})

		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if !s.isRetryableError(err) {
			s.logger.Debug("non-retryable error encountered",
				slog.String("host", host),
				slog.String("command", command),
				slog.String("error", err.Error()))
			break
		}

		if attempt < s.config.RetryAttempts {
			backoff := time.Duration(attempt) * s.config.RetryBackoff
			s.logger.Debug("command failed, retrying",
				slog.String("host", host),
				slog.String("command", command),
				slog.Int("attempt", attempt),
				slog.Duration("backoff", backoff),
				slog.String("error", err.Error()))

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Continue to next attempt
			}
		}
	}

	return nil, fmt.Errorf("command failed after %d attempts: %w", s.config.RetryAttempts, lastErr)
}

// executeCommandOnce executes a command once without retry logic
func (s *SSHNodeInteractor) executeCommandOnce(ctx context.Context, host, command string) (*CommandResult, error) {
	start := time.Now()

	// Create timeout context
	cmdCtx, cancel := context.WithTimeout(ctx, s.config.CommandTimeout)
	defer cancel()

	output, err := s.sshPool.ExecuteCommand(cmdCtx, host, command)
	duration := time.Since(start)

	if err != nil {
		return &CommandResult{
			ExitCode: 1,
			Stderr:   err.Error(),
			Duration: duration,
		}, NewSSHCommandError(host, command, 1, "", err.Error(), err)
	}

	return &CommandResult{
		ExitCode: 0,
		Stdout:   output,
		Duration: duration,
	}, nil
}

// isRetryableError determines if an error is worth retrying
func (s *SSHNodeInteractor) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific error types that are retryable
	switch err.(type) {
	case *SSHConnectionError:
		return err.(*SSHConnectionError).Retryable
	case *SSHTimeoutError:
		return true
	case *CircuitBreakerError:
		return false // Circuit breaker errors should not be retried immediately
	default:
		// By default, errors are not retryable unless explicitly marked.
		return false
	}
}

// ExecuteCommand executes a command on a node with comprehensive retry logic and error handling
func (s *SSHNodeInteractor) uninstrumentedExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
	s.logger.Debug("executing command on node",
		slog.String("host", nodeHost),
		slog.String("command", command))

	result, err := s.executeWithRetry(ctx, nodeHost, command)
	if err != nil {
		s.logger.Error("command execution failed",
			slog.String("host", nodeHost),
			slog.String("command", command),
			slog.String("error", err.Error()))
		return nil, err
	}

	s.logger.Debug("command executed successfully",
		slog.String("host", nodeHost),
		slog.String("command", command),
		slog.Duration("duration", result.Duration))

	return result, nil
}

// UploadFile uploads a file to a remote node (placeholder implementation)
func (s *SSHNodeInteractor) uninstrumentedUploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error {
	s.logger.Debug("uploading file to node",
		slog.String("host", nodeHost),
		slog.String("local_path", localPath),
		slog.String("remote_path", remotePath))

	// This is a placeholder implementation
	// In a real implementation, you would use SCP or SFTP
	return NewFileOperationError(nodeHost, "upload", localPath, remotePath,
		fmt.Errorf("file upload not implemented yet"))
}

// DownloadFile downloads a file from a remote node (placeholder implementation)
func (s *SSHNodeInteractor) uninstrumentedDownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error {
	s.logger.Debug("downloading file from node",
		slog.String("host", nodeHost),
		slog.String("remote_path", remotePath),
		slog.String("local_path", localPath))

	// This is a placeholder implementation
	// In a real implementation, you would use SCP or SFTP
	return NewFileOperationError(nodeHost, "download", localPath, remotePath,
		fmt.Errorf("file download not implemented yet"))
}

// GetCircuitBreakerStats returns circuit breaker statistics for all hosts
func (s *SSHNodeInteractor) GetCircuitBreakerStats() map[string]CircuitBreakerStats {
	s.cbMutex.RLock()
	defer s.cbMutex.RUnlock()

	stats := make(map[string]CircuitBreakerStats)
	for host, cb := range s.circuitBreakers {
		stats[host] = cb.GetStats()
	}
	return stats
}

// ResetCircuitBreaker resets the circuit breaker for a specific host
func (s *SSHNodeInteractor) ResetCircuitBreaker(host string) {
	s.cbMutex.RLock()
	if cb, exists := s.circuitBreakers[host]; exists {
		cb.Reset()
		s.logger.Info("circuit breaker reset", slog.String("host", host))
	}
	s.cbMutex.RUnlock()
}

// Close closes all connections and cleans up resources
func (s *SSHNodeInteractor) Close() {
	if s.healthMonitor != nil {
		s.healthMonitor()
	}
	s.logger.Info("closing SSH node interactor")
	s.sshPool.CloseAllConnections()
}

// instrument is a generic decorator for operations that return only an error.
func (s *SSHNodeInteractor) instrument(operationType string, nodeHost string, args map[string]interface{}, operation func() error) error {
	start := time.Now()
	s.logOperationStart(operationType, nodeHost, args)

	err := operation()

	duration := time.Since(start)
	s.metricsCollector.RecordOperation(operationType, duration, err)
	s.recordOperationMetrics(operationType, start, err)

	if err != nil {
		errorArgs := make(map[string]interface{})
		for k, v := range args {
			errorArgs[k] = v
		}
		errorArgs["duration"] = duration
		return s.wrapError(err, operationType, nodeHost, errorArgs)
	}

	s.logOperationSuccess(operationType, nodeHost, duration, args)
	return nil
}

// instrumentWithResult is a generic decorator for operations that return a value and an error.
func instrumentWithResult[T any](s *SSHNodeInteractor, operationType string, nodeHost string, args map[string]interface{}, operation func() (T, error), getSuccessArgs func(T) map[string]interface{}) (T, error) {
	var result T
	start := time.Now()
	s.logOperationStart(operationType, nodeHost, args)

	result, err := operation()

	duration := time.Since(start)
	s.metricsCollector.RecordOperation(operationType, duration, err)
	s.recordOperationMetrics(operationType, start, err)

	if err != nil {
		errorArgs := make(map[string]interface{})
		for k, v := range args {
			errorArgs[k] = v
		}
		errorArgs["duration"] = duration
		return result, s.wrapError(err, operationType, nodeHost, errorArgs)
	}

	successArgs := make(map[string]interface{})
	for k, v := range args {
		successArgs[k] = v
	}
	if getSuccessArgs != nil {
		for k, v := range getSuccessArgs(result) {
			successArgs[k] = v
		}
	}
	s.logOperationSuccess(operationType, nodeHost, duration, successArgs)

	return result, nil
}

// CheckNodeHealth with metrics collection
func (s *SSHNodeInteractor) CheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error) {
	return instrumentWithResult(
		s,
		"check_health",
		nodeHost,
		map[string]interface{}{},
		func() (*NodeHealthStatus, error) {
			return s.uninstrumentedCheckNodeHealth(ctx, nodeHost)
		},
		func(result *NodeHealthStatus) map[string]interface{} {
			if result == nil {
				return nil
			}
			return map[string]interface{}{
				"is_healthy":       result.IsHealthy,
				"connected_peers":  result.ConnectedPeers,
				"system_load":      result.SystemLoad,
				"memory_usage":     result.MemoryUsage,
				"disk_usage":       result.DiskUsage,
				"wireguard_status": result.WireGuardStatus,
			}
		},
	)
}

// GetNodeSystemInfo with metrics collection
func (s *SSHNodeInteractor) GetNodeSystemInfo(ctx context.Context, nodeHost string) (*NodeSystemInfo, error) {
	return instrumentWithResult(
		s,
		"get_system_info",
		nodeHost,
		map[string]interface{}{},
		func() (*NodeSystemInfo, error) {
			return s.uninstrumentedGetNodeSystemInfo(ctx, nodeHost)
		},
		func(result *NodeSystemInfo) map[string]interface{} {
			if result == nil {
				return nil
			}
			return map[string]interface{}{
				"hostname":  result.Hostname,
				"os":        result.OS,
				"cpu_cores": result.CPUCores,
			}
		},
	)
}

// AddPeer with metrics collection
func (s *SSHNodeInteractor) AddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	return s.instrument(
		"add_peer",
		nodeHost,
		map[string]interface{}{
			"public_key":  config.PublicKey[:8] + "...",
			"allowed_ips": config.AllowedIPs,
		},
		func() error {
			return s.uninstrumentedAddPeer(ctx, nodeHost, config)
		},
	)
}

// RemovePeer with metrics collection
func (s *SSHNodeInteractor) RemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	return s.instrument(
		"remove_peer",
		nodeHost,
		map[string]interface{}{
			"public_key": publicKey[:8] + "...",
		},
		func() error {
			return s.uninstrumentedRemovePeer(ctx, nodeHost, publicKey)
		},
	)
}

// GetWireGuardStatus with metrics collection
func (s *SSHNodeInteractor) GetWireGuardStatus(ctx context.Context, nodeHost string) (*WireGuardStatus, error) {
	return instrumentWithResult(
		s,
		"get_wireguard_status",
		nodeHost,
		map[string]interface{}{},
		func() (*WireGuardStatus, error) {
			return s.uninstrumentedGetWireGuardStatus(ctx, nodeHost)
		},
		func(result *WireGuardStatus) map[string]interface{} {
			if result == nil {
				return nil
			}
			return map[string]interface{}{
				"peer_count": result.PeerCount,
				"is_running": result.IsRunning,
			}
		},
	)
}

// UpdatePeer with metrics collection
func (s *SSHNodeInteractor) UpdatePeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	return s.instrument(
		"update_peer",
		nodeHost,
		map[string]interface{}{
			"public_key":  config.PublicKey[:8] + "...",
			"allowed_ips": config.AllowedIPs,
		},
		func() error {
			return s.uninstrumentedUpdatePeer(ctx, nodeHost, config)
		},
	)
}

// SyncPeers with metrics collection
func (s *SSHNodeInteractor) SyncPeers(ctx context.Context, nodeHost string, configs []PeerWireGuardConfig) error {
	return s.instrument(
		"sync_peers",
		nodeHost,
		map[string]interface{}{
			"desired_peer_count": len(configs),
		},
		func() error {
			return s.uninstrumentedSyncPeers(ctx, nodeHost, configs)
		},
	)
}

// UpdateWireGuardConfig with metrics collection
func (s *SSHNodeInteractor) UpdateWireGuardConfig(ctx context.Context, nodeHost string, config WireGuardConfig) error {
	return s.instrument(
		"update_wireguard_config",
		nodeHost,
		map[string]interface{}{
			"interface_name": config.InterfaceName,
		},
		func() error {
			return s.uninstrumentedUpdateWireGuardConfig(ctx, nodeHost, config)
		},
	)
}

// RestartWireGuard with metrics collection
func (s *SSHNodeInteractor) RestartWireGuard(ctx context.Context, nodeHost string) error {
	return s.instrument(
		"restart_wireguard",
		nodeHost,
		map[string]interface{}{},
		func() error {
			return s.uninstrumentedRestartWireGuard(ctx, nodeHost)
		},
	)
}

// SaveWireGuardConfig with metrics collection
func (s *SSHNodeInteractor) SaveWireGuardConfig(ctx context.Context, nodeHost string) error {
	return s.instrument(
		"save_wireguard_config",
		nodeHost,
		map[string]interface{}{},
		func() error {
			return s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost)
		},
	)
}

// ListPeers with metrics collection
func (s *SSHNodeInteractor) ListPeers(ctx context.Context, nodeHost string) ([]*WireGuardPeerStatus, error) {
	return instrumentWithResult(
		s,
		"list_peers",
		nodeHost,
		map[string]interface{}{},
		func() ([]*WireGuardPeerStatus, error) {
			return s.uninstrumentedListPeers(ctx, nodeHost)
		},
		func(result []*WireGuardPeerStatus) map[string]interface{} {
			return map[string]interface{}{"peer_count": len(result)}
		},
	)
}

// ExecuteCommand with metrics collection
func (s *SSHNodeInteractor) ExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
	return instrumentWithResult(
		s,
		"execute_command",
		nodeHost,
		map[string]interface{}{"command": command},
		func() (*CommandResult, error) {
			return s.uninstrumentedExecuteCommand(ctx, nodeHost, command)
		},
		func(result *CommandResult) map[string]interface{} {
			if result == nil {
				return nil
			}
			return map[string]interface{}{"exit_code": result.ExitCode}
		},
	)
}

// UploadFile with metrics collection
func (s *SSHNodeInteractor) UploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error {
	return s.instrument(
		"upload_file",
		nodeHost,
		map[string]interface{}{
			"local_path":  localPath,
			"remote_path": remotePath,
		},
		func() error {
			return s.uninstrumentedUploadFile(ctx, nodeHost, localPath, remotePath)
		},
	)
}

// DownloadFile with metrics collection
func (s *SSHNodeInteractor) DownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error {
	return s.instrument(
		"download_file",
		nodeHost,
		map[string]interface{}{
			"remote_path": remotePath,
			"local_path":  localPath,
		},
		func() error {
			return s.uninstrumentedDownloadFile(ctx, nodeHost, remotePath, localPath)
		},
	)
}

// GetMetrics returns comprehensive metrics about NodeInteractor operations
func (s *SSHNodeInteractor) GetMetrics() NodeInteractorMetrics {
	return NodeInteractorMetrics{
		Operations:      s.metricsCollector.GetOperationMetrics(),
		Connections:     s.metricsCollector.GetConnectionMetrics(),
		CircuitBreakers: s.GetCircuitBreakerStats(),
	}
}

// --- Monitoring Helper Methods ---

// recordOperationMetrics is a helper method to record operation metrics
func (s *SSHNodeInteractor) recordOperationMetrics(operationType string, start time.Time, err error) {
	duration := time.Since(start)

	// Log operation with structured fields
	logFields := []slog.Attr{
		slog.String("operation", operationType),
		slog.Duration("duration", duration),
		slog.Bool("success", err == nil),
	}

	if err != nil {
		logFields = append(logFields, slog.String("error", err.Error()))
		logFields = append(logFields, slog.String("error_type", getErrorType(err)))
	}

	s.logger.LogAttrs(context.Background(), slog.LevelDebug, "operation completed", logFields...)
}

// wrapError wraps an error with additional context
func (s *SSHNodeInteractor) wrapError(err error, operation, host string, contextData map[string]interface{}) error {
	if err == nil {
		return nil
	}

	// Log error with full context
	logFields := []slog.Attr{
		slog.String("operation", operation),
		slog.String("host", host),
		slog.String("error", err.Error()),
		slog.String("error_type", getErrorType(err)),
	}

	// Add context fields
	for key, value := range contextData {
		logFields = append(logFields, slog.Any(key, value))
	}

	s.logger.LogAttrs(context.Background(), slog.LevelError, "operation failed", logFields...)

	return err
}

// logOperationStart logs the start of an operation
func (s *SSHNodeInteractor) logOperationStart(operation, host string, contextData map[string]interface{}) {
	logFields := []slog.Attr{
		slog.String("operation", operation),
		slog.String("host", host),
	}

	for key, value := range contextData {
		logFields = append(logFields, slog.Any(key, value))
	}

	s.logger.LogAttrs(context.Background(), slog.LevelDebug, "operation started", logFields...)
}

// logOperationSuccess logs successful operation completion
func (s *SSHNodeInteractor) logOperationSuccess(operation, host string, duration time.Duration, contextData map[string]interface{}) {
	logFields := []slog.Attr{
		slog.String("operation", operation),
		slog.String("host", host),
		slog.Duration("duration", duration),
	}

	for key, value := range contextData {
		logFields = append(logFields, slog.Any(key, value))
	}

	s.logger.LogAttrs(context.Background(), slog.LevelInfo, "operation completed successfully", logFields...)
}

// monitorConnectionHealth starts a background goroutine to monitor connection health
func (s *SSHNodeInteractor) monitorConnectionHealth(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks performs health checks on all known connections
func (s *SSHNodeInteractor) performHealthChecks(ctx context.Context) {
	// This would typically check all hosts that have been connected to
	// For now, we'll just log that health checks are being performed
	s.logger.Debug("performing connection health checks")

	// In a real implementation, you would:
	// 1. Get list of all hosts from connection pool
	// 2. Perform basic connectivity tests
	// 3. Update connection health metrics
	// 4. Close unhealthy connections
	for host := range s.sshPool.GetStats().ConnectionsByNode {
		res, err := s.CheckNodeHealth(ctx, host)
		if err != nil || !res.IsHealthy {
			s.logger.Warn("node health check failed or unhealthy",
				slog.String("host", host),
				slog.String("error", fmt.Sprintf("%v", err)))
			// Optionally close connections or take other actions

		} else {
			s.logger.Debug("node is healthy",
				slog.String("host", host))
		}
	}
}
