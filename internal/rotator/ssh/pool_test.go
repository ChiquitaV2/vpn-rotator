package ssh

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	privateKey := "test-private-key"

	pool := NewPool(privateKey, logger, 5*time.Minute)

	if pool == nil {
		t.Fatal("NewPool returned nil")
	}

	if pool.privateKey != privateKey {
		t.Errorf("Expected private key %s, got %s", privateKey, pool.privateKey)
	}

	if pool.maxIdle != 5*time.Minute {
		t.Errorf("Expected maxIdle %v, got %v", 5*time.Minute, pool.maxIdle)
	}

	if pool.connections == nil {
		t.Error("connections map should be initialized")
	}
}

func TestPoolStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pool := NewPool("test-key", logger, 5*time.Minute)

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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pool := NewPool("test-key", logger, 1*time.Millisecond) // Very short idle time

	// Manually add a connection that should be considered idle
	pool.mutex.Lock()
	pool.connections["test-node"] = &Connection{
		client:   nil,                                   // Mock client
		lastUsed: time.Now().Add(-2 * time.Millisecond), // Make it older than maxIdle
		nodeIP:   "test-node",
	}
	pool.mutex.Unlock()

	// Verify connection exists
	stats := pool.GetStats()
	if stats.TotalConnections != 1 {
		t.Errorf("Expected 1 connection before cleanup, got %d", stats.TotalConnections)
	}

	// Run cleanup
	pool.CleanupIdleConnections()

	// Verify connection was removed
	stats = pool.GetStats()
	if stats.TotalConnections != 0 {
		t.Errorf("Expected 0 connections after cleanup, got %d", stats.TotalConnections)
	}
}

func TestIsRetryableSSHError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pool := NewPool("test-key", logger, 5*time.Minute)

	testCases := []struct {
		err       error
		retryable bool
	}{
		{nil, false},
		{context.DeadlineExceeded, false},
		{context.Canceled, false},
	}

	for _, tc := range testCases {
		result := pool.isRetryableSSHError(tc.err)
		if result != tc.retryable {
			t.Errorf("Expected isRetryableSSHError(%v) = %v, got %v", tc.err, tc.retryable, result)
		}
	}
}
