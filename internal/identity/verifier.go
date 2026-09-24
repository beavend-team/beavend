package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/beavend/beavend/internal/core"
)

const (
	defaultClockSkew = 60 * time.Second

	// Standard Google/Firebase JWKS URL
	firebaseJWKSURL = "https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com"
	firebaseIssBase = "https://securetoken.google.com/"
)

// DefaultAllowedAlgorithms lists the default whitelisted signature algorithms.
var DefaultAllowedAlgorithms = map[jwa.SignatureAlgorithm]bool{
	jwa.RS256: true,
	jwa.RS384: true,
	jwa.RS512: true,
	jwa.ES256: true,
	jwa.ES384: true,
	jwa.ES512: true,
	jwa.EdDSA: true,
	jwa.PS256: true,
	jwa.PS384: true,
	jwa.PS512: true,
}

// Verifier validates JWT signatures, expiration, issuer, and audience using cached JWKS.
type Verifier struct {
	jwks *JWKSManager
}

// NewVerifier creates a new token Verifier.
func NewVerifier(jwks *JWKSManager) *Verifier {
	if jwks == nil {
		jwks = NewJWKSManager()
	}
	return &Verifier{
		jwks: jwks,
	}
}

// JWKS returns the underlying JWKSManager.
func (v *Verifier) JWKS() *JWKSManager {
	return v.jwks
}

// ResolveAuthConfig applies provider-specific conventions (Firebase, Supabase) to AuthConfig.
func ResolveAuthConfig(cfg *core.AuthConfig) *core.AuthConfig {
	if cfg == nil {
		return nil
	}

	resolved := *cfg
	provider := strings.ToLower(strings.TrimSpace(resolved.Provider))

	switch provider {
	case "firebase":
		if resolved.JWKSURL == "" {
			resolved.JWKSURL = firebaseJWKSURL
		}
		if resolved.ProjectID != "" {
			if resolved.Issuer == "" {
				resolved.Issuer = firebaseIssBase + resolved.ProjectID
			}
			if len(resolved.Audience) == 0 {
				resolved.Audience = []string{resolved.ProjectID}
			}
		}
	case "supabase":
		if resolved.JWKSURL == "" && resolved.Issuer != "" {
			resolved.JWKSURL = strings.TrimSuffix(resolved.Issuer, "/") + "/.well-known/jwks.json"
		}
		if len(resolved.Audience) == 0 {
			resolved.Audience = []string{"authenticated"}
		}
	}

	if resolved.ClockSkew <= 0 {
		resolved.ClockSkew = defaultClockSkew
	}

	return &resolved
}

