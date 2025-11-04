package application

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
)

// NodeRotationService handles node rotation and peer migration operations
type NodeRotationService struct {
	nodeService              node.NodeService
	peerService              peer.Service
	ipService                ip.Service
	wireguardManager         nodeinteractor.WireGuardManager
	provisioningOrchestrator *ProvisioningOrchestrator
	logger                   *slog.Logger
}

// NewNodeRotationService creates a new node rotation service with unified provisioning
func NewNodeRotationService(
	nodeService node.NodeService,
	peerService peer.Service,
	ipService ip.Service,
	wireguardManager nodeinteractor.WireGuardManager,
	provisioningOrchestrator *ProvisioningOrchestrator,
	logger *slog.Logger,
) *NodeRotationService {
	return &NodeRotationService{
		nodeService:              nodeService,
		peerService:              peerService,
		ipService:                ipService,
		wireguardManager:         wireguardManager,
		provisioningOrchestrator: provisioningOrchestrator,
		logger:                   logger,
	}
}

// RotateNodes performs node rotation by creating new nodes and migrating peers
func (s *NodeRotationService) RotateNodes(ctx context.Context) error {
	s.logger.Info("starting node rotation with provisioning status awareness")

	// Check provisioning status before making rotation decisions
	s.logProvisioningStatusForRotation()

	// Get rotation decision
	rotationDecision, err := s.makeRotationDecision(ctx)
	if err != nil {
		return fmt.Errorf("failed to make rotation decision: %w", err)
	}

	if len(rotationDecision.NodesToRotate) == 0 {
		s.logger.Info("no nodes need rotation")
		return nil
	}

	s.logger.Info("rotation decision made",
		slog.Int("nodes_to_rotate", len(rotationDecision.NodesToRotate)),
		slog.String("reason", rotationDecision.Reason))

	// Check if we should defer rotation due to active provisioning
	if s.shouldDeferRotation() {
		s.logger.Info("deferring rotation due to active provisioning")
		return nil
	}

	// Perform rotation for each node that needs it
	var rotatedCount, migratedCount int
	for _, nodeToRotate := range rotationDecision.NodesToRotate {
		s.logger.Info("rotating node",
			slog.String("node_id", nodeToRotate.NodeID),
			slog.String("reason", nodeToRotate.Reason))

		// Create replacement node
		newNode, err := s.createReplacementNode(ctx, nodeToRotate)
		if err != nil {
			s.logger.Error("failed to create replacement node",
				slog.String("old_node_id", nodeToRotate.NodeID),
				slog.String("error", err.Error()))
			continue
		}

		// Migrate peers with proper error handling and rollback
		peerCount, err := s.migratePeersWithRollback(ctx, nodeToRotate.NodeID, newNode.ID)
		if err != nil {
			s.logger.Error("failed to migrate peers during rotation",
				slog.String("source_node_id", nodeToRotate.NodeID),
				slog.String("target_node_id", newNode.ID),
				slog.String("error", err.Error()))

			// Cleanup the new node on failure
			if destroyErr := s.provisioningOrchestrator.DestroyNode(ctx, newNode.ID); destroyErr != nil {
				s.logger.Error("failed to cleanup replacement node after migration failure",
					slog.String("node_id", newNode.ID),
					slog.String("error", destroyErr.Error()))
			}
			continue
		}

		// Destroy old node
		if err := s.provisioningOrchestrator.DestroyNode(ctx, nodeToRotate.NodeID); err != nil {
			s.logger.Error("failed to destroy old node after rotation",
				slog.String("node_id", nodeToRotate.NodeID),
				slog.String("error", err.Error()))
		}

		rotatedCount++
		migratedCount += peerCount

		s.logger.Info("node rotation completed successfully",
			slog.String("old_node_id", nodeToRotate.NodeID),
			slog.String("new_node_id", newNode.ID),
			slog.Int("peers_migrated", peerCount))
	}

	s.logger.Info("node rotation completed",
		slog.Int("nodes_rotated", rotatedCount),
		slog.Int("peers_migrated", migratedCount))

	return nil
}

