package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteSuccess writes a successful JSON response.
func WriteSuccess[T any](w http.ResponseWriter, data T) error {
	return WriteJSON(w, http.StatusOK, api.Response[T]{
		Success: true,
		Data:    data,
	})
}

// WriteErrorResponse is the new centralized error handler for the API.
// It logs the error and translates DomainErrors into correct HTTP responses.
func WriteErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	logger := GetLogger(ctx)
	requestID := GetRequestID(ctx)

	// Log the error with all its rich context
	logger.ErrorCtx(ctx, "API request failed", err)

	// Default to a 500 Internal Server Error
	statusCode := http.StatusInternalServerError
	errorCode := apperrors.ErrCodeInternal
	message := "An internal server error occurred"
	metadata := make(map[string]any)

	// Check if it's our standard DomainError
	if domainErr, ok := err.(apperrors.DomainError); ok {
		errorCode = domainErr.Code()
		metadata = domainErr.Metadata()

		// Map error codes to HTTP status codes and messages
		statusCode, message = mapErrorCodeToHTTP(domainErr)

		// Special handling for provisioning errors - add Retry-After header
		if domainErr.Code() == apperrors.ErrCodeNodeNotReady {
			if retry, ok := metadata["retry_after_sec"]; ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%v", retry))
			} else if wait, ok := metadata["estimated_wait_sec"]; ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%v", wait))
			}
		}
	}

	// Write the final JSON error response
	_ = WriteJSON(w, statusCode, api.Response[any]{
		Success: false,
		Error: &api.ErrorInfo{
			Code:      errorCode,
			Message:   message,
			RequestID: requestID,
			Metadata:  metadata,
		},
	})
}

// mapErrorCodeToHTTP maps domain error codes to HTTP status codes and user-friendly messages.
func mapErrorCodeToHTTP(err apperrors.DomainError) (int, string) {
	code := err.Code()
	errMsg := err.Error()

	switch code {
	// 400 Bad Request - validation errors
	case apperrors.ErrCodeValidation, apperrors.ErrCodePeerValidation,
		apperrors.ErrCodeInvalidCIDR, apperrors.ErrCodeInvalidIPAddress:
		return http.StatusBadRequest, "Validation failed: " + errMsg

	// 404 Not Found - resource not found
	case apperrors.ErrCodeNodeNotFound, apperrors.ErrCodePeerNotFound,
		apperrors.ErrCodeSubnetNotFound:
		return http.StatusNotFound, "Resource not found: " + errMsg

	// 409 Conflict - resource conflicts
	case apperrors.ErrCodeNodeConflict, apperrors.ErrCodePeerConflict,
		apperrors.ErrCodePeerIPConflict, apperrors.ErrCodePeerKeyConflict:
		return http.StatusConflict, "Resource conflict: " + errMsg

	// 503 Service Unavailable - provisioning or capacity issues
	case apperrors.ErrCodeNodeNotReady:
		return http.StatusServiceUnavailable, "Service is not ready, provisioning in progress. Please try again."

	case apperrors.ErrCodeNodeAtCapacity, apperrors.ErrCodeCapacityExceeded,
		apperrors.ErrCodeSubnetExhausted:
		return http.StatusServiceUnavailable, "Service capacity exceeded. Please try again later."

	// 500 Internal Server Error - default fallback
	default:
		return http.StatusInternalServerError, "An internal server error occurred"
	}
}
