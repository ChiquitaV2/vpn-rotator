package rotator

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
)

// NewServiceForTesting creates a service with injected mock components for testing
func NewServiceForTesting(cfg *config.Config, orchestrator OrchestratorInterface, scheduler SchedulerInterface, apiServer APIServerInterface, logger *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:                cfg,
		orchestrator:          orchestrator,
		scheduler:             scheduler,
		apiServer:             apiServer,
		logger:                logger,
		ctx:                   ctx,
		cancel:                cancel,
		signalChan:            make(chan os.Signal, 1),
		disableSignalHandling: true, // Disable signal handling in tests
	}

	return service
}

// Mock implementations for testing

type mockOrchestrator struct {
	shouldFail bool
}

func (m *mockOrchestrator) GetNodeConfig(ctx context.Context, nodeID string) (*models.NodeConfig, error) {
	if m.shouldFail {
		return nil, errors.New("mock orchestrator error")
	}
	return &models.NodeConfig{}, nil
}

func (m *mockOrchestrator) SelectNodeForPeer(ctx context.Context) (string, error) {
	if m.shouldFail {
		return "", errors.New("mock orchestrator error")
	}
	return "mock-node-id", nil
}

func (m *mockOrchestrator) GetNodeLoadBalance(ctx context.Context) (map[string]int, error) {
	if m.shouldFail {
		return nil, errors.New("mock orchestrator error")
	}
	return map[string]int{"mock-node-id": 0}, nil
}

func (m *mockOrchestrator) ShouldRotate(ctx context.Context) (bool, error) {
	if m.shouldFail {
		return false, errors.New("mock orchestrator error")
	}
	return false, nil
}

func (m *mockOrchestrator) RotateNodes(ctx context.Context) error {
	if m.shouldFail {
		return errors.New("mock orchestrator error")
	}
	return nil
}

func (m *mockOrchestrator) CleanupNodes(ctx context.Context) error {
	if m.shouldFail {
		return errors.New("mock orchestrator error")
	}
	return nil
}

func (m *mockOrchestrator) GetNodesByStatus(ctx context.Context, status string) ([]db.Node, error) {
	if m.shouldFail {
		return nil, errors.New("mock orchestrator error")
	}
	return []db.Node{}, nil
}

func (m *mockOrchestrator) DestroyNode(ctx context.Context, node db.Node) error {
	if m.shouldFail {
		return errors.New("mock orchestrator error")
	}
	return nil
}

type mockScheduler struct {
	started    bool
	stopped    bool
	startError error
	stopError  error
}

func (m *mockScheduler) Start(ctx context.Context) error {
	if m.startError != nil {
		return m.startError
	}
	m.started = true
	return nil
}

func (m *mockScheduler) Stop(ctx context.Context) error {
	if m.stopError != nil {
		return m.stopError
	}
	m.stopped = true
	return nil
}

type mockAPIServer struct {
	started    bool
	stopped    bool
	startError error
	stopError  error
}

func (m *mockAPIServer) Start(ctx context.Context) error {
	if m.startError != nil {
		return m.startError
	}
	m.started = true
	return nil
}

func (m *mockAPIServer) Stop(ctx context.Context) error {
	if m.stopError != nil {
		return m.stopError
	}
	m.stopped = true
	return nil
}

func TestService_StartStop(t *testing.T) {
	// Create test configuration
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	// Create mock components
	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create service
	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	// Test service creation
	if service == nil {
		t.Fatal("expected service to be created")
	}

	// Test health check before start (should fail - service not running)
	if err := service.Health(); err == nil {
		t.Error("expected health check to fail before start")
	}

	// Test start
	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		t.Fatalf("expected service to start successfully, got: %v", err)
	}

	// Verify service is running
	if !service.IsRunning() {
		t.Error("expected service to be running after start")
	}

	// Verify components were started
	if !scheduler.started {
		t.Error("expected scheduler to be started")
	}
	if !apiServer.started {
		t.Error("expected API server to be started")
	}

	// Test health check after start
	if err := service.Health(); err != nil {
		t.Errorf("expected health check to pass after start, got: %v", err)
	}

	// Test stop
	if err := service.Stop(ctx); err != nil {
		t.Fatalf("expected service to stop successfully, got: %v", err)
	}

	// Verify service is not running
	if service.IsRunning() {
		t.Error("expected service to not be running after stop")
	}

	// Verify components were stopped
	if !scheduler.stopped {
		t.Error("expected scheduler to be stopped")
	}
	if !apiServer.stopped {
		t.Error("expected API server to be stopped")
	}

	// Test health check after stop (context should be cancelled)
	time.Sleep(10 * time.Millisecond) // Give context cancellation time to propagate
	if err := service.Health(); err == nil {
		t.Error("expected health check to fail after stop")
	}
}

