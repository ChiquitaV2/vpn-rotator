package errors

import (
	"errors"
	"fmt"
	"time"
)

// DomainError is the base interface for all structured errors in the application
type DomainError interface {
	error

	// Domain returns the domain context (e.g., "node", "peer", "provisioning")
	Domain() string

	// Code returns a stable error code for API responses
	Code() string

	// Retryable indicates if the operation can be retried
	Retryable() bool

	// Metadata returns additional error context
	Metadata() map[string]any

	// WithMetadata adds metadata to the error
	WithMetadata(key string, value any) DomainError

	// Timestamp returns when the error occurred
	Timestamp() time.Time
}

// BaseError is the foundational implementation of DomainError
type BaseError struct {
	domain    string
	code      string
	message   string
	cause     error
	retryable bool
	metadata  map[string]any
	timestamp time.Time
}

func (e *BaseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.domain, e.code, e.message, e.cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.domain, e.code, e.message)
}

func (e *BaseError) Unwrap() error            { return e.cause }
func (e *BaseError) Domain() string           { return e.domain }
func (e *BaseError) Code() string             { return e.code }
func (e *BaseError) Retryable() bool          { return e.retryable }
func (e *BaseError) Metadata() map[string]any { return e.metadata }
func (e *BaseError) Timestamp() time.Time     { return e.timestamp }

// NewBaseError creates a new BaseError with the specified parameters
func NewBaseError(domain, code, message string, retryable bool, cause error, metadata map[string]any) *BaseError {
	if metadata == nil {
		metadata = make(map[string]any)
	}

	return &BaseError{
		domain:    domain,
		code:      code,
		message:   message,
		cause:     cause,
		retryable: retryable,
		metadata:  metadata,
		timestamp: time.Now(),
	}
}

// WithMetadata adds metadata to the error
func (e *BaseError) WithMetadata(key string, value any) DomainError {
	// 1. Create a new map with capacity for the old data + 1
	newMeta := make(map[string]any, len(e.metadata)+1)

	// 2. Copy old metadata
	for k, v := range e.metadata {
		newMeta[k] = v
	}

	// 3. Add the new metadata
	newMeta[key] = value

	// 4. Return a *new* struct that is a copy of the old one
	//    but with the new map.
	return &BaseError{
		domain:    e.domain,
		code:      e.code,
		message:   e.message,
		cause:     e.cause,
		retryable: e.retryable,
		metadata:  newMeta,     // <-- Use the new map
		timestamp: e.timestamp, // Keep the original error timestamp
	}
}

// Standardized Error Codes
const (
	// Node Domain Errors
	ErrCodeNodeNotFound     = "node_not_found"
	ErrCodeNodeAtCapacity   = "node_at_capacity"
	ErrCodeNodeUnhealthy    = "node_unhealthy"
	ErrCodeNodeProvisioning = "node_provisioning"
	ErrCodeNodeDestroying   = "node_destroying"
	ErrCodeNodeValidation   = "node_validation"
	ErrCodeNodeConflict     = "node_conflict"
	ErrCodeNodeNotReady     = "node_not_ready"
	ErrCodeNodeReq

	// Peer Domain Errors
	ErrCodePeerNotFound    = "peer_not_found"
	ErrCodePeerConflict    = "peer_conflict"
	ErrCodePeerValidation  = "peer_validation"
	ErrCodePeerIPConflict  = "peer_ip_conflict"
	ErrCodePeerKeyConflict = "peer_public_key_conflict"

	// Infrastructure Errors
	ErrCodeProvisionInProgress = "provision_in_progress"
	ErrCodeProvisionTimeout    = "provision_timeout"
	ErrCodeProvisionFailed     = "provision_failed"
	ErrCodeDestructionFailed   = "destruction_failed"
	ErrCodeSSHConnection       = "ssh_connection"
	ErrCodeSSHTimeout          = "ssh_timeout"
	ErrCodeSSHCommand          = "ssh_command_failed"
	ErrCodeWireGuardError      = "wireguard_error"
	ErrCodeHealthCheckFailed   = "health_check_failed"
	ErrCodeCircuitOpen         = "circuit_breaker_open"
	ErrCodeRateLimit           = "rate_limit_exceeded"
	ErrCodeQuotaExceeded       = "quota_exceeded"
	ErrCodeNetworkError        = "network_error"

	// System Errors
	ErrCodeDatabase         = "database_error"
	ErrCodeConfiguration    = "config_error"
	ErrCodeInternal         = "internal_error"
	ErrCodeValidation       = "validation_error"
	ErrCodeTimeout          = "timeout"
	ErrCodeCapacityExceeded = "capacity_exceeded"
	ErrCodeFileOperation    = "file_operation_error"

	// IP Allocation Errors
	ErrCodeIPAllocation     = "ip_allocation_failed"
	ErrCodeSubnetExhausted  = "subnet_exhausted"
	ErrCodeInvalidCIDR      = "invalid_cidr"
	ErrCodeInvalidIPAddress = "invalid_ip_address"

	ErrCodeSubnetNotFound = "resource_not_found"
)

