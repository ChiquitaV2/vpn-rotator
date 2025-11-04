package nodeinteractor

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// OperationMetrics tracks metrics for NodeInteractor operations
type OperationMetrics struct {
	TotalOperations     int64                           `json:"total_operations"`
	SuccessfulOps       int64                           `json:"successful_operations"`
	FailedOps           int64                           `json:"failed_operations"`
	AverageResponseTime time.Duration                   `json:"average_response_time"`
	LastOperation       time.Time                       `json:"last_operation"`
	OperationsByType    map[string]OperationTypeMetrics `json:"operations_by_type"`
	ErrorsByType        map[string]int64                `json:"errors_by_type"`
	mutex               sync.RWMutex
}

// OperationTypeMetrics tracks metrics for specific operation types
type OperationTypeMetrics struct {
	Count           int64         `json:"count"`
	SuccessCount    int64         `json:"success_count"`
	FailureCount    int64         `json:"failure_count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	LastExecution   time.Time     `json:"last_execution"`
}

// ConnectionHealthMetrics tracks connection health for each host
type ConnectionHealthMetrics struct {
	Host                string        `json:"host"`
	TotalConnections    int64         `json:"total_connections"`
	SuccessfulConns     int64         `json:"successful_connections"`
	FailedConns         int64         `json:"failed_connections"`
	AverageConnTime     time.Duration `json:"average_connection_time"`
	LastConnection      time.Time     `json:"last_connection"`
	LastFailure         time.Time     `json:"last_failure"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	IsHealthy           bool          `json:"is_healthy"`
}

// MetricsCollector collects and manages metrics for NodeInteractor operations
type MetricsCollector struct {
	operationMetrics  *OperationMetrics
	connectionMetrics map[string]*ConnectionHealthMetrics
	logger            *slog.Logger
	mutex             sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *slog.Logger) *MetricsCollector {
	return &MetricsCollector{
		operationMetrics: &OperationMetrics{
			OperationsByType: make(map[string]OperationTypeMetrics),
			ErrorsByType:     make(map[string]int64),
		},
		connectionMetrics: make(map[string]*ConnectionHealthMetrics),
		logger:            logger,
	}
}

// RecordOperation records metrics for an operation
func (mc *MetricsCollector) RecordOperation(operationType string, duration time.Duration, err error) {
	mc.operationMetrics.mutex.Lock()
	defer mc.operationMetrics.mutex.Unlock()

	// Update overall metrics
	mc.operationMetrics.TotalOperations++
	mc.operationMetrics.LastOperation = time.Now()

	if err == nil {
		mc.operationMetrics.SuccessfulOps++
	} else {
		mc.operationMetrics.FailedOps++

		// Record error type
		errorType := getErrorType(err)
		mc.operationMetrics.ErrorsByType[errorType]++
	}

	// Update average response time
	if mc.operationMetrics.TotalOperations == 1 {
		mc.operationMetrics.AverageResponseTime = duration
	} else {
		// Calculate running average
		totalTime := time.Duration(mc.operationMetrics.TotalOperations-1) * mc.operationMetrics.AverageResponseTime
		mc.operationMetrics.AverageResponseTime = (totalTime + duration) / time.Duration(mc.operationMetrics.TotalOperations)
	}

	// Update operation type metrics
	opMetrics := mc.operationMetrics.OperationsByType[operationType]
	opMetrics.Count++
	opMetrics.TotalDuration += duration
	opMetrics.AverageDuration = opMetrics.TotalDuration / time.Duration(opMetrics.Count)
	opMetrics.LastExecution = time.Now()

	if err == nil {
		opMetrics.SuccessCount++
	} else {
		opMetrics.FailureCount++
	}

	mc.operationMetrics.OperationsByType[operationType] = opMetrics
}

// RecordConnection records connection metrics for a host
func (mc *MetricsCollector) RecordConnection(host string, duration time.Duration, err error) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	metrics, exists := mc.connectionMetrics[host]
	if !exists {
		metrics = &ConnectionHealthMetrics{
			Host:      host,
			IsHealthy: true,
		}
		mc.connectionMetrics[host] = metrics
	}

	metrics.TotalConnections++
	metrics.LastConnection = time.Now()

	if err == nil {
		metrics.SuccessfulConns++
		metrics.ConsecutiveFailures = 0
		metrics.IsHealthy = true

		// Update average connection time
		if metrics.TotalConnections == 1 {
			metrics.AverageConnTime = duration
		} else {
			totalTime := time.Duration(metrics.TotalConnections-1) * metrics.AverageConnTime
			metrics.AverageConnTime = (totalTime + duration) / time.Duration(metrics.TotalConnections)
		}
	} else {
		metrics.FailedConns++
		metrics.LastFailure = time.Now()
		metrics.ConsecutiveFailures++

		// Mark as unhealthy after 3 consecutive failures
		if metrics.ConsecutiveFailures >= 3 {
			metrics.IsHealthy = false
		}
	}
}

