package nodeinteractor

import (
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// OperationMetrics tracks metrics for NodeInteractor operations
type OperationMetrics struct {
	TotalOperations     int64                           `json:"total_operations"`
	SuccessfulOps       int64                           `json:"successful_operations"`
	FailedOps           int64                           `json:"failed_operations"`
	TotalDuration       time.Duration                   `json:"total_duration"`
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
	logger            *logger.Logger
	mutex             sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(log *logger.Logger) *MetricsCollector {
	return &MetricsCollector{
		operationMetrics: &OperationMetrics{
			OperationsByType: make(map[string]OperationTypeMetrics),
			ErrorsByType:     make(map[string]int64),
		},
		connectionMetrics: make(map[string]*ConnectionHealthMetrics),
		logger:            log.WithComponent("metrics.collector"),
	}
}

// RecordOperation records metrics for an operation
func (mc *MetricsCollector) RecordOperation(operationType string, duration time.Duration, err error) {
	mc.operationMetrics.mutex.Lock()
	defer mc.operationMetrics.mutex.Unlock()

	// Update overall metrics
	mc.operationMetrics.TotalOperations++
	mc.operationMetrics.LastOperation = time.Now()
	mc.operationMetrics.TotalDuration += duration

	if err == nil {
		mc.operationMetrics.SuccessfulOps++
	} else {
		mc.operationMetrics.FailedOps++

		// Record error type
		errorType := errors.GetErrorCode(err)
		mc.operationMetrics.ErrorsByType[errorType]++
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

	if metrics.TotalOperations > 0 {
		metrics.AverageResponseTime = metrics.TotalDuration / time.Duration(metrics.TotalOperations)
	}

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

	mc.logger.Debug("metrics reset")
}
