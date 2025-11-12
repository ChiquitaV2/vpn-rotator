package remote

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/remote/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// SSHNodeInteractor implements NodeInteractor using SSH connections with pooling
type SSHNodeInteractor struct {
	sshPool          *ssh.Pool
	config           NodeInteractorConfig
	logger           *logger.Logger
	circuitBreakers  map[string]*CircuitBreaker
	cbMutex          sync.RWMutex
	metricsCollector *MetricsCollector
	healthMonitor    context.CancelFunc
}

// NewSSHNodeInteractor creates a new SSH-based NodeInteractor
func NewSSHNodeInteractor(sshPrivateKey string, config NodeInteractorConfig, log *logger.Logger) (*SSHNodeInteractor, error) {
	if err := ValidateConfig(config); err != nil {
		return nil, errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeConfiguration, "invalid node interactor configuration", false)
	}

	sshPoolLogger := log.WithComponent("ssh.pool")
	sshPool := ssh.NewPool(sshPrivateKey, sshPoolLogger, 5*time.Minute, gossh.InsecureIgnoreHostKey())
	if sshPool == nil {
		return nil, errors.NewInfrastructureError(errors.ErrCodeConfiguration, "failed to create SSH connection pool", false, nil)
	}

	metricsCollector := NewMetricsCollector(log)

	interactor := &SSHNodeInteractor{
		sshPool:          sshPool,
		config:           config,
		logger:           log.WithComponent("node.interactor"),
		circuitBreakers:  make(map[string]*CircuitBreaker),
		metricsCollector: metricsCollector,
	}

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

		if !errors.IsRetryable(err) {
			s.logger.DebugContext(ctx, "non-retryable error encountered", "host", host, "command", command, "error", err)
			break
		}

		if attempt < s.config.RetryAttempts {
			backoff := time.Duration(attempt) * s.config.RetryBackoff
			s.logger.DebugContext(ctx, "command failed, retrying", "host", host, "command", command, "attempt", attempt, "backoff", backoff, "error", err)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil, errors.WrapWithDomain(lastErr, errors.DomainInfrastructure, errors.ErrCodeSSHCommand, fmt.Sprintf("command failed after %d attempts", s.config.RetryAttempts), false).
		WithMetadata("host", host).
		WithMetadata("command", command)
}

// executeCommandOnce executes a command once without retry logic
func (s *SSHNodeInteractor) executeCommandOnce(ctx context.Context, host, command string) (*CommandResult, error) {
	start := time.Now()

	cmdCtx, cancel := context.WithTimeout(ctx, s.config.CommandTimeout)
	defer cancel()

	output, err := s.sshPool.ExecuteCommand(cmdCtx, host, command)
	duration := time.Since(start)

	if err != nil {
		return &CommandResult{
				ExitCode: 1,
				Stderr:   err.Error(),
				Duration: duration,
			}, errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeSSHCommand, "ssh command execution failed", true).
				WithMetadata("host", host).
				WithMetadata("command", command)
	}

	return &CommandResult{
		ExitCode: 0,
		Stdout:   output,
		Duration: duration,
	}, nil
}

// isRetryableError determines if an error is worth retrying
func (s *SSHNodeInteractor) isRetryableError(err error) bool {
	return errors.IsRetryable(err)
}

// ExecuteCommand executes a command on a node with comprehensive retry logic and error handling
func (s *SSHNodeInteractor) uninstrumentedExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
	op := s.logger.StartOp(ctx, "ExecuteCommand", "host", nodeHost, "command", command)

	result, err := s.executeWithRetry(ctx, nodeHost, command)
	if err != nil {
		op.Fail(err, "command execution failed")
		return nil, err
	}

	op.Complete("command executed successfully", "duration", result.Duration)
	return result, nil
}

// UploadFile uploads a file to a remote node (placeholder implementation)
func (s *SSHNodeInteractor) uninstrumentedUploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error {
	return errors.NewSystemError("not_implemented", "file upload not implemented yet", false, nil)
}

// DownloadFile downloads a file from a remote node (placeholder implementation)
func (s *SSHNodeInteractor) uninstrumentedDownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error {
	return errors.NewSystemError("not_implemented", "file download not implemented yet", false, nil)
}

// ResetCircuitBreaker resets the circuit breaker for a specific host
func (s *SSHNodeInteractor) ResetCircuitBreaker(host string) {
	s.cbMutex.RLock()
	if cb, exists := s.circuitBreakers[host]; exists {
		cb.Reset()
		s.logger.Debug("circuit breaker reset", "host", host)
	}
	s.cbMutex.RUnlock()
}

// Close closes all connections and cleans up resources
func (s *SSHNodeInteractor) Close() {
	if s.healthMonitor != nil {
		s.healthMonitor()
	}
	s.logger.Debug("closing SSH node interactor")
	s.sshPool.CloseAllConnections(context.Background())
}

