package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// ValidationError represents a validation error with field-specific details
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrors represents multiple validation errors
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

func (ve ValidationErrors) Error() string {
	var messages []string
	for _, err := range ve.Errors {
		messages = append(messages, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}
	return strings.Join(messages, "; ")
}

// ValidateConnectRequest validates a connect request
func ValidateConnectRequest(req *api.ConnectRequest) error {
	var errors []ValidationError

	// Validate that either public key is provided or key generation is requested
	if req.PublicKey == nil && !req.GenerateKeys {
		errors = append(errors, ValidationError{
			Field:   "public_key",
			Message: "either provide a public_key or set generate_keys to true",
		})
	}

	// If public key is provided, validate its format
	if req.PublicKey != nil {
		if err := validateWireGuardPublicKey(*req.PublicKey); err != nil {
			errors = append(errors, ValidationError{
				Field:   "public_key",
				Message: err.Error(),
			})
		}
	}

	// Cannot have both public key and generate keys
	if req.PublicKey != nil && req.GenerateKeys {
		errors = append(errors, ValidationError{
			Field:   "generate_keys",
			Message: "cannot provide public_key and request key generation simultaneously",
		})
	}

	if len(errors) > 0 {
		return ValidationErrors{Errors: errors}
	}

	return nil
}

// ValidateDisconnectRequest validates a disconnect request
func ValidateDisconnectRequest(req *api.DisconnectRequest) error {
	var errors []ValidationError

	if strings.TrimSpace(req.PeerID) == "" {
		errors = append(errors, ValidationError{
			Field:   "peer_id",
			Message: "peer_id is required and cannot be empty",
		})
	}

	if len(errors) > 0 {
		return ValidationErrors{Errors: errors}
	}

	return nil
}

// ValidatePeerListParams validates query parameters for peer listing
func ValidatePeerListParams(r *http.Request) (map[string]interface{}, error) {
	var errors []ValidationError
	params := make(map[string]interface{})

	// Validate node_id parameter
	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		if strings.TrimSpace(nodeID) == "" {
			errors = append(errors, ValidationError{
				Field:   "node_id",
				Message: "node_id cannot be empty",
			})
		} else {
			params["node_id"] = nodeID
		}
	}

	// Validate status parameter
	if status := r.URL.Query().Get("status"); status != "" {
		if !isValidPeerStatus(status) {
			errors = append(errors, ValidationError{
				Field:   "status",
				Message: "status must be one of: active, disconnected, removing",
			})
		} else {
			params["status"] = status
		}
	}

	// Validate limit parameter
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		var limit int
		if err := json.Unmarshal([]byte(limitStr), &limit); err != nil || limit < 1 || limit > 1000 {
			errors = append(errors, ValidationError{
				Field:   "limit",
				Message: "limit must be a number between 1 and 1000",
			})
		} else {
			params["limit"] = limit
		}
	}

	// Validate offset parameter
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		var offset int
		if err := json.Unmarshal([]byte(offsetStr), &offset); err != nil || offset < 0 {
			errors = append(errors, ValidationError{
				Field:   "offset",
				Message: "offset must be a non-negative number",
			})
		} else {
			params["offset"] = offset
		}
	}

	if len(errors) > 0 {
		return nil, ValidationErrors{Errors: errors}
	}

	return params, nil
}

// validateWireGuardPublicKey validates a WireGuard public key format
func validateWireGuardPublicKey(key string) error {
	if key == "" {
		return fmt.Errorf("public key cannot be empty")
	}

	// WireGuard keys are base64-encoded 32-byte values = 44 characters with '=' padding
	if len(key) != 44 {
		return fmt.Errorf("public key must be 44 characters long, got %d", len(key))
	}

	// Check it's valid base64
	validBase64 := regexp.MustCompile(`^[A-Za-z0-9+/]+=*$`)
	if !validBase64.MatchString(key) {
		return fmt.Errorf("public key contains invalid base64 characters")
	}

	return nil
}

// validateIPAddress validates an IP address format
func validateIPAddress(ip string) error {
	if ip == "" {
		return fmt.Errorf("IP address cannot be empty")
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address format: %s", ip)
	}

	// Ensure it's IPv4
	if parsedIP.To4() == nil {
		return fmt.Errorf("IP address must be IPv4: %s", ip)
	}

	return nil
}

// isValidPeerStatus checks if a peer status is valid
func isValidPeerStatus(status string) bool {
	switch status {
	case "active", "disconnected", "removing":
		return true
	default:
		return false
	}
}

// WriteValidationError writes a validation error response
func WriteValidationError(w http.ResponseWriter, err error, requestID string) error {
	if validationErr, ok := err.(ValidationErrors); ok {
		return WriteJSON(w, http.StatusBadRequest, api.Response[any]{
			Success: false,
			Error: &api.ErrorInfo{
				Code:      "validation_error",
				Message:   validationErr.Error(),
				RequestID: requestID,
			},
		})
	}

	return WriteErrorWithRequestID(w, http.StatusBadRequest, "validation_error", err.Error(), requestID)
}

// ParseJSONRequest parses and validates a JSON request body
func ParseJSONRequest[T any](r *http.Request, target *T) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("content-type must be application/json")
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return nil
}
