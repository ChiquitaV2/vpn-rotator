package node

import "fmt"

// Common domain errors
var (
	ErrNodeNotFound            = fmt.Errorf("node not found")
	ErrInvalidStatus           = fmt.Errorf("invalid node status")
	ErrInvalidStatusTransition = fmt.Errorf("invalid status transition")
	ErrNodeAtCapacity          = fmt.Errorf("node at capacity")
	ErrNodeNotReady            = fmt.Errorf("node not ready")
	ErrNodeUnhealthy           = fmt.Errorf("node is unhealthy")
	ErrServerIDConflict        = fmt.Errorf("server ID already in use")
	ErrIPAddressConflict       = fmt.Errorf("IP address already in use")
	ErrConcurrentModification  = fmt.Errorf("concurrent modification detected - version mismatch")
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

// ProvisioningError represents errors during node provisioning
type ProvisioningError struct {
	NodeID string
	Phase  string
	Err    error
}

func (e *ProvisioningError) Error() string {
	return fmt.Sprintf("provisioning failed for node %s during %s: %v", e.NodeID, e.Phase, e.Err)
}

func (e *ProvisioningError) Unwrap() error {
	return e.Err
}

// NewProvisioningError creates a new provisioning error
func NewProvisioningError(nodeID, phase string, err error) *ProvisioningError {
	return &ProvisioningError{
		NodeID: nodeID,
		Phase:  phase,
		Err:    err,
	}
}

// HealthCheckError represents errors during node health checks
type HealthCheckError struct {
	NodeID string
	Check  string
	Err    error
}

func (e *HealthCheckError) Error() string {
	return fmt.Sprintf("health check '%s' failed for node %s: %v", e.Check, e.NodeID, e.Err)
}

func (e *HealthCheckError) Unwrap() error {
	return e.Err
}

// NewHealthCheckError creates a new health check error
func NewHealthCheckError(nodeID, check string, err error) *HealthCheckError {
	return &HealthCheckError{
		NodeID: nodeID,
		Check:  check,
		Err:    err,
	}
}

// CapacityError represents capacity-related errors
type CapacityError struct {
	NodeID          string
	CurrentPeers    int
	MaxPeers        int
	RequestedPeers  int
	ThresholdFailed bool
	Err             error
}

func (e *CapacityError) Error() string {
	if e.ThresholdFailed {
		return fmt.Sprintf("capacity threshold exceeded for node %s: current=%d, max=%d, requested=%d",
			e.NodeID, e.CurrentPeers, e.MaxPeers, e.RequestedPeers)
	}
	return fmt.Sprintf("insufficient capacity for node %s: current=%d, max=%d, requested=%d: %v",
		e.NodeID, e.CurrentPeers, e.MaxPeers, e.RequestedPeers, e.Err)
}

func (e *CapacityError) Unwrap() error {
	return e.Err
}

// NewCapacityError creates a new capacity error
func NewCapacityError(nodeID string, current, max, requested int, thresholdFailed bool, err error) *CapacityError {
	return &CapacityError{
		NodeID:          nodeID,
		CurrentPeers:    current,
		MaxPeers:        max,
		RequestedPeers:  requested,
		ThresholdFailed: thresholdFailed,
		Err:             err,
	}
}

// DestructionError represents errors during node destruction
type DestructionError struct {
	NodeID string
	Phase  string
	Err    error
}

func (e *DestructionError) Error() string {
	return fmt.Sprintf("destruction failed for node %s during %s: %v", e.NodeID, e.Phase, e.Err)
}

func (e *DestructionError) Unwrap() error {
	return e.Err
}

// NewDestructionError creates a new destruction error
func NewDestructionError(nodeID, phase string, err error) *DestructionError {
	return &DestructionError{
		NodeID: nodeID,
		Phase:  phase,
		Err:    err,
	}
}

// PeerOperationError represents a peer management operation error
type PeerOperationError struct {
	NodeIP    string `json:"node_ip"`
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"`
	Operation string `json:"operation"`
	Cause     error  `json:"cause"`
}

func (e *PeerOperationError) Error() string {
	return fmt.Sprintf("peer %s operation '%s' failed on node %s: %v", e.PeerID, e.Operation, e.NodeIP, e.Cause)
}

func (e *PeerOperationError) Unwrap() error {
	return e.Cause
}

// NewPeerOperationError creates a new peer operation error
func NewPeerOperationError(nodeIP, peerID, publicKey, operation string, cause error) *PeerOperationError {
	return &PeerOperationError{
		NodeIP:    nodeIP,
		PeerID:    peerID,
		PublicKey: publicKey,
		Operation: operation,
		Cause:     cause,
	}
}
