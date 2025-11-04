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
	sshPool         *ssh.Pool
	config          NodeInteractorConfig
	logger          *slog.Logger
	circuitBreakers map[string]*CircuitBreaker
	cbMutex         sync.RWMutex
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

	return &SSHNodeInteractor{
		sshPool:         sshPool,
		config:          config,
		logger:          logger,
		circuitBreakers: make(map[string]*CircuitBreaker),
	}, nil
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
		// For other errors, check the error message
		return s.sshPool.ExecuteCommand != nil // This is a placeholder - the actual pool has retry logic
	}
}

// ExecuteCommand executes a command on a node with comprehensive retry logic and error handling
func (s *SSHNodeInteractor) ExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error) {
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
func (s *SSHNodeInteractor) UploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error {
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
func (s *SSHNodeInteractor) DownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error {
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
	s.logger.Info("closing SSH node interactor")
	s.sshPool.CloseAllConnections()
}