func TestService_StartAlreadyRunning(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Start service first time
	if err := service.Start(ctx); err != nil {
		t.Fatalf("expected service to start successfully, got: %v", err)
	}

	// Try to start again - should return error
	if err := service.Start(ctx); err == nil {
		t.Error("expected error when starting already running service")
	}

	// Clean up
	service.Stop(ctx)
}

func TestService_StopNotRunning(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Stop service without starting - should not error
	if err := service.Stop(ctx); err != nil {
		t.Errorf("expected no error when stopping non-running service, got: %v", err)
	}
}

func TestService_StartSchedulerError(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{
		startError: errors.New("scheduler start failed"),
	}
	apiServer := &mockAPIServer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Start should fail due to scheduler error
	if err := service.Start(ctx); err == nil {
		t.Error("expected error when scheduler fails to start")
	}

	// Service should not be running
	if service.IsRunning() {
		t.Error("expected service to not be running after start failure")
	}
}

func TestService_StartAPIServerError(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{
		startError: errors.New("API server start failed"),
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Start should fail due to API server error
	if err := service.Start(ctx); err == nil {
		t.Error("expected error when API server fails to start")
	}

	// Service should not be running
	if service.IsRunning() {
		t.Error("expected service to not be running after start failure")
	}

	// Scheduler should have been stopped during cleanup
	if !scheduler.stopped {
		t.Error("expected scheduler to be stopped during cleanup")
	}
}

func TestService_StopWithErrors(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{
		stopError: errors.New("scheduler stop failed"),
	}
	apiServer := &mockAPIServer{
		stopError: errors.New("API server stop failed"),
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Start service
	if err := service.Start(ctx); err != nil {
		t.Fatalf("expected service to start successfully, got: %v", err)
	}

	// Stop should return error but still complete shutdown
	if err := service.Stop(ctx); err == nil {
		t.Error("expected error when components fail to stop")
	}

	// Service should still be marked as not running
	if service.IsRunning() {
		t.Error("expected service to not be running after stop with errors")
	}
}

func TestService_StopTimeout(t *testing.T) {
	cfg := &config.Config{
		Service: config.ServiceConfig{
			ShutdownTimeout: 50 * time.Millisecond, // Very short timeout
		},
	}

	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	ctx := context.Background()

	// Start service
	if err := service.Start(ctx); err != nil {
		t.Fatalf("expected service to start successfully, got: %v", err)
	}

	// Create a context that will timeout quickly
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Stop with timeout context - should handle timeout gracefully
	err := service.Stop(timeoutCtx)
	if err == nil {
		t.Log("stop completed within timeout")
	} else {
		t.Logf("stop timed out as expected: %v", err)
	}

	// Service should eventually be marked as not running
	if service.IsRunning() {
		t.Error("expected service to not be running after stop")
	}
}

func TestService_Context(t *testing.T) {
	cfg := &config.Config{}
	orchestrator := &mockOrchestrator{}
	scheduler := &mockScheduler{}
	apiServer := &mockAPIServer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	service := NewServiceForTesting(cfg, orchestrator, scheduler, apiServer, logger)

	// Test context is available
	ctx := service.Context()
	if ctx == nil {
		t.Fatal("expected service context to be available")
	}

	// Test context is not cancelled initially
	select {
	case <-ctx.Done():
		t.Error("expected service context to not be cancelled initially")
	default:
		// Expected
	}

	// Start the service first so we can test context cancellation
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("expected service to start successfully, got: %v", err)
	}

	// Stop service and verify context is cancelled
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("expected service to stop successfully, got: %v", err)
	}

	// Context should be cancelled after stop
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("expected service context to be cancelled after stop")
	}
}
