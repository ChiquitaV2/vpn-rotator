package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	requestIDKey contextKey = "request_id"
)

// Middleware wraps an http.Handler and returns a new http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain chains multiple middlewares together.
func Chain(middlewares ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// RequestID generates a unique request ID for each request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// Logging logs HTTP requests and responses.
func Logging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := GetRequestID(r.Context())

			// Create response writer wrapper to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Create logger with request ID
			reqLogger := log.With("request_id", requestID)

			// Log incoming request
			reqLogger.InfoContext(r.Context(), "incoming request",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)

			// Serve the request
			next.ServeHTTP(wrapped, r)

			// Log response
			duration := time.Since(start)
			reqLogger.InfoContext(r.Context(), "request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"duration_ms", duration.Milliseconds(),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// CORS adds CORS headers to responses.
func CORS(allowedOrigins []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if allowed {
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if len(allowedOrigins) > 0 && allowedOrigins[0] == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Recovery recovers from panics and returns a 500 error.
func Recovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					requestID := GetRequestID(r.Context())
					log.ErrorContext(r.Context(), "panic recovered",
						"error", err,
						"request_id", requestID,
						"path", r.URL.Path,
						"method", r.Method,
					)

					WriteErrorWithRequestID(w, http.StatusInternalServerError,
						"internal_error",
						"An internal server error occurred",
						requestID,
					)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// ErrorHandling provides unified error handling and structured logging
func ErrorHandling(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a custom response writer to capture errors
			errorWriter := &errorResponseWriter{
				ResponseWriter: w,
				logger:         log,
				request:        r,
			}

			// Serve the request with error handling
			next.ServeHTTP(errorWriter, r)
		})
	}
}

// errorResponseWriter wraps http.ResponseWriter to provide unified error handling
type errorResponseWriter struct {
	http.ResponseWriter
	logger      *slog.Logger
	request     *http.Request
	statusCode  int
	headersSent bool
}

func (ew *errorResponseWriter) WriteHeader(code int) {
	if ew.headersSent {
		return
	}

	ew.statusCode = code
	ew.headersSent = true

	// Log error responses with structured context
	if code >= 400 {
		requestID := GetRequestID(ew.request.Context())

		logLevel := slog.LevelWarn
		if code >= 500 {
			logLevel = slog.LevelError
		}

		ew.logger.Log(ew.request.Context(), logLevel, "HTTP error response",
			slog.String("request_id", requestID),
			slog.String("method", ew.request.Method),
			slog.String("path", ew.request.URL.Path),
			slog.String("remote_addr", ew.request.RemoteAddr),
			slog.String("user_agent", ew.request.UserAgent()),
			slog.Int("status_code", code),
			slog.String("error_category", categorizeHTTPError(code)),
		)
	}

	ew.ResponseWriter.WriteHeader(code)
}

func (ew *errorResponseWriter) Write(data []byte) (int, error) {
	if !ew.headersSent {
		ew.WriteHeader(http.StatusOK)
	}
	return ew.ResponseWriter.Write(data)
}

// categorizeHTTPError categorizes HTTP error codes for better logging
func categorizeHTTPError(code int) string {
	switch {
	case code >= 400 && code < 500:
		switch code {
		case http.StatusBadRequest:
			return "client_error_bad_request"
		case http.StatusUnauthorized:
			return "client_error_unauthorized"
		case http.StatusForbidden:
			return "client_error_forbidden"
		case http.StatusNotFound:
			return "client_error_not_found"
		case http.StatusConflict:
			return "client_error_conflict"
		case http.StatusTooManyRequests:
			return "client_error_rate_limit"
		default:
			return "client_error"
		}
	case code >= 500:
		switch code {
		case http.StatusInternalServerError:
			return "server_error_internal"
		case http.StatusServiceUnavailable:
			return "server_error_unavailable"
		case http.StatusGatewayTimeout:
			return "server_error_timeout"
		default:
			return "server_error"
		}
	default:
		return "success"
	}
}

// CorrelationID adds correlation ID tracking for distributed tracing
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = uuid.New().String()
		}

		// Add correlation ID to response headers
		w.Header().Set("X-Correlation-ID", correlationID)

		// Add to context for downstream use
		ctx := context.WithValue(r.Context(), "correlation_id", correlationID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCorrelationID retrieves the correlation ID from the context
func GetCorrelationID(ctx context.Context) string {
	if correlationID, ok := ctx.Value("correlation_id").(string); ok {
		return correlationID
	}
	return ""
}

// StructuredLogging enhances the existing logging middleware with more structured fields
func StructuredLogging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := GetRequestID(r.Context())
			correlationID := GetCorrelationID(r.Context())

			// Create response writer wrapper to capture status code and response size
			wrapped := &structuredResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Create structured logger with request context
			reqLogger := log.With(
				slog.String("request_id", requestID),
				slog.String("correlation_id", correlationID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("query", r.URL.RawQuery),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
				slog.String("content_type", r.Header.Get("Content-Type")),
				slog.Int64("content_length", r.ContentLength),
			)

			// Log incoming request
			reqLogger.InfoContext(r.Context(), "incoming request")

			// Serve the request
			next.ServeHTTP(wrapped, r)

			// Calculate request duration
			duration := time.Since(start)

			// Log response with comprehensive metrics
			responseLogger := reqLogger.With(
				slog.Int("status", wrapped.statusCode),
				slog.Int64("response_size", wrapped.responseSize),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.Float64("duration_seconds", duration.Seconds()),
			)

			// Choose log level based on status code
			logLevel := slog.LevelInfo
			if wrapped.statusCode >= 400 && wrapped.statusCode < 500 {
				logLevel = slog.LevelWarn
			} else if wrapped.statusCode >= 500 {
				logLevel = slog.LevelError
			}

			responseLogger.Log(r.Context(), logLevel, "request completed")
		})
	}
}

// structuredResponseWriter wraps http.ResponseWriter to capture response metrics
type structuredResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	responseSize int64
}

func (srw *structuredResponseWriter) WriteHeader(code int) {
	srw.statusCode = code
	srw.ResponseWriter.WriteHeader(code)
}

func (srw *structuredResponseWriter) Write(data []byte) (int, error) {
	size, err := srw.ResponseWriter.Write(data)
	srw.responseSize += int64(size)
	return size, err
}

// RequestMetrics adds request metrics collection
func RequestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create metrics wrapper
		metricsWriter := &metricsResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Serve request
		next.ServeHTTP(metricsWriter, r)

		// Record metrics (in a real implementation, this would send to a metrics system)
		duration := time.Since(start)

		// Add metrics headers for debugging
		w.Header().Set("X-Response-Time", duration.String())
		w.Header().Set("X-Status-Code", strconv.Itoa(metricsWriter.statusCode))
	})
}

// metricsResponseWriter wraps http.ResponseWriter for metrics collection
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (mrw *metricsResponseWriter) WriteHeader(code int) {
	mrw.statusCode = code
	mrw.ResponseWriter.WriteHeader(code)
}
