package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
)

// ResourceCleanupService handles cleanup of inactive resources
type ResourceCleanupService struct {
	nodeService node.NodeService
	peerService peer.Service
	ipService   ip.Service
	logger      *slog.Logger
}

// NewResourceCleanupService creates a new resource cleanup service
func NewResourceCleanupService(nodeService node.NodeService, peerService peer.Service, ipService ip.Service, logger *slog.Logger) *ResourceCleanupService {
	return &ResourceCleanupService{
		nodeService: nodeService,
		peerService: peerService,
		ipService:   ipService,
		logger:      logger,
	}
}

// CleanupInactiveResources cleans up inactive peers and orphaned resources
func (s *ResourceCleanupService) CleanupInactiveResources(ctx context.Context) error {
	return s.CleanupInactiveResourcesWithOptions(ctx, CleanupOptions{
		InactivePeers:   true,
		OrphanedNodes:   true,
		UnusedSubnets:   true,
		InactiveMinutes: 30,
		DryRun:          false,
	})
}

// CleanupInactiveResourcesWithOptions performs comprehensive cleanup with configurable options
func (s *ResourceCleanupService) CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error {
	startTime := time.Now()
	s.logger.Info("starting comprehensive cleanup of inactive resources",
		slog.Bool("inactive_peers", options.InactivePeers),
		slog.Bool("orphaned_nodes", options.OrphanedNodes),
		slog.Bool("unused_subnets", options.UnusedSubnets),
		slog.Int("inactive_minutes", options.InactiveMinutes),
		slog.Bool("dry_run", options.DryRun))

	result := &CleanupResult{
		Timestamp: startTime,
	}

	// Clean up inactive peers
	if options.InactivePeers {
		peersRemoved, err := s.cleanupInactivePeers(ctx, options.InactiveMinutes, options.DryRun)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("inactive peers cleanup failed: %v", err))
		} else {
			result.PeersRemoved = peersRemoved
		}
	}

	// Clean up orphaned nodes
	if options.OrphanedNodes {
		nodesDestroyed, err := s.cleanupOrphanedNodes(ctx, options.DryRun)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("orphaned nodes cleanup failed: %v", err))
		} else {
			result.NodesDestroyed = nodesDestroyed
		}
	}

	// Clean up unused subnets
	if options.UnusedSubnets {
		subnetsReleased, err := s.cleanupUnusedSubnets(ctx, options.DryRun)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("unused subnets cleanup failed: %v", err))
		} else {
			result.SubnetsReleased = subnetsReleased
		}
	}

	result.Duration = time.Since(startTime).Milliseconds()

	s.logger.Info("cleanup of inactive resources completed",
		slog.Int("peers_removed", result.PeersRemoved),
		slog.Int("nodes_destroyed", result.NodesDestroyed),
		slog.Int("subnets_released", result.SubnetsReleased),
		slog.Int("errors", len(result.Errors)),
		slog.Int64("duration_ms", result.Duration),
		slog.Bool("dry_run", options.DryRun))

	if len(result.Errors) > 0 {
		return fmt.Errorf("cleanup completed with %d errors: %v", len(result.Errors), result.Errors)
	}

	return nil
}

// DetectOrphanedResources identifies resources that may need cleanup
func (s *ResourceCleanupService) DetectOrphanedResources(ctx context.Context) (*OrphanedResourcesReport, error) {
	s.logger.Info("detecting orphaned resources")

	report := &OrphanedResourcesReport{
		Timestamp: time.Now(),
	}

	// Detect inactive peers
	inactivePeers, err := s.peerService.GetInactive(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("failed to detect inactive peers: %w", err)
	}
	report.InactivePeers = len(inactivePeers)

	// Detect orphaned nodes
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	var orphanedNodeCount int
	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			continue
		}
		if peerCount == 0 && time.Since(activeNode.UpdatedAt) > time.Hour {
			orphanedNodeCount++
		}
	}
	report.OrphanedNodes = orphanedNodeCount

	// Detect unused subnets (simplified detection)
	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		return nil, fmt.Errorf("failed to list all nodes: %w", err)
	}

	activeNodeIDs := make(map[string]bool)
	for _, activeNode := range activeNodes {
		activeNodeIDs[activeNode.ID] = true
	}

	var unusedSubnetCount int
	for _, nodeInfo := range allNodes {
		if !activeNodeIDs[nodeInfo.ID] {
			if _, err := s.ipService.GetNodeSubnet(ctx, nodeInfo.ID); err == nil {
				unusedSubnetCount++
			}
		}
	}
	report.UnusedSubnets = unusedSubnetCount

	s.logger.Info("orphaned resources detected",
		slog.Int("inactive_peers", report.InactivePeers),
		slog.Int("orphaned_nodes", report.OrphanedNodes),
		slog.Int("unused_subnets", report.UnusedSubnets))

	return report, nil
}

