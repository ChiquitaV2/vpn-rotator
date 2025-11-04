package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
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
	provisioner *ProvisioningOrchestrator
	logger      *slog.Logger
}

// NewAdminService creates a new admin service instance with async provisioning support
func NewAdminService(nodeService node.NodeService, peerService peer.Service, ipService ip.Service, vpnService VPNService, provisioner *ProvisioningOrchestrator, logger *slog.Logger) AdminService {
	return &adminService{
		nodeService: nodeService,
		peerService: peerService,
		ipService:   ipService,
		vpnService:  vpnService,
		provisioner: provisioner,
		logger:      logger,
	}
}

// GetSystemStatus retrieves comprehensive system status by coordinating all domain services
func (s *adminService) GetSystemStatus(ctx context.Context) (*SystemStatus, error) {
	s.logger.Debug("retrieving system status")

	// Get node statistics
	nodeStats, err := s.nodeService.GetNodeStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node statistics: %w", err)
	}

	// Get peer statistics
	peerStats, err := s.peerService.GetStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer statistics: %w", err)
	}

	// Get all active nodes for distribution and health
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	// Build node distribution and status map
	nodeDistribution := make(map[string]int)
	nodeStatuses := make(map[string]NodeStatus)

	for _, activeNode := range activeNodes {
		// Get peer count for this node
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to get peer count for node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			peerCount = 0
		}

		nodeDistribution[activeNode.ID] = int(peerCount)

		// Get node health
		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to get node health",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			// Create default health status
			health = &node.Health{
				IsHealthy:    false,
				SystemLoad:   0,
				MemoryUsage:  0,
				DiskUsage:    0,
				ResponseTime: 0,
				LastChecked:  time.Now(),
			}
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

	// Determine overall system health
	systemHealth := s.calculateSystemHealth(nodeStatuses)

	status := &SystemStatus{
		TotalNodes:       int(nodeStats.TotalNodes),
		ActiveNodes:      int(nodeStats.ActiveNodes),
		TotalPeers:       int(peerStats.TotalPeers),
		ActivePeers:      int(peerStats.ActivePeers),
		NodeDistribution: nodeDistribution,
		SystemHealth:     systemHealth,
		LastUpdated:      time.Now(),
		NodeStatuses:     nodeStatuses,
	}

	// Add provisioning information if async provisioning service is available
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

	s.logger.Debug("system status retrieved",
		slog.Int("total_nodes", status.TotalNodes),
		slog.Int("active_nodes", status.ActiveNodes),
		slog.Int("total_peers", status.TotalPeers),
		slog.String("system_health", status.SystemHealth))

	return status, nil
}

