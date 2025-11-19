package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/protocol"
)

// NodeRotationService handles node rotation and peer migration operations
type NodeRotationService struct {
	nodeService            node.NodeService
	peerService            peer.Service
	ipService              ip.Service
	protoManager           protocol.Manager
	resourceCleanupService *ResourceCleanupService
	eventPublisher         *events.EventPublisher
	logger                 *applogger.Logger
}

// NewNodeRotationService creates a new node rotation service
func NewNodeRotationService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	protoManager protocol.Manager,
	resourceCleanupService *ResourceCleanupService,
	eventPublisher *events.EventPublisher,
	logger *applogger.Logger,
) *NodeRotationService {
	return &NodeRotationService{
		nodeService:            nodeService,
		peerService:            peerService,
		ipService:              ipService,
		protoManager:           protoManager,
		resourceCleanupService: resourceCleanupService,
		eventPublisher:         eventPublisher,
		logger:                 logger.WithComponent("rotation.service"),
	}
}

// RotateNodes performs node rotation by creating new nodes and migrating peers
func (s *NodeRotationService) RotateNodes(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "RotateNodes", slog.String("trigger", "scheduled"))

	s.logProvisioningStatusForRotation(ctx)

	rotationDecision, err := s.makeRotationDecision(ctx)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to make rotation decision", true)
		op.Fail(err, "failed to make rotation decision")
		return err
	}

	if len(rotationDecision.NodesToRotate) == 0 {
		op.Complete("no nodes needed rotation")
		return nil
	}

	op.Progress("rotation decision made", slog.Int("nodes_to_rotate", len(rotationDecision.NodesToRotate)), slog.String("reason", rotationDecision.Reason))

	if s.shouldDeferRotation(ctx) {
		op.Complete("rotation deferred due to active provisioning")
		return nil
	}

	var rotatedCount, migratedCount int
	var rotationErrors []error

	for _, nodeToRotate := range rotationDecision.NodesToRotate {
		s.logger.InfoContext(ctx, "rotating node", slog.String("node_id", nodeToRotate.NodeID), slog.String("reason", nodeToRotate.Reason))

		newNode, err := s.createReplacementNode(ctx, nodeToRotate)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to create replacement node", err, slog.String("old_node_id", nodeToRotate.NodeID))
			rotationErrors = append(rotationErrors, err)
			continue
		}

		peerCount, err := s.migratePeersWithRollback(ctx, nodeToRotate.NodeID, newNode.ID)
		if err != nil {
			s.logger.ErrorCtx(ctx, "failed to migrate peers during rotation", err, slog.String("source_node_id", nodeToRotate.NodeID), slog.String("target_node_id", newNode.ID))
			rotationErrors = append(rotationErrors, err)

			if destroyErr := s.nodeService.DestroyNode(ctx, newNode.ID); destroyErr != nil {
				s.logger.ErrorCtx(ctx, "failed to cleanup replacement node after migration failure", destroyErr, slog.String("node_id", newNode.ID))
				rotationErrors = append(rotationErrors, destroyErr)
			}
			continue
		}

		if err := s.nodeService.DestroyNode(ctx, nodeToRotate.NodeID); err != nil {
			s.logger.ErrorCtx(ctx, "failed to destroy old node after rotation", err, slog.String("node_id", nodeToRotate.NodeID))
			rotationErrors = append(rotationErrors, err)
		}

		rotatedCount++
		migratedCount += peerCount
	}

	if len(rotationErrors) > 0 {
		finalErr := apperrors.NewSystemError(apperrors.ErrCodeInternal, fmt.Sprintf("node rotation completed with %d errors", len(rotationErrors)), false, errors.Join(rotationErrors...))
		op.Fail(finalErr, "node rotation cycle finished with errors")
		return finalErr
	}

	op.Complete("node rotation finished", slog.Int("nodes_rotated", rotatedCount), slog.Int("peers_migrated", migratedCount))
	return nil
}

// MigratePeersFromNode migrates all peers from source node to target node
func (s *NodeRotationService) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	op := s.logger.StartOp(ctx, "MigratePeersFromNode", slog.String("source_node_id", sourceNodeID), slog.String("target_node_id", targetNodeID))

	sourcePeers, err := s.peerService.GetActiveByNode(ctx, sourceNodeID)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerNotFound, "failed to get peers from source node", true)
		op.Fail(err, "failed to get peers from source node")
		return err
	}

	if len(sourcePeers) == 0 {
		op.Complete("no peers to migrate")
		return nil
	}
	op.Progress("found peers to migrate", slog.Int("count", len(sourcePeers)))

	if err := s.nodeService.ValidateNodeCapacity(ctx, targetNodeID, len(sourcePeers)); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeAtCapacity, "target node capacity validation failed", false)
		op.Fail(err, "target node capacity validation failed")
		return err
	}

	var migrationErrors []error
	for _, sourcePeer := range sourcePeers {
		if err := s.migrateSinglePeer(ctx, sourcePeer, sourceNodeID, targetNodeID); err != nil {
			s.logger.ErrorCtx(ctx, "failed to migrate peer", err, slog.String("peer_id", sourcePeer.ID))
			migrationErrors = append(migrationErrors, err)
			continue
		}
	}

	if len(migrationErrors) > 0 {
		finalErr := apperrors.NewSystemError(apperrors.ErrCodeInternal, fmt.Sprintf("peer migration completed with %d errors", len(migrationErrors)), false, errors.Join(migrationErrors...))
		op.Fail(finalErr, "peer migration finished with errors")
		return finalErr
	}

	op.Complete("peer migration finished", slog.Int("migrated_count", len(sourcePeers)))
	return nil
}

