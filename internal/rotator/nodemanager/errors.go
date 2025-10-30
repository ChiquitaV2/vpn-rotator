package nodemanager

import "fmt"

// SSHError represents an SSH-related error with additional context
type SSHError struct {
	NodeIP    string `json:"node_ip"`
	Command   string `json:"command"`
	Attempt   int    `json:"attempt"`
	Cause     error  `json:"cause"`
	Retryable bool   `json:"retryable"`
}

func (e *SSHError) Error() string {
	return fmt.Sprintf("SSH error on node %s (attempt %d): %v", e.NodeIP, e.Attempt, e.Cause)
}

func (e *SSHError) Unwrap() error {
	return e.Cause
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

// NewSSHError creates a new SSH error
func NewSSHError(nodeIP, command string, attempt int, retryable bool, cause error) *SSHError {
	return &SSHError{
		NodeIP:    nodeIP,
		Command:   command,
		Attempt:   attempt,
		Retryable: retryable,
		Cause:     cause,
	}
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
