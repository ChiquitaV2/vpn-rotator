package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// ResourceCleanupService handles cleanup of inactive resources
type ResourceCleanupService struct {
	nodeService node.NodeService
	peerService peer.Service
	ipService   ip.Service
	logger      *applogger.Logger
}

// NewResourceCleanupService creates a new resource cleanup service
func NewResourceCleanupService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	logger *applogger.Logger,
) *ResourceCleanupService {
	return &ResourceCleanupService{
		nodeService: nodeService,
		peerService: peerService,
		ipService:   ipService,
		logger:      logger.WithComponent("cleanup.service"),
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
	op := s.logger.StartOp(ctx, "CleanupInactiveResources", slog.Any("options", options))

	result := &CleanupResult{Timestamp: op.StartTime}
	var cleanupErrors []error

	if options.InactivePeers {
		peersRemoved, err := s.cleanupInactivePeers(ctx, options.InactiveMinutes, options.DryRun)
		if err != nil {
			cleanupErrors = append(cleanupErrors, apperrors.WrapWithDomain(err, apperrors.DomainSystem, apperrors.ErrCodeInternal, "inactive peers cleanup failed", false))
		} else {
			result.PeersRemoved = peersRemoved
		}
	}

	if options.OrphanedNodes {
		nodesDestroyed, err := s.cleanupOrphanedNodes(ctx, options.DryRun)
		if err != nil {
			cleanupErrors = append(cleanupErrors, apperrors.WrapWithDomain(err, apperrors.DomainSystem, apperrors.ErrCodeInternal, "orphaned nodes cleanup failed", false))
		} else {
			result.NodesDestroyed = nodesDestroyed
		}
	}

	if options.UnusedSubnets {
		subnetsReleased, err := s.cleanupUnusedSubnets(ctx, options.DryRun)
		if err != nil {
			cleanupErrors = append(cleanupErrors, apperrors.WrapWithDomain(err, apperrors.DomainSystem, apperrors.ErrCodeInternal, "unused subnets cleanup failed", false))
		} else {
			result.SubnetsReleased = subnetsReleased
		}
	}

	result.Duration = time.Since(op.StartTime).Milliseconds()

	if len(cleanupErrors) > 0 {
		finalErr := apperrors.NewSystemError(apperrors.ErrCodeInternal, fmt.Sprintf("cleanup completed with %d errors", len(cleanupErrors)), false, errors.Join(cleanupErrors...))
		op.Fail(finalErr, "cleanup of inactive resources failed")
		return finalErr
	}

	op.Complete("cleanup of inactive resources finished", slog.Int("peers_removed", result.PeersRemoved), slog.Int("nodes_destroyed", result.NodesDestroyed), slog.Int("subnets_released", result.SubnetsReleased))
	return nil
}

// DetectOrphanedResources identifies resources that may need cleanup
func (s *ResourceCleanupService) DetectOrphanedResources(ctx context.Context) (*OrphanedResourcesReport, error) {
	op := s.logger.StartOp(ctx, "DetectOrphanedResources")

	report := &OrphanedResourcesReport{Timestamp: op.StartTime}

	inactivePeers, err := s.peerService.GetInactive(ctx, 30)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerNotFound, "failed to detect inactive peers", true)
		op.Fail(err, "detection failed during inactive peer check")
		return nil, err
	}
	report.InactivePeers = len(inactivePeers)

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
		op.Fail(err, "detection failed during active node list")
		return nil, err
	}

	var orphanedNodeCount int
	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to count peers for node during detection", err, slog.String("node_id", activeNode.ID))
			continue
		}
		if peerCount == 0 && time.Since(activeNode.UpdatedAt) > time.Hour {
			orphanedNodeCount++
		}
	}
	report.OrphanedNodes = orphanedNodeCount

	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list all nodes", true)
		op.Fail(err, "detection failed during all node list")
		return nil, err
	}

	activeNodeIDs := make(map[string]bool)
	for _, n := range allNodes {
		if n.IsActive() || n.Status == node.StatusProvisioning {
			activeNodeIDs[n.ID] = true
		}
	}

	var unusedSubnetCount int
	for _, nodeInfo := range allNodes {
		if !activeNodeIDs[nodeInfo.ID] {
			if _, err := s.ipService.GetNodeSubnet(ctx, nodeInfo.ID); err == nil {
				unusedSubnetCount++
			} else if !apperrors.IsErrorCode(err, apperrors.ErrCodeSubnetNotFound) {
				s.logger.ErrorCtx(ctx, "failed to check subnet for inactive node", err, slog.String("node_id", nodeInfo.ID))
			}
		}
	}
	report.UnusedSubnets = unusedSubnetCount

	op.Complete("detected orphaned resources")
	return report, nil
}