// Domain Constants
const (
	DomainNode           = "node"
	DomainPeer           = "peer"
	DomainInfrastructure = "infrastructure"
	DomainProvisioning   = "provisioning"
	DomainIP             = "ip"
	DomainDatabase       = "database"
	DomainSystem         = "system"
	DomainAPI            = "api"
	DomainEvent          = "event"
)

// Domain-specific error constructors

// NewNodeError creates a standardized node domain error
func NewNodeError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainNode, code, message, retryable, cause, nil)
}

// NewPeerError creates a standardized peer domain error
func NewPeerError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainPeer, code, message, retryable, cause, nil)
}

// NewInfrastructureError creates a standardized infrastructure error
func NewInfrastructureError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainInfrastructure, code, message, retryable, cause, nil)
}

// NewProvisioningError creates a standardized provisioning error
func NewProvisioningError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainProvisioning, code, message, retryable, cause, nil)
}

// NewIPError creates a standardized IP allocation error
func NewIPError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainIP, code, message, retryable, cause, nil)
}

// NewDatabaseError creates a standardized database error
func NewDatabaseError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainDatabase, code, message, retryable, cause, nil)
}

// NewSystemError creates a standardized system error
func NewSystemError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainSystem, code, message, retryable, cause, nil)
}

// NewDomainAPIError creates a standardized API error (renamed to avoid conflict)
func NewDomainAPIError(code, message string, retryable bool, cause error) DomainError {
	return NewBaseError(DomainAPI, code, message, retryable, cause, nil)
}

// Domain Sentinel Errors - Pre-created common errors for fast comparison
// These complement (but don't replace) the existing simple sentinel errors
var (
	// Node domain errors
	DomainErrNodeNotFound   = NewNodeError(ErrCodeNodeNotFound, "node not found", false, nil)
	DomainErrNodeAtCapacity = NewNodeError(ErrCodeNodeAtCapacity, "node at capacity", true, nil)
	DomainErrNodeUnhealthy  = NewNodeError(ErrCodeNodeUnhealthy, "node is unhealthy", true, nil)
	DomainErrNodeNotReady   = NewNodeError(ErrCodeNodeNotReady, "node not ready", true, nil)

	// Peer domain errors
	DomainErrPeerNotFound      = NewPeerError(ErrCodePeerNotFound, "peer not found", false, nil)
	DomainErrPublicKeyConflict = NewPeerError(ErrCodePeerKeyConflict, "public key already in use", false, nil)
	DomainErrIPConflict        = NewPeerError(ErrCodePeerIPConflict, "IP address already allocated", false, nil)

	// Infrastructure domain errors
	DomainErrProvisionTimeout  = NewInfrastructureError(ErrCodeProvisionTimeout, "provisioning timeout exceeded", true, nil)
	DomainErrHealthCheckFailed = NewInfrastructureError(ErrCodeHealthCheckFailed, "health check failed", true, nil)

	// System domain errors
	DomainErrNoActiveNode  = NewSystemError(ErrCodeNodeNotFound, "no active node available", true, nil)
	DomainErrInvalidConfig = NewSystemError(ErrCodeConfiguration, "invalid configuration", false, nil)
	DomainErrDatabaseError = NewDatabaseError(ErrCodeDatabase, "database error", true, nil)
)

// Helper functions for error checking

// IsDomainError checks if an error is a DomainError
func IsDomainError(err error) bool {
	_, ok := err.(DomainError)
	return ok
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Retryable()
	}
	return false
}

// GetErrorCode returns the error code if it's a DomainError, otherwise returns "unknown"
func GetErrorCode(err error) string {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Code()
	}
	return "unknown"
}

// GetErrorDomain returns the error domain if it's a DomainError, otherwise returns "unknown"
func GetErrorDomain(err error) string {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Domain()
	}
	return "unknown"
}

// HasErrorCode checks if an error has a specific error code
func HasErrorCode(err error, code string) bool {
	return GetErrorCode(err) == code
}

// IsErrorCode checks if any error in the chain has the specified code
func IsErrorCode(err error, code string) bool {
	for err != nil {
		if HasErrorCode(err, code) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// WrapWithDomain wraps an existing error with domain context
func WrapWithDomain(err error, domain, code, message string, retryable bool) DomainError {
	return NewBaseError(domain, code, message, retryable, err, nil)
}

// UnpackError unpacks a DomainError into its components
func UnpackError(err error) (domain, code, message string, retryable bool, metadata map[string]any, timestamp time.Time, cause error) {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Domain(), domainErr.Code(), domainErr.Error(), domainErr.Retryable(), domainErr.Metadata(), domainErr.Timestamp(), errors.Unwrap(err)
	}
	return "unknown", "unknown", err.Error(), false, nil, time.Time{}, nil
}
