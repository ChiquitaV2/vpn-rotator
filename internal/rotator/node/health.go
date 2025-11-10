package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// HealthService provides comprehensive node health checking with caching and background monitoring
type HealthService struct {
	nodeService    NodeService
	repository     NodeRepository
	nodeInteractor nodeinteractor.HealthChecker
	logger         *applogger.Logger
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

func NewHealthService(
	nodeService NodeService,
	repository NodeRepository,
	nodeInteractor nodeinteractor.HealthChecker,
	logger *applogger.Logger, // <-- Changed
	config HealthConfig,
) *HealthService {
	return &HealthService{
		nodeService:    nodeService,
		repository:     repository,
		nodeInteractor: nodeInteractor,
		logger:         logger.WithComponent("health.service"),
		config:         config,
		healthCache:    make(map[string]*CachedHealth),
		backgroundStop: make(chan struct{}),
		backgroundDone: make(chan struct{}),
	}
}

// StartBackgroundHealthChecks starts background health monitoring
func (hs *HealthService) StartBackgroundHealthChecks(ctx context.Context) {
	hs.logger.InfoContext(ctx, "starting background health checks",
		slog.Duration("interval", hs.config.BackgroundCheckInterval))

	go hs.backgroundHealthChecker(ctx)
}

// StopBackgroundHealthChecks stops background health monitoring
func (hs *HealthService) StopBackgroundHealthChecks() {
	hs.logger.InfoContext(context.Background(), "stopping background health checks")
	close(hs.backgroundStop)
	<-hs.backgroundDone
}

// CheckNodeHealth checks node health with caching and alerting
func (hs *HealthService) CheckNodeHealth(ctx context.Context, nodeID string) (*Health, error) {
	op := hs.logger.StartOp(ctx, "CheckNodeHealth", slog.String("node_id", nodeID))

	// Check cache first
	if cachedHealth := hs.getCachedHealth(nodeID); cachedHealth != nil && !cachedHealth.IsStale {
		hs.logger.DebugContext(ctx, "returning cached health",
			slog.Time("last_checked", cachedHealth.LastChecked))
		op.Complete("returning cached health", slog.String("result", "from_cache"))
		return cachedHealth.Health, nil
	}

	// Perform fresh health check
	health, err := hs.performHealthCheck(ctx, nodeID)
	if err != nil {
		hs.updateHealthCache(nodeID, nil, err)
		hs.logger.ErrorCtx(ctx, "fresh health check failed", err)
		op.Fail(err, "fresh health check failed")
		return nil, err
	}

	// Update cache and check for alerts
	hs.updateHealthCache(nodeID, health, nil)
	hs.checkForAlerts(ctx, nodeID, health)

	op.Complete("health check completed", slog.String("result", "fresh_check"))
	return health, nil
}

// GetNodeHealthStatus retrieves comprehensive health status with interpretation
func (hs *HealthService) GetNodeHealthStatus(ctx context.Context, nodeID string) (*HealthStatus, error) {
	health, err := hs.CheckNodeHealth(ctx, nodeID)
	if err != nil {
		return nil, err // CheckNodeHealth already logs and wraps
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
	op := hs.logger.StartOp(ctx, "GetAllNodesHealth")

	// Get all active nodes
	filters := Filters{}
	activeStatus := StatusActive
	filters.Status = &activeStatus

	nodes, err := hs.repository.List(ctx, filters)
	if err != nil {
		domainErr := apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeDatabase, "failed to list nodes", true)
		hs.logger.ErrorCtx(ctx, "failed to list nodes", domainErr)
		op.Fail(domainErr, "failed to list nodes")
		return nil, domainErr
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
				hs.logger.ErrorCtx(ctx, "failed to get node health status for node", err,
					slog.String("node_id", nodeID))
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
	op.Complete("all nodes health retrieved", slog.Int("node_count", len(results)))
	return results, nil
}

// GetUnhealthyNodes retrieves nodes that are currently unhealthy
func (hs *HealthService) GetUnhealthyNodes(ctx context.Context) ([]*Node, error) {
	op := hs.logger.StartOp(ctx, "GetUnhealthyNodes")

	unhealthyNodes, err := hs.repository.GetUnhealthyNodes(ctx)
	if err != nil {
		hs.logger.ErrorCtx(ctx, "failed to get unhealthy nodes", err)
		op.Fail(err, "failed to get unhealthy nodes")
		return nil, err // Repo returns DomainError
	}

	op.Complete("unhealthy nodes retrieved", slog.Int("count", len(unhealthyNodes)))
	return unhealthyNodes, nil
}

// UpdateNodeHealthStatus updates a node's health status in the repository
func (hs *HealthService) UpdateNodeHealthStatus(ctx context.Context, nodeID string, health *Health) error {
	op := hs.logger.StartOp(ctx, "UpdateNodeHealthStatus", slog.String("node_id", nodeID))

	// Update health in repository
	if err := hs.repository.UpdateHealth(ctx, nodeID, health); err != nil {
		hs.logger.ErrorCtx(ctx, "failed to update health", err)
		op.Fail(err, "failed to update health")
		return err // Repo returns DomainError
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
		hs.logger.ErrorCtx(ctx, "failed to get node after health update", err)
		op.Fail(err, "failed to get node after health update")
		return err
	}

	if node.Status != newStatus {
		if err := hs.nodeService.UpdateNodeStatus(ctx, nodeID, newStatus); err != nil {
			// This is a sub-operation failure, log it but don't fail the whole update
			hs.logger.ErrorCtx(ctx, "failed to update node status based on health", err,
				slog.String("new_status", string(newStatus)))
		}
	}

	op.Complete("node health status updated")
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
		return nil, err // Repo returns DomainError
	}

	// Use NodeInteractor for comprehensive health check
	healthStatus, err := hs.nodeInteractor.CheckNodeHealth(checkCtx, node.IPAddress)
	if err != nil {
		return nil, apperrors.NewInfrastructureError(apperrors.ErrCodeHealthCheckFailed, "comprehensive health check failed", true, err).
			WithMetadata("node_id", nodeID)
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
}

func (hs *HealthService) checkForAlerts(ctx context.Context, nodeID string, health *Health) {
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
		hs.sendAlert(ctx, alert)
	}

	// ... (check for recovery) ...
}

func (hs *HealthService) sendAlert(ctx context.Context, alert HealthAlert) {
	hs.logger.WarnContext(ctx, "health alert",
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
			hs.logger.InfoContext(ctx, "background health checker stopped due to context cancellation")
			return
		case <-hs.backgroundStop:
			hs.logger.InfoContext(ctx, "background health checker stopped")
			return
		case <-ticker.C:
			hs.performBackgroundHealthChecks(ctx)
		}
	}
}

func (hs *HealthService) performBackgroundHealthChecks(ctx context.Context) {
	op := hs.logger.StartOp(ctx, "BackgroundHealthChecks")

	// Get all active nodes
	filters := Filters{}
	activeStatus := StatusActive
	filters.Status = &activeStatus

	nodes, err := hs.repository.List(ctx, filters)
	if err != nil {
		hs.logger.ErrorCtx(ctx, "failed to list nodes for background health checks", err)
		op.Fail(err, "failed to list nodes for background health checks")
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
			hs.logger.DebugContext(ctx, "background health check failed for node",
				slog.String("node_id", node.ID),
				slog.String("error", err.Error()))
			hs.updateHealthCache(node.ID, nil, err)
			continue
		}

		// Update cache and repository
		hs.updateHealthCache(node.ID, health, nil)
		hs.checkForAlerts(ctx, node.ID, health)

		// Update repository
		if err := hs.UpdateNodeHealthStatus(ctx, node.ID, health); err != nil {
			hs.logger.ErrorCtx(ctx, "failed to update node health in repository", err,
				slog.String("node_id", node.ID))
		}
	}
	op.Complete("background health checks completed", slog.Int("nodes_checked", len(nodes)))
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
