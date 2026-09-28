package tools

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
)

var (
	// ErrUnsupportedAuthMode indicates an unrecognized downstream auth mode.
	ErrUnsupportedAuthMode = errors.New("unsupported downstream auth mode")
	// ErrMissingUserToken indicates forward_user_token was requested but no token exists in context.
	ErrMissingUserToken = errors.New("missing user token in request context")
	// ErrMissingSecret indicates a required secret or key is missing.
	ErrMissingSecret = errors.New("missing downstream auth secret")
)

// SecretResolver resolves a secret reference to its plaintext value.
type SecretResolver interface {
	ResolveSecret(ctx context.Context, ref string) (string, error)
}

// DefaultSecretResolver is a no-op / literal resolver that returns ref as literal if no prefix.
type DefaultSecretResolver struct{}

// ResolveSecret returns the ref as the literal secret value.
func (d *DefaultSecretResolver) ResolveSecret(_ context.Context, ref string) (string, error) {
	return ref, nil
}

// DownstreamAuthenticator implements core.Authenticator for HTTP requests.
type DownstreamAuthenticator struct {
	secrets SecretResolver
}

// NewDownstreamAuthenticator creates a new DownstreamAuthenticator.
func NewDownstreamAuthenticator(resolver SecretResolver) *DownstreamAuthenticator {
	if resolver == nil {
		resolver = &DefaultSecretResolver{}
	}
	return &DownstreamAuthenticator{secrets: resolver}
}

// Authenticate applies the downstream authentication strategy to the outbound HTTP request.
func (a *DownstreamAuthenticator) Authenticate(ctx context.Context, req any, id *core.Identity, cfg core.DownstreamAuthConfig) error {
	httpReq, ok := req.(*http.Request)
	if !ok {
		return fmt.Errorf("expected *http.Request, got %T", req)
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		return nil
	}

	switch mode {
	case "forward_user_token":
		return a.applyForwardUserToken(ctx, httpReq, cfg)
	case "service_token":
		return a.applyServiceToken(ctx, httpReq, cfg)
	case "signed_headers":
		return a.applySignedHeaders(ctx, httpReq, id, cfg)
	case "token_exchange":
		return a.applyTokenExchange(ctx, httpReq, id, cfg)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAuthMode, mode)
	}
}

func (a *DownstreamAuthenticator) applyForwardUserToken(ctx context.Context, req *http.Request, cfg core.DownstreamAuthConfig) error {
	token, ok := identity.RawTokenFromContext(ctx)
	if !ok || token == "" {
		return ErrMissingUserToken
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "Authorization"
	}

	if strings.EqualFold(headerName, "Authorization") {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set(headerName, token)
	}

	return nil
}

func (a *DownstreamAuthenticator) applyServiceToken(ctx context.Context, req *http.Request, cfg core.DownstreamAuthConfig) error {
	if cfg.SecretRef == "" {
		return ErrMissingSecret
	}

	secret, err := a.secrets.ResolveSecret(ctx, cfg.SecretRef)
	if err != nil {
		return fmt.Errorf("resolve service secret %q: %w", cfg.SecretRef, err)
	}
	if secret == "" {
		return fmt.Errorf("%w: secret %q is empty", ErrMissingSecret, cfg.SecretRef)
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "Authorization"
	}

	if strings.EqualFold(headerName, "Authorization") {
		req.Header.Set("Authorization", "Bearer "+secret)
	} else {
		req.Header.Set(headerName, secret)
	}

	return nil
}

func (a *DownstreamAuthenticator) applySignedHeaders(ctx context.Context, req *http.Request, id *core.Identity, cfg core.DownstreamAuthConfig) error {
	if cfg.SecretRef == "" {
		return ErrMissingSecret
	}

	secret, err := a.secrets.ResolveSecret(ctx, cfg.SecretRef)
	if err != nil {
		return fmt.Errorf("resolve hmac secret %q: %w", cfg.SecretRef, err)
	}
	if secret == "" {
		return fmt.Errorf("%w: hmac secret %q is empty", ErrMissingSecret, cfg.SecretRef)
	}

	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)

	// Read body for hash if present
	var bodyHash string
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("read request body for signing: %w", err)
		}
		// Reset body reader for transport
		req.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
		h := sha256.Sum256(bodyBytes)
		bodyHash = hex.EncodeToString(h[:])
	} else {
		h := sha256.Sum256([]byte(""))
		bodyHash = hex.EncodeToString(h[:])
	}

	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	// Canonical signature string: METHOD\nPATH\nTIMESTAMP\nBODY_HASH
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s", req.Method, path, timestamp, bodyHash)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Beavend-Signature", signature)
	req.Header.Set("X-Beavend-Timestamp", timestamp)

	if id != nil {
		if id.Subject != "" {
			req.Header.Set("X-User-Subject", id.Subject)
		}
		if id.Issuer != "" {
			req.Header.Set("X-User-Issuer", id.Issuer)
		}
	}

	return nil
}

func (a *DownstreamAuthenticator) applyTokenExchange(_ context.Context, req *http.Request, id *core.Identity, cfg core.DownstreamAuthConfig) error {
	// RFC 8693 token exchange placeholder
	// In production, exchanges subject token with external TokenService for downstream scoped token
	subject := "unknown"
	if id != nil && id.Subject != "" {
		subject = id.Subject
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "Authorization"
	}

	token := fmt.Sprintf("exchanged-token-for-%s", subject)
	if strings.EqualFold(headerName, "Authorization") {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set(headerName, token)
	}

	return nil
}
