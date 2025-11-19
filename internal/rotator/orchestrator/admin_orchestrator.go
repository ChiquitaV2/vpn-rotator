package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	pkgapi "github.com/chiquitav2/vpn-rotator/pkg/api"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// AdminOrchestrator defines the interface for administrative orchestration operations.
// It coordinates various services to provide system monitoring, statistics, and administrative controls.
type AdminOrchestrator interface {
	// System status and monitoring
	GetSystemStatus(ctx context.Context) (*pkgapi.SystemStatus, error)
	GetNodeStatistics(ctx context.Context) (*pkgapi.NodeStatistics, error)
	GetPeerStatistics(ctx context.Context) (*pkgapi.PeerStatsResponse, error)

	// System health monitoring
	ValidateSystemHealth(ctx context.Context) (*pkgapi.HealthReport, error)

	// Capacity management
	GetCapacityReport(ctx context.Context) (*pkgapi.CapacityReport, error)
}

// adminOrchestrator implements AdminOrchestrator by coordinating domain and application services.
type adminOrchestrator struct {
	nodeService      node.NodeService
	peerService      peer.Service
	ipService        ip.Service
	nodeStateTracker *node.ProvisioningStateTracker
	logger           *applogger.Logger
}

// NewAdminOrchestrator creates a new admin orchestrator instance.
func NewAdminOrchestrator(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	nodeStateTracker *node.ProvisioningStateTracker,
	logger *applogger.Logger,
) AdminOrchestrator {
	return &adminOrchestrator{
		nodeService:      nodeService,
		peerService:      peerService,
		ipService:        ipService,
		nodeStateTracker: nodeStateTracker,
		logger:           logger.WithComponent("admin.orchestrator"),
	}
}

// GetSystemStatus retrieves comprehensive system status by coordinating all domain services
func (s *adminOrchestrator) GetSystemStatus(ctx context.Context) (*pkgapi.SystemStatus, error) {
	op := s.logger.StartOp(ctx, "GetSystemStatus")

	nodeStats, err := s.nodeService.GetNodeStatistics(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, "stats_failed", "failed to get node statistics", true)
		op.Fail(err, "failed to get node statistics")
		return nil, err
	}

	peerStats, err := s.peerService.GetStatistics(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, "stats_failed", "failed to get peer statistics", true)
		op.Fail(err, "failed to get peer statistics")
		return nil, err
	}

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "failed to list active nodes")
		return nil, err
	}

	nodeDistribution := make(map[string]int)
	nodeStatuses := make(map[string]pkgapi.NodeStatus)
	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to get peer count for node", err, slog.String("node_id", activeNode.ID))
			peerCount = 0
		}
		nodeDistribution[activeNode.ID] = int(peerCount)

		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to get node health", err, slog.String("node_id", activeNode.ID))
			health = &node.NodeHealthStatus{IsHealthy: false, LastChecked: time.Now()}
		}

		nodeStatuses[activeNode.ID] = pkgapi.NodeStatus{
			NodeID:      activeNode.ID,
			Status:      string(activeNode.Status),
			PeerCount:   int(peerCount),
			IsHealthy:   health.IsHealthy,
			SystemLoad:  health.SystemLoad,
			MemoryUsage: health.MemoryUsage,
			DiskUsage:   health.DiskUsage,
			Timestamp:   health.LastChecked,
		}
	}

	status := &pkgapi.SystemStatus{
		TotalNodes:       int(nodeStats.TotalNodes),
		ActiveNodes:      int(nodeStats.ActiveNodes),
		TotalPeers:       int(peerStats.TotalPeers),
		ActivePeers:      int(peerStats.ActivePeers),
		NodeDistribution: nodeDistribution,
		SystemHealth:     s.calculateSystemHealth(nodeStatuses),
		NodeStatuses:     nodeStatuses,
		Provisioning:     nil,
		Timestamp:        time.Now(),
	}

	if s.nodeStateTracker != nil {
		provisioningStatus := s.nodeStateTracker.GetActiveProvisioning()
		if provisioningStatus != nil {
			status.Provisioning = &pkgapi.ProvisioningInfo{
				IsActive:     provisioningStatus.IsActive,
				Phase:        provisioningStatus.Phase,
				Progress:     provisioningStatus.Progress,
				EstimatedETA: provisioningStatus.EstimatedETA,
			}
		}
	}

	op.Complete("system status retrieved")
	return status, nil
}

