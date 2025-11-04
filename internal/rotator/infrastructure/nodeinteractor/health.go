package nodeinteractor

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// CheckNodeHealth performs comprehensive health checking with system metrics collection
func (s *SSHNodeInteractor) CheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error) {
	s.logger.Debug("checking node health", slog.String("host", nodeHost))

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
		s.logger.Warn("failed to collect system metrics",
			slog.String("host", nodeHost),
			slog.String("error", err.Error()))
		status.Errors = append(status.Errors, fmt.Sprintf("system_metrics: %v", err))
	}

	// Check WireGuard status
	if err := s.checkWireGuardHealth(ctx, nodeHost, status); err != nil {
		status.IsHealthy = false
		status.Errors = append(status.Errors, fmt.Sprintf("wireguard: %v", err))
	}

	status.ResponseTime = time.Since(start)

	s.logger.Debug("node health check completed",
		slog.String("host", nodeHost),
		slog.Bool("healthy", status.IsHealthy),
		slog.Duration("response_time", status.ResponseTime),
		slog.Int("error_count", len(status.Errors)))

	return status, nil
}

// executeHealthCheck executes a single health check command
func (s *SSHNodeInteractor) executeHealthCheck(ctx context.Context, nodeHost string, healthCmd HealthCommand, status *NodeHealthStatus) error {
	// Create timeout context for this specific health check
	checkCtx, cancel := context.WithTimeout(ctx, healthCmd.Timeout)
	defer cancel()

	result, err := s.executeCommandOnce(checkCtx, nodeHost, healthCmd.Command)
	if err != nil {
		s.sshPool.CloseConnection(nodeHost)
		return NewHealthCheckError(nodeHost, healthCmd.Name, healthCmd.Command, healthCmd.Timeout, healthCmd.Critical, err)
	}

	if result.ExitCode != healthCmd.ExpectedExit {
		s.sshPool.CloseConnection(nodeHost)
		return NewHealthCheckError(nodeHost, healthCmd.Name, healthCmd.Command, healthCmd.Timeout, healthCmd.Critical,
			fmt.Errorf("unexpected exit code %d, expected %d", result.ExitCode, healthCmd.ExpectedExit))
	}

	return nil
}

// collectSystemMetrics collects system metrics from the node
func (s *SSHNodeInteractor) collectSystemMetrics(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
	// Collect system load
	if err := s.collectSystemLoad(ctx, nodeHost, status); err != nil {
		s.logger.Debug("failed to collect system load", slog.String("host", nodeHost), slog.String("error", err.Error()))
	}

	// Collect memory usage
	if err := s.collectMemoryUsage(ctx, nodeHost, status); err != nil {
		s.logger.Debug("failed to collect memory usage", slog.String("host", nodeHost), slog.String("error", err.Error()))
	}

	// Collect disk usage
	if err := s.collectDiskUsage(ctx, nodeHost, status); err != nil {
		s.logger.Debug("failed to collect disk usage", slog.String("host", nodeHost), slog.String("error", err.Error()))
	}

	return nil
}

// collectSystemLoad collects system load average
func (s *SSHNodeInteractor) collectSystemLoad(ctx context.Context, nodeHost string, status *NodeHealthStatus) error {
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.LoadAvg))
	if err != nil {
		return err
	}

	// Parse load average (format: "0.15 0.25 0.30 1/123 456")
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

	// Parse memory info to calculate usage percentage
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

	// Parse df output to get root filesystem usage
	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		if strings.Contains(line, " / ") || strings.HasSuffix(line, " /") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				// Usage percentage is typically in the 5th column (0-indexed: 4)
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
	// Check if WireGuard interface is up and running
	result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("wg show %s", s.config.WireGuardInterface))
	if err != nil {
		status.WireGuardStatus = "down"
		return NewWireGuardOperationError(nodeHost, "status_check", s.config.WireGuardInterface, "", err)
	}

	status.WireGuardStatus = "up"

	// Count connected peers
	peers, err := s.parseWireGuardPeers(result.Stdout)
	if err != nil {
		s.logger.Warn("failed to parse WireGuard peers for health check",
			slog.String("host", nodeHost),
			slog.String("error", err.Error()))
	} else {
		status.ConnectedPeers = len(peers)
	}

	return nil
}

// GetNodeSystemInfo retrieves detailed system information
func (s *SSHNodeInteractor) GetNodeSystemInfo(ctx context.Context, nodeHost string) (*NodeSystemInfo, error) {
	s.logger.Debug("getting node system info", slog.String("host", nodeHost))

	info := &NodeSystemInfo{
		NetworkInterfaces: []NetworkInterface{},
	}

	// Get hostname
	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.Hostname)); err == nil {
		info.Hostname = strings.TrimSpace(result.Stdout)
	}

	// Get OS information
	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.OSRelease)); err == nil {
		info.OS = s.parseOSInfo(result.Stdout)
	}

	// Get kernel version
	if result, err := s.executeCommandOnce(ctx, nodeHost, "uname -r"); err == nil {
		info.Kernel = strings.TrimSpace(result.Stdout)
	}

	// Get uptime
	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.Uptime)); err == nil {
		info.Uptime = s.parseUptime(result.Stdout)
	}

	// Get CPU cores
	if result, err := s.executeCommandOnce(ctx, nodeHost, "nproc"); err == nil {
		if cores, err := strconv.Atoi(strings.TrimSpace(result.Stdout)); err == nil {
			info.CPUCores = cores
		}
	}

	// Get total memory
	if result, err := s.executeCommandOnce(ctx, nodeHost, fmt.Sprintf("cat %s", s.config.SystemMetricsPath.MemInfo)); err == nil {
		info.TotalMemory = s.parseTotalMemory(result.Stdout)
	}

	// Get total disk space
	if result, err := s.executeCommandOnce(ctx, nodeHost, s.config.SystemMetricsPath.DiskUsage); err == nil {
		info.TotalDisk = s.parseTotalDisk(result.Stdout)
	}

	// Get network interfaces
	if result, err := s.executeCommandOnce(ctx, nodeHost, "ip addr show"); err == nil {
		info.NetworkInterfaces = s.parseNetworkInterfaces(result.Stdout)
	}

	s.logger.Debug("node system info retrieved",
		slog.String("host", nodeHost),
		slog.String("hostname", info.Hostname),
		slog.String("os", info.OS),
		slog.Int("cpu_cores", info.CPUCores))

	return info, nil
}

// parseOSInfo parses OS information from /etc/os-release
func (s *SSHNodeInteractor) parseOSInfo(osRelease string) string {
	lines := strings.Split(osRelease, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			// Remove PRETTY_NAME= and quotes
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
				// Total size is typically in the 2nd column (0-indexed: 1)
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

		// Interface line starts with a number
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
			// IP address line
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ip := strings.Split(parts[1], "/")[0] // Remove CIDR notation
				currentInterface.IPAddress = ip
			}
		}
	}

	if currentInterface != nil {
		interfaces = append(interfaces, *currentInterface)
	}

	return interfaces
}
