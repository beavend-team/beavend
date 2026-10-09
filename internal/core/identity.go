package core

import (
	"context"
	"fmt"
	"time"
)

const (
	identityContextKey contextKey = "beavend_identity"
)

// ContextWithIdentity returns a new context containing the Identity.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, id)
}

// IdentityFromContext extracts the Identity from context if present.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	if ctx == nil {
		return nil, false
	}
	id, ok := ctx.Value(identityContextKey).(*Identity)
	return id, ok
}

// AuthConfig defines identity provider and JWT verification parameters for a project environment.
type AuthConfig struct {
	Provider          string        `json:"provider" yaml:"provider"` // "oidc", "firebase", "supabase", "jwt"
	Issuer            string        `json:"issuer" yaml:"issuer"`
	Audience          []string      `json:"audience" yaml:"audience"`
	JWKSURL           string        `json:"jwks_url" yaml:"jwks_url"`
	AllowedAlgorithms []string      `json:"allowed_algorithms,omitempty" yaml:"allowed_algorithms,omitempty"`
	ProjectID         string        `json:"project_id,omitempty" yaml:"project_id,omitempty"` // Provider project ID (e.g. Firebase project_id)
	ClockSkew         time.Duration `json:"clock_skew,omitempty" yaml:"clock_skew,omitempty"`
	AllowAnonymous    bool          `json:"allow_anonymous,omitempty" yaml:"allow_anonymous,omitempty"`
}

// Identity represents the normalized end-user identity derived from a validated token.
type Identity struct {
	Project     string         `json:"project" yaml:"project"`
	Environment string         `json:"environment" yaml:"environment"`
	Issuer      string         `json:"issuer" yaml:"issuer"`
	Subject     string         `json:"subject" yaml:"subject"`
	Audience    string         `json:"audience" yaml:"audience"`
	Claims      map[string]any `json:"claims" yaml:"claims"`
}

// StableKey returns the unique composite identifier for this identity: project:issuer:subject.
func (i Identity) StableKey() string {
	return fmt.Sprintf("%s:%s:%s", i.Project, i.Issuer, i.Subject)
}

// UserKey returns the issuer:subject identifier.
func (i Identity) UserKey() string {
	return fmt.Sprintf("%s:%s", i.Issuer, i.Subject)
}

// Claim returns a claim value if present.
func (i Identity) Claim(key string) (any, bool) {
	if i.Claims == nil {
		return nil, false
	}
	val, ok := i.Claims[key]
	return val, ok
}

// ClaimString returns a claim as string if present and of string type.
func (i Identity) ClaimString(key string) (string, bool) {
	val, ok := i.Claim(key)
	if !ok {
		return "", false
	}
	s, ok := val.(string)
	return s, ok
}

// ClaimBool returns a claim as bool if present and of boolean type.
func (i Identity) ClaimBool(key string) (bool, bool) {
	val, ok := i.Claim(key)
	if !ok {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}

// HasAudience checks if target audience matches either the Audience field or any entry in the "aud" claim.
func (i Identity) HasAudience(target string) bool {
	if i.Audience == target {
		return true
	}
	if audVal, ok := i.Claims["aud"]; ok {
		switch a := audVal.(type) {
		case string:
			return a == target
		case []string:
			for _, aud := range a {
				if aud == target {
					return true
				}
			}
		case []any:
			for _, item := range a {
				if str, isStr := item.(string); isStr && str == target {
					return true
				}
			}
		}
	}
	return false
}