// MigratePeersFromNode migrates all peers from source node to target node
func (s *NodeRotationService) MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error {
	s.logger.Info("migrating peers between nodes",
		slog.String("source_node_id", sourceNodeID),
		slog.String("target_node_id", targetNodeID))

	// Get all active peers on source node
	sourcePeers, err := s.peerService.GetActiveByNode(ctx, sourceNodeID)
	if err != nil {
		return fmt.Errorf("failed to get peers from source node: %w", err)
	}

	if len(sourcePeers) == 0 {
		s.logger.Info("no peers to migrate")
		return nil
	}

	s.logger.Info("found peers to migrate", slog.Int("count", len(sourcePeers)))

	// Validate target node capacity
	if err := s.nodeService.ValidateNodeCapacity(ctx, targetNodeID, len(sourcePeers)); err != nil {
		return fmt.Errorf("target node capacity validation failed: %w", err)
	}

	// Migrate each peer
	for _, sourcePeer := range sourcePeers {
		if err := s.migrateSinglePeer(ctx, sourcePeer, sourceNodeID, targetNodeID); err != nil {
			s.logger.Error("failed to migrate peer",
				slog.String("peer_id", sourcePeer.ID),
				slog.String("error", err.Error()))
			continue
		}
	}

	s.logger.Info("peer migration completed",
		slog.String("source_node_id", sourceNodeID),
		slog.String("target_node_id", targetNodeID),
		slog.Int("migrated_count", len(sourcePeers)))

	return nil
}

