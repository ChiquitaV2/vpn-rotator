package provisioner

import (
	"errors"
	"fmt"
)

// Common error types for categorizing failures
var (
	// Transient errors - safe to retry
	ErrNetworkTimeout    = errors.New("network timeout")
	ErrAPIRateLimit      = errors.New("API rate limit exceeded")
	ErrTemporaryFailure  = errors.New("temporary failure")
	ErrHealthCheckFailed = errors.New("health check failed")

	// Permanent errors - don't retry
	ErrInvalidCredentials = errors.New("invalid API credentials")
	ErrQuotaExceeded      = errors.New("quota exceeded")
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrServerNotFound     = errors.New("server not found")
)

// ProvisionError provides structured error information with context.
// It tracks where the error occurred, whether it's retryable, and any
// cleanup information needed.
type ProvisionError struct {
	Stage     string // Where did it fail? (key_generation, cloud_init, server_creation, health_check)
	Message   string // Human-readable description
	Err       error  // Underlying error (can be unwrapped)
	Retryable bool   // Can we retry this operation?
	ServerID  string // Server ID for cleanup (if server was created)
}

// Error implements the error interface.
func (e *ProvisionError) Error() string {
	if e.ServerID != "" {
		return fmt.Sprintf("%s (stage: %s, server_id: %s): %v", e.Message, e.Stage, e.ServerID, e.Err)
	}
	return fmt.Sprintf("%s (stage: %s): %v", e.Message, e.Stage, e.Err)
}

// Unwrap allows errors.Is and errors.As to work with wrapped errors.
func (e *ProvisionError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error can be retried.
func (e *ProvisionError) IsRetryable() bool {
	return e.Retryable
}

// NewProvisionError creates a new ProvisionError.
func NewProvisionError(stage, message string, err error, retryable bool) *ProvisionError {
	return &ProvisionError{
		Stage:     stage,
		Message:   message,
		Err:       err,
		Retryable: retryable,
	}
}

// WithServerID adds server ID to the error for cleanup purposes.
func (e *ProvisionError) WithServerID(serverID string) *ProvisionError {
	e.ServerID = serverID
	return e
}

// DestroyError represents an error during node destruction.
type DestroyError struct {
	ServerID string
	Message  string
	Err      error
}

// Error implements the error interface.
func (e *DestroyError) Error() string {
	return fmt.Sprintf("destroy failed for server %s: %s: %v", e.ServerID, e.Message, e.Err)
}

// Unwrap allows errors.Is and errors.As to work.
func (e *DestroyError) Unwrap() error {
	return e.Err
}

// NewDestroyError creates a new DestroyError.
func NewDestroyError(serverID, message string, err error) *DestroyError {
	return &DestroyError{
		ServerID: serverID,
		Message:  message,
		Err:      err,
	}
}

// IsTransientError checks if an error is transient and can be retried.
func IsTransientError(err error) bool {
	return errors.Is(err, ErrNetworkTimeout) ||
		errors.Is(err, ErrAPIRateLimit) ||
		errors.Is(err, ErrTemporaryFailure) ||
		errors.Is(err, ErrHealthCheckFailed)
}

// IsPermanentError checks if an error is permanent and should not be retried.
func IsPermanentError(err error) bool {
	return errors.Is(err, ErrInvalidCredentials) ||
		errors.Is(err, ErrQuotaExceeded) ||
		errors.Is(err, ErrInvalidConfig)
}
