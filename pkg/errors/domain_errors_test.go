package errors

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBaseError(t *testing.T) {
	t.Run("creates error with all fields", func(t *testing.T) {
		cause := errors.New("underlying error")
		metadata := map[string]any{"key": "value"}

		err := NewBaseError("test", "test_code", "test message", true, cause, metadata)

		if err.Domain() != "test" {
			t.Errorf("expected domain 'test', got '%s'", err.Domain())
		}
		if err.Code() != "test_code" {
			t.Errorf("expected code 'test_code', got '%s'", err.Code())
		}
		if !err.Retryable() {
			t.Error("expected error to be retryable")
		}
		if err.Unwrap() != cause {
			t.Error("expected error to wrap cause")
		}
		if err.Metadata()["key"] != "value" {
			t.Error("expected metadata to be preserved")
		}
		if err.Timestamp().IsZero() {
			t.Error("expected timestamp to be set")
		}
	})

	t.Run("formats error message correctly", func(t *testing.T) {
		tests := []struct {
			name     string
			cause    error
			expected string
		}{
			{
				name:     "without cause",
				cause:    nil,
				expected: "[test:test_code] test message",
			},
			{
				name:     "with cause",
				cause:    errors.New("underlying"),
				expected: "[test:test_code] test message: underlying",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := NewBaseError("test", "test_code", "test message", false, tt.cause, nil)
				if err.Error() != tt.expected {
					t.Errorf("expected '%s', got '%s'", tt.expected, err.Error())
				}
			})
		}
	})

	t.Run("adds metadata correctly", func(t *testing.T) {
		err := NewBaseError("test", "test_code", "test message", false, nil, nil).WithMetadata("key1", "value1").WithMetadata("key2", 42)

		metadata := err.Metadata()
		if metadata["key1"] != "value1" {
			t.Errorf("expected key1='value1', got '%v'", metadata["key1"])
		}
		if metadata["key2"] != 42 {
			t.Errorf("expected key2=42, got '%v'", metadata["key2"])
		}
	})
}

