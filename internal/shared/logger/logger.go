package logger

import (
	"context"
	"log/slog"
	"os"
)

// ContextKey is the type for context keys used in logging
type ContextKey string

const (
	// RequestIDKey is the context key for request IDs
	RequestIDKey ContextKey = "request_id"
	// NodeIDKey is the context key for node IDs
	NodeIDKey ContextKey = "node_id"
	// OperationKey is the context key for operation names
	OperationKey ContextKey = "operation"
)

// Logger wraps slog.Logger with additional helper methods
type Logger struct {
	*slog.Logger
}

// New creates a new Logger with the specified level and format
func New(level, format string) *Logger {
	var handler slog.Handler

	// Parse level
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	// Create handler based on format
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

// WithRequestID returns a new logger with the request ID in context
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger: l.Logger.With(slog.String("request_id", requestID)),
	}
}

// WithNodeID returns a new logger with the node ID in context
func (l *Logger) WithNodeID(nodeID string) *Logger {
	return &Logger{
		Logger: l.Logger.With(slog.String("node_id", nodeID)),
	}
}

// WithOperation returns a new logger with the operation name in context
func (l *Logger) WithOperation(operation string) *Logger {
	return &Logger{
		Logger: l.Logger.With(slog.String("operation", operation)),
	}
}

// WithContext extracts common fields from context and returns a new logger
func (l *Logger) WithContext(ctx context.Context) *Logger {
	logger := l.Logger

	// Extract request ID
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
		logger = logger.With(slog.String("request_id", requestID))
	}

	// Extract node ID
	if nodeID, ok := ctx.Value(NodeIDKey).(string); ok && nodeID != "" {
		logger = logger.With(slog.String("node_id", nodeID))
	}

	// Extract operation
	if operation, ok := ctx.Value(OperationKey).(string); ok && operation != "" {
		logger = logger.With(slog.String("operation", operation))
	}

	return &Logger{Logger: logger}
}

// WithAttrs returns a new logger with the provided attributes
func (l *Logger) WithAttrs(attrs ...slog.Attr) *Logger {
	return &Logger{
		Logger: l.Logger.With(attrsToAny(attrs)...),
	}
}

// InfoContext logs at Info level with context
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Info(msg, args...)
}

// DebugContext logs at Debug level with context
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Debug(msg, args...)
}

// WarnContext logs at Warn level with context
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Warn(msg, args...)
}

// ErrorContext logs at Error level with context
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Error(msg, args...)
}

// LogProvisionStart logs the start of a provisioning operation
func (l *Logger) LogProvisionStart(ctx context.Context, serverName string) {
	l.WithContext(ctx).Info("starting node provisioning",
		slog.String("server_name", serverName),
	)
}

// LogProvisionSuccess logs successful provisioning
func (l *Logger) LogProvisionSuccess(ctx context.Context, nodeID, ip string) {
	l.WithContext(ctx).Info("node provisioned successfully",
		slog.String("node_id", nodeID),
		slog.String("ip", ip),
	)
}

// LogProvisionFailure logs failed provisioning
func (l *Logger) LogProvisionFailure(ctx context.Context, stage string, err error) {
	l.WithContext(ctx).Error("provisioning failed",
		slog.String("stage", stage),
		slog.String("error", err.Error()),
	)
}

// LogRotationStart logs the start of rotation
func (l *Logger) LogRotationStart(ctx context.Context, oldNodeID string) {
	l.WithContext(ctx).Info("starting node rotation",
		slog.String("old_node_id", oldNodeID),
	)
}

// LogRotationSuccess logs successful rotation
func (l *Logger) LogRotationSuccess(ctx context.Context, oldNodeID, newNodeID string) {
	l.WithContext(ctx).Info("node rotation completed",
		slog.String("old_node_id", oldNodeID),
		slog.String("new_node_id", newNodeID),
	)
}

// LogRotationFailure logs failed rotation
func (l *Logger) LogRotationFailure(ctx context.Context, oldNodeID string, err error) {
	l.WithContext(ctx).Error("rotation failed",
		slog.String("old_node_id", oldNodeID),
		slog.String("error", err.Error()),
	)
}

// LogDestructionStart logs the start of node destruction
func (l *Logger) LogDestructionStart(ctx context.Context, nodeID string) {
	l.WithContext(ctx).Info("starting node destruction",
		slog.String("node_id", nodeID),
	)
}

// LogDestructionSuccess logs successful destruction
func (l *Logger) LogDestructionSuccess(ctx context.Context, nodeID string) {
	l.WithContext(ctx).Info("node destroyed successfully",
		slog.String("node_id", nodeID),
	)
}

// LogHTTPRequest logs an HTTP request
func (l *Logger) LogHTTPRequest(ctx context.Context, method, path string, statusCode int, duration int64) {
	l.WithContext(ctx).Info("http request",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", statusCode),
		slog.Int64("duration_ms", duration),
	)
}

// LogDatabaseQuery logs a database query (for debugging)
func (l *Logger) LogDatabaseQuery(ctx context.Context, query string, duration int64) {
	l.WithContext(ctx).Debug("database query",
		slog.String("query", query),
		slog.Int64("duration_ms", duration),
	)
}

// Helper function to convert slog.Attr to []any
func attrsToAny(attrs []slog.Attr) []any {
	result := make([]any, len(attrs))
	for i, attr := range attrs {
		result[i] = attr
	}
	return result
}

// AddRequestIDToContext adds a request ID to the context
func AddRequestIDToContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// AddNodeIDToContext adds a node ID to the context
func AddNodeIDToContext(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, NodeIDKey, nodeID)
}

// AddOperationToContext adds an operation name to the context
func AddOperationToContext(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, OperationKey, operation)
}

// GetRequestIDFromContext retrieves the request ID from context
func GetRequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}