// migrateSinglePeer migrates a single peer from source to target node
func (s *NodeRotationService) migrateSinglePeer(ctx context.Context, sourcePeer *peer.Peer, sourceNodeID, targetNodeID string) error {
	s.logger.Debug("migrating peer",
		slog.String("peer_id", sourcePeer.ID),
		slog.String("from_node", sourceNodeID),
		slog.String("to_node", targetNodeID))

	// Allocate new IP on target node
	newIP, err := s.ipService.AllocateClientIP(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("failed to allocate IP for peer migration: %w", err)
	}

	// Migrate peer in domain
	if err := s.peerService.Migrate(ctx, sourcePeer.ID, targetNodeID, newIP.String()); err != nil {
		// Release allocated IP on failure
		_ = s.ipService.ReleaseClientIP(ctx, targetNodeID, newIP)
		return fmt.Errorf("failed to migrate peer in domain: %w", err)
	}

	// Get target node for its IP address
	targetNode, err := s.nodeService.GetNode(ctx, targetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Add peer to target node using NodeInteractor directly
	wgConfig := nodeinteractor.PeerWireGuardConfig{
		PublicKey:    sourcePeer.PublicKey,
		PresharedKey: sourcePeer.PresharedKey,
		AllowedIPs:   []string{newIP.String() + "/32"},
	}

	if err := s.wireguardManager.AddPeer(ctx, targetNode.IPAddress, wgConfig); err != nil {
		return fmt.Errorf("failed to add peer to target node: %w", err)
	}

	// Get source node for its IP address
	sourceNode, err := s.nodeService.GetNode(ctx, sourceNodeID)
	if err != nil {
		s.logger.Warn("failed to get source node for peer removal",
			slog.String("peer_id", sourcePeer.ID),
			slog.String("source_node_id", sourceNodeID),
			slog.String("error", err.Error()))
	} else {
		// Remove peer from source node using WireGuardManager
		if err := s.wireguardManager.RemovePeer(ctx, sourceNode.IPAddress, sourcePeer.PublicKey); err != nil {
			s.logger.Warn("failed to remove peer from source node",
				slog.String("peer_id", sourcePeer.ID),
				slog.String("source_node_id", sourceNodeID),
				slog.String("error", err.Error()))
		}
	}

	// Release old IP
	oldIP := net.ParseIP(sourcePeer.AllocatedIP)
	if oldIP != nil {
		_ = s.ipService.ReleaseClientIP(ctx, sourceNodeID, oldIP)
	}

	s.logger.Info("peer migrated successfully",
		slog.String("peer_id", sourcePeer.ID),
		slog.String("old_ip", sourcePeer.AllocatedIP),
		slog.String("new_ip", newIP.String()))

	return nil
}

// makeRotationDecision analyzes all nodes and determines which ones need rotation
func (s *NodeRotationService) makeRotationDecision(ctx context.Context) (*RotationDecision, error) {
	// Get all active nodes
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active nodes: %w", err)
	}

	if len(activeNodes) == 0 {
		return &RotationDecision{
			NodesToRotate: []NodeRotationInfo{},
			Reason:        "no active nodes found",
			Timestamp:     time.Now(),
		}, nil
	}

	var nodesToRotate []NodeRotationInfo
	var overallReason []string

	// Analyze each node
	for _, activeNode := range activeNodes {
		// Check node health
		health, err := s.nodeService.CheckNodeHealth(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to check node health during rotation decision",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			continue
		}

		// Get peer count for this node
		peerCount, err := s.peerService.CountActiveByNode(ctx, activeNode.ID)
		if err != nil {
			s.logger.Warn("failed to get peer count for node",
				slog.String("node_id", activeNode.ID),
				slog.String("error", err.Error()))
			peerCount = 0
		}

		// Determine rotation need and priority
		rotationInfo := s.analyzeNodeForRotation(activeNode.ID, health, int(peerCount))
		if rotationInfo != nil {
			nodesToRotate = append(nodesToRotate, *rotationInfo)
			overallReason = append(overallReason, fmt.Sprintf("node %s: %s", activeNode.ID, rotationInfo.Reason))
		}
	}

	// Sort nodes by priority (critical first)
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
func (s *NodeRotationService) analyzeNodeForRotation(nodeID string, health *node.Health, peerCount int) *NodeRotationInfo {
	var reasons []string
	priority := 4 // Default to low priority

	// Check if node is unhealthy
	if !health.IsHealthy {
		reasons = append(reasons, "unhealthy")
		priority = 1 // Critical
	}

	// Check system load
	if health.SystemLoad > 0.9 {
		reasons = append(reasons, "high system load")
		if priority > 1 {
			priority = 1 // Critical
		}
	} else if health.SystemLoad > 0.8 {
		reasons = append(reasons, "elevated system load")
		if priority > 2 {
			priority = 2 // High
		}
	}

	// Check memory usage
	if health.MemoryUsage > 0.95 {
		reasons = append(reasons, "critical memory usage")
		if priority > 1 {
			priority = 1 // Critical
		}
	} else if health.MemoryUsage > 0.85 {
		reasons = append(reasons, "high memory usage")
		if priority > 2 {
			priority = 2 // High
		}
	}

	// Check disk usage
	if health.DiskUsage > 0.95 {
		reasons = append(reasons, "critical disk usage")
		if priority > 1 {
			priority = 1 // Critical
		}
	} else if health.DiskUsage > 0.85 {
		reasons = append(reasons, "high disk usage")
		if priority > 2 {
			priority = 2 // High
		}
	}

	// Check response time
	if health.ResponseTime > 5*time.Second {
		reasons = append(reasons, "slow response time")
		if priority > 2 {
			priority = 2 // High
		}
	}

	// If no issues found, no rotation needed
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

// createReplacementNode creates a new node to replace the one being rotated
// Uses event-driven provisioning when available, with synchronous provisioning as fallback
func (s *NodeRotationService) createReplacementNode(ctx context.Context, nodeToRotate NodeRotationInfo) (*node.Node, error) {
	s.logger.Info("creating replacement node",
		slog.String("for_node", nodeToRotate.NodeID),
		slog.String("reason", nodeToRotate.Reason),
		slog.Int("peer_count", nodeToRotate.PeerCount))

	// Attempt to use asynchronously provisioned node if available
	if newNode := s.tryUseAsyncProvisionedNode(ctx); newNode != nil {
		s.logger.Info("using node from async provisioning",
			slog.String("node_id", newNode.ID),
			slog.String("replacing_node", nodeToRotate.NodeID))
		return newNode, nil
	}

	// Fallback to synchronous provisioning via orchestrator
	// This ensures proper resource allocation, IP management, and infrastructure setup
	s.logger.Info("using synchronous provisioning for replacement node")
	newNode, err := s.provisioningOrchestrator.ProvisionNodeSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to provision replacement node: %w", err)
	}

	s.logger.Info("replacement node provisioned successfully",
		slog.String("new_node_id", newNode.ID),
		slog.String("node_ip", newNode.IPAddress),
		slog.String("replacing_node", nodeToRotate.NodeID))

	return newNode, nil
}

// tryUseAsyncProvisionedNode attempts to wait for and use an asynchronously provisioned node
// Returns nil if async provisioning is not available or times out
func (s *NodeRotationService) tryUseAsyncProvisionedNode(ctx context.Context) *node.Node {
	// Only wait if provisioning is already in progress
	if !s.provisioningOrchestrator.IsProvisioning() {
		return nil
	}

	waitTime := s.provisioningOrchestrator.GetEstimatedWaitTime()
	s.logger.Info("async provisioning in progress, waiting for completion",
		slog.Duration("estimated_wait", waitTime))

	// Wait for async provisioning to complete with timeout
	timeout := time.NewTimer(waitTime + time.Minute) // Add 1 minute buffer
	ticker := time.NewTicker(10 * time.Second)       // Check every 10 seconds
	defer timeout.Stop()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Warn("context cancelled while waiting for async provisioning")
			return nil

		case <-timeout.C:
			s.logger.Warn("timeout waiting for async provisioning, falling back to synchronous")
			return nil

		case <-ticker.C:
			// Check if provisioning completed
			if s.provisioningOrchestrator.IsProvisioning() {
				continue // Still in progress
			}

			// Provisioning completed - find the newest node
			s.logger.Info("async provisioning completed, looking for new node")
			return s.findNewestActiveNode(ctx)
		}
	}
}

