package remote

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/pkg/errors"
)

// CheckNodeHealth performs comprehensive health checking with system metrics collection
func (s *SSHNodeInteractor) uninstrumentedCheckNodeHealth(ctx context.Context, nodeHost string) (*node.NodeHealthStatus, error) {
	op := s.logger.StartOp(ctx, "CheckNodeHealth", "host", nodeHost)

	start := time.Now()
	status := &node.NodeHealthStatus{
		LastChecked: start,
		IsHealthy:   true,
		Errors:      []string{},
	}

	// Execute all health check commands
	for _, healthCmd := range s.config.HealthCheckCommands {
		if err := s.executeHealthCheck(ctx, nodeHost, healthCmd, status); err != nil {
			if healthCmd.Critical {
				status.IsHealthy = false
			}
			status.Errors = append(status.Errors, fmt.Sprintf("%s: %v", healthCmd.Name, err))
		}
	}

	// Collect system metrics
	if err := s.collectSystemMetrics(ctx, nodeHost, status); err != nil {
		s.logger.WarnContext(ctx, "failed to collect system metrics", "host", nodeHost, "error", err)
		status.Errors = append(status.Errors, fmt.Sprintf("system_metrics: %v", err))
	}

	// Check WireGuard status
	wgStatus, err := s.uninstrumentedGetWireGuardStatus(ctx, nodeHost)
	if err != nil {
		status.IsHealthy = false
		status.Errors = append(status.Errors, fmt.Sprintf("wireguard: %v", err))
	} else if !wgStatus.IsRunning {
		status.IsHealthy = false
		status.Errors = append(status.Errors, "wireguard: not running")
	}

	status.ResponseTime = time.Since(start)

	if !status.IsHealthy {
		op.Fail(fmt.Errorf("node is unhealthy"), "health check completed with errors", "error_count", len(status.Errors))
	} else {
		op.Complete("health check completed successfully")
	}

	return status, nil
}

// executeHealthCheck executes a single health check command
func (s *SSHNodeInteractor) executeHealthCheck(ctx context.Context, nodeHost string, healthCmd HealthCommand, status *node.NodeHealthStatus) error {
	checkCtx, cancel := context.WithTimeout(ctx, healthCmd.Timeout)
	defer cancel()

	result, err := s.executeCommandOnce(checkCtx, nodeHost, healthCmd.Command)
	if err != nil {
		s.sshPool.CloseConnection(ctx, nodeHost)
		return errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeHealthCheckFailed, "health check command failed", true).
			WithMetadata("check_name", healthCmd.Name).
			WithMetadata("command", healthCmd.Command).
			WithMetadata("critical", healthCmd.Critical)
	}

	if result.ExitCode != healthCmd.ExpectedExit {
		s.sshPool.CloseConnection(ctx, nodeHost)
		return errors.NewInfrastructureError(errors.ErrCodeHealthCheckFailed, "unexpected exit code", true, nil).
			WithMetadata("check_name", healthCmd.Name).
			WithMetadata("command", healthCmd.Command).
			WithMetadata("critical", healthCmd.Critical).
			WithMetadata("exit_code", result.ExitCode).
			WithMetadata("expected_exit_code", healthCmd.ExpectedExit)
	}

	return nil
}

// collectSystemMetrics collects system metrics from the node
func (s *SSHNodeInteractor) collectSystemMetrics(ctx context.Context, nodeHost string, status *node.NodeHealthStatus) error {
	if err := s.collectSystemLoad(ctx, nodeHost, status); err != nil {
		s.logger.DebugContext(ctx, "failed to collect system load", "host", nodeHost, "error", err)
	}

	if err := s.collectMemoryUsage(ctx, nodeHost, status); err != nil {
		s.logger.DebugContext(ctx, "failed to collect memory usage", "host", nodeHost, "error", err)
	}

	if err := s.collectDiskUsage(ctx, nodeHost, status); err != nil {
		s.logger.DebugContext(ctx, "failed to collect disk usage", "host", nodeHost, "error", err)
	}

	return nil
}

// collectSystemLoad collects system load average
func (s *SSHNodeInteractor) collectSystemLoad(ctx context.Context, nodeHost string, status *node.NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.LoadAvg))
	if err != nil {
		return err
	}

	parts := strings.Fields(result.Stdout)
	if len(parts) >= 1 {
		if load, err := strconv.ParseFloat(parts[0], 64); err == nil {
			status.SystemLoad = load
		}
	}

	return nil
}

// collectMemoryUsage collects memory usage information
func (s *SSHNodeInteractor) collectMemoryUsage(ctx context.Context, nodeHost string, status *node.NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.MemInfo))
	if err != nil {
		return err
	}

	var memTotal, memAvailable int64
	lines := strings.Split(result.Stdout, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					memTotal = total
				}
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if available, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					memAvailable = available
				}
			}
		}
	}

	if memTotal > 0 {
		memUsed := memTotal - memAvailable
		status.MemoryUsage = float64(memUsed) / float64(memTotal) * 100
	}

	return nil
}

// collectDiskUsage collects disk usage information
func (s *SSHNodeInteractor) collectDiskUsage(ctx context.Context, nodeHost string, status *node.NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, s.config.SystemMetricsPath.DiskUsage)
	if err != nil {
		return err
	}

	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "/") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				usageStr := strings.TrimSuffix(parts[4], "%")
				if usage, err := strconv.ParseFloat(usageStr, 64); err == nil {
					status.DiskUsage = usage
					break
				}
			}
		}
	}

	return nil
}
