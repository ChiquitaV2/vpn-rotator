package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/internal/shared/events"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	"github.com/google/uuid"
)

// Event is an alias for the shared events.Event interface
type Event = events.Event

// ConnectPeerAsync initiates an async peer connection
func (s *PeerConnectionService) ConnectPeerAsync(ctx context.Context, req ConnectRequest) (string, error) {
	op := s.logger.StartOp(ctx, "ConnectPeerAsync")

	// Generate request ID for tracking
	requestID := uuid.New().String()
	op.With(slog.String("request_id", requestID))

	// Generate keys if requested (need them for the event)
	if req.GenerateKeys {
		keyPair, err := crypto.GenerateKeyPair()
		if err != nil {
			err = apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to generate key pair", false, err)
			op.Fail(err, "key generation failed")
			return "", err
		}
		req.PublicKey = &keyPair.PublicKey
		op.Progress("keys generated")
	}

	// Validate request
	if err := s.validateConnectRequest(req); err != nil {
		op.Fail(err, "invalid connect request")
		return "", err
	}

	// Check for existing peer
	if req.PublicKey != nil {
		if _, err := s.peerService.GetByPublicKey(ctx, *req.PublicKey); err == nil {
			// Peer already exists, return request ID (no async work needed)
			op.Complete("existing peer found")
			return requestID, nil
		}
	}

	// Publish connection requested event
	if err := s.eventPublisher.Peer.PublishPeerConnectRequested(ctx, requestID, req.PublicKey, req.GenerateKeys); err != nil {
		err = apperrors.NewSystemError(apperrors.ErrCodeInternal, "failed to publish connect requested event", true, err)
		op.Fail(err, "event publication failed")
		return "", err
	}

	op.Complete("connection request published")
	return requestID, nil
}

// processConnectionRequest handles a single async connection request
func (s *PeerConnectionService) processConnectionRequest(ctx context.Context, requestID string, publicKey *string) {
	startTime := time.Now()
	op := s.logger.StartOp(ctx, "processConnectionRequest",
		slog.String("request_id", requestID),
	)
	if publicKey != nil {
		op.With(slog.String("public_key", *publicKey))
	}

	// Report initial progress
	if err := s.progressReporter.ReportProgress(ctx, requestID, "", "initializing", 0.0, "Starting connection", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report initial progress", "error", err.Error())
	}

	// Select node (10% progress)
	selectedNode, err := s.selectNodeWithProgress(ctx, requestID)
	if err != nil {
		s.handleConnectionError(ctx, requestID, "", "node_selection", err)
		op.Fail(err, "node selection failed")
		return
	}
	op.With(slog.String("node_id", selectedNode.ID))

	// Validate capacity (20% progress)
	if err := s.progressReporter.ReportProgress(ctx, requestID, "", "validating_capacity", 0.2, "Validating node capacity", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report capacity validation progress", "error", err.Error())
	}
	if err := s.nodeService.ValidateNodeCapacity(ctx, selectedNode.ID, 1); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeNodeAtCapacity, "node capacity validation failed", false)
		s.handleConnectionError(ctx, requestID, "", "capacity_validation", err)
		op.Fail(err, "node at capacity")
		return
	}

	// Allocate IP (40% progress)
	if err := s.progressReporter.ReportProgress(ctx, requestID, "", "allocating_ip", 0.4, "Allocating IP address", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report IP allocation progress", "error", err.Error())
	}
	allocatedIP, err := s.ipService.AllocateClientIP(ctx, selectedNode.ID)
	if err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainIP, apperrors.ErrCodeIPAllocation, "failed to allocate IP for peer", true)
		s.handleConnectionError(ctx, requestID, "", "ip_allocation", err)
		op.Fail(err, "ip allocation failed")
		return
	}
	op.With(slog.String("allocated_ip", allocatedIP.String()))

	// Create peer (60% progress)
	if err := s.progressReporter.ReportProgress(ctx, requestID, "", "creating_peer", 0.6, "Creating peer record", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report peer creation progress", "error", err.Error())
	}
	peerReq := &peer.CreateRequest{
		NodeID:      selectedNode.ID,
		PublicKey:   *publicKey,
		AllocatedIP: allocatedIP.String(),
	}

	createdPeer, err := s.peerService.Create(ctx, peerReq)
	if err != nil {
		if cleanupErr := s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to release IP after peer creation failure", cleanupErr)
		}
		err = apperrors.WrapWithDomain(err, apperrors.DomainPeer, apperrors.ErrCodePeerConflict, "failed to create peer", false)
		s.handleConnectionError(ctx, requestID, "", "peer_creation", err)
		op.Fail(err, "peer creation failed")
		return
	}
	op.With(slog.String("peer_id", createdPeer.ID))

	// Configure WireGuard (80% progress)
	if err := s.progressReporter.ReportProgress(ctx, requestID, createdPeer.ID, "configuring_wireguard", 0.8, "Configuring WireGuard", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report WireGuard config progress", "error", err.Error())
	}
	wgConfig := node.PeerWireGuardConfig{
		PublicKey:  createdPeer.PublicKey,
		AllowedIPs: []string{createdPeer.AllocatedIP + "/32"},
	}

	if err := s.wireguardManager.AddPeer(ctx, selectedNode.IPAddress, wgConfig); err != nil {
		if cleanupErr := s.peerService.Remove(ctx, createdPeer.ID); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to remove peer after infra add failure", cleanupErr)
		}
		if cleanupErr := s.ipService.ReleaseClientIP(ctx, selectedNode.ID, allocatedIP); cleanupErr != nil {
			s.logger.ErrorCtx(ctx, "cleanup failed: failed to release IP after infra add failure", cleanupErr)
		}
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "failed to add peer to node infrastructure", true, err)
		s.handleConnectionError(ctx, requestID, createdPeer.ID, "wireguard_config", err)
		op.Fail(err, "failed to add peer to node")
		return
	}

	// Get node public key if needed
	nodePublicKey := selectedNode.ServerPublicKey
	if nodePublicKey == "" {
		wgStatus, err := s.wireguardManager.GetWireGuardStatus(ctx, selectedNode.IPAddress)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to get node public key from infrastructure", "error", err.Error())
		} else {
			nodePublicKey = wgStatus.PublicKey
		}
	}

	// Publish connected event (100% progress)
	if err := s.progressReporter.ReportProgress(ctx, requestID, createdPeer.ID, "completed", 1.0, "Connection complete", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report completion progress", "error", err.Error())
	}

	duration := time.Since(startTime)
	if err := s.progressReporter.ReportConnected(ctx, requestID, createdPeer.ID, selectedNode.ID,
		createdPeer.PublicKey, createdPeer.AllocatedIP, nodePublicKey, selectedNode.IPAddress, 51820, nil, duration); err != nil {
		s.logger.ErrorCtx(ctx, "failed to publish peer connected event", err)
		// Don't fail the operation - peer is actually connected
	}

	op.Complete("peer connected successfully")
}

