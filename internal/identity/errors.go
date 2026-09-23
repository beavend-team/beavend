package identity

import (
	"errors"
	"fmt"
)

var (
	// ErrMissingToken is returned when the Authorization header or Bearer token is missing.
	ErrMissingToken = errors.New("missing or empty bearer token")

	// ErrInvalidTokenFormat is returned when the token is not a valid JWT format.
	ErrInvalidTokenFormat = errors.New("invalid jwt format")

	// ErrInvalidAlgorithm is returned when the token uses a forbidden, insecure, or unwhitelisted algorithm.
	ErrInvalidAlgorithm = errors.New("unsupported or insecure signing algorithm")

	// ErrTokenExpired is returned when the token expiration claim (exp) has passed.
	ErrTokenExpired = errors.New("token is expired")

	// ErrTokenNotYetValid is returned when the token not-before claim (nbf) is in the future.
	ErrTokenNotYetValid = errors.New("token is not yet valid")

	// ErrInvalidIssuer is returned when the token issuer (iss) does not match expected issuer.
	ErrInvalidIssuer = errors.New("token issuer mismatch")

	// ErrInvalidAudience is returned when the token audience (aud) does not match expected audience.
	ErrInvalidAudience = errors.New("token audience mismatch")

	// ErrInvalidSignature is returned when cryptographic signature verification fails.
	ErrInvalidSignature = errors.New("signature verification failed")

	// ErrKeyNotFound is returned when the key ID (kid) cannot be found in the JWKS keyset.
	ErrKeyNotFound = errors.New("signing key not found in jwks")

	// ErrUnauthenticated is returned as the general unauthenticated error.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrProjectNotFound is returned when the requested project snapshot is not found in the registry.
	ErrProjectNotFound = errors.New("project or environment not found")

	// ErrAuthConfigMissing is returned when authentication is not configured for a project.
	ErrAuthConfigMissing = errors.New("authentication configuration missing")
)

// AuthError represents a structured authentication error that maps to RFC 7807 responses.
type AuthError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	RequestID  string         `json:"request_id,omitempty"`
	StatusCode int            `json:"-"`
	Cause      error          `json:"-"`
	Details    map[string]any `json:"details,omitempty"`
}

func (e *AuthError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AuthError) Unwrap() error {
	return e.Cause
}

// NewAuthError creates a standard 401 unauthenticated error.
func NewAuthError(message string, cause error, reqID string) *AuthError {
	return &AuthError{
		Code:       "unauthenticated",
		Message:    message,
		RequestID:  reqID,
		StatusCode: 401,
		Cause:      cause,
	}
}
