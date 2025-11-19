package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/errors"
	"github.com/lmittmann/tint"
)

// Logger wraps slog.Logger with domain-specific helpers while staying thin
type Logger struct {
	*slog.Logger
	config LoggerConfig
}

// LogLevel represents the logging level
type LogLevel string

const (
	LevelTrace LogLevel = "trace"
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// OutputFormat represents the log output format
type OutputFormat string

const (
	FormatJSON OutputFormat = "json"
	FormatText OutputFormat = "text"
)

// LoggerConfig holds configuration for the logger
type LoggerConfig struct {
	Level      LogLevel     `mapstructure:"level" yaml:"level" json:"level"`
	Format     OutputFormat `mapstructure:"format" yaml:"format" json:"format"`
	AddSource  bool         `mapstructure:"add_source" yaml:"add_source" json:"add_source"`
	Component  string       `mapstructure:"component" yaml:"component" json:"component"`
	Version    string       `mapstructure:"version" yaml:"version" json:"version"`
	TimeFormat string       `mapstructure:"time_format" yaml:"time_format" json:"time_format"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() LoggerConfig {
	return LoggerConfig{
		Level:      LevelInfo,
		Format:     FormatText,
		AddSource:  false,
		Component:  "vpn-rotator",
		Version:    "unknown",
		TimeFormat: time.RFC3339,
	}
}

// New creates a new logger with the provided configuration
func New(config LoggerConfig) *Logger {
	level := parseLogLevel(config.Level)
	handler := createHandler(config, level)

	slogger := slog.New(handler)

	return &Logger{
		Logger: slogger,
		config: config,
	}
}

// NewDevelopment creates a logger optimized for development
func NewDevelopment(component string) *Logger {
	return New(LoggerConfig{
		Level:      LevelDebug,
		Format:     FormatText,
		AddSource:  true,
		Component:  component,
		Version:    "dev",
		TimeFormat: time.Kitchen,
	})
}

// NewProduction creates a logger optimized for production
func NewProduction(component, version string) *Logger {
	return New(LoggerConfig{
		Level:      LevelInfo,
		Format:     FormatJSON,
		AddSource:  false,
		Component:  component,
		Version:    version,
		TimeFormat: time.RFC3339,
	})
}

// Context keys for structured logging
type contextKey string

const (
	RequestIDKey   contextKey = "request_id"
	CorrelationKey contextKey = "correlation_id"
	NodeIDKey      contextKey = "node_id"
	PeerIDKey      contextKey = "peer_id"
	OperationKey   contextKey = "operation"
	TraceIDKey     contextKey = "trace_id"
	SpanIDKey      contextKey = "span_id"
	UserIDKey      contextKey = "user_id"
)

// Core logger methods that add value over raw slog

// With returns a new logger with additional attributes
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		Logger: l.Logger.With(args...),
		config: l.config,
	}
}

// WithComponent returns a logger scoped to a sub-component (hierarchical)
func (l *Logger) WithComponent(name string) *Logger {
	l.config.Component = name
	return &Logger{
		Logger: l.Logger,
		config: l.config,
	}
}

// WithContext extracts logging context and returns a scoped logger
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := extractContextAttrs(ctx)
	attrs = append(attrs, slog.String("component", l.config.Component))
	attrs = append(attrs, slog.String("version", l.config.Version))
	if len(attrs) == 0 {
		return l
	}

	return &Logger{
		Logger: l.Logger.With(attrsToAny(attrs)...),
		config: l.config,
	}
}

// Unwrap returns the underlying slog.Logger for direct access
func (l *Logger) Unwrap() *slog.Logger {
	return l.Logger
}

// Enhanced context-aware logging methods

// ErrorCtx logs an error with automatic context enrichment
// This is the main improvement - simplifies error logging
func (l *Logger) ErrorCtx(ctx context.Context, msg string, err error, args ...any) {
	attrs := []any{slog.String("error", err.Error())}

	// Extract domain error details if available
	if domainErr, ok := err.(errors.DomainError); ok {
		attrs = append(attrs,
			slog.String("error_domain", domainErr.Domain()),
			slog.String("error_code", domainErr.Code()),
			slog.Bool("retryable", domainErr.Retryable()),
		)

		// Add metadata
		if metadata := domainErr.Metadata(); len(metadata) > 0 {
			for k, v := range metadata {
				attrs = append(attrs, slog.Any(k, v))
			}
		}
	}

	attrs = append(attrs, args...)
	l.WithContext(ctx).Error(msg, attrs...)
}

// Trace logs at trace level (maps to Debug with prefix)
func (l *Logger) Trace(msg string, args ...any) {
	if l.config.Level == LevelTrace {
		l.Debug(msg, args...)
	}
}

// TraceCtx logs at trace level with context
func (l *Logger) TraceCtx(ctx context.Context, msg string, args ...any) {
	if l.config.Level == LevelTrace {
		l.WithContext(ctx).Debug(msg, args...)
	}
}

// Domain-specific helpers (these add real value)

// HTTPRequest logs HTTP request/response with smart level selection
func (l *Logger) HTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration, args ...any) {
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	} else if status >= 400 {
		level = slog.LevelWarn
	}

	attrs := []any{
		slog.String("http_method", method),
		slog.String("http_path", path),
		slog.Int("http_status", status),
		slog.Duration("duration_ms", duration),
	}
	attrs = append(attrs, args...)

	msg := fmt.Sprintf("%s %s %d", method, path, status)
	l.WithContext(ctx).Log(ctx, level, msg, attrs...)
}

// DBQuery logs database operations with slow query detection
func (l *Logger) DBQuery(ctx context.Context, operation, table string, duration time.Duration, args ...any) {
	attrs := []any{
		slog.String("db_operation", operation),
		slog.String("db_table", table),
		slog.Duration("duration_ms", duration),
	}
	attrs = append(attrs, args...)

	msg := fmt.Sprintf("%s %s", operation, table)

	// Warn on slow queries
	if duration > 100*time.Millisecond {
		l.WithContext(ctx).Warn(msg+" (slow)", attrs...)
	} else {
		l.WithContext(ctx).Debug(msg, attrs...)
	}
}

// StackTrace logs a stack trace for debugging (debug level only)
func (l *Logger) StackTrace(ctx context.Context, msg string) {
	if l.config.Level != LevelDebug && l.config.Level != LevelTrace {
		return
	}

	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)

	l.WithContext(ctx).Debug(msg, slog.String("stack", string(buf[:n])))
}

// Helper functions

func parseLogLevel(level LogLevel) slog.Level {
	switch level {
	case LevelTrace, LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func createHandler(config LoggerConfig, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: config.AddSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Simplify time in development
			if a.Key == slog.TimeKey && config.Format == FormatText {
				return slog.Attr{
					Key:   "time",
					Value: slog.StringValue(a.Value.Time().Format(config.TimeFormat)),
				}
			}
			return a
		},
	}

	switch config.Format {
	case FormatJSON:
		return slog.NewJSONHandler(os.Stdout, opts)
	case FormatText:
		return tint.NewHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: config.TimeFormat,
			AddSource:  config.AddSource,
			NoColor:    false,
		})
	default:
		return slog.NewJSONHandler(os.Stdout, opts)
	}
}

func extractContextAttrs(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr

	// Extract all known context values
	contextKeys := []contextKey{
		RequestIDKey, CorrelationKey, NodeIDKey, PeerIDKey,
		OperationKey, TraceIDKey, SpanIDKey, UserIDKey,
	}

	for _, key := range contextKeys {
		if val := getFromContext[string](ctx, key); val != "" {
			attrs = append(attrs, slog.String(string(key), val))
		}
	}

	return attrs
}

func getFromContext[T any](ctx context.Context, key contextKey) T {
	if val, ok := ctx.Value(key).(T); ok {
		return val
	}
	var zero T
	return zero
}

func attrsToAny(attrs []slog.Attr) []any {
	result := make([]any, len(attrs))
	for i, attr := range attrs {
		result[i] = attr
	}
	return result
}

// Context helper functions for adding IDs to context

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, CorrelationKey, id)
}

func WithNodeID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, NodeIDKey, id)
}

func WithPeerID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, PeerIDKey, id)
}

func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, OperationKey, operation)
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, TraceIDKey, id)
}

func WithSpanID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, SpanIDKey, id)
}

func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, UserIDKey, id)
}

// Getters for context values

func GetRequestID(ctx context.Context) string {
	return getFromContext[string](ctx, RequestIDKey)
}

func GetCorrelationID(ctx context.Context) string {
	return getFromContext[string](ctx, CorrelationKey)
}

func GetNodeID(ctx context.Context) string {
	return getFromContext[string](ctx, NodeIDKey)
}

func GetPeerID(ctx context.Context) string {
	return getFromContext[string](ctx, PeerIDKey)
}

func GetOperation(ctx context.Context) string {
	return getFromContext[string](ctx, OperationKey)
}

func GetTraceID(ctx context.Context) string {
	return getFromContext[string](ctx, TraceIDKey)
}

func GetSpanID(ctx context.Context) string {
	return getFromContext[string](ctx, SpanIDKey)
}

func GetUserID(ctx context.Context) string {
	return getFromContext[string](ctx, UserIDKey)
}