// GetNodeStatistics retrieves detailed node statistics
func (s *adminService) GetNodeStatistics(ctx context.Context) (*NodeStatistics, error) {
	s.logger.Debug("retrieving node statistics")

	// Get basic node statistics from domain service
	domainStats, err := s.nodeService.GetNodeStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node statistics: %w", err)
	}

	// Get all nodes for detailed analysis
	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		return nil, fmt.Errorf("failed to list all nodes: %w", err)
	}

	// Calculate additional statistics
	var totalLoad, totalMemory, totalDisk float64
	var healthyNodes int
	nodeDistribution := make(map[string]int) // region -> count

	for _, nodeInfo := range allNodes {
		// Count by region (simplified - using status as region for now)
		region := string(nodeInfo.Status)
		nodeDistribution[region]++

		// Get health for active nodes
		if nodeInfo.Status == node.StatusActive {
			health, err := s.nodeService.CheckNodeHealth(ctx, nodeInfo.ID)
			if err != nil {
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

	// Calculate averages
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

	s.logger.Debug("node statistics retrieved",
		slog.Int("total_nodes", statistics.TotalNodes),
		slog.Int("active_nodes", statistics.ActiveNodes),
		slog.Int("unhealthy_nodes", statistics.UnhealthyNodes),
		slog.Float64("average_load", statistics.AverageLoad))

	return statistics, nil
}

// GetPeerStatistics retrieves detailed peer statistics
func (s *adminService) GetPeerStatistics(ctx context.Context) (*api.PeerStatsResponse, error) {
	s.logger.Debug("retrieving peer statistics")

	// Get basic peer statistics from domain service
	domainStats, err := s.peerService.GetStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer statistics: %w", err)
	}

	// Get all active nodes to build distribution
	activeStatus4 := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus4,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	// Build peer distribution across nodes
	peerDistribution := make(map[string]int)

	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to get peer count for node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
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

	s.logger.Debug("peer statistics retrieved",
		slog.Int("total_peers", statistics.TotalPeers),
		slog.Int("active_nodes", statistics.ActiveNodes))

	return statistics, nil
}

// ForceNodeRotation forces an immediate node rotation regardless of health status
func (s *adminService) ForceNodeRotation(ctx context.Context) error {
	s.logger.Info("forcing node rotation")

	// Use the VPN service rotation logic
	if err := s.vpnService.RotateNodes(ctx); err != nil {
		return fmt.Errorf("forced node rotation failed: %w", err)
	}

	s.logger.Info("forced node rotation completed")
	return nil
}

// ManualCleanup performs manual cleanup with specified options
func (s *adminService) ManualCleanup(ctx context.Context, options CleanupOptions) (*CleanupResult, error) {
	s.logger.Info("performing manual cleanup",
		slog.Bool("inactive_peers", options.InactivePeers),
		slog.Bool("orphaned_nodes", options.OrphanedNodes),
		slog.Bool("unused_subnets", options.UnusedSubnets),
		slog.Bool("dry_run", options.DryRun))

	// Use the VPN service cleanup logic
	if err := s.vpnService.CleanupInactiveResourcesWithOptions(ctx, options); err != nil {
		return nil, fmt.Errorf("manual cleanup failed: %w", err)
	}

	// Create result summary (simplified for now)
	result := &CleanupResult{
		Timestamp: time.Now(),
		Duration:  0, // Would be calculated by the cleanup operation
	}

	s.logger.Info("manual cleanup completed")
	return result, nil
}

// GetRotationStatus retrieves the current rotation status
func (s *adminService) GetRotationStatus(ctx context.Context) (*RotationStatus, error) {
	s.logger.Debug("retrieving rotation status")

	// Check if any nodes are currently in provisioning state (indicates rotation in progress)
	provisioningStatus := node.StatusProvisioning
	provisioningNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &provisioningStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check provisioning nodes: %w", err)
	}

	// For now, create a basic rotation status
	// In a full implementation, this would track actual rotation operations
	status := &RotationStatus{
		InProgress:    len(provisioningNodes) > 0,
		LastRotation:  time.Now().Add(-24 * time.Hour), // Placeholder
		NodesRotated:  0,                               // Would be tracked
		PeersMigrated: 0,                               // Would be tracked
	}

	if status.InProgress {
		status.RotationReason = "rotation in progress"
		status.EstimatedComplete = time.Now().Add(10 * time.Minute) // Estimate
	}

	s.logger.Debug("rotation status retrieved",
		slog.Bool("in_progress", status.InProgress),
		slog.Int("nodes_rotated", status.NodesRotated))

	return status, nil
}

