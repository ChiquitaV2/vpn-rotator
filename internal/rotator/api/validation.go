package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// ValidateConnectRequest validates a connect request
func ValidateConnectRequest(req *api.ConnectRequest) apperrors.DomainError {
	// Validate that either public key is provided or key generation is requested
	if req.PublicKey == nil && !req.GenerateKeys {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation,
			"either provide a public_key or set generate_keys to true", false, nil).
			WithMetadata("field", "public_key")
	}

	// If public key is provided, validate its format
	if req.PublicKey != nil {
		if err := validateWireGuardPublicKey(*req.PublicKey); err != nil {
			return err
		}
	}

	// Cannot have both public key and generate keys
	if req.PublicKey != nil && req.GenerateKeys {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation,
			"cannot provide public_key and request key generation simultaneously", false, nil).
			WithMetadata("field", "generate_keys")
	}

	return nil
}

// ValidateDisconnectRequest validates a disconnect request
func ValidateDisconnectRequest(req *api.DisconnectRequest) apperrors.DomainError {
	if strings.TrimSpace(req.PeerID) == "" {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation,
			"peer_id is required and cannot be empty", false, nil).
			WithMetadata("field", "peer_id")
	}
	return nil
}

// ValidatePeerListParams validates query parameters for peer listing
func ValidatePeerListParams(r *http.Request) (map[string]interface{}, error) {
	params := make(map[string]interface{})

	// Validate node_id parameter
	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		if strings.TrimSpace(nodeID) == "" {
			return nil, apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "node_id cannot be empty", false, nil).
				WithMetadata("field", "node_id")
		}
		params["node_id"] = nodeID
	}

	// Validate status parameter
	if status := r.URL.Query().Get("status"); status != "" {
		if !isValidPeerStatus(status) {
			return nil, apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "status must be one of: active, disconnected, removing", false, nil).
				WithMetadata("field", "status")
		}
		params["status"] = status
	}

	// Validate limit parameter
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 1000 {
			return nil, apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "limit must be a number between 1 and 1000", false, err).
				WithMetadata("field", "limit")
		}
		params["limit"] = limit
	}

	// Validate offset parameter
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			return nil, apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "offset must be a non-negative number", false, err).
				WithMetadata("field", "offset")
		}
		params["offset"] = offset
	}

	return params, nil
}

// validateWireGuardPublicKey validates a WireGuard public key format
func validateWireGuardPublicKey(key string) apperrors.DomainError {
	if key == "" || !crypto.IsValidWireGuardKey(key) {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "public key cannot be empty", false, nil)
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
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "invalid IP address format", false, nil).
			WithMetadata("ip", ip)
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

// ParseJSONRequest parses and validates a JSON request body
func ParseJSONRequest[T any](r *http.Request, target *T) apperrors.DomainError {
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "content-type must be application/json", false, nil)
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "invalid JSON", false, err)
	}

	return nil
}
