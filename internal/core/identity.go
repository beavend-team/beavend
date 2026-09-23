package core

import "fmt"

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