// cleanupInactivePeers removes peers that have been inactive for the specified duration
func (s *ResourceCleanupService) cleanupInactivePeers(ctx context.Context, inactiveMinutes int, dryRun bool) (int, error) {
	s.logger.Info("cleaning up inactive peers",
		slog.Int("inactive_minutes", inactiveMinutes),
		slog.Bool("dry_run", dryRun))

	// Get inactive peers
	inactivePeers, err := s.peerService.GetInactive(ctx, inactiveMinutes)
	if err != nil {
		return 0, fmt.Errorf("failed to get inactive peers: %w", err)
	}

	if len(inactivePeers) == 0 {
		s.logger.Info("no inactive peers found")
		return 0, nil
	}

	s.logger.Info("found inactive peers to cleanup",
		slog.Int("count", len(inactivePeers)))

	var removedCount int
	for _, inactivePeer := range inactivePeers {
		inactiveDuration := time.Since(inactivePeer.UpdatedAt)
		s.logger.Info("processing inactive peer",
			slog.String("peer_id", inactivePeer.ID),
			slog.String("last_seen", inactivePeer.UpdatedAt.Format(time.RFC3339)),
			slog.Float64("inactive_hours", inactiveDuration.Hours()),
			slog.Bool("dry_run", dryRun))

		if !dryRun {
			// Use peer connection service to disconnect peer
			// This would require injecting the peer connection service
			if err := s.peerService.Remove(ctx, inactivePeer.ID); err != nil {
				s.logger.Error("failed to cleanup inactive peer",
					slog.String("peer_id", inactivePeer.ID),
					slog.String("error", err.Error()))
				continue
			}
		}

		removedCount++
		s.logger.Info("inactive peer cleaned up",
			slog.String("peer_id", inactivePeer.ID),
			slog.Bool("dry_run", dryRun))
	}

	return removedCount, nil
}

// cleanupOrphanedNodes removes nodes that have no active peers and are not needed
func (s *ResourceCleanupService) cleanupOrphanedNodes(ctx context.Context, dryRun bool) (int, error) {
	s.logger.Info("cleaning up orphaned nodes", slog.Bool("dry_run", dryRun))

	// Get all active nodes
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list active nodes: %w", err)
	}

	var orphanedNodes []*node.Node
	var totalActiveNodes int

	// Check each node for orphaned status
	for _, activeNode := range activeNodes {
		// Count active peers on this node
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to count peers for node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			continue
		}

		if peerCount == 0 {
			// Check if node has been empty for a reasonable time (e.g., 1 hour)
			if time.Since(activeNode.UpdatedAt) > time.Hour {
				orphanedNodes = append(orphanedNodes, activeNode)
			}
		} else {
			totalActiveNodes++
		}
	}

	// Always keep at least one active node
	if totalActiveNodes <= 1 && len(orphanedNodes) > 0 {
		s.logger.Info("keeping last active node, skipping orphaned node cleanup")
		return 0, nil
	}

	if len(orphanedNodes) == 0 {
		s.logger.Info("no orphaned nodes found")
		return 0, nil
	}

	s.logger.Info("found orphaned nodes to cleanup",
		slog.Int("count", len(orphanedNodes)),
		slog.Int("total_active_nodes", totalActiveNodes))

	var destroyedCount int
	for _, orphanedNode := range orphanedNodes {
		emptyDuration := time.Since(orphanedNode.UpdatedAt)
		s.logger.Info("processing orphaned node",
			slog.String("node_id", orphanedNode.ID),
			slog.String("node_ip", orphanedNode.IPAddress),
			slog.Float64("empty_hours", emptyDuration.Hours()),
			slog.Bool("dry_run", dryRun))

		if !dryRun {
			if err := s.nodeService.DestroyNode(ctx, orphanedNode.ID); err != nil {
				s.logger.Error("failed to destroy orphaned node",
					slog.String("node_id", orphanedNode.ID),
					slog.String("error", err.Error()))
				continue
			}
		}

		destroyedCount++
		s.logger.Info("orphaned node cleaned up",
			slog.String("node_id", orphanedNode.ID),
			slog.Bool("dry_run", dryRun))
	}

	return destroyedCount, nil
}

// cleanupUnusedSubnets releases subnets that are no longer associated with active nodes
func (s *ResourceCleanupService) cleanupUnusedSubnets(ctx context.Context, dryRun bool) (int, error) {
	s.logger.Info("cleaning up unused subnets", slog.Bool("dry_run", dryRun))

	// Get all active nodes to determine which subnets should exist
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list active nodes: %w", err)
	}

	// Create map of active node IDs
	activeNodeIDs := make(map[string]bool)
	for _, activeNode := range activeNodes {
		activeNodeIDs[activeNode.ID] = true
	}

	// Get all nodes (including inactive ones) that might have subnets
	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		return 0, fmt.Errorf("failed to list all nodes: %w", err)
	}

	var unusedSubnets []string
	for _, nodeInfo := range allNodes {
		// Skip active nodes
		if activeNodeIDs[nodeInfo.ID] {
			continue
		}

		// Check if this inactive node has a subnet
		_, err := s.ipService.GetNodeSubnet(ctx, nodeInfo.ID)
		if err == nil {
			// Subnet exists for inactive node
			unusedSubnets = append(unusedSubnets, nodeInfo.ID)
		}
	}

	if len(unusedSubnets) == 0 {
		s.logger.Info("no unused subnets found")
		return 0, nil
	}

	s.logger.Info("found unused subnets to cleanup",
		slog.Int("count", len(unusedSubnets)))

	var releasedCount int
	for _, nodeID := range unusedSubnets {
		s.logger.Info("processing unused subnet",
			slog.String("node_id", nodeID),
			slog.Bool("dry_run", dryRun))

		if !dryRun {
			if err := s.ipService.ReleaseNodeSubnet(ctx, nodeID); err != nil {
				s.logger.Error("failed to release unused subnet",
					slog.String("node_id", nodeID),
					slog.String("error", err.Error()))
				continue
			}
		}

		releasedCount++
		s.logger.Info("unused subnet cleaned up",
			slog.String("node_id", nodeID),
			slog.Bool("dry_run", dryRun))
	}

	return releasedCount, nil
}
