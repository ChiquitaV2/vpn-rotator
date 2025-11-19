// explanation: add unit tests for Logger.ErrorCtx and WithContext to ensure JSON output contains enriched fields
package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/errors"
)

func TestErrorCtx_EnrichesAndOutputsJSON(t *testing.T) {
	// Prepare a buffer-backed JSON handler so we can inspect output
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Format = FormatJSON
	cfg.Component = "test-component"
	cfg.Version = "v1"
	cfg.TimeFormat = time.RFC3339

	level := parseLogLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level, AddSource: false}
	handler := slog.NewJSONHandler(&buf, opts)
	slogger := slog.New(handler)
	l := &Logger{Logger: slogger, config: cfg}

	// Build a context with several known keys
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-123")
	ctx = WithOperation(ctx, "op-test")
	ctx = WithTraceID(ctx, "trace-1")
	ctx = WithUserID(ctx, "user-7")

	// Create a DomainError and log it
	domainErr := errors.NewSystemError(errors.ErrCodeConfiguration, "invalid config", false, nil)
	l.ErrorCtx(ctx, "operation failed", domainErr, slog.String("extra", "value"))

	// Decode JSON output and assert expected fields are present
	var entry map[string]any
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&entry); err != nil {
		t.Fatalf("failed to decode log output: %v", err)
	}

	// keys we expect to be present (some are implementation-specific but useful)
	wantKeys := []string{
		"error",
		"error_domain",
		"error_code",
		"retryable",
		"request_id",
		"operation",
		"trace_id",
		"component",
		"version",
		"extra",
		"msg",
		"time",
		"level",
	}

	for _, k := range wantKeys {
		if _, ok := entry[k]; !ok {
			t.Errorf("missing key %q in log entry: %+v", k, entry)
		}
	}

	if got := entry["error_code"]; got != errors.ErrCodeConfiguration {
		t.Errorf("unexpected error_code: got %v want %v", got, errors.ErrCodeConfiguration)
	}
}
