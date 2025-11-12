package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// AdminService defines the application layer interface for administrative operations
// It provides system monitoring, statistics, and administrative controls
type AdminService interface {
	// System status and monitoring
	GetSystemStatus(ctx context.Context) (*SystemStatus, error)
	GetNodeStatistics(ctx context.Context) (*NodeStatistics, error)
	GetPeerStatistics(ctx context.Context) (*api.PeerStatsResponse, error)

	// Administrative operations
	ForceNodeRotation(ctx context.Context) error
	ManualCleanup(ctx context.Context, options CleanupOptions) (*CleanupResult, error)

	// Rotation and health monitoring
	GetRotationStatus(ctx context.Context) (*RotationStatus, error)
	ValidateSystemHealth(ctx context.Context) (*HealthReport, error)

	// Resource management
	GetOrphanedResourcesReport(ctx context.Context) (*OrphanedResourcesReport, error)
	GetCapacityReport(ctx context.Context) (*CapacityReport, error)
}

// adminService implements AdminService by coordinating domain services
type adminService struct {
	nodeService node.NodeService
	peerService peer.Service
	ipService   ip.Service
	vpnService  VPNService // For rotation and cleanup operations
	provisioner *ProvisioningService
	logger      *applogger.Logger
}

// NewAdminService creates a new admin service instance with async provisioning support
func NewAdminService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	vpnService VPNService,
	provisioner *ProvisioningService,
	logger *applogger.Logger,
) AdminService {
	return &adminService{
		nodeService: nodeService,
		peerService: peerService,
		ipService:   ipService,
		vpnService:  vpnService,
		provisioner: provisioner,
		logger:      logger.WithComponent("admin.service"),
	}
}

