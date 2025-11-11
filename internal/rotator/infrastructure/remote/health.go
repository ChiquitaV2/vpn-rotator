package remote

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
)

// CheckNodeHealth performs comprehensive health checking with system metrics collection
func (s *SSHNodeInteractor) uninstrumentedCheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error) {
	op := s.logger.StartOp(ctx, "CheckNodeHealth", "host", nodeHost)

	start := time.Now()
	status := &NodeHealthStatus{
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
	if err := s.checkWireGuardHealth(ctx, nodeHost, status); err != nil {
		status.IsHealthy = false
		status.Errors = append(status.Errors, fmt.Sprintf("wireguard: %v", err))
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
func (s *SSHNodeInteractor) executeHealthCheck(ctx context.Context, nodeHost string, healthCmd HealthCommand, status *NodeHealthStatus) error {
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
func (s *SSHNodeInteractor) collectSystemMetrics(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
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
func (s *SSHNodeInteractor) collectSystemLoad(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
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
func (s *SSHNodeInteractor) collectMemoryUsage(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
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
func (s *SSHNodeInteractor) collectDiskUsage(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, s.config.SystemMetricsPath.DiskUsage)
	if err != nil {
		return err
	}

	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		if strings.Contains(line, " / ") || strings.HasSuffix(line, " /") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				usageStr := strings.TrimSuffix(parts[4], "%")
				if usage, err := strconv.ParseFloat(usageStr, 64); err == nil {
					status.DiskUsage = usage
				}
			}
			break
		}
	}

	return nil
}

// checkWireGuardHealth checks WireGuard service health and peer count
func (s *SSHNodeInteractor) checkWireGuardHealth(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s", s.config.WireGuardInterface))
	if err != nil {
		status.WireGuardStatus = "down"
		return errors.WrapWithDomain(err, errors.DomainInfrastructure, errors.ErrCodeHealthCheckFailed, "failed to check WireGuard status", true).
			WithMetadata("host", nodeHost).
			WithMetadata("interface", s.config.WireGuardInterface)
	}

	status.WireGuardStatus = "up"

	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to parse WireGuard peers for health check", "host", nodeHost, "error", err)
	} else {
		status.ConnectedPeers = len(peers)
	}

	return nil
}

// GetNodeSystemInfo retrieves detailed system information
func (s *SSHNodeInteractor) uninstrumentedGetNodeSystemInfo(ctx context.Context, nodeHost string) (*NodeSystemInfo, error) {
	op := s.logger.StartOp(ctx, "GetNodeSystemInfo", "host", nodeHost)

	info := &NodeSystemInfo{
		NetworkInterfaces: []NetworkInterface{},
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.Hostname)); err == nil {
		info.Hostname = strings.TrimSpace(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.OSRelease)); err == nil {
		info.OS = s.parseOSInfo(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, "uname -r"); err == nil {
		info.Kernel = strings.TrimSpace(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.Uptime)); err == nil {
		info.Uptime = s.parseUptime(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, "grep -c ^processor /proc/cpuinfo"); err == nil {
		if cores, err := strconv.Atoi(strings.TrimSpace(result.Stdout)); err == nil {
			info.CPUCores = cores
		}
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.MemInfo)); err == nil {
		info.TotalMemory = s.parseTotalMemory(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, s.config.SystemMetricsPath.DiskUsage); err == nil {
		info.TotalDisk = s.parseTotalDisk(result.Stdout)
	}

	if result, err := s.executeCommandOnce(ctx, nodeHost, "ip addr show"); err == nil {
		info.NetworkInterfaces = s.parseNetworkInterfaces(result.Stdout)
	}

	op.Complete("system info retrieved")
	return info, nil
}

// parseOSInfo parses OS information from /etc/os-release
func (s *SSHNodeInteractor) parseOSInfo(osRelease string) string {
	lines := strings.Split(osRelease, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name := strings.TrimPrefix(line, "PRETTY_NAME=")
			name = strings.Trim(name, "\"")
			return name
		}
	}
	return "Unknown"
}

// parseUptime parses uptime from /proc/uptime
func (s *SSHNodeInteractor) parseUptime(uptime string) time.Duration {
	parts := strings.Fields(uptime)
	if len(parts) >= 1 {
		if seconds, err := strconv.ParseFloat(parts[0], 64); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

// parseTotalMemory parses total memory from /proc/meminfo
func (s *SSHNodeInteractor) parseTotalMemory(meminfo string) int64 {
	lines := strings.Split(meminfo, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					return total * 1024 // Convert from KB to bytes
				}
			}
		}
	}
	return 0
}

// parseTotalDisk parses total disk space from df output
func (s *SSHNodeInteractor) parseTotalDisk(dfOutput string) int64 {
	lines := strings.Split(dfOutput, "\n")
	for _, line := range lines {
		if strings.Contains(line, " / ") || strings.HasSuffix(line, " /") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					return total * 1024 // Convert from KB to bytes
				}
			}
			break
		}
	}
	return 0
}

// parseNetworkInterfaces parses network interfaces from ip addr show output
func (s *SSHNodeInteractor) parseNetworkInterfaces(ipOutput string) []NetworkInterface {
	var interfaces []NetworkInterface
	lines := strings.Split(ipOutput, "\n")

	var currentInterface *NetworkInterface

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, ":") && len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			if currentInterface != nil {
				interfaces = append(interfaces, *currentInterface)
			}

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], ":")
				status := "down"
				if strings.Contains(line, "UP") {
					status = "up"
				}

				currentInterface = &NetworkInterface{
					Name:   name,
					Status: status,
				}
			}
		} else if currentInterface != nil && strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ip := strings.Split(parts[1], "/")[0]
				currentInterface.IPAddress = ip
			}
		}
	}

	if currentInterface != nil {
		interfaces = append(interfaces, *currentInterface)
	}

	return interfaces
}
