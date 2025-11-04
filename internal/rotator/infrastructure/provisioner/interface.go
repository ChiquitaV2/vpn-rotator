package provisioner

import (
	"context"
	"fmt"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

// CloudProvisioner defines the interface for cloud infrastructure provisioning
type CloudProvisioner interface {
	ProvisionNode(ctx context.Context, config node.ProvisioningConfig) (*node.ProvisionedNode, error)
	DestroyNode(ctx context.Context, serverID string) error
	GetNodeStatus(ctx context.Context, serverID string) (*node.CloudNodeStatus, error)
}

// ProvisioningError represents errors during node provisioning
type ProvisioningError struct {
	ServerID  string `json:"server_id"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Err       error  `json:"error"`
	Retryable bool   `json:"retryable"`
}

func (e *ProvisioningError) Error() string {
	if e.ServerID != "" {
		return fmt.Sprintf("provisioning failed for server %s at stage %s: %s: %v", e.ServerID, e.Stage, e.Message, e.Err)
	}
	return fmt.Sprintf("provisioning failed at stage %s: %s: %v", e.Stage, e.Message, e.Err)
}

func (e *ProvisioningError) Unwrap() error {
	return e.Err
}

// DestructionError represents errors during node destruction
type DestructionError struct {
	ServerID string `json:"server_id"`
	Message  string `json:"message"`
	Err      error  `json:"error"`
}

func (e *DestructionError) Error() string {
	return fmt.Sprintf("destruction failed for server %s: %s: %v", e.ServerID, e.Message, e.Err)
}

func (e *DestructionError) Unwrap() error {
	return e.Err
}

// NewProvisioningError creates a new provisioning error
func NewProvisioningError(serverID, stage, message string, err error, retryable bool) *ProvisioningError {
	return &ProvisioningError{
		ServerID:  serverID,
		Stage:     stage,
		Message:   message,
		Err:       err,
		Retryable: retryable,
	}
}

// NewDestructionError creates a new destruction error
func NewDestructionError(serverID, message string, err error) *DestructionError {
	return &DestructionError{
		ServerID: serverID,
		Message:  message,
		Err:      err,
	}
}