// GetSystemStatus retrieves comprehensive system status by coordinating all domain services
func (s *adminService) GetSystemStatus(ctx context.Context) (*SystemStatus, error) {
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
	nodeStatuses := make(map[string]NodeStatus)
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

		nodeStatuses[activeNode.ID] = NodeStatus{
			NodeID:      activeNode.ID,
			Status:      string(activeNode.Status),
			PeerCount:   int(peerCount),
			IsHealthy:   health.IsHealthy,
			SystemLoad:  health.SystemLoad,
			MemoryUsage: health.MemoryUsage,
			DiskUsage:   health.DiskUsage,
			LastChecked: health.LastChecked,
		}
	}

	status := &SystemStatus{
		TotalNodes:       int(nodeStats.TotalNodes),
		ActiveNodes:      int(nodeStats.ActiveNodes),
		TotalPeers:       int(peerStats.TotalPeers),
		ActivePeers:      int(peerStats.ActivePeers),
		NodeDistribution: nodeDistribution,
		SystemHealth:     s.calculateSystemHealth(nodeStatuses),
		LastUpdated:      time.Now(),
		NodeStatuses:     nodeStatuses,
	}

	if s.provisioner != nil {
		provisioningStatus := s.provisioner.GetCurrentStatus()
		if provisioningStatus != nil {
			status.Provisioning = &ProvisioningInfo{
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
func (s *adminService) GetNodeStatistics(ctx context.Context) (*NodeStatistics, error) {
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

	statistics := &NodeStatistics{
		TotalNodes:        int(domainStats.TotalNodes),
		ActiveNodes:       int(domainStats.ActiveNodes),
		ProvisioningNodes: int(domainStats.ProvisioningNodes),
		UnhealthyNodes:    int(domainStats.ActiveNodes) - healthyNodes,
		AverageLoad:       avgLoad,
		AverageMemory:     avgMemory,
		AverageDisk:       avgDisk,
		NodeDistribution:  nodeDistribution,
		LastUpdated:       time.Now(),
	}

	op.Complete("retrieved node statistics")
	return statistics, nil
}

// GetPeerStatistics retrieves detailed peer statistics
func (s *adminService) GetPeerStatistics(ctx context.Context) (*api.PeerStatsResponse, error) {
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

	statistics := &api.PeerStatsResponse{
		TotalPeers:   int(domainStats.TotalPeers),
		ActiveNodes:  len(activeNodes),
		Distribution: peerDistribution,
		LastUpdated:  time.Now(),
	}

	op.Complete("retrieved peer statistics")
	return statistics, nil
}

// ForceNodeRotation forces an immediate node rotation
func (s *adminService) ForceNodeRotation(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "ForceNodeRotation", slog.String("trigger", "manual"))
	if err := s.vpnService.RotateNodes(ctx); err != nil {
		op.Fail(err, "forced node rotation failed")
		return err
	}
	op.Complete("forced node rotation initiated")
	return nil
}

// ManualCleanup performs manual cleanup with specified options
func (s *adminService) ManualCleanup(ctx context.Context, options CleanupOptions) (*CleanupResult, error) {
	op := s.logger.StartOp(ctx, "ManualCleanup", slog.Any("options", options))
	if err := s.vpnService.CleanupInactiveResourcesWithOptions(ctx, options); err != nil {
		op.Fail(err, "manual cleanup failed")
		return nil, err
	}
	result := &CleanupResult{Timestamp: time.Now(), Duration: time.Since(op.StartTime).Milliseconds()}
	op.Complete("manual cleanup finished")
	return result, nil
}

// GetRotationStatus retrieves the current rotation status
func (s *adminService) GetRotationStatus(ctx context.Context) (*RotationStatus, error) {
	op := s.logger.StartOp(ctx, "GetRotationStatus")

	provisioningStatus := node.StatusProvisioning
	provisioningNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &provisioningStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to check provisioning nodes", true)
		op.Fail(err, "failed to check provisioning nodes")
		return nil, err
	}

	status := &RotationStatus{
		InProgress:    len(provisioningNodes) > 0,
		LastRotation:  time.Now().Add(-24 * time.Hour),
		NodesRotated:  0,
		PeersMigrated: 0,
	}

	if status.InProgress {
		status.RotationReason = "rotation in progress"
		status.EstimatedComplete = time.Now().Add(10 * time.Minute)
	}

	op.Complete("retrieved rotation status")
	return status, nil
}

// ValidateSystemHealth performs comprehensive system health validation
func (s *adminService) ValidateSystemHealth(ctx context.Context) (*HealthReport, error) {
	op := s.logger.StartOp(ctx, "ValidateSystemHealth")

	report := &HealthReport{
		NodeHealth:      make(map[string]NodeHealth),
		LastChecked:     time.Now(),
		Issues:          []HealthIssue{},
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
		report.Issues = append(report.Issues, HealthIssue{Severity: "critical", Component: "system", Message: "no active nodes available", Timestamp: time.Now()})
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
			report.Issues = append(report.Issues, HealthIssue{Severity: "warning", Component: "node", ComponentID: activeNode.ID, Message: fmt.Sprintf("failed to check node health: %v", err), Timestamp: time.Now()})
			continue
		}

		nodeHealth := NodeHealth{
			NodeID:       activeNode.ID,
			IsHealthy:    health.IsHealthy,
			SystemLoad:   health.SystemLoad,
			MemoryUsage:  health.MemoryUsage,
			DiskUsage:    health.DiskUsage,
			ResponseTime: health.ResponseTime.Milliseconds(),
			LastChecked:  health.LastChecked,
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
			report.Issues = append(report.Issues, HealthIssue{Severity: severity, Component: "node", ComponentID: activeNode.ID, Message: issue, Timestamp: time.Now()})
		}
	}

	report.SystemMetrics = SystemMetrics{
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

// GetOrphanedResourcesReport retrieves a report of orphaned resources
func (s *adminService) GetOrphanedResourcesReport(ctx context.Context) (*OrphanedResourcesReport, error) {
	op := s.logger.StartOp(ctx, "GetOrphanedResourcesReport")

	resourceCleanupService := NewResourceCleanupService(s.nodeService, s.peerService, s.ipService, s.logger)
	report, err := resourceCleanupService.DetectOrphanedResources(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainSystem, "report_failed", "failed to detect orphaned resources", true)
		op.Fail(err, "failed to detect orphaned resources")
		return nil, err
	}

	op.Complete("retrieved orphaned resources report")
	return report, nil
}

// GetCapacityReport retrieves a comprehensive capacity report
func (s *adminService) GetCapacityReport(ctx context.Context) (*CapacityReport, error) {
	op := s.logger.StartOp(ctx, "GetCapacityReport")

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "failed to list active nodes")
		return nil, err
	}

	report := &CapacityReport{
		Timestamp: time.Now(),
		Nodes:     make(map[string]NodeCapacityInfo),
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

		nodeCapacity := NodeCapacityInfo{
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
func (s *adminService) calculateSystemHealth(nodeStatuses map[string]NodeStatus) string {
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

// CapacityReport represents a comprehensive capacity report
type CapacityReport struct {
	TotalCapacity  int                         `json:"total_capacity"`
	TotalUsed      int                         `json:"total_used"`
	TotalAvailable int                         `json:"total_available"`
	OverallUsage   float64                     `json:"overall_usage"`
	Nodes          map[string]NodeCapacityInfo `json:"nodes"`
	Timestamp      time.Time                   `json:"timestamp"`
}

// NodeCapacityInfo represents capacity information for a single node
type NodeCapacityInfo struct {
	NodeID         string  `json:"node_id"`
	MaxPeers       int     `json:"max_peers"`
	CurrentPeers   int     `json:"current_peers"`
	AvailablePeers int     `json:"available_peers"`
	CapacityUsed   float64 `json:"capacity_used"`
}