// Verify validates the JWT string against the provided auth configuration.
func (v *Verifier) Verify(ctx context.Context, tokenStr string, cfg *core.AuthConfig) (jwt.Token, error) {
	if strings.TrimSpace(tokenStr) == "" {
		return nil, ErrMissingToken
	}

	resolvedCfg := ResolveAuthConfig(cfg)
	if resolvedCfg == nil {
		return nil, ErrAuthConfigMissing
	}

	tokenBytes := []byte(tokenStr)

	// 1. Inspect JWS headers without verifying yet to check alg & kid
	msg, err := jws.Parse(tokenBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTokenFormat, err)
	}

	sigs := msg.Signatures()
	if len(sigs) == 0 {
		return nil, fmt.Errorf("%w: no signatures found", ErrInvalidTokenFormat)
	}

	sig := sigs[0]
	headers := sig.ProtectedHeaders()
	alg := headers.Algorithm()

	// 2. Reject "none" / unsigned tokens
	if alg == jwa.NoSignature || strings.EqualFold(string(alg), "none") {
		return nil, fmt.Errorf("%w: none algorithm is forbidden", ErrInvalidAlgorithm)
	}

	// 3. Reject symmetric HMAC algorithms (prevent symmetric/asymmetric confusion)
	if alg == jwa.HS256 || alg == jwa.HS384 || alg == jwa.HS512 {
		return nil, fmt.Errorf("%w: symmetric hmac algorithms are forbidden", ErrInvalidAlgorithm)
	}

	// 4. Algorithm whitelist enforcement
	allowedMap := DefaultAllowedAlgorithms
	if len(resolvedCfg.AllowedAlgorithms) > 0 {
		allowedMap = make(map[jwa.SignatureAlgorithm]bool)
		for _, a := range resolvedCfg.AllowedAlgorithms {
			allowedMap[jwa.SignatureAlgorithm(a)] = true
		}
	}

	if !allowedMap[alg] {
		return nil, fmt.Errorf("%w: algorithm %q is not permitted", ErrInvalidAlgorithm, alg)
	}

	// 5. Retrieve cached JWKS
	if resolvedCfg.JWKSURL == "" {
		return nil, fmt.Errorf("%w: missing jwks_url", ErrAuthConfigMissing)
	}

	set, err := v.jwks.Get(ctx, resolvedCfg.JWKSURL)
	if err != nil {
		return nil, fmt.Errorf("retrieve jwks: %w", err)
	}

	kid := headers.KeyID()
	var verifyKey jwk.Key
	if kid != "" {
		key, found := set.LookupKeyID(kid)
		if !found {
			// Trigger single-flight JWKS refresh for key rotation
			refreshedSet, refreshErr := v.jwks.Refresh(ctx, resolvedCfg.JWKSURL)
			if refreshErr != nil {
				return nil, fmt.Errorf("%w: failed to refresh jwks for key %s: %v", ErrKeyNotFound, kid, refreshErr)
			}
			set = refreshedSet
			key, found = set.LookupKeyID(kid)
			if !found {
				return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, kid)
			}
		}
		verifyKey = key
	}

	// 5b. Cross-check key algorithm and usage if specified on the JWK
	if verifyKey != nil {
		if keyAlg := verifyKey.Algorithm(); keyAlg != nil && keyAlg.String() != "" && keyAlg.String() != string(alg) {
			return nil, fmt.Errorf("%w: key algorithm %s does not match token algorithm %s", ErrInvalidAlgorithm, keyAlg.String(), alg)
		}
		if keyUsage := verifyKey.KeyUsage(); keyUsage != "" && keyUsage != "sig" {
			return nil, fmt.Errorf("%w: key usage is %q, expected 'sig'", ErrInvalidAlgorithm, keyUsage)
		}
	}

	// 6. Verify signature using jwx
	var parseOptions []jwt.ParseOption
	if verifyKey != nil {
		parseOptions = append(parseOptions, jwt.WithKey(alg, verifyKey))
	} else {
		parseOptions = append(parseOptions, jwt.WithKeySet(set))
	}
	parseOptions = append(parseOptions, jwt.WithValidate(false)) // we perform explicit claims verification below

	token, err := jwt.Parse(tokenBytes, parseOptions...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	// 7. Ensure subject (sub) and issuer (iss) are non-empty
	if strings.TrimSpace(token.Subject()) == "" {
		return nil, fmt.Errorf("%w: missing or empty subject (sub) claim", ErrInvalidTokenFormat)
	}
	if strings.TrimSpace(token.Issuer()) == "" {
		return nil, fmt.Errorf("%w: missing or empty issuer (iss) claim", ErrInvalidTokenFormat)
	}

	// 8. Validate expiration (exp) and not-before (nbf) with clock skew tolerance
	now := time.Now()
	skew := resolvedCfg.ClockSkew

	if !token.Expiration().IsZero() {
		if now.Add(-skew).After(token.Expiration()) {
			return nil, fmt.Errorf("%w: expired at %s", ErrTokenExpired, token.Expiration().UTC().Format(time.RFC3339))
		}
	}

	if !token.NotBefore().IsZero() {
		if now.Add(skew).Before(token.NotBefore()) {
			return nil, fmt.Errorf("%w: not valid before %s", ErrTokenNotYetValid, token.NotBefore().UTC().Format(time.RFC3339))
		}
	}

	// 9. Validate issuer (iss)
	if resolvedCfg.Issuer != "" {
		if token.Issuer() != resolvedCfg.Issuer {
			return nil, fmt.Errorf("%w: got %q, expected %q", ErrInvalidIssuer, token.Issuer(), resolvedCfg.Issuer)
		}
	}

	// 10. Validate audience (aud)
	if len(resolvedCfg.Audience) > 0 {
		matched := false
		tokenAuds := token.Audience()
		for _, expected := range resolvedCfg.Audience {
			for _, actual := range tokenAuds {
				if actual == expected {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("%w: got %v, expected any of %v", ErrInvalidAudience, tokenAuds, resolvedCfg.Audience)
		}
	}

	return token, nil
}
