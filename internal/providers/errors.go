package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/beavend/beavend/internal/core"
)

// NormalizeHTTPError maps an upstream HTTP error status and body to a standardized core.ErrorResponse.
func NormalizeHTTPError(statusCode int, body []byte, headers http.Header, requestID string) *core.ErrorResponse {
	var errEnv OpenAIErrorEnvelope
	var upstreamMsg string
	var upstreamType string
	var upstreamCode any

	if len(body) > 0 {
		if err := json.Unmarshal(body, &errEnv); err == nil && errEnv.Error.Message != "" {
			upstreamMsg = errEnv.Error.Message
			upstreamType = errEnv.Error.Type
			upstreamCode = errEnv.Error.Code
		} else {
			upstreamMsg = string(body)
		}
	}

	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("upstream provider error (HTTP %d)", statusCode)
	}

	var code string
	switch statusCode {
	case http.StatusBadRequest:
		code = "invalid_request"
	case http.StatusUnauthorized:
		code = "unauthenticated"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	case http.StatusBadGateway:
		code = "provider_error"
	case http.StatusServiceUnavailable:
		code = "unavailable"
	case http.StatusGatewayTimeout:
		code = "provider_error"
	default:
		if statusCode >= 500 {
			code = "provider_error"
		} else {
			code = "invalid_request"
		}
	}

	retryAfter := 0
	if headers != nil {
		if val := headers.Get("Retry-After"); val != "" {
			if s, err := strconv.Atoi(val); err == nil {
				retryAfter = s
			}
		}
	}

	details := make(map[string]any)
	if upstreamType != "" {
		details["upstream_type"] = upstreamType
	}
	if upstreamCode != nil {
		details["upstream_code"] = upstreamCode
	}
	details["http_status"] = statusCode

	return &core.ErrorResponse{
		Code:       code,
		Message:    upstreamMsg,
		RequestID:  requestID,
		RetryAfter: retryAfter,
		Details:    details,
	}
}
