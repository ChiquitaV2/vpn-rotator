package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/google/uuid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	requestIDKey contextKey = "request_id"
	loggerKey    contextKey = "logger"
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

// RequestID generates a unique request ID and injects a request-scoped logger.
func RequestID(baseLogger *applogger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Add request ID to response header
			w.Header().Set("X-Request-ID", requestID)

			// Create a new logger scoped to this request
			reqLogger := baseLogger.With(string(requestIDKey), requestID)

			// Add both the ID and the scoped logger to the context
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			ctx = context.WithValue(ctx, loggerKey, reqLogger)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// GetLogger retrieves the request-scoped logger from the context.
func GetLogger(ctx context.Context) *applogger.Logger {
	if logger, ok := ctx.Value(loggerKey).(*applogger.Logger); ok {
		return logger
	}
	// Fallback to a default logger if not found
	return applogger.NewDevelopment("fallback")
}

// Logging logs HTTP requests and responses using the applogger.
func Logging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// Get the request-scoped logger from the context
			logger := GetLogger(r.Context())

			// Create response writer wrapper to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Log incoming request
			logger.InfoContext(r.Context(), "incoming request",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)

			// Serve the request
			next.ServeHTTP(wrapped, r)

			// Log completed request using our standardized helper
			logger.HTTPRequest(
				r.Context(),
				r.Method,
				r.URL.Path,
				wrapped.statusCode,
				time.Since(start),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	written      bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
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
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After") // Added Retry-After
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
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get logger from context
			logger := GetLogger(r.Context())

			defer func() {
				if err := recover(); err != nil {
					// Create a standard domain error from the panic
					panicErr := apperrors.NewSystemError(
						apperrors.ErrCodeInternal,
						"panic recovered",
						false,
						fmt.Errorf("%v", err),
					).WithMetadata("path", r.URL.Path).
						WithMetadata("method", r.Method)

					// Log the error
					logger.ErrorCtx(r.Context(), "panic recovered", panicErr)

					// Write the standard error response
					WriteErrorResponse(w, r, panicErr)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
