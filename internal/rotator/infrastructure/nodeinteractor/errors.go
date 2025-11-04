package nodeinteractor

import (
	"fmt"
	"time"
)

// SSH connection and operation errors

// SSHConnectionError represents SSH connection failures
type SSHConnectionError struct {
	Host      string        `json:"host"`
	Timeout   time.Duration `json:"timeout"`
	Attempt   int           `json:"attempt"`
	Cause     error         `json:"cause"`
	Retryable bool          `json:"retryable"`
}

func (e *SSHConnectionError) Error() string {
	return fmt.Sprintf("SSH connection failed to %s (attempt %d, timeout %v): %v",
		e.Host, e.Attempt, e.Timeout, e.Cause)
}

func (e *SSHConnectionError) Unwrap() error {
	return e.Cause
}

// NewSSHConnectionError creates a new SSH connection error
func NewSSHConnectionError(host string, timeout time.Duration, attempt int, retryable bool, cause error) *SSHConnectionError {
	return &SSHConnectionError{
		Host:      host,
		Timeout:   timeout,
		Attempt:   attempt,
		Retryable: retryable,
		Cause:     cause,
	}
}

// SSHTimeoutError represents SSH operation timeouts
type SSHTimeoutError struct {
	Host      string        `json:"host"`
	Operation string        `json:"operation"`
	Timeout   time.Duration `json:"timeout"`
	Cause     error         `json:"cause"`
}

func (e *SSHTimeoutError) Error() string {
	return fmt.Sprintf("SSH operation '%s' timed out on %s after %v: %v",
		e.Operation, e.Host, e.Timeout, e.Cause)
}

func (e *SSHTimeoutError) Unwrap() error {
	return e.Cause
}

// NewSSHTimeoutError creates a new SSH timeout error
func NewSSHTimeoutError(host, operation string, timeout time.Duration, cause error) *SSHTimeoutError {
	return &SSHTimeoutError{
		Host:      host,
		Operation: operation,
		Timeout:   timeout,
		Cause:     cause,
	}
}

// SSHCommandError represents SSH command execution failures
type SSHCommandError struct {
	Host     string `json:"host"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Cause    error  `json:"cause"`
}

func (e *SSHCommandError) Error() string {
	return fmt.Sprintf("SSH command failed on %s (exit code %d): %s",
		e.Host, e.ExitCode, e.Command)
}

func (e *SSHCommandError) Unwrap() error {
	return e.Cause
}

// NewSSHCommandError creates a new SSH command error
func NewSSHCommandError(host, command string, exitCode int, stdout, stderr string, cause error) *SSHCommandError {
	return &SSHCommandError{
		Host:     host,
		Command:  command,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Cause:    cause,
	}
}

// WireGuard operation errors

// WireGuardOperationError represents WireGuard operation failures
type WireGuardOperationError struct {
	Host      string `json:"host"`
	Operation string `json:"operation"`
	Interface string `json:"interface"`
	PeerKey   string `json:"peer_key,omitempty"`
	Cause     error  `json:"cause"`
}

func (e *WireGuardOperationError) Error() string {
	if e.PeerKey != "" {
		return fmt.Sprintf("WireGuard %s operation failed on %s interface %s for peer %s: %v",
			e.Operation, e.Host, e.Interface, e.PeerKey[:8]+"...", e.Cause)
	}
	return fmt.Sprintf("WireGuard %s operation failed on %s interface %s: %v",
		e.Operation, e.Host, e.Interface, e.Cause)
}

func (e *WireGuardOperationError) Unwrap() error {
	return e.Cause
}

// NewWireGuardOperationError creates a new WireGuard operation error
func NewWireGuardOperationError(host, operation, iface, peerKey string, cause error) *WireGuardOperationError {
	return &WireGuardOperationError{
		Host:      host,
		Operation: operation,
		Interface: iface,
		PeerKey:   peerKey,
		Cause:     cause,
	}
}

// WireGuardConfigError represents WireGuard configuration errors
type WireGuardConfigError struct {
	Host      string `json:"host"`
	Interface string `json:"interface"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	Cause     error  `json:"cause"`
}

func (e *WireGuardConfigError) Error() string {
	return fmt.Sprintf("WireGuard config error on %s interface %s field '%s' (value: %s): %v",
		e.Host, e.Interface, e.Field, e.Value, e.Cause)
}

func (e *WireGuardConfigError) Unwrap() error {
	return e.Cause
}

// NewWireGuardConfigError creates a new WireGuard configuration error
func NewWireGuardConfigError(host, iface, field, value string, cause error) *WireGuardConfigError {
	return &WireGuardConfigError{
		Host:      host,
		Interface: iface,
		Field:     field,
		Value:     value,
		Cause:     cause,
	}
}

// Health check errors

// HealthCheckError represents node health check failures
type HealthCheckError struct {
	Host      string        `json:"host"`
	CheckName string        `json:"check_name"`
	Command   string        `json:"command"`
	Timeout   time.Duration `json:"timeout"`
	Critical  bool          `json:"critical"`
	Cause     error         `json:"cause"`
}

func (e *HealthCheckError) Error() string {
	criticality := "non-critical"
	if e.Critical {
		criticality = "critical"
	}
	return fmt.Sprintf("%s health check '%s' failed on %s (timeout %v): %v",
		criticality, e.CheckName, e.Host, e.Timeout, e.Cause)
}

func (e *HealthCheckError) Unwrap() error {
	return e.Cause
}

