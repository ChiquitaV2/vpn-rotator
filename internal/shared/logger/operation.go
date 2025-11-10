package logger

import (
	"context"
	"log/slog"
	"time"
)

// Operation logs operation lifecycle (start/complete/fail)
type Operation struct {
	logger    *Logger
	ctx       context.Context
	name      string
	StartTime time.Time
	attrs     []any
}

// StartOp begins tracking an operation (replaces NewOperation + LogOperation)
func (l *Logger) StartOp(ctx context.Context, name string, args ...any) *Operation {
	op := &Operation{
		logger:    l,
		ctx:       ctx,
		name:      name,
		StartTime: time.Now(),
		attrs:     args,
	}

	attrs := append([]any{slog.String("operation", name)}, args...)
	l.WithContext(ctx).Info("operation started", attrs...)

	return op
}

// With adds attributes to the operation
func (op *Operation) With(args ...any) *Operation {
	op.attrs = append(op.attrs, args...)
	return op
}

// Complete logs successful operation completion
func (op *Operation) Complete(msg string, args ...any) {
	attrs := append(
		[]any{
			slog.String("operation", op.name),
			slog.Duration("duration_ms", time.Since(op.StartTime)),
		},
		op.attrs...,
	)
	attrs = append(attrs, args...)

	if msg == "" {
		msg = "operation completed"
	}

	op.logger.WithContext(op.ctx).Info(msg, attrs...)
}

// Fail logs failed operation
func (op *Operation) Fail(err error, msg string, args ...any) {
	attrs := append(
		[]any{
			slog.String("operation", op.name),
			slog.Duration("duration_ms", time.Since(op.StartTime)),
			slog.String("error", err.Error()),
		},
		op.attrs...,
	)
	attrs = append(attrs, args...)

	if msg == "" {
		msg = "operation failed"
	}

	op.logger.WithContext(op.ctx).Error(msg, attrs...)
}

// Progress logs operation progress (debug level)
func (op *Operation) Progress(msg string, args ...any) {
	attrs := append(
		[]any{
			slog.String("operation", op.name),
			slog.Duration("elapsed_ms", time.Since(op.StartTime)),
		},
		op.attrs...,
	)
	attrs = append(attrs, args...)

	op.logger.WithContext(op.ctx).Debug(msg, attrs...)
}