func TestDomainErrorConstructors(t *testing.T) {
	tests := []struct {
		name        string
		constructor func(string, string, bool, error) DomainError
		domain      string
	}{
		{
			name:        "NewNodeError",
			constructor: NewNodeError,
			domain:      DomainNode,
		},
		{
			name:        "NewPeerError",
			constructor: NewPeerError,
			domain:      DomainPeer,
		},
		{
			name:        "NewInfrastructureError",
			constructor: NewInfrastructureError,
			domain:      DomainInfrastructure,
		},
		{
			name:        "NewProvisioningError",
			constructor: NewProvisioningError,
			domain:      DomainProvisioning,
		},
		{
			name:        "NewIPError",
			constructor: NewIPError,
			domain:      DomainIP,
		},
		{
			name:        "NewDatabaseError",
			constructor: NewDatabaseError,
			domain:      DomainDatabase,
		},
		{
			name:        "NewSystemError",
			constructor: NewSystemError,
			domain:      DomainSystem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.constructor("test_code", "test message", true, nil)

			if err.Domain() != tt.domain {
				t.Errorf("expected domain '%s', got '%s'", tt.domain, err.Domain())
			}
			if err.Code() != "test_code" {
				t.Errorf("expected code 'test_code', got '%s'", err.Code())
			}
			if !err.Retryable() {
				t.Error("expected error to be retryable")
			}
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       DomainError
		domain    string
		code      string
		retryable bool
	}{
		{
			name:      "DomainErrNodeNotFound",
			err:       DomainErrNodeNotFound,
			domain:    DomainNode,
			code:      ErrCodeNodeNotFound,
			retryable: false,
		},
		{
			name:      "DomainErrNodeAtCapacity",
			err:       DomainErrNodeAtCapacity,
			domain:    DomainNode,
			code:      ErrCodeNodeAtCapacity,
			retryable: true,
		},
		{
			name:      "DomainErrPeerNotFound",
			err:       DomainErrPeerNotFound,
			domain:    DomainPeer,
			code:      ErrCodePeerNotFound,
			retryable: false,
		},
		{
			name:      "DomainErrProvisionTimeout",
			err:       DomainErrProvisionTimeout,
			domain:    DomainInfrastructure,
			code:      ErrCodeProvisionTimeout,
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Domain() != tt.domain {
				t.Errorf("expected domain '%s', got '%s'", tt.domain, tt.err.Domain())
			}
			if tt.err.Code() != tt.code {
				t.Errorf("expected code '%s', got '%s'", tt.code, tt.err.Code())
			}
			if tt.err.Retryable() != tt.retryable {
				t.Errorf("expected retryable %v, got %v", tt.retryable, tt.err.Retryable())
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("IsDomainError", func(t *testing.T) {
		domainErr := NewNodeError("test", "test", false, nil)
		regularErr := errors.New("regular error")

		if !IsDomainError(domainErr) {
			t.Error("expected IsDomainError to return true for domain error")
		}
		if IsDomainError(regularErr) {
			t.Error("expected IsDomainError to return false for regular error")
		}
	})

	t.Run("IsRetryable", func(t *testing.T) {
		retryableErr := NewNodeError("test", "test", true, nil)
		nonRetryableErr := NewNodeError("test", "test", false, nil)
		regularErr := errors.New("regular error")

		if !IsRetryable(retryableErr) {
			t.Error("expected IsRetryable to return true for retryable error")
		}
		if IsRetryable(nonRetryableErr) {
			t.Error("expected IsRetryable to return false for non-retryable error")
		}
		if IsRetryable(regularErr) {
			t.Error("expected IsRetryable to return false for regular error")
		}
	})

	t.Run("GetErrorCode", func(t *testing.T) {
		domainErr := NewNodeError("test_code", "test", false, nil)
		regularErr := errors.New("regular error")

		if GetErrorCode(domainErr) != "test_code" {
			t.Errorf("expected 'test_code', got '%s'", GetErrorCode(domainErr))
		}
		if GetErrorCode(regularErr) != "unknown" {
			t.Errorf("expected 'unknown', got '%s'", GetErrorCode(regularErr))
		}
	})

	t.Run("GetErrorDomain", func(t *testing.T) {
		domainErr := NewNodeError("test_code", "test", false, nil)
		regularErr := errors.New("regular error")

		if GetErrorDomain(domainErr) != DomainNode {
			t.Errorf("expected '%s', got '%s'", DomainNode, GetErrorDomain(domainErr))
		}
		if GetErrorDomain(regularErr) != "unknown" {
			t.Errorf("expected 'unknown', got '%s'", GetErrorDomain(regularErr))
		}
	})

	t.Run("HasErrorCode", func(t *testing.T) {
		domainErr := NewNodeError("test_code", "test", false, nil)
		regularErr := errors.New("regular error")

		if !HasErrorCode(domainErr, "test_code") {
			t.Error("expected HasErrorCode to return true for matching code")
		}
		if HasErrorCode(domainErr, "other_code") {
			t.Error("expected HasErrorCode to return false for non-matching code")
		}
		if HasErrorCode(regularErr, "test_code") {
			t.Error("expected HasErrorCode to return false for regular error")
		}
	})

	t.Run("IsErrorCode", func(t *testing.T) {
		innerErr := NewNodeError("inner_code", "inner", false, nil)
		wrappedErr := fmt.Errorf("wrapped: %w", innerErr)

		if !IsErrorCode(wrappedErr, "inner_code") {
			t.Error("expected IsErrorCode to find code in wrapped error")
		}
		if IsErrorCode(wrappedErr, "other_code") {
			t.Error("expected IsErrorCode to return false for non-matching code")
		}
	})
}

func TestWrapWithDomain(t *testing.T) {
	originalErr := errors.New("original error")
	wrappedErr := WrapWithDomain(originalErr, "test", "test_code", "wrapped message", true)

	if wrappedErr.Domain() != "test" {
		t.Errorf("expected domain 'test', got '%s'", wrappedErr.Domain())
	}
	if wrappedErr.Code() != "test_code" {
		t.Errorf("expected code 'test_code', got '%s'", wrappedErr.Code())
	}
	if !wrappedErr.Retryable() {
		t.Error("expected wrapped error to be retryable")
	}
	if !errors.Is(wrappedErr, originalErr) {
		t.Error("expected wrapped error to contain original error")
	}
}

func TestErrorConstants(t *testing.T) {
	// Test that all error codes are defined and non-empty
	errorCodes := []string{
		ErrCodeNodeNotFound,
		ErrCodeNodeAtCapacity,
		ErrCodeNodeUnhealthy,
		ErrCodePeerNotFound,
		ErrCodePeerConflict,
		ErrCodeProvisionTimeout,
		ErrCodeProvisionFailed,
		ErrCodeSSHConnection,
		ErrCodeDatabase,
		ErrCodeConfiguration,
		ErrCodeInternal,
	}

	for _, code := range errorCodes {
		if code == "" {
			t.Errorf("error code should not be empty")
		}
	}

	// Test that all domains are defined and non-empty
	domains := []string{
		DomainNode,
		DomainPeer,
		DomainInfrastructure,
		DomainProvisioning,
		DomainIP,
		DomainDatabase,
		DomainSystem,
		DomainAPI,
		DomainEvent,
	}

	for _, domain := range domains {
		if domain == "" {
			t.Errorf("domain should not be empty")
		}
	}
}

func TestErrorTimestamp(t *testing.T) {
	before := time.Now()
	err := NewNodeError("test", "test", false, nil)
	after := time.Now()

	timestamp := err.Timestamp()
	if timestamp.Before(before) || timestamp.After(after) {
		t.Errorf("timestamp should be between %v and %v, got %v", before, after, timestamp)
	}
}

func TestMetadataNilSafety(t *testing.T) {
	var err DomainError
	err = NewBaseError("test", "test", "test", false, nil, nil)

	// Should not panic
	metadata := err.Metadata()
	if metadata == nil {
		t.Error("metadata should not be nil")
	}

	// Should be able to add metadata
	err = err.WithMetadata("key", "value")
	if err.Metadata()["key"] != "value" {
		t.Error("metadata should be added correctly")
	}
}