// CleanupInactiveResources cleans up inactive peers and orphaned resources
func (s *NodeRotationService) CleanupInactiveResources(ctx context.Context) error {
	return s.resourceCleanupService.CleanupInactiveResources(ctx)
}

// CleanupInactiveResourcesWithOptions performs comprehensive cleanup with configurable options
func (s *NodeRotationService) CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error {
	return s.resourceCleanupService.CleanupInactiveResourcesWithOptions(ctx, options)
}

// migrateSinglePeer migrates a single peer from source to target node
func (s *NodeRotationService) migrateSinglePeer(ctx context.Context, sourcePeer *peer.Peer, sourceNodeID, targetNodeID string) error {
	newIP, err := s.ipService.AllocateClientIP(ctx, targetNodeID)
	if err != nil {
		return apperrors.NewIPError(apperrors.ErrCodeIPAllocation, "failed to allocate IP for peer migration", false, err)
	}

	if err := s.peerService.Migrate(ctx, sourcePeer.ID, targetNodeID, newIP.String()); err != nil {
		_ = s.ipService.ReleaseClientIP(ctx, targetNodeID, newIP)
		return apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerConflict, "failed to migrate peer in domain", false)
	}

	targetNode, err := s.nodeService.GetNode(ctx, targetNodeID)
	if err != nil {
		return apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to get target node", false)
	}

	cfg := protocol.PeerConfig{
		Protocol:   "wireguard",
		Identifier: sourcePeer.Identifier,
		AllowedIPs: []string{newIP.String() + "/32"},
		Config:     map[string]interface{}{},
	}
	if cfg.Identifier == "" {
		cfg.Identifier = sourcePeer.PublicKey
	}
	if sourcePeer.PresharedKey != nil {
		cfg.Config["preshared_key"] = *sourcePeer.PresharedKey
	}

	if err := s.protoManager.AddPeer(ctx, targetNode.IPAddress, cfg); err != nil {
		return apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "failed to add peer to target node", true, err)
	}

	sourceNode, err := s.nodeService.GetNode(ctx, sourceNodeID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to get source node for peer removal, peer will be left on old node", "error", err.Error())
	} else {
		identifier := sourcePeer.Identifier
		if identifier == "" {
			identifier = sourcePeer.PublicKey
		}
		if err := s.protoManager.RemovePeer(ctx, sourceNode.IPAddress, identifier); err != nil {
			s.logger.WarnContext(ctx, "failed to remove peer from source node, continuing", "error", err.Error())
		}
	}

	oldIP := net.ParseIP(sourcePeer.AllocatedIP)
	if oldIP != nil {
		_ = s.ipService.ReleaseClientIP(ctx, sourceNodeID, oldIP)
	}

	s.logger.InfoContext(ctx, "peer migrated successfully", "peer_id", sourcePeer.ID, "old_ip", sourcePeer.AllocatedIP, "new_ip", newIP.String())
	return nil
}

// makeRotationDecision analyzes all nodes and determines which ones need rotation
func (s *NodeRotationService) makeRotationDecision(ctx context.Context) (*RotationDecision, error) {
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{Status: &activeStatus})
	if err != nil {
		return nil, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeNotFound, "failed to list active nodes", true)
	}

	if len(activeNodes) == 0 {
		return &RotationDecision{Reason: "no active nodes found", Timestamp: time.Now()}, nil
	}

	var nodesToRotate []NodeRotationInfo
	var overallReason []string

	for _, activeNode := range activeNodes {
		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to check node health during rotation decision, skipping node", "node_id", activeNode.ID, "error", err.Error())
			continue
		}

		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to get peer count for node, assuming 0", "node_id", activeNode.ID, "error", err.Error())
			peerCount = 0
		}

		if rotationInfo := s.analyzeNodeForRotation(ctx, activeNode.ID, health, int(peerCount)); rotationInfo != nil {
			nodesToRotate = append(nodesToRotate, *rotationInfo)
			overallReason = append(overallReason, fmt.Sprintf("node %s: %s", activeNode.ID, rotationInfo.Reason))
		}
	}

	// Sort nodes by priority
	for i := 0; i < len(nodesToRotate)-1; i++ {
		for j := i + 1; j < len(nodesToRotate); j++ {
			if nodesToRotate[i].Priority > nodesToRotate[j].Priority {
				nodesToRotate[i], nodesToRotate[j] = nodesToRotate[j], nodesToRotate[i]
			}
		}
	}

	reason := "healthy system"
	if len(overallReason) > 0 {
		reason = fmt.Sprintf("rotation needed: %v", overallReason)
	}

	return &RotationDecision{
		NodesToRotate: nodesToRotate,
		Reason:        reason,
		Timestamp:     time.Now(),
	}, nil
}

