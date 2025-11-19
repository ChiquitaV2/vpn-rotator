package events

import (
	"context"
	"log/slog"
	"time"

	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// ProgressReporter defines an interface for reporting provisioning progress
// This allows the domain layer to report progress without coupling to event bus implementation
type ProgressReporter interface {
	// ReportProgress reports provisioning progress for a specific phase
	ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error

	// ReportPhaseStart reports the start of a provisioning phase
	ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error

	// ReportPhaseComplete reports the completion of a provisioning phase
	ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error

	// ReportError reports an error during provisioning
	ReportError(ctx context.Context, nodeID, phase string, err error) error

	// ReportCompleted reports successful completion of provisioning
	ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error
}

// PeerConnectionProgressReporter defines an interface for reporting peer connection progress
// This allows the domain layer to report progress without coupling to event bus implementation
type PeerConnectionProgressReporter interface {
	// ReportProgress reports peer connection progress for a specific phase
	ReportProgress(ctx context.Context, requestID, peerID, phase string, progress float64, message string, metadata map[string]interface{}) error

	// ReportPhaseStart reports the start of a peer connection phase
	ReportPhaseStart(ctx context.Context, requestID, peerID, phase, message string) error

	// ReportPhaseComplete reports the completion of a peer connection phase
	ReportPhaseComplete(ctx context.Context, requestID, peerID, phase, message string) error

	// ReportError reports an error during peer connection
	ReportError(ctx context.Context, requestID, phase string, err error) error

	// ReportConnected reports successful completion of peer connection
	ReportConnected(ctx context.Context, requestID, peerID, nodeID, publicKey, allocatedIP, serverPublicKey, serverIP string, serverPort int, clientPrivateKey *string, duration time.Duration) error
}

// EventBasedProgressReporter implements ProgressReporter using the new event system
type EventBasedProgressReporter struct {
	publisher *ProvisioningEventPublisher
	logger    *applogger.Logger
}

// NewEventBasedProgressReporter creates a new event-based progress reporter
func NewEventBasedProgressReporter(publisher *ProvisioningEventPublisher, logger *applogger.Logger) ProgressReporter {
	return &EventBasedProgressReporter{
		publisher: publisher,
		logger:    logger.WithComponent("progress.reporter"),
	}
}

func (r *EventBasedProgressReporter) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	r.logger.DebugContext(ctx, "reporting progress",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.Float64("progress", progress),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (r *EventBasedProgressReporter) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	r.logger.InfoContext(ctx, "phase started",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 0.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	r.logger.InfoContext(ctx, "phase complete",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 1.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportError(ctx context.Context, nodeID, phase string, err error) error {
	r.logger.ErrorCtx(ctx, "reporting provisioning error", err,
		slog.String("node_id", nodeID),
		slog.String("phase", phase))
	return r.publisher.PublishProvisionFailed(ctx, nodeID, phase, err)
}

func (r *EventBasedProgressReporter) ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	r.logger.InfoContext(ctx, "reporting provisioning completed",
		slog.String("node_id", nodeID),
		slog.String("server_id", serverID),
		slog.Duration("duration", duration))
	return r.publisher.PublishProvisionCompleted(ctx, nodeID, serverID, ipAddress, duration)
}

// EventBasedPeerConnectionProgressReporter implements PeerConnectionProgressReporter using the event system
type EventBasedPeerConnectionProgressReporter struct {
	publisher *PeerEventPublisher
	logger    *applogger.Logger
}

// NewEventBasedPeerConnectionProgressReporter creates a new event-based peer connection progress reporter
func NewEventBasedPeerConnectionProgressReporter(publisher *PeerEventPublisher, logger *applogger.Logger) PeerConnectionProgressReporter {
	return &EventBasedPeerConnectionProgressReporter{
		publisher: publisher,
		logger:    logger.WithComponent("peer.progress.reporter"),
	}
}

func (r *EventBasedPeerConnectionProgressReporter) ReportProgress(ctx context.Context, requestID, peerID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	r.logger.DebugContext(ctx, "reporting peer connection progress",
		slog.String("request_id", requestID),
		slog.String("peer_id", peerID),
		slog.String("phase", phase),
		slog.Float64("progress", progress),
		slog.String("message", message))
	return r.publisher.PublishPeerConnectProgress(ctx, requestID, peerID, phase, progress, message, metadata)
}

func (r *EventBasedPeerConnectionProgressReporter) ReportPhaseStart(ctx context.Context, requestID, peerID, phase, message string) error {
	r.logger.InfoContext(ctx, "peer connection phase started",
		slog.String("request_id", requestID),
		slog.String("peer_id", peerID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishPeerConnectProgress(ctx, requestID, peerID, phase, 0.0, message, nil)
}

func (r *EventBasedPeerConnectionProgressReporter) ReportPhaseComplete(ctx context.Context, requestID, peerID, phase, message string) error {
	r.logger.InfoContext(ctx, "peer connection phase complete",
		slog.String("request_id", requestID),
		slog.String("peer_id", peerID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishPeerConnectProgress(ctx, requestID, peerID, phase, 1.0, message, nil)
}

func (r *EventBasedPeerConnectionProgressReporter) ReportError(ctx context.Context, requestID, phase string, err error) error {
	r.logger.ErrorCtx(ctx, "reporting peer connection error", err,
		slog.String("request_id", requestID),
		slog.String("phase", phase))
	return r.publisher.PublishPeerConnectFailed(ctx, requestID, phase, err)
}

func (r *EventBasedPeerConnectionProgressReporter) ReportConnected(ctx context.Context, requestID, peerID, nodeID, publicKey, allocatedIP, serverPublicKey, serverIP string, serverPort int, clientPrivateKey *string, duration time.Duration) error {
	r.logger.InfoContext(ctx, "reporting peer connection completed",
		slog.String("request_id", requestID),
		slog.String("peer_id", peerID),
		slog.String("node_id", nodeID),
		slog.Duration("duration", duration))
	return r.publisher.PublishPeerConnected(ctx, requestID, peerID, nodeID, publicKey, allocatedIP, serverPublicKey, serverIP, serverPort, clientPrivateKey, duration)
}
