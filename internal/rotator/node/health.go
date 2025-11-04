package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
)

// HealthService provides comprehensive node health checking with caching and background monitoring
type HealthService struct {
	nodeService    NodeService
	repository     NodeRepository
	nodeInteractor nodeinteractor.HealthChecker
	logger         *slog.Logger
	config         HealthConfig

	// Health cache and background monitoring
	healthCache    map[string]*CachedHealth
	cacheMutex     sync.RWMutex
	backgroundStop chan struct{}
	backgroundDone chan struct{}
}

// HealthConfig contains configuration for health checking
type HealthConfig struct {
	CacheTimeout            time.Duration `json:"cache_timeout"`
	BackgroundCheckInterval time.Duration `json:"background_check_interval"`
	HealthCheckTimeout      time.Duration `json:"health_check_timeout"`
	UnhealthyThreshold      int           `json:"unhealthy_threshold"` // Number of consecutive failures
	RecoveryThreshold       int           `json:"recovery_threshold"`  // Number of consecutive successes
	AlertingEnabled         bool          `json:"alerting_enabled"`
	SystemMetricsEnabled    bool          `json:"system_metrics_enabled"`
	MaxConcurrentChecks     int           `json:"max_concurrent_checks"`
}

// CachedHealth represents cached health information
type CachedHealth struct {
	Health           *Health
	LastChecked      time.Time
	ConsecutiveFails int
	ConsecutiveOK    int
	IsStale          bool
}

// HealthAlert represents a health alert
type HealthAlert struct {
	NodeID    string    `json:"node_id"`
	AlertType string    `json:"alert_type"` // "unhealthy", "recovered", "critical"
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"` // "warning", "error", "critical"
	Health    *Health   `json:"health"`
}

// NewHealthService creates a new health service
func NewHealthService(
	nodeService NodeService,
	repository NodeRepository,
	nodeInteractor nodeinteractor.HealthChecker,
	logger *slog.Logger,
	config HealthConfig,
) *HealthService {
	return &HealthService{
		nodeService:    nodeService,
		repository:     repository,
		nodeInteractor: nodeInteractor,
		logger:         logger,
		config:         config,
		healthCache:    make(map[string]*CachedHealth),
		backgroundStop: make(chan struct{}),
		backgroundDone: make(chan struct{}),
	}
}

// StartBackgroundHealthChecks starts background health monitoring
func (hs *HealthService) StartBackgroundHealthChecks(ctx context.Context) {
	hs.logger.Info("starting background health checks",
		slog.Duration("interval", hs.config.BackgroundCheckInterval))

	go hs.backgroundHealthChecker(ctx)
}

// StopBackgroundHealthChecks stops background health monitoring
func (hs *HealthService) StopBackgroundHealthChecks() {
	hs.logger.Info("stopping background health checks")
	close(hs.backgroundStop)
	<-hs.backgroundDone
}

// CheckNodeHealth checks node health with caching and alerting
func (hs *HealthService) CheckNodeHealth(ctx context.Context, nodeID string) (*Health, error) {
	hs.logger.Debug("checking node health", slog.String("node_id", nodeID))

	// Check cache first
	if cachedHealth := hs.getCachedHealth(nodeID); cachedHealth != nil && !cachedHealth.IsStale {
		hs.logger.Debug("returning cached health",
			slog.String("node_id", nodeID),
			slog.Time("last_checked", cachedHealth.LastChecked))
		return cachedHealth.Health, nil
	}

	// Perform fresh health check
	health, err := hs.performHealthCheck(ctx, nodeID)
	if err != nil {
		hs.updateHealthCache(nodeID, nil, err)
		return nil, err
	}

	// Update cache and check for alerts
	hs.updateHealthCache(nodeID, health, nil)
	hs.checkForAlerts(nodeID, health)

	return health, nil
}