// GetOperationMetrics returns current operation metrics
func (mc *MetricsCollector) GetOperationMetrics() OperationMetrics {
	mc.operationMetrics.mutex.RLock()
	defer mc.operationMetrics.mutex.RUnlock()

	// Create a copy to avoid race conditions
	metrics := *mc.operationMetrics
	metrics.OperationsByType = make(map[string]OperationTypeMetrics)
	metrics.ErrorsByType = make(map[string]int64)

	for k, v := range mc.operationMetrics.OperationsByType {
		metrics.OperationsByType[k] = v
	}
	for k, v := range mc.operationMetrics.ErrorsByType {
		metrics.ErrorsByType[k] = v
	}

	return metrics
}

// GetConnectionMetrics returns connection metrics for all hosts
func (mc *MetricsCollector) GetConnectionMetrics() map[string]ConnectionHealthMetrics {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	metrics := make(map[string]ConnectionHealthMetrics)
	for host, connMetrics := range mc.connectionMetrics {
		metrics[host] = *connMetrics
	}

	return metrics
}

// GetHostHealth returns health status for a specific host
func (mc *MetricsCollector) GetHostHealth(host string) *ConnectionHealthMetrics {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	if metrics, exists := mc.connectionMetrics[host]; exists {
		// Return a copy
		result := *metrics
		return &result
	}

	return nil
}

// ResetMetrics resets all metrics
func (mc *MetricsCollector) ResetMetrics() {
	mc.operationMetrics.mutex.Lock()
	mc.mutex.Lock()
	defer mc.operationMetrics.mutex.Unlock()
	defer mc.mutex.Unlock()

	mc.operationMetrics = &OperationMetrics{
		OperationsByType: make(map[string]OperationTypeMetrics),
		ErrorsByType:     make(map[string]int64),
	}
	mc.connectionMetrics = make(map[string]*ConnectionHealthMetrics)

	mc.logger.Info("metrics reset")
}

// getErrorType extracts error type from error for categorization
func getErrorType(err error) string {
	if err == nil {
		return "none"
	}

	switch err.(type) {
	case *SSHConnectionError:
		return "ssh_connection"
	case *SSHTimeoutError:
		return "ssh_timeout"
	case *SSHCommandError:
		return "ssh_command"
	case *WireGuardOperationError:
		return "wireguard_operation"
	case *WireGuardConfigError:
		return "wireguard_config"
	case *HealthCheckError:
		return "health_check"
	case *SystemInfoError:
		return "system_info"
	case *FileOperationError:
		return "file_operation"
	case *CircuitBreakerError:
		return "circuit_breaker"
	case *ValidationError:
		return "validation"
	case *PeerNotFoundError:
		return "peer_not_found"
	case *PeerAlreadyExistsError:
		return "peer_already_exists"
	case *IPConflictError:
		return "ip_conflict"
	default:
		return "unknown"
	}
}

// Enhanced SSHNodeInteractor with monitoring integration

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

// recordConnectionMetrics is a helper method to record connection metrics
func (s *SSHNodeInteractor) recordConnectionMetrics(host string, start time.Time, err error) {
	duration := time.Since(start)

	logFields := []slog.Attr{
		slog.String("host", host),
		slog.Duration("connection_time", duration),
		slog.Bool("success", err == nil),
	}

	if err != nil {
		logFields = append(logFields, slog.String("error", err.Error()))
	}

	s.logger.LogAttrs(context.Background(), slog.LevelDebug, "connection attempt", logFields...)
}

// Enhanced error handling with context

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

// Connection health monitoring

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
}

// Automatic recovery mechanisms

// attemptRecovery attempts to recover from connection failures
func (s *SSHNodeInteractor) attemptRecovery(host string, err error) error {
	s.logger.Info("attempting connection recovery",
		slog.String("host", host),
		slog.String("error", err.Error()))

	// Close any existing connections to the host
	s.sshPool.CloseConnection(host)

	// Reset circuit breaker if it exists
	s.ResetCircuitBreaker(host)

	// Wait a moment before allowing new connections
	time.Sleep(1 * time.Second)

	s.logger.Info("recovery attempt completed", slog.String("host", host))
	return nil
}