// selectNodeWithProgress selects a node and reports progress
func (s *PeerConnectionService) selectNodeWithProgress(ctx context.Context, requestID string) (*node.Node, error) {
	if err := s.progressReporter.ReportProgress(ctx, requestID, "", "selecting_node", 0.1, "Selecting optimal node", nil); err != nil {
		s.logger.WarnContext(ctx, "failed to report node selection progress", "error", err.Error())
	}

	activeStatus := node.StatusActive
	selectedNode, err := s.nodeService.SelectOptimalNode(ctx, node.Filters{Status: &activeStatus})
	if err == nil {
		return selectedNode, nil
	}

	if apperrors.IsErrorCode(err, apperrors.ErrCodeNodeNotFound) || apperrors.IsErrorCode(err, apperrors.ErrCodeNodeAtCapacity) {
		s.logger.InfoContext(ctx, "no active nodes available, requesting provisioning")

		// Publish event requesting node provisioning
		if s.eventPublisher != nil {
			_ = s.eventPublisher.Provisioning.PublishProvisionRequested(ctx, "peer-connect", "no_capacity")
		}

		return nil, apperrors.NewSystemError(apperrors.ErrCodeNodeNotReady, "No active nodes available, provisioning requested. Please try again in a few minutes.", true, err).
			WithMetadata("estimated_wait_sec", 180).
			WithMetadata("retry_after_sec", 90)
	}

	return nil, apperrors.WrapWithDomain(err, apperrors.DomainNode, apperrors.ErrCodeInternal, "failed to select node", false)
}

// handleConnectionError publishes a connection failed event
func (s *PeerConnectionService) handleConnectionError(ctx context.Context, requestID, peerID, phase string, err error) {
	if pubErr := s.eventPublisher.Peer.PublishPeerConnectFailed(ctx, requestID, phase, err); pubErr != nil {
		s.logger.ErrorCtx(ctx, "failed to publish peer connect failed event", pubErr)
	}
}

// StartConnectionWorker starts the async peer connection worker that processes connection requests
func (s *PeerConnectionService) StartConnectionWorker(ctx context.Context) {
	s.logger.InfoContext(ctx, "starting peer connection worker")

	// Subscribe to PeerConnectRequested events
	handler := func(workerCtx context.Context, e Event) error {
		// Extract event data from metadata
		metadata := e.Metadata()
		requestID, _ := metadata["request_id"].(string)
		publicKeyVal, _ := metadata["public_key"].(string)
		var publicKey *string
		if publicKeyVal != "" {
			publicKey = &publicKeyVal
		}

		if requestID == "" {
			s.logger.WarnContext(workerCtx, "received connect request with no request_id")
			return nil
		}

		// Process connection in a goroutine to avoid blocking event bus
		go s.processConnectionRequest(context.Background(), requestID, publicKey)
		return nil
	}

	// Subscribe using the event publisher's Subscribe method
	unsubscribe, err := s.eventPublisher.Peer.Subscribe("peer.connect.requested", handler)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to subscribe to peer connect requested events", err)
		return
	}

	// Handle shutdown
	go func() {
		<-ctx.Done()
		s.logger.InfoContext(context.Background(), "peer connection worker shutting down")
		if err := unsubscribe(); err != nil {
			s.logger.ErrorCtx(context.Background(), "failed to unsubscribe from events", err)
		}
	}()

	s.logger.InfoContext(ctx, "peer connection worker started")
}