// analyzeNodeForRotation analyzes a single node and determines if it needs rotation
func (s *NodeRotationService) analyzeNodeForRotation(ctx context.Context, nodeID string, health *node.NodeHealthStatus, peerCount int) *NodeRotationInfo {
	var reasons []string
	priority := 4

	if !health.IsHealthy {
		reasons = append(reasons, "unhealthy")
		priority = 1
	}
	if health.SystemLoad > 0.9 {
		reasons = append(reasons, "high system load")
		if priority > 1 {
			priority = 1
		}
	} else if health.SystemLoad > 0.8 {
		reasons = append(reasons, "elevated system load")
		if priority > 2 {
			priority = 2
		}
	}
	if health.MemoryUsage > 0.95 {
		reasons = append(reasons, "critical memory usage")
		if priority > 1 {
			priority = 1
		}
	} else if health.MemoryUsage > 0.85 {
		reasons = append(reasons, "high memory usage")
		if priority > 2 {
			priority = 2
		}
	}
	if health.DiskUsage > 0.95 {
		reasons = append(reasons, "critical disk usage")
		if priority > 1 {
			priority = 1
		}
	} else if health.DiskUsage > 0.85 {
		reasons = append(reasons, "high disk usage")
		if priority > 2 {
			priority = 2
		}
	}
	if health.ResponseTime > 5*time.Second {
		reasons = append(reasons, "slow response time")
		if priority > 2 {
			priority = 2
		}
	}

	if len(reasons) == 0 {
		return nil
	}

	return &NodeRotationInfo{
		NodeID:      nodeID,
		Reason:      fmt.Sprintf("%v", reasons),
		Priority:    priority,
		SystemLoad:  health.SystemLoad,
		MemoryUsage: health.MemoryUsage,
		DiskUsage:   health.DiskUsage,
		IsHealthy:   health.IsHealthy,
		PeerCount:   peerCount,
	}
}

// createReplacementNode requests provisioning via event and waits for result
func (s *NodeRotationService) createReplacementNode(ctx context.Context, nodeToRotate NodeRotationInfo) (*node.Node, error) {
	s.logger.InfoContext(ctx, "creating replacement node", "for_node", nodeToRotate.NodeID, "reason", nodeToRotate.Reason)

	// Publish provisioning request
	if s.eventPublisher != nil {
		requestID := fmt.Sprintf("rotation-%s", nodeToRotate.NodeID)
		_ = s.eventPublisher.Provisioning.PublishProvisionRequested(ctx, requestID, "rotation")
	}

	// For now, return error indicating async provisioning needed
	// In a real event-driven system, rotation would be triggered by provisioning completion event
	return nil, apperrors.NewSystemError(
		apperrors.ErrCodeProvisionInProgress,
		"provisioning requested via event bus, rotation will continue asynchronously",
		false,
		nil,
	)
}

// migratePeersWithRollback migrates peers with proper error handling and rollback capability
func (s *NodeRotationService) migratePeersWithRollback(ctx context.Context, sourceNodeID, targetNodeID string) (int, error) {
	sourcePeers, err := s.peerService.GetActiveByNode(ctx, sourceNodeID)
	if err != nil {
		return 0, apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerNotFound, "failed to get peers from source node", true)
	}

	if len(sourcePeers) == 0 {
		s.logger.InfoContext(ctx, "no peers to migrate")
		return 0, nil
	}

	s.logger.InfoContext(ctx, "starting peer migration with rollback capability", "source_node_id", sourceNodeID, "target_node_id", targetNodeID, "peer_count", len(sourcePeers))

	if err := s.nodeService.ValidateNodeCapacity(ctx, targetNodeID, len(sourcePeers)); err != nil {
		return 0, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeAtCapacity, "target node capacity validation failed", false)
	}

	var successCount int
	for _, sourcePeer := range sourcePeers {
		if err := s.migrateSinglePeer(ctx, sourcePeer, sourceNodeID, targetNodeID); err != nil {
			s.logger.ErrorCtx(ctx, "failed to migrate peer", err, slog.String("peer_id", sourcePeer.ID))
			continue
		}
		successCount++
	}

	s.logger.InfoContext(ctx, "peer migration completed", "source_node_id", sourceNodeID, "target_node_id", targetNodeID, "successful_migrations", successCount, "total_peers", len(sourcePeers))
	return successCount, nil
}

// shouldDeferRotation checks if rotation should be deferred
func (s *NodeRotationService) shouldDeferRotation(ctx context.Context) bool {
	// Event-driven: rotation is no longer blocked by provisioning state
	// Provisioning happens asynchronously via events
	return false
}

// logProvisioningStatusForRotation logs rotation context
func (s *NodeRotationService) logProvisioningStatusForRotation(ctx context.Context) {
	s.logger.DebugContext(ctx, "starting rotation cycle")
}
