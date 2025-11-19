package ssh

import (
	"context"
	"testing"
	"time"

	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"golang.org/x/crypto/ssh"
)

func TestNewPool(t *testing.T) {
	logger := applogger.NewDevelopment("test")
	privateKey := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACCvVt9z9geYu6OXwOgii7N6hrQPv6ufeT3UzmPn8g55aAAAAJhWOTfhVjk3
4QAAAAtzc2gtZWQyNTUxOQAAACCvVt9z9geYu6OXwOgii7N6hrQPv6ufeT3UzmPn8g55aA
AAAEDmqaXKIinGHUxZ0J+sihUPP4E9PDGplOCAoBK49VunQ69W33P2B5i7o5fA6CKLs3qG
tA+/q595PdTOY+fyDnloAAAAEG93ZW5AUGFsbWEubG9jYWwBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----`

	pool := NewPool(privateKey, logger, 5*time.Minute, ssh.InsecureIgnoreHostKey())
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
	logger := applogger.NewDevelopment("test")
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
	logger := applogger.NewDevelopment("test")
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