// GetNodeHealthStatus retrieves comprehensive health status with interpretation
func (hs *HealthService) GetNodeHealthStatus(ctx context.Context, nodeID string) (*HealthStatus, error) {
	health, err := hs.CheckNodeHealth(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// Get cached health for trend analysis
	cached := hs.getCachedHealth(nodeID)

	status := &HealthStatus{
		NodeID:           nodeID,
		Health:           health,
		Status:           hs.interpretHealthStatus(health),
		Trend:            hs.calculateHealthTrend(cached),
		LastChecked:      health.LastChecked,
		ConsecutiveFails: 0,
		ConsecutiveOK:    0,
		Alerts:           []HealthAlert{},
	}

	if cached != nil {
		status.ConsecutiveFails = cached.ConsecutiveFails
		status.ConsecutiveOK = cached.ConsecutiveOK
	}

	return status, nil
}

// GetAllNodesHealth retrieves health status for all active nodes
func (hs *HealthService) GetAllNodesHealth(ctx context.Context) ([]*HealthStatus, error) {
	hs.logger.Debug("getting health for all nodes")

	// Get all active nodes
	filters := Filters{}
	activeStatus := StatusActive
	filters.Status = &activeStatus

	nodes, err := hs.repository.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Check health for each node concurrently
	results := make([]*HealthStatus, len(nodes))
	semaphore := make(chan struct{}, hs.config.MaxConcurrentChecks)
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(index int, nodeID string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			status, err := hs.GetNodeHealthStatus(ctx, nodeID)
			if err != nil {
				hs.logger.Warn("failed to get node health",
					slog.String("node_id", nodeID),
					slog.String("error", err.Error()))
				// Create error status
				status = &HealthStatus{
					NodeID:      nodeID,
					Status:      "error",
					LastChecked: time.Now(),
					Alerts: []HealthAlert{{
						NodeID:    nodeID,
						AlertType: "check_failed",
						Message:   fmt.Sprintf("Health check failed: %v", err),
						Timestamp: time.Now(),
						Severity:  "error",
					}},
				}
			}
			results[index] = status
		}(i, node.ID)
	}

	wg.Wait()
	return results, nil
}

// GetUnhealthyNodes retrieves nodes that are currently unhealthy
func (hs *HealthService) GetUnhealthyNodes(ctx context.Context) ([]*Node, error) {
	hs.logger.Debug("getting unhealthy nodes")

	unhealthyNodes, err := hs.repository.GetUnhealthyNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get unhealthy nodes: %w", err)
	}

	return unhealthyNodes, nil
}

