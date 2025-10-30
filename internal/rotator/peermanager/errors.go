package peermanager

import "fmt"

// ErrorType represents different types of peer management errors
type ErrorType string

const (
	ErrorTypeValidation    ErrorType = "validation"
	ErrorTypeNotFound      ErrorType = "not_found"
	ErrorTypeConflict      ErrorType = "conflict"
	ErrorTypeDatabase      ErrorType = "database"
	ErrorTypeNodeOperation ErrorType = "node_operation"
)

// Error represents a peer management error with context
type Error struct {
	Type      ErrorType `json:"type"`
	Message   string    `json:"message"`
	PeerID    string    `json:"peer_id,omitempty"`
	NodeID    string    `json:"node_id,omitempty"`
	PublicKey string    `json:"public_key,omitempty"`
	Operation string    `json:"operation,omitempty"`
	Cause     error     `json:"-"`
}

func (e *Error) Error() string {
	if e.PeerID != "" {
		return fmt.Sprintf("peer %s: %s", e.PeerID, e.Message)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new peer management error
func NewError(errorType ErrorType, message string) *Error {
	return &Error{
		Type:    errorType,
		Message: message,
	}
}

// WithPeerID adds peer ID context to the error
func (e *Error) WithPeerID(peerID string) *Error {
	e.PeerID = peerID
	return e
}

// WithNodeID adds node ID context to the error
func (e *Error) WithNodeID(nodeID string) *Error {
	e.NodeID = nodeID
	return e
}

// WithPublicKey adds public key context to the error
func (e *Error) WithPublicKey(publicKey string) *Error {
	e.PublicKey = publicKey
	return e
}

// WithOperation adds operation context to the error
func (e *Error) WithOperation(operation string) *Error {
	e.Operation = operation
	return e
}

// WithCause adds underlying error cause
func (e *Error) WithCause(cause error) *Error {
	e.Cause = cause
	return e
}
