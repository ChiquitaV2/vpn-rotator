package peer

import (
	"context"
	"time"
)

// Metrics interface for monitoring peer operations
type Metrics interface {
	// Operation metrics
	RecordPeerCreated(nodeID string)
	RecordPeerDeleted(nodeID string, reason string)
	RecordStatusTransition(from, to Status)
	RecordMigration(fromNode, toNode string)

	// Performance metrics
	RecordOperationDuration(operation string, duration time.Duration)
	RecordBatchSize(operation string, size int)

	// Error metrics
	RecordValidationError(field string)
	RecordConflictError(resource string)
}

// NoOpMetrics provides a no-op implementation
type NoOpMetrics struct{}

func (m *NoOpMetrics) RecordPeerCreated(nodeID string)                                  {}
func (m *NoOpMetrics) RecordPeerDeleted(nodeID string, reason string)                   {}
func (m *NoOpMetrics) RecordStatusTransition(from, to Status)                           {}
func (m *NoOpMetrics) RecordMigration(fromNode, toNode string)                          {}
func (m *NoOpMetrics) RecordOperationDuration(operation string, duration time.Duration) {}
func (m *NoOpMetrics) RecordBatchSize(operation string, size int)                       {}
func (m *NoOpMetrics) RecordValidationError(field string)                               {}
func (m *NoOpMetrics) RecordConflictError(resource string)                              {}

// MetricsService wraps the peer service with metrics
type MetricsService struct {
	Service
	metrics Metrics
}

// NewMetricsService creates a new metrics-enabled service
func NewMetricsService(service Service, metrics Metrics) Service {
	return &MetricsService{
		Service: service,
		metrics: metrics,
	}
}

// Create wraps the create operation with metrics
func (m *MetricsService) Create(ctx context.Context, req *CreateRequest) (*Peer, error) {
	start := time.Now()
	defer func() {
		m.metrics.RecordOperationDuration("create", time.Since(start))
	}()

	peer, err := m.Service.Create(ctx, req)
	if err != nil {
		if validationErr, ok := err.(*ValidationError); ok {
			m.metrics.RecordValidationError(validationErr.Field)
		}
		if conflictErr, ok := err.(*ConflictError); ok {
			m.metrics.RecordConflictError(conflictErr.Resource)
		}
		return nil, err
	}

	m.metrics.RecordPeerCreated(req.NodeID)
	return peer, nil
}

// UpdateStatus wraps status updates with metrics
func (m *MetricsService) UpdateStatus(ctx context.Context, peerID string, status Status) error {
	// Get current peer to track transition
	peer, err := m.Service.Get(ctx, peerID)
	if err != nil {
		return err
	}

	oldStatus := peer.Status
	err = m.Service.UpdateStatus(ctx, peerID, status)
	if err == nil {
		m.metrics.RecordStatusTransition(oldStatus, status)
	}

	return err
}

// CreateBatch wraps batch creation with metrics
func (m *MetricsService) CreateBatch(ctx context.Context, requests []*CreateRequest) ([]*Peer, error) {
	start := time.Now()
	defer func() {
		m.metrics.RecordOperationDuration("create_batch", time.Since(start))
		m.metrics.RecordBatchSize("create", len(requests))
	}()

	return m.Service.CreateBatch(ctx, requests)
}