// instrument is a generic decorator for operations that return only an error.
func (s *SSHNodeInteractor) instrument(ctx context.Context, operationType string, nodeHost string, args []any, operation func() error) error {
	opArgs := append([]any{"host", nodeHost}, args...)
	op := s.logger.StartOp(ctx, operationType, opArgs...)

	err := operation()

	duration := time.Since(op.StartTime)
	s.metricsCollector.RecordOperation(operationType, duration, err)

	if err != nil {
		op.Fail(err, "operation failed")
		return err
	}

	op.Complete("operation completed successfully")
	return nil
}

// instrumentWithResult is a generic decorator for operations that return a value and an error.
func instrumentWithResult[T any](s *SSHNodeInteractor, ctx context.Context, operationType string, nodeHost string, args []any, operation func() (T, error), getSuccessArgs func(T) []any) (T, error) {
	var result T
	opArgs := append([]any{"host", nodeHost}, args...)
	op := s.logger.StartOp(ctx, operationType, opArgs...)

	result, err := operation()

	duration := time.Since(op.StartTime)
	s.metricsCollector.RecordOperation(operationType, duration, err)

	if err != nil {
		op.Fail(err, "operation failed")
		return result, err
	}

	var successArgs []any
	if getSuccessArgs != nil {
		successArgs = getSuccessArgs(result)
	}
	op.Complete("operation completed successfully", successArgs...)

	return result, nil
}

// CheckNodeHealth with metrics collection
func (s *SSHNodeInteractor) CheckNodeHealth(ctx context.Context, nodeHost string) (*node.NodeHealthStatus, error) {
	return instrumentWithResult(
		s,
		ctx,
		"check_health",
		nodeHost,
		nil,
		func() (*node.NodeHealthStatus, error) {
			return s.uninstrumentedCheckNodeHealth(ctx, nodeHost)
		},
		func(result *node.NodeHealthStatus) []any {
			if result == nil {
				return nil
			}
			return []any{
				"is_healthy", result.IsHealthy,
				"connected_peers", result.ConnectedPeers,
				"system_load", result.SystemLoad,
				"memory_usage", result.MemoryUsage,
				"disk_usage", result.DiskUsage,
				"wireguard_status", result.WireGuardStatus,
			}
		},
	)
}

// GetNodeSystemInfo with metrics collection
func (s *SSHNodeInteractor) GetNodeSystemInfo(ctx context.Context, nodeHost string) (*node.NodeSystemInfo, error) {
	return instrumentWithResult(
		s,
		ctx,
		"get_system_info",
		nodeHost,
		nil,
		func() (*node.NodeSystemInfo, error) {
			// return s.uninstrumentedGetNodeSystemInfo(ctx, nodeHost)
			return nil, nil
		},
		func(result *node.NodeSystemInfo) []any {
			if result == nil {
				return nil
			}
			return []any{
				"hostname", result.Hostname,
				"os", result.OS,
				"cpu_cores", result.CPUCores,
			}
		},
	)
}

// AddPeer with metrics collection
func (s *SSHNodeInteractor) AddPeer(ctx context.Context, nodeHost string, config node.PeerWireGuardConfig) error {
	return s.instrument(
		ctx,
		"add_peer",
		nodeHost,
		[]any{
			"public_key", config.PublicKey[:8] + "...",
			"allowed_ips", config.AllowedIPs,
		},
		func() error {
			return s.uninstrumentedAddPeer(ctx, nodeHost, config)
		},
	)
}

// RemovePeer with metrics collection
func (s *SSHNodeInteractor) RemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	return s.instrument(
		ctx,
		"remove_peer",
		nodeHost,
		[]any{
			"public_key", publicKey[:8] + "...",
		},
		func() error {
			return s.uninstrumentedRemovePeer(ctx, nodeHost, publicKey)
		},
	)
}

// GetWireGuardStatus with metrics collection
func (s *SSHNodeInteractor) GetWireGuardStatus(ctx context.Context, nodeHost string) (*node.WireGuardStatus, error) {
	return instrumentWithResult(
		s,
		ctx,
		"get_wireguard_status",
		nodeHost,
		nil,
		func() (*node.WireGuardStatus, error) {
			return s.uninstrumentedGetWireGuardStatus(ctx, nodeHost)
		},
		func(result *node.WireGuardStatus) []any {
			if result == nil {
				return nil
			}
			return []any{
				"peer_count", result.PeerCount,
				"is_running", result.IsRunning,
			}
		},
	)
}