// GetNodeStatistics retrieves detailed node statistics
func (s *adminOrchestrator) GetNodeStatistics(ctx context.Context) (*pkgapi.NodeStatistics, error) {
	op := s.logger.StartOp(ctx, "GetNodeStatistics")

	domainStats, err := s.nodeService.GetNodeStatistics(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, "stats_failed", "failed to get node statistics", true)
		op.Fail(err, "failed to get node statistics")
		return nil, err
	}

	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list all nodes", true)
		op.Fail(err, "failed to list all nodes")
		return nil, err
	}

	var totalLoad, totalMemory, totalDisk float64
	var healthyNodes int
	nodeDistribution := make(map[string]int)

	for _, nodeInfo := range allNodes {
		region := string(nodeInfo.Status)
		nodeDistribution[region]++

		if nodeInfo.Status == node.StatusActive {
			health, err := s.nodeService.CheckNodeHealth(ctx, nodeInfo.ID)
			if err != nil {
				s.logger.ErrorCtx(ctx, "failed to check node health, skipping node from stats", err, slog.String("node_id", nodeInfo.ID))
				continue
			}
			totalLoad += health.SystemLoad
			totalMemory += health.MemoryUsage
			totalDisk += health.DiskUsage
			if health.IsHealthy {
				healthyNodes++
			}
		}
	}

	activeCount := domainStats.ActiveNodes
	var avgLoad, avgMemory, avgDisk float64
	if activeCount > 0 {
		avgLoad = totalLoad / float64(activeCount)
		avgMemory = totalMemory / float64(activeCount)
		avgDisk = totalDisk / float64(activeCount)
	}

	statistics := &pkgapi.NodeStatistics{
		TotalNodes:        int(domainStats.TotalNodes),
		ActiveNodes:       int(domainStats.ActiveNodes),
		ProvisioningNodes: int(domainStats.ProvisioningNodes),
		UnhealthyNodes:    int(domainStats.ActiveNodes) - healthyNodes,
		AverageLoad:       avgLoad,
		AverageMemory:     avgMemory,
		AverageDisk:       avgDisk,
		NodeDistribution:  nodeDistribution,
		Timestamp:         time.Now(),
	}

	op.Complete("retrieved node statistics")
	return statistics, nil
}

// GetPeerStatistics retrieves detailed peer statistics
func (s *adminOrchestrator) GetPeerStatistics(ctx context.Context) (*pkgapi.PeerStatsResponse, error) {
	op := s.logger.StartOp(ctx, "GetPeerStatistics")

	domainStats, err := s.peerService.GetStatistics(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, "stats_failed", "failed to get peer statistics", true)
		op.Fail(err, "failed to get peer statistics")
		return nil, err
	}

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "failed to list active nodes")
		return nil, err
	}

	peerDistribution := make(map[string]int)
	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to get peer count for node", err, slog.String("node_id", activeNode.ID))
			continue
		}
		peerDistribution[activeNode.ID] = int(peerCount)
	}

	statistics := &pkgapi.PeerStatsResponse{
		TotalPeers:   int(domainStats.TotalPeers),
		ActiveNodes:  len(activeNodes),
		Distribution: peerDistribution,
		Timestamp:    time.Now(),
	}

	op.Complete("retrieved peer statistics")
	return statistics, nil
}

