package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common cases
var (
	ErrNoActiveNode      = errors.New("no active node available")
	ErrProvisionTimeout  = errors.New("provisioning timeout exceeded")
	ErrHealthCheckFailed = errors.New("health check failed")
	ErrNodeNotFound      = errors.New("node not found")
	ErrInvalidConfig     = errors.New("invalid configuration")
	ErrDatabaseError     = errors.New("database error")
)

// ProvisionError represents an error during node provisioning
type ProvisionError struct {
	Stage   string // e.g., "server_creation", "health_check", "key_retrieval"
	NodeID  string
	Message string
	Err     error
}

func (e *ProvisionError) Error() string {
	if e.NodeID != "" {
		return fmt.Sprintf("provision failed at %s (node=%s): %s: %v", e.Stage, e.NodeID, e.Message, e.Err)
	}
	return fmt.Sprintf("provision failed at %s: %s: %v", e.Stage, e.Message, e.Err)
}

func (e *ProvisionError) Unwrap() error {
	return e.Err
}

// NewProvisionError creates a new provision error
func NewProvisionError(stage, nodeID, message string, err error) *ProvisionError {
	return &ProvisionError{
		Stage:   stage,
		NodeID:  nodeID,
		Message: message,
		Err:     err,
	}
}

// RotationError represents an error during node rotation
type RotationError struct {
	OldNodeID string
	NewNodeID string
	Message   string
	Err       error
}

func (e *RotationError) Error() string {
	if e.NewNodeID != "" {
		return fmt.Sprintf("rotation failed (old=%s, new=%s): %s: %v", e.OldNodeID, e.NewNodeID, e.Message, e.Err)
	}
	return fmt.Sprintf("rotation failed (old=%s): %s: %v", e.OldNodeID, e.Message, e.Err)
}

func (e *RotationError) Unwrap() error {
	return e.Err
}

// NewRotationError creates a new rotation error
func NewRotationError(oldNodeID, newNodeID, message string, err error) *RotationError {
	return &RotationError{
		OldNodeID: oldNodeID,
		NewNodeID: newNodeID,
		Message:   message,
		Err:       err,
	}
}

// DestructionError represents an error during node destruction
type DestructionError struct {
	NodeID  string
	Message string
	Err     error
}

func (e *DestructionError) Error() string {
	return fmt.Sprintf("destruction failed (node=%s): %s: %v", e.NodeID, e.Message, e.Err)
}

func (e *DestructionError) Unwrap() error {
	return e.Err
}

// NewDestructionError creates a new destruction error
func NewDestructionError(nodeID, message string, err error) *DestructionError {
	return &DestructionError{
		NodeID:  nodeID,
		Message: message,
		Err:     err,
	}
}

// ConfigError represents a configuration error
type ConfigError struct {
	Field   string
	Message string
	Err     error
}

func (e *ConfigError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("config error [%s]: %s: %v", e.Field, e.Message, e.Err)
	}
	return fmt.Sprintf("config error: %s: %v", e.Message, e.Err)
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NewConfigError creates a new config error
func NewConfigError(field, message string, err error) *ConfigError {
	return &ConfigError{
		Field:   field,
		Message: message,
		Err:     err,
	}
}

// APIError represents an API-level error with HTTP context
type APIError struct {
	Code      int    // HTTP status code
	Message   string // User-facing message
	Internal  string // Internal error details (not exposed to client)
	Err       error  // Underlying error
	RequestID string // Request ID for tracing
}

func (e *APIError) Error() string {
	if e.Internal != "" {
		return fmt.Sprintf("api error [%d] (request=%s): %s (internal: %s)", e.Code, e.RequestID, e.Message, e.Internal)
	}
	return fmt.Sprintf("api error [%d] (request=%s): %s", e.Code, e.RequestID, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// NewAPIError creates a new API error
func NewAPIError(code int, message, internal, requestID string, err error) *APIError {
	return &APIError{
		Code:      code,
		Message:   message,
		Internal:  internal,
		Err:       err,
		RequestID: requestID,
	}
}