// UpdateNodeHealthStatus updates a node's health status in the repository
func (hs *HealthService) UpdateNodeHealthStatus(ctx context.Context, nodeID string, health *Health) error {
	hs.logger.Debug("updating node health status", slog.String("node_id", nodeID))

	// Update health in repository
	if err := hs.repository.UpdateHealth(ctx, nodeID, health); err != nil {
		return fmt.Errorf("failed to update health: %w", err)
	}

	// Update node status based on health
	var newStatus Status
	if health.IsHealthy {
		newStatus = StatusActive
	} else {
		newStatus = StatusUnhealthy
	}

	// Update node status if it changed
	node, err := hs.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Status != newStatus {
		if err := hs.nodeService.UpdateNodeStatus(ctx, nodeID, newStatus); err != nil {
			hs.logger.Warn("failed to update node status based on health",
				slog.String("node_id", nodeID),
				slog.String("new_status", string(newStatus)),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// Private methods

func (hs *HealthService) performHealthCheck(ctx context.Context, nodeID string) (*Health, error) {
	// Create timeout context for health check
	checkCtx, cancel := context.WithTimeout(ctx, hs.config.HealthCheckTimeout)
	defer cancel()

	// Get node from repository
	node, err := hs.repository.GetByID(checkCtx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Use NodeInteractor for comprehensive health check
	healthStatus, err := hs.nodeInteractor.CheckNodeHealth(checkCtx, node.IPAddress)
	if err != nil {
		return nil, NewHealthCheckError(nodeID, "comprehensive", err)
	}

	// Convert to domain Health
	health := &Health{
		NodeID:          nodeID,
		IsHealthy:       healthStatus.IsHealthy,
		ResponseTime:    healthStatus.ResponseTime,
		SystemLoad:      healthStatus.SystemLoad,
		MemoryUsage:     healthStatus.MemoryUsage,
		DiskUsage:       healthStatus.DiskUsage,
		WireGuardStatus: healthStatus.WireGuardStatus,
		ConnectedPeers:  healthStatus.ConnectedPeers,
		LastChecked:     healthStatus.LastChecked,
		Errors:          healthStatus.Errors,
	}

	return health, nil
}

func (hs *HealthService) getCachedHealth(nodeID string) *CachedHealth {
	hs.cacheMutex.RLock()
	defer hs.cacheMutex.RUnlock()

	cached, exists := hs.healthCache[nodeID]
	if !exists {
		return nil
	}

	// Check if cache is stale
	if time.Since(cached.LastChecked) > hs.config.CacheTimeout {
		cached.IsStale = true
	}

	return cached
}

func (hs *HealthService) updateHealthCache(nodeID string, health *Health, err error) {
	hs.cacheMutex.Lock()
	defer hs.cacheMutex.Unlock()

	cached, exists := hs.healthCache[nodeID]
	if !exists {
		cached = &CachedHealth{}
		hs.healthCache[nodeID] = cached
	}

	cached.LastChecked = time.Now()
	cached.IsStale = false

	if err != nil {
		cached.ConsecutiveFails++
		cached.ConsecutiveOK = 0
	} else {
		cached.Health = health
		if health.IsHealthy {
			cached.ConsecutiveOK++
			cached.ConsecutiveFails = 0
		} else {
			cached.ConsecutiveFails++
			cached.ConsecutiveOK = 0
		}
	}
}

func (hs *HealthService) checkForAlerts(nodeID string, health *Health) {
	if !hs.config.AlertingEnabled {
		return
	}

	cached := hs.getCachedHealth(nodeID)
	if cached == nil {
		return
	}

	// Check for unhealthy threshold
	if cached.ConsecutiveFails >= hs.config.UnhealthyThreshold {
		alert := HealthAlert{
			NodeID:    nodeID,
			AlertType: "unhealthy",
			Message:   fmt.Sprintf("Node has failed %d consecutive health checks", cached.ConsecutiveFails),
			Timestamp: time.Now(),
			Severity:  "error",
			Health:    health,
		}
		hs.sendAlert(alert)
	}

	// Check for recovery
	if cached.ConsecutiveOK >= hs.config.RecoveryThreshold && cached.ConsecutiveFails > 0 {
		alert := HealthAlert{
			NodeID:    nodeID,
			AlertType: "recovered",
			Message:   fmt.Sprintf("Node has recovered after %d consecutive successful checks", cached.ConsecutiveOK),
			Timestamp: time.Now(),
			Severity:  "warning",
			Health:    health,
		}
		hs.sendAlert(alert)
	}
}

func (hs *HealthService) sendAlert(alert HealthAlert) {
	hs.logger.Warn("health alert",
		slog.String("node_id", alert.NodeID),
		slog.String("type", alert.AlertType),
		slog.String("severity", alert.Severity),
		slog.String("message", alert.Message))

	// In a full implementation, this would send alerts to monitoring systems
}

func (hs *HealthService) interpretHealthStatus(health *Health) string {
	if health.IsHealthy {
		return "healthy"
	}

	// Analyze specific issues
	if len(health.Errors) > 0 {
		return "unhealthy"
	}

	if health.SystemLoad > 5.0 {
		return "overloaded"
	}

	if health.MemoryUsage > 90.0 {
		return "memory_pressure"
	}

	if health.DiskUsage > 90.0 {
		return "disk_full"
	}

	return "degraded"
}

func (hs *HealthService) calculateHealthTrend(cached *CachedHealth) string {
	if cached == nil {
		return "unknown"
	}

	if cached.ConsecutiveOK > cached.ConsecutiveFails {
		return "improving"
	} else if cached.ConsecutiveFails > cached.ConsecutiveOK {
		return "degrading"
	}

	return "stable"
}

func (hs *HealthService) backgroundHealthChecker(ctx context.Context) {
	defer close(hs.backgroundDone)

	ticker := time.NewTicker(hs.config.BackgroundCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			hs.logger.Info("background health checker stopped due to context cancellation")
			return
		case <-hs.backgroundStop:
			hs.logger.Info("background health checker stopped")
			return
		case <-ticker.C:
			hs.performBackgroundHealthChecks(ctx)
		}
	}
}

func (hs *HealthService) performBackgroundHealthChecks(ctx context.Context) {
	hs.logger.Debug("performing background health checks")

	// Get all active nodes
	filters := Filters{}
	activeStatus := StatusActive
	filters.Status = &activeStatus

	nodes, err := hs.repository.List(ctx, filters)
	if err != nil {
		hs.logger.Error("failed to list nodes for background health checks",
			slog.String("error", err.Error()))
		return
	}

	// Check each node's health
	for _, node := range nodes {
		// Check if we need to update this node's health
		cached := hs.getCachedHealth(node.ID)
		if cached != nil && !cached.IsStale {
			continue // Skip if cache is still fresh
		}

		// Perform health check
		health, err := hs.performHealthCheck(ctx, node.ID)
		if err != nil {
			hs.logger.Debug("background health check failed",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
			hs.updateHealthCache(node.ID, nil, err)
			continue
		}

		// Update cache and repository
		hs.updateHealthCache(node.ID, health, nil)
		hs.checkForAlerts(node.ID, health)

		// Update repository
		if err := hs.UpdateNodeHealthStatus(ctx, node.ID, health); err != nil {
			hs.logger.Warn("failed to update node health in repository",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
		}
	}
}

// HealthStatus represents comprehensive health status with trends and alerts
type HealthStatus struct {
	NodeID           string        `json:"node_id"`
	Health           *Health       `json:"health"`
	Status           string        `json:"status"` // "healthy", "unhealthy", "degraded", etc.
	Trend            string        `json:"trend"`  // "improving", "degrading", "stable"
	LastChecked      time.Time     `json:"last_checked"`
	ConsecutiveFails int           `json:"consecutive_fails"`
	ConsecutiveOK    int           `json:"consecutive_ok"`
	Alerts           []HealthAlert `json:"alerts"`
}
