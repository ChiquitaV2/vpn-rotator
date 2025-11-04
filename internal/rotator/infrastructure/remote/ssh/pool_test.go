package ssh

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	privateKey := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAFwAAAAdzc2gtcn
NhAAAAAwEAAQAAAQEA1234567890abcdef...
-----END OPENSSH PRIVATE KEY-----`

	pool := NewPool(privateKey, logger, 5*time.Minute)
	if pool == nil {
		t.Fatal("Expected pool to be created, got nil")
	}

	if pool.maxIdle != 5*time.Minute {
		t.Errorf("Expected maxIdle to be 5 minutes, got %v", pool.maxIdle)
	}

	if len(pool.connections) != 0 {
		t.Errorf("Expected empty connections map, got %d connections", len(pool.connections))
	}
}

func TestPoolStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	privateKey := "test-key"

	pool := &Pool{
		connections: make(map[string]*Connection),
		maxIdle:     5 * time.Minute,
		privateKey:  privateKey,
		logger:      logger,
	}

	stats := pool.GetStats()
	if stats.TotalConnections != 0 {
		t.Errorf("Expected 0 total connections, got %d", stats.TotalConnections)
	}

	if stats.ActiveConnections != 0 {
		t.Errorf("Expected 0 active connections, got %d", stats.ActiveConnections)
	}

	if stats.IdleConnections != 0 {
		t.Errorf("Expected 0 idle connections, got %d", stats.IdleConnections)
	}
}

func TestCleanupIdleConnections(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	privateKey := "test-key"

	pool := &Pool{
		connections: make(map[string]*Connection),
		maxIdle:     1 * time.Second,
		privateKey:  privateKey,
		logger:      logger,
	}

	// Add a mock connection that's already idle
	mockClient := &mockClient{}
	pool.connections["192.168.1.1"] = &Connection{
		client:   mockClient,
		lastUsed: time.Now().Add(-2 * time.Second), // 2 seconds ago
		nodeIP:   "192.168.1.1",
	}

	// Clean up idle connections
	pool.CleanupIdleConnections()

	// Should have removed the idle connection
	if len(pool.connections) != 0 {
		t.Errorf("Expected 0 connections after cleanup, got %d", len(pool.connections))
	}

	if !mockClient.closed {
		t.Error("Expected mock client to be closed")
	}
}

func TestIsRetryableSSHError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pool := &Pool{logger: logger}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      &mockNetError{msg: "connection refused", temp: true},
			expected: true,
		},
		{
			name:     "connection timeout",
			err:      &mockNetError{msg: "connection timeout", timeout: true},
			expected: true,
		},
		{
			name:     "authentication failed",
			err:      &mockError{msg: "authentication failed"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pool.isRetryableSSHError(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for error: %v", tt.expected, result, tt.err)
			}
		})
	}
}

// Mock implementations for testing

type mockClient struct {
	closed bool
}

func (m *mockClient) RunCommand(ctx context.Context, command string) (string, error) {
	return "mock output", nil
}

func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

func (m *mockClient) IsHealthy() bool {
	return !m.closed
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

type mockNetError struct {
	msg     string
	temp    bool
	timeout bool
}

func (e *mockNetError) Error() string {
	return e.msg
}

func (e *mockNetError) Temporary() bool {
	return e.temp
}

func (e *mockNetError) Timeout() bool {
	return e.timeout
}