// UpdatePeer with metrics collection
func (s *SSHNodeInteractor) UpdatePeer(ctx context.Context, nodeHost string, config node.PeerWireGuardConfig) error {
	return s.instrument(
		ctx,
		"update_peer",
		nodeHost,
		[]any{
			"public_key", config.PublicKey[:8] + "...",
			"allowed_ips", config.AllowedIPs,
		},
		func() error {
			return s.uninstrumentedUpdatePeer(ctx, nodeHost, config)
		},
	)
}

// SyncPeers with metrics collection
func (s *SSHNodeInteractor) SyncPeers(ctx context.Context, nodeHost string, configs []node.PeerWireGuardConfig) error {
	return s.instrument(
		ctx,
		"sync_peers",
		nodeHost,
		[]any{
			"desired_peer_count", len(configs),
		},
		func() error {
			return s.uninstrumentedSyncPeers(ctx, nodeHost, configs)
		},
	)
}

// UpdateWireGuardConfig with metrics collection
func (s *SSHNodeInteractor) UpdateWireGuardConfig(ctx context.Context, nodeHost string, config node.WireGuardConfig) error {
	return s.instrument(
		ctx,
		"update_wireguard_config",
		nodeHost,
		[]any{
			"interface_name", config.InterfaceName,
		},
		func() error {
			return s.uninstrumentedUpdateWireGuardConfig(ctx, nodeHost, config)
		},
	)
}

// RestartWireGuard with metrics collection
func (s *SSHNodeInteractor) RestartWireGuard(ctx context.Context, nodeHost string) error {
	return s.instrument(
		ctx,
		"restart_wireguard",
		nodeHost,
		nil,
		func() error {
			return s.uninstrumentedRestartWireGuard(ctx, nodeHost)
		},
	)
}

// SaveWireGuardConfig with metrics collection
func (s *SSHNodeInteractor) SaveWireGuardConfig(ctx context.Context, nodeHost string) error {
	return s.instrument(
		ctx,
		"save_wireguard_config",
		nodeHost,
		nil,
		func() error {
			return s.uninstrumentedSaveWireGuardConfig(ctx, nodeHost)
		},
	)
}

// ListPeers with metrics collection
func (s *SSHNodeInteractor) ListPeers(ctx context.Context, nodeHost string) ([]*node.WireGuardPeerStatus, error) {
	return instrumentWithResult(
		s,
		ctx,
		"list_peers",
		nodeHost,
		nil,
		func() ([]*node.WireGuardPeerStatus, error) {
			return s.uninstrumentedListPeers(ctx, nodeHost)
		},
		func(result []*node.WireGuardPeerStatus) []any {
			return []any{"peer_count", len(result)}
		},
	)
}

// ExecuteCommand with metrics collection
func (s *SSHNodeInteractor) ExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
	return instrumentWithResult(
		s,
		ctx,
		"execute_command",
		nodeHost,
		[]any{"command", command},
		func() (*CommandResult, error) {
			return s.uninstrumentedExecuteCommand(ctx, nodeHost, command)
		},
		func(result *CommandResult) []any {
			if result == nil {
				return nil
			}
			return []any{"exit_code", result.ExitCode}
		},
	)
}

// UploadFile with metrics collection
func (s *SSHNodeInteractor) UploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error {
	return s.instrument(
		ctx,
		"upload_file",
		nodeHost,
		[]any{
			"local_path", localPath,
			"remote_path", remotePath,
		},
		func() error {
			return s.uninstrumentedUploadFile(ctx, nodeHost, localPath, remotePath)
		},
	)
}

// DownloadFile with metrics collection
func (s *SSHNodeInteractor) DownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error {
	return s.instrument(
		ctx,
		"download_file",
		nodeHost,
		[]any{
			"remote_path", remotePath,
			"local_path", localPath,
		},
		func() error {
			return s.uninstrumentedDownloadFile(ctx, nodeHost, remotePath, localPath)
		},
	)
}

// GetMetrics returns comprehensive metrics about NodeInteractor operations
func (s *SSHNodeInteractor) GetMetrics() NodeInteractorMetrics {
	return NodeInteractorMetrics{
		Operations:  s.metricsCollector.GetOperationMetrics(),
		Connections: s.metricsCollector.GetConnectionMetrics(),
		// CircuitBreakers: s.GetCircuitBreakerStats(),
	}
}

// --- Monitoring Helper Methods ---

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

func (s *SSHNodeInteractor) performHealthChecks(ctx context.Context) {
	s.logger.DebugContext(ctx, "performing connection health checks")

	for host := range s.sshPool.GetStats().ConnectionsByNode {
		res, err := s.CheckNodeHealth(ctx, host)
		if err != nil || !res.IsHealthy {
			s.logger.WarnContext(ctx, "node health check failed or unhealthy", "host", host, "error", fmt.Sprintf("%v", err))
		} else {
			s.logger.DebugContext(ctx, "node is healthy", "host", host)
		}
	}
}
