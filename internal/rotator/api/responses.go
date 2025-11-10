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
		// It is! Translate the code to an HTTP status.
		message = domainErr.Error() // Start with the full error
		errorCode = domainErr.Code()
		metadata = domainErr.Metadata()

		switch domainErr.Code() {
		case apperrors.ErrCodeValidation, apperrors.ErrCodePeerValidation, apperrors.ErrCodeInvalidCIDR, apperrors.ErrCodeInvalidIPAddress:
			statusCode = http.StatusBadRequest
			message = "Validation failed: " + domainErr.Error()

		case apperrors.ErrCodeNodeNotFound, apperrors.ErrCodePeerNotFound, apperrors.ErrCodeSubnetNotFound:
			statusCode = http.StatusNotFound
			message = "Resource not found: " + domainErr.Error()

		case apperrors.ErrCodeNodeConflict, apperrors.ErrCodePeerConflict, apperrors.ErrCodePeerIPConflict, apperrors.ErrCodePeerKeyConflict:
			statusCode = http.StatusConflict
			message = "Resource conflict: " + domainErr.Error()

		case apperrors.ErrCodeNodeNotReady:
			// This is the special provisioning case
			statusCode = http.StatusServiceUnavailable // 503
			message = "Service is not ready, provisioning in progress. Please try again."

			// Add Retry-After header from error metadata
			if retry, ok := metadata["retry_after_sec"]; ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%v", retry))
			} else if wait, ok := metadata["estimated_wait_sec"]; ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%v", wait))
			}

		case apperrors.ErrCodeNodeAtCapacity, apperrors.ErrCodeCapacityExceeded, apperrors.ErrCodeSubnetExhausted:
			statusCode = http.StatusServiceUnavailable // 503
			message = "Service capacity exceeded. Please try again later."
		}
	}

	// Write the final JSON error response
	_ = WriteJSON(w, statusCode, api.Response[any]{
		Success: false,
		Error: &api.ErrorInfo{
			Code:      errorCode,
			Message:   message,
			RequestID: requestID,
			Metadata:  metadata, // Include metadata
		},
	})
}
