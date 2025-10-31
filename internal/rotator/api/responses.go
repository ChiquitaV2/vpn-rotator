package api

import (
	"encoding/json"
	"net/http"

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

// WriteErrorWithRequestID writes an error JSON response with a request ID.
func WriteErrorWithRequestID(w http.ResponseWriter, statusCode int, code, message, requestID string) error {
	return WriteJSON(w, statusCode, api.Response[any]{
		Success: false,
		Error: &api.ErrorInfo{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}