// ValidateSystemHealth performs comprehensive system health validation
func (s *adminService) ValidateSystemHealth(ctx context.Context) (*HealthReport, error) {
	s.logger.Info("validating system health")

	report := &HealthReport{
		NodeHealth:  make(map[string]NodeHealth),
		LastChecked: time.Now(),
	}

	// Get all active nodes
	activeStatus3 := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus3,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	if len(activeNodes) == 0 {
		report.OverallHealth = "unhealthy"
		report.Issues = append(report.Issues, HealthIssue{
			Severity:  "critical",
			Component: "system",
			Message:   "no active nodes available",
			Timestamp: time.Now(),
		})
		report.Recommendations = append(report.Recommendations, "Create at least one active node")
		return report, nil
	}

	// Check health of each node
	var healthyNodes, totalNodes int
	var totalLoad, totalMemory, totalDisk, totalResponse float64

	for _, activeNode := range activeNodes {
		totalNodes++

		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			report.Issues = append(report.Issues, HealthIssue{
				Severity:    "warning",
				Component:   "node",
				ComponentID: activeNode.ID,
				Message:     fmt.Sprintf("failed to check node health: %v", err),
				Timestamp:   time.Now(),
			})
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

		// Check for issues
		if !health.IsHealthy {
			nodeHealth.Issues = append(nodeHealth.Issues, "node reported as unhealthy")
		}
		if health.SystemLoad > 0.8 {
			nodeHealth.Issues = append(nodeHealth.Issues, "high system load")
		}
		if health.MemoryUsage > 0.9 {
			nodeHealth.Issues = append(nodeHealth.Issues, "high memory usage")
		}
		if health.DiskUsage > 0.9 {
			nodeHealth.Issues = append(nodeHealth.Issues, "high disk usage")
		}
		if health.ResponseTime > 5*time.Second {
			nodeHealth.Issues = append(nodeHealth.Issues, "slow response time")
		}

		// Add to report
		report.NodeHealth[activeNode.ID] = nodeHealth

		// Accumulate metrics
		if health.IsHealthy {
			healthyNodes++
		}
		totalLoad += health.SystemLoad
		totalMemory += health.MemoryUsage
		totalDisk += health.DiskUsage
		totalResponse += float64(health.ResponseTime.Milliseconds())

		// Add issues to report
		for _, issue := range nodeHealth.Issues {
			severity := "warning"
			if health.SystemLoad > 0.9 || health.MemoryUsage > 0.95 || health.DiskUsage > 0.95 {
				severity = "critical"
			}

			report.Issues = append(report.Issues, HealthIssue{
				Severity:    severity,
				Component:   "node",
				ComponentID: activeNode.ID,
				Message:     issue,
				Timestamp:   time.Now(),
			})
		}
	}

	// Calculate system metrics
	report.SystemMetrics = SystemMetrics{
		TotalCapacity:   totalNodes * 100, // Simplified capacity calculation
		UsedCapacity:    totalNodes - healthyNodes,
		CapacityUsage:   float64(totalNodes-healthyNodes) / float64(totalNodes),
		AverageLoad:     totalLoad / float64(totalNodes),
		AverageMemory:   totalMemory / float64(totalNodes),
		AverageDisk:     totalDisk / float64(totalNodes),
		AverageResponse: int64(totalResponse / float64(totalNodes)),
	}

	// Determine overall health
	healthyPercentage := float64(healthyNodes) / float64(totalNodes)
	switch {
	case healthyPercentage >= 0.8:
		report.OverallHealth = "healthy"
	case healthyPercentage >= 0.5:
		report.OverallHealth = "degraded"
	default:
		report.OverallHealth = "unhealthy"
	}

	// Add recommendations
	if healthyPercentage < 0.8 {
		report.Recommendations = append(report.Recommendations, "Consider rotating unhealthy nodes")
	}
	if report.SystemMetrics.AverageLoad > 0.8 {
		report.Recommendations = append(report.Recommendations, "System load is high, consider adding more nodes")
	}
	if report.SystemMetrics.AverageMemory > 0.8 {
		report.Recommendations = append(report.Recommendations, "Memory usage is high across nodes")
	}

	s.logger.Info("system health validation completed",
		slog.String("overall_health", report.OverallHealth),
		slog.Int("healthy_nodes", healthyNodes),
		slog.Int("total_nodes", totalNodes),
		slog.Int("issues", len(report.Issues)))

	return report, nil
}

// GetOrphanedResourcesReport retrieves a report of orphaned resources
func (s *adminService) GetOrphanedResourcesReport(ctx context.Context) (*OrphanedResourcesReport, error) {
	s.logger.Debug("generating orphaned resources report")

	// Create a resource cleanup service to detect orphaned resources
	resourceCleanupService := NewResourceCleanupService(
		s.nodeService,
		s.peerService,
		s.ipService,
		s.logger,
	)

	report, err := resourceCleanupService.DetectOrphanedResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect orphaned resources: %w", err)
	}

	s.logger.Debug("orphaned resources report generated",
		slog.Int("inactive_peers", report.InactivePeers),
		slog.Int("orphaned_nodes", report.OrphanedNodes),
		slog.Int("unused_subnets", report.UnusedSubnets))

	return report, nil
}

// GetCapacityReport retrieves a comprehensive capacity report
func (s *adminService) GetCapacityReport(ctx context.Context) (*CapacityReport, error) {
	s.logger.Debug("generating capacity report")

	// Get all active nodes
	activeStatus5 := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus5,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	report := &CapacityReport{
		Timestamp: time.Now(),
		Nodes:     make(map[string]NodeCapacityInfo),
	}

	var totalCapacity, totalUsed int
	for _, activeNode := range activeNodes {
		// Get peer count
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			continue
		}

		// Get available IP count
		availableIPs, err := s.ipService.GetAvailableIPCount(ctx, activeNode.ID)
		if err != nil {
			continue
		}

		nodeCapacity := NodeCapacityInfo{
			NodeID:         activeNode.ID,
			MaxPeers:       availableIPs + int(peerCount), // Total capacity
			CurrentPeers:   int(peerCount),
			AvailablePeers: availableIPs,
			CapacityUsed:   float64(peerCount) / float64(availableIPs+int(peerCount)),
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

	s.logger.Debug("capacity report generated",
		slog.Int("total_capacity", report.TotalCapacity),
		slog.Int("total_used", report.TotalUsed),
		slog.Float64("overall_usage", report.OverallUsage))

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
