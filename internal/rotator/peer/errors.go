package peer

import (
	"fmt"
)

import "errors"

// Common domain errors - using errors.New for better performance
var (
	ErrPeerNotFound            = errors.New("peer not found")
	ErrPublicKeyConflict       = errors.New("public key already in use")
	ErrIPConflict              = errors.New("IP address already allocated")
	ErrInvalidStatus           = errors.New("invalid peer status")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrNodeNotFound            = errors.New("node not found")
	ErrNodeAtCapacity          = errors.New("node at capacity")
)

// ValidationError represents a validation error with context
type ValidationError struct {
	Field   string
	Message string
	Value   interface{}
}

func (e *ValidationError) Error() string {
	if e.Value != nil {
		return fmt.Sprintf("validation failed for %s: %s (value: %v)", e.Field, e.Message, e.Value)
	}
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string, value interface{}) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	}
}

// ConflictError represents a resource conflict error
type ConflictError struct {
	Resource string
	Value    string
	Message  string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s conflict: %s (value: %s)", e.Resource, e.Message, e.Value)
}

// NewConflictError creates a new conflict error
func NewConflictError(resource, value, message string) *ConflictError {
	return &ConflictError{
		Resource: resource,
		Value:    value,
		Message:  message,
	}
}