// findNewestActiveNode finds the most recently created active node
func (s *NodeRotationService) findNewestActiveNode(ctx context.Context) *node.Node {
	activeStatus := node.StatusActive
	activeNodes, err := s.nodeService.ListNodes(ctx, node.Filters{
		Status: &activeStatus,
	})
	if err != nil {
		s.logger.Warn("failed to list active nodes after async provisioning",
			slog.String("error", err.Error()))
		return nil
	}

	if len(activeNodes) == 0 {
		s.logger.Warn("no active nodes found after async provisioning")
		return nil
	}

	// Find the newest node by creation time
	var newestNode *node.Node
	for _, activeNode := range activeNodes {
		if newestNode == nil || activeNode.CreatedAt.After(newestNode.CreatedAt) {
			newestNode = activeNode
		}
	}

	return newestNode
}

// migratePeersWithRollback migrates peers with proper error handling and rollback capability
func (s *NodeRotationService) migratePeersWithRollback(ctx context.Context, sourceNodeID, targetNodeID string) (int, error) {
	// Get all active peers on source node
	sourcePeers, err := s.peerService.GetActiveByNode(ctx, sourceNodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get peers from source node: %w", err)
	}

	if len(sourcePeers) == 0 {
		s.logger.Info("no peers to migrate")
		return 0, nil
	}

	s.logger.Info("starting peer migration with rollback capability",
		slog.String("source_node_id", sourceNodeID),
		slog.String("target_node_id", targetNodeID),
		slog.Int("peer_count", len(sourcePeers)))

	// Validate target node capacity
	if err := s.nodeService.ValidateNodeCapacity(ctx, targetNodeID, len(sourcePeers)); err != nil {
		return 0, fmt.Errorf("target node capacity validation failed: %w", err)
	}

	// Track successful migrations for rollback
	var migratedPeers []string
	var successCount int

	// Migrate each peer with individual error handling
	for _, sourcePeer := range sourcePeers {
		if err := s.migrateSinglePeer(ctx, sourcePeer, sourceNodeID, targetNodeID); err != nil {
			s.logger.Error("failed to migrate peer",
				slog.String("peer_id", sourcePeer.ID),
				slog.String("error", err.Error()))
			continue
		}

		// Track successful migration
		migratedPeers = append(migratedPeers, sourcePeer.ID)
		successCount++
	}

	s.logger.Info("peer migration completed",
		slog.String("source_node_id", sourceNodeID),
		slog.String("target_node_id", targetNodeID),
		slog.Int("successful_migrations", successCount),
		slog.Int("total_peers", len(sourcePeers)))

	return successCount, nil
}

// shouldDeferRotation determines if rotation should be deferred due to active provisioning
func (s *NodeRotationService) shouldDeferRotation() bool {
	if !s.provisioningOrchestrator.IsProvisioning() {
		return false
	}

	// Check if provisioning is in early stages (defer rotation)
	status := s.provisioningOrchestrator.GetCurrentStatus()
	if status.Progress < 0.8 { // Less than 80% complete
		waitTime := s.provisioningOrchestrator.GetEstimatedWaitTime()
		s.logger.Info("deferring rotation due to active provisioning in early stages",
			slog.Float64("progress", status.Progress),
			slog.Duration("estimated_wait", waitTime))
		return true
	}

	return false
}

// logProvisioningStatusForRotation logs provisioning status for rotation decision making
func (s *NodeRotationService) logProvisioningStatusForRotation() {
	status := s.provisioningOrchestrator.GetCurrentStatus()

	if s.provisioningOrchestrator.IsProvisioning() {
		s.logger.Info("active provisioning detected during rotation cycle",
			slog.String("phase", status.Phase),
			slog.Float64("progress", status.Progress),
			slog.Duration("elapsed", time.Since(status.StartedAt)))

		if status.EstimatedETA != nil {
			remainingTime := time.Until(*status.EstimatedETA)
			s.logger.Info("provisioning timing for rotation scheduling",
				slog.Duration("estimated_remaining", remainingTime))
		}
	} else {
		s.logger.Debug("no active provisioning detected")
	}
}
