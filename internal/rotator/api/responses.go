package api

import (
	"encoding/json"
	"net/http"
)

// Response is the standard API response wrapper.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
}

// ErrorInfo contains detailed error information.
type ErrorInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteSuccess writes a successful JSON response.
func WriteSuccess(w http.ResponseWriter, data interface{}) error {
	return WriteJSON(w, http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// WriteError writes an error JSON response.
func WriteError(w http.ResponseWriter, statusCode int, code, message string) error {
	return WriteJSON(w, statusCode, Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	})
}

// WriteErrorWithRequestID writes an error JSON response with a request ID.
func WriteErrorWithRequestID(w http.ResponseWriter, statusCode int, code, message, requestID string) error {
	return WriteJSON(w, statusCode, Response{
		Success: false,
		Error: &ErrorInfo{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// ConfigResponse represents the VPN configuration response.
type ConfigResponse struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerIP        string `json:"server_ip"`
	Port            int    `json:"port"`
}

// ProvisioningResponse represents a response when provisioning is in progress.
type ProvisioningResponse struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	EstimatedWait int    `json:"estimated_wait_seconds"`
	RetryAfter    int    `json:"retry_after_seconds"`
}