// ValidateSystemHealth performs comprehensive system health validation
func (s *adminOrchestrator) ValidateSystemHealth(ctx context.Context) (*pkgapi.HealthReport, error) {
	op := s.logger.StartOp(ctx, "ValidateSystemHealth")

	report := &pkgapi.HealthReport{
		NodeHealth:      make(map[string]pkgapi.NodeStatus),
		Timestamp:       time.Now(),
		Issues:          []pkgapi.HealthIssue{},
		Recommendations: []string{},
	}

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "failed to list active nodes")
		return nil, err
	}

	if len(activeNodes) == 0 {
		report.OverallHealth = "unhealthy"
		report.Issues = append(report.Issues, pkgapi.HealthIssue{Severity: "critical", Component: "system", Message: "no active nodes available", Timestamp: time.Now()})
		report.Recommendations = append(report.Recommendations, "Create at least one active node")
		op.Fail(fmt.Errorf("no active nodes"), "system health validation failed")
		return report, nil
	}

	var healthyNodes, totalNodes int
	var totalLoad, totalMemory, totalDisk, totalResponse float64

	for _, activeNode := range activeNodes {
		totalNodes++
		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to check node health", err, slog.String("node_id", activeNode.ID))
			report.Issues = append(report.Issues, pkgapi.HealthIssue{Severity: "warning", Component: "node", ComponentID: activeNode.ID, Message: fmt.Sprintf("failed to check node health: %v", err), Timestamp: time.Now()})
			continue
		}

		nodeHealth := pkgapi.NodeStatus{
			NodeID:       activeNode.ID,
			Status:       string(activeNode.Status),
			IsHealthy:    health.IsHealthy,
			SystemLoad:   health.SystemLoad,
			MemoryUsage:  health.MemoryUsage,
			DiskUsage:    health.DiskUsage,
			ResponseTime: health.ResponseTime.Milliseconds(),
			Issues:       []string{},
			Timestamp:    health.LastChecked,
		}

		if !health.IsHealthy {
			nodeHealth.Issues = append(nodeHealth.Issues, "node reported as unhealthy")
		}
		if health.SystemLoad > 0.8 {
			nodeHealth.Issues = append(nodeHealth.Issues, "high system load")
		}

		report.NodeHealth[activeNode.ID] = nodeHealth

		if health.IsHealthy {
			healthyNodes++
		}
		totalLoad += health.SystemLoad
		totalMemory += health.MemoryUsage
		totalDisk += health.DiskUsage
		totalResponse += float64(health.ResponseTime.Milliseconds())

		for _, issue := range nodeHealth.Issues {
			severity := "warning"
			if health.SystemLoad > 0.9 || health.MemoryUsage > 0.95 || health.DiskUsage > 0.95 {
				severity = "critical"
			}
			report.Issues = append(report.Issues, pkgapi.HealthIssue{Severity: severity, Component: "node", ComponentID: activeNode.ID, Message: issue, Timestamp: time.Now()})
		}
	}

	report.SystemMetrics = pkgapi.SystemMetrics{
		TotalCapacity:   totalNodes * 100,
		UsedCapacity:    totalNodes - healthyNodes,
		CapacityUsage:   float64(totalNodes-healthyNodes) / float64(totalNodes),
		AverageLoad:     totalLoad / float64(totalNodes),
		AverageMemory:   totalMemory / float64(totalNodes),
		AverageDisk:     totalDisk / float64(totalNodes),
		AverageResponse: int64(totalResponse / float64(totalNodes)),
	}

	healthyPercentage := float64(healthyNodes) / float64(totalNodes)
	switch {
	case healthyPercentage >= 0.8:
		report.OverallHealth = "healthy"
	case healthyPercentage >= 0.5:
		report.OverallHealth = "degraded"
	default:
		report.OverallHealth = "unhealthy"
	}

	op.Complete("system health validation finished", slog.String("overall_health", report.OverallHealth))
	return report, nil
}

// GetCapacityReport retrieves a comprehensive capacity report
func (s *adminOrchestrator) GetCapacityReport(ctx context.Context) (*pkgapi.CapacityReport, error) {
	op := s.logger.StartOp(ctx, "GetCapacityReport")

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "failed to list active nodes")
		return nil, err
	}

	report := &pkgapi.CapacityReport{
		Timestamp: time.Now(),
		Nodes:     make(map[string]pkgapi.NodeCapacity),
	}

	var totalCapacity, totalUsed int
	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to get peer count, skipping node in capacity report", err, slog.String("node_id", activeNode.ID))
			continue
		}

		availableIPs, err := s.ipService.GetAvailableIPCount(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to get available IP count, skipping node in capacity report", err, slog.String("node_id", activeNode.ID))
			continue
		}

		nodeCapacity := pkgapi.NodeCapacity{
			NodeID:         activeNode.ID,
			MaxPeers:       availableIPs + int(peerCount),
			CurrentPeers:   int(peerCount),
			AvailablePeers: availableIPs,
		}
		if nodeCapacity.MaxPeers > 0 {
			nodeCapacity.CapacityUsed = float64(peerCount) / float64(nodeCapacity.MaxPeers)
		}

		report.Nodes[activeNode.ID] = nodeCapacity
		totalCapacity += nodeCapacity.MaxPeers
		totalUsed += nodeCapacity.CurrentPeers
	}

	report.TotalCapacity = totalCapacity
	report.TotalUsed = totalUsed
	report.TotalAvailable = totalCapacity - totalUsed
	if totalCapacity > 0 {
		report.OverallUsage = float64(totalUsed) / float64(totalCapacity)
	}

	op.Complete("retrieved capacity report")
	return report, nil
}

// calculateSystemHealth determines overall system health based on node statuses
func (s *adminOrchestrator) calculateSystemHealth(nodeStatuses map[string]pkgapi.NodeStatus) string {
	if len(nodeStatuses) == 0 {
		return "unhealthy"
	}

	var healthyCount int
	for _, status := range nodeStatuses {
		if status.IsHealthy {
			healthyCount++
		}
	}

	healthyPercentage := float64(healthyCount) / float64(len(nodeStatuses))
	switch {
	case healthyPercentage >= 0.8:
		return "healthy"
	case healthyPercentage >= 0.5:
		return "degraded"
	default:
		return "unhealthy"
	}
}
