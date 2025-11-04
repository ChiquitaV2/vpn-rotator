package events

import "context"

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
	ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error
}

// NoOpProgressReporter is a progress reporter that does nothing
// Useful for testing or when progress reporting is not needed
type NoOpProgressReporter struct{}

func (n *NoOpProgressReporter) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	return nil
}

func (n *NoOpProgressReporter) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	return nil
}

func (n *NoOpProgressReporter) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	return nil
}

func (n *NoOpProgressReporter) ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	return nil
}

// NewNoOpProgressReporter creates a new no-op progress reporter
func NewNoOpProgressReporter() ProgressReporter {
	return &NoOpProgressReporter{}
}