// cleanupInactivePeers removes peers that have been inactive for the specified duration
func (s *ResourceCleanupService) cleanupInactivePeers(ctx context.Context, inactiveMinutes int, dryRun bool) (int, error) {
	s.logger.InfoContext(ctx, "cleaning up inactive peers", "inactive_minutes", inactiveMinutes, "dry_run", dryRun)

	inactivePeers, err := s.peerService.GetInactive(ctx, inactiveMinutes)
	if err != nil {
		return 0, apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerNotFound, "failed to get inactive peers", true)
	}

	if len(inactivePeers) == 0 {
		s.logger.InfoContext(ctx, "no inactive peers found")
		return 0, nil
	}

	s.logger.InfoContext(ctx, "found inactive peers to cleanup", "count", len(inactivePeers))

	var removedCount int
	for _, inactivePeer := range inactivePeers {
		if !dryRun {
			if err := s.peerService.Remove(ctx, inactivePeer.ID); err != nil {
				s.logger.ErrorCtx(ctx, "failed to cleanup inactive peer", err, slog.String("peer_id", inactivePeer.ID))
				continue
			}
		}
		removedCount++
	}

	return removedCount, nil
}

// cleanupOrphanedNodes removes nodes that have no active peers and are not needed
func (s *ResourceCleanupService) cleanupOrphanedNodes(ctx context.Context, dryRun bool) (int, error) {
	s.logger.InfoContext(ctx, "cleaning up orphaned nodes", "dry_run", dryRun)

	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		return 0, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
	}

	var orphanedNodes []*node.Node
	var totalActiveNodes int

	for _, activeNode := range activeNodes {
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to count peers for node, skipping node", "node_id", activeNode.ID, "error", err.Error())
			continue
		}

		if peerCount == 0 {
			if time.Since(activeNode.UpdatedAt) > time.Hour {
				orphanedNodes = append(orphanedNodes, activeNode)
			}
		} else {
			totalActiveNodes++
		}
	}

	if totalActiveNodes <= 1 && len(orphanedNodes) > 0 {
		s.logger.InfoContext(ctx, "keeping last active node, skipping orphaned node cleanup")
		return 0, nil
	}

	if len(orphanedNodes) == 0 {
		s.logger.InfoContext(ctx, "no orphaned nodes found")
		return 0, nil
	}

	s.logger.InfoContext(ctx, "found orphaned nodes to cleanup", "count", len(orphanedNodes))

	var destroyedCount int
	for _, orphanedNode := range orphanedNodes {
		if !dryRun {
			if err := s.nodeService.DestroyNode(ctx, orphanedNode.ID); err != nil {
				s.logger.ErrorCtx(ctx, "failed to destroy orphaned node", err, slog.String("node_id", orphanedNode.ID))
				continue
			}
		}
		destroyedCount++
	}

	return destroyedCount, nil
}

// cleanupUnusedSubnets releases subnets that are no longer associated with active nodes
func (s *ResourceCleanupService) cleanupUnusedSubnets(ctx context.Context, dryRun bool) (int, error) {
	s.logger.InfoContext(ctx, "cleaning up unused subnets", "dry_run", dryRun)

	allNodes, err := s.nodeService.ListNodes(ctx, node.Filters{})
	if err != nil {
		return 0, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list all nodes", true)
	}

	activeNodeIDs := make(map[string]bool)
	for _, n := range allNodes {
		if n.IsActive() || n.Status == node.StatusProvisioning {
			activeNodeIDs[n.ID] = true
		}
	}

	var unusedSubnets []string
	for _, nodeInfo := range allNodes {
		if !activeNodeIDs[nodeInfo.ID] {
			if _, err := s.ipService.GetNodeSubnet(ctx, nodeInfo.ID); err == nil {
				unusedSubnets = append(unusedSubnets, nodeInfo.ID)
			} else if !apperrors.IsErrorCode(err, apperrors.ErrCodeSubnetNotFound) {
				s.logger.ErrorCtx(ctx, "failed to check subnet for inactive node", err, slog.String("node_id", nodeInfo.ID))
			}
		}
	}

	if len(unusedSubnets) == 0 {
		s.logger.InfoContext(ctx, "no unused subnets found")
		return 0, nil
	}

	s.logger.InfoContext(ctx, "found unused subnets to cleanup", "count", len(unusedSubnets))

	var releasedCount int
	for _, nodeID := range unusedSubnets {
		if !dryRun {
			if err := s.ipService.ReleaseNodeSubnet(ctx, nodeID); err != nil {
				s.logger.ErrorCtx(ctx, "failed to release unused subnet", err, slog.String("node_id", nodeID))
				continue
			}
		}
		releasedCount++
	}

	return releasedCount, nil
}