// NewHealthCheckError creates a new health check error
func NewHealthCheckError(host, checkName, command string, timeout time.Duration, critical bool, cause error) *HealthCheckError {
	return &HealthCheckError{
		Host:      host,
		CheckName: checkName,
		Command:   command,
		Timeout:   timeout,
		Critical:  critical,
		Cause:     cause,
	}
}

// System operation errors

// SystemInfoError represents system information retrieval failures
type SystemInfoError struct {
	Host     string `json:"host"`
	InfoType string `json:"info_type"`
	Source   string `json:"source"`
	Cause    error  `json:"cause"`
}

func (e *SystemInfoError) Error() string {
	return fmt.Sprintf("failed to retrieve %s info from %s on %s: %v",
		e.InfoType, e.Source, e.Host, e.Cause)
}

func (e *SystemInfoError) Unwrap() error {
	return e.Cause
}

// NewSystemInfoError creates a new system info error
func NewSystemInfoError(host, infoType, source string, cause error) *SystemInfoError {
	return &SystemInfoError{
		Host:     host,
		InfoType: infoType,
		Source:   source,
		Cause:    cause,
	}
}

// File operation errors

// FileOperationError represents file upload/download failures
type FileOperationError struct {
	Host       string `json:"host"`
	Operation  string `json:"operation"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Cause      error  `json:"cause"`
}

func (e *FileOperationError) Error() string {
	return fmt.Sprintf("file %s operation failed on %s (local: %s, remote: %s): %v",
		e.Operation, e.Host, e.LocalPath, e.RemotePath, e.Cause)
}

func (e *FileOperationError) Unwrap() error {
	return e.Cause
}

// NewFileOperationError creates a new file operation error
func NewFileOperationError(host, operation, localPath, remotePath string, cause error) *FileOperationError {
	return &FileOperationError{
		Host:       host,
		Operation:  operation,
		LocalPath:  localPath,
		RemotePath: remotePath,
		Cause:      cause,
	}
}

// Circuit breaker errors

// CircuitBreakerError represents circuit breaker state errors
type CircuitBreakerError struct {
	Host         string    `json:"host"`
	Operation    string    `json:"operation"`
	State        string    `json:"state"`
	FailureCount int       `json:"failure_count"`
	LastFailure  time.Time `json:"last_failure"`
	ResetTime    time.Time `json:"reset_time"`
	Cause        error     `json:"cause"`
}

func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker %s for %s operation on %s (failures: %d, last: %v): %v",
		e.State, e.Operation, e.Host, e.FailureCount, e.LastFailure, e.Cause)
}

func (e *CircuitBreakerError) Unwrap() error {
	return e.Cause
}

// NewCircuitBreakerError creates a new circuit breaker error
func NewCircuitBreakerError(host, operation, state string, failureCount int, lastFailure, resetTime time.Time, cause error) *CircuitBreakerError {
	return &CircuitBreakerError{
		Host:         host,
		Operation:    operation,
		State:        state,
		FailureCount: failureCount,
		LastFailure:  lastFailure,
		ResetTime:    resetTime,
		Cause:        cause,
	}
}

// Validation errors

// ValidationError represents configuration or parameter validation failures
type ValidationError struct {
	Field   string      `json:"field"`
	Value   interface{} `json:"value"`
	Rule    string      `json:"rule"`
	Message string      `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field '%s' (value: %v, rule: %s): %s",
		e.Field, e.Value, e.Rule, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, rule, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Rule:    rule,
		Message: message,
	}
}

// Peer operation specific errors

// PeerNotFoundError represents peer not found errors
type PeerNotFoundError struct {
	Host      string `json:"host"`
	PublicKey string `json:"public_key"`
	Interface string `json:"interface"`
}

func (e *PeerNotFoundError) Error() string {
	return fmt.Sprintf("peer %s not found on %s interface %s",
		e.PublicKey[:8]+"...", e.Host, e.Interface)
}

// NewPeerNotFoundError creates a new peer not found error
func NewPeerNotFoundError(host, publicKey, iface string) *PeerNotFoundError {
	return &PeerNotFoundError{
		Host:      host,
		PublicKey: publicKey,
		Interface: iface,
	}
}

// PeerAlreadyExistsError represents peer already exists errors
type PeerAlreadyExistsError struct {
	Host      string `json:"host"`
	PublicKey string `json:"public_key"`
	Interface string `json:"interface"`
}

func (e *PeerAlreadyExistsError) Error() string {
	return fmt.Sprintf("peer %s already exists on %s interface %s",
		e.PublicKey[:8]+"...", e.Host, e.Interface)
}

// NewPeerAlreadyExistsError creates a new peer already exists error
func NewPeerAlreadyExistsError(host, publicKey, iface string) *PeerAlreadyExistsError {
	return &PeerAlreadyExistsError{
		Host:      host,
		PublicKey: publicKey,
		Interface: iface,
	}
}

// IPConflictError represents IP address conflicts
type IPConflictError struct {
	Host         string `json:"host"`
	IPAddress    string `json:"ip_address"`
	ExistingPeer string `json:"existing_peer"`
	NewPeer      string `json:"new_peer"`
}

func (e *IPConflictError) Error() string {
	return fmt.Sprintf("IP address %s conflict on %s: already assigned to peer %s, requested for peer %s",
		e.IPAddress, e.Host, e.ExistingPeer[:8]+"...", e.NewPeer[:8]+"...")
}

// NewIPConflictError creates a new IP conflict error
func NewIPConflictError(host, ipAddress, existingPeer, newPeer string) *IPConflictError {
	return &IPConflictError{
		Host:         host,
		IPAddress:    ipAddress,
		ExistingPeer: existingPeer,
		NewPeer:      newPeer,
	}
}
