package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
)

var (
	ErrOAuthInvalidState = errors.New("invalid or expired oauth state token")
	ErrOAuthDiscovery    = errors.New("oauth discovery failed")
	ErrOAuthDCR          = errors.New("dynamic client registration failed")
)

// PKCEPair contains PKCE code verifier and challenge.
type PKCEPair struct {
	CodeVerifier string `json:"code_verifier"`
	Challenge    string `json:"code_challenge"`
	Method       string `json:"code_challenge_method"` // "S256"
}

// GeneratePKCE creates a cryptographically secure PKCE verifier and S256 challenge.
func GeneratePKCE() (*PKCEPair, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return &PKCEPair{
		CodeVerifier: verifier,
		Challenge:    challenge,
		Method:       "S256",
	}, nil
}

// OAuthState holds state data bound to an administrative OAuth initiation flow.
type OAuthState struct {
	Token        string    `json:"token"`
	AdminUserID  string    `json:"admin_user_id"`
	ProjectID    string    `json:"project_id"`
	Environment  string    `json:"environment"`
	ToolID       string    `json:"tool_id"`
	CodeVerifier string    `json:"code_verifier"`
	RedirectURI  string    `json:"redirect_uri"`
	CreatedAt    time.Time `json:"created_at"`
}

// ComputeOAuthStateKey formats the Redis/CacheStore key for single-use OAuth state.
func ComputeOAuthStateKey(token string) string {
	return fmt.Sprintf("oauth:state:%s", token)
}

// SaveOAuthState stores an OAuth state record in CacheStore with given TTL (default 10m).
func SaveOAuthState(ctx context.Context, store core.CacheStore, state *OAuthState, ttl time.Duration) error {
	if store == nil {
		return errors.New("cache store is nil")
	}
	if state == nil || state.Token == "" {
		return errors.New("invalid oauth state")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal oauth state: %w", err)
	}
	return store.Set(ctx, ComputeOAuthStateKey(state.Token), data, ttl)
}

// ConsumeOAuthState atomically retrieves and consumes the single-use OAuth state token.
func ConsumeOAuthState(ctx context.Context, store core.CacheStore, token string) (*OAuthState, error) {
	if store == nil {
		return nil, errors.New("cache store is nil")
	}
	key := ComputeOAuthStateKey(token)
	data, err := store.Get(ctx, key)
	if err != nil || len(data) == 0 {
		return nil, ErrOAuthInvalidState
	}
	// Single-use: delete immediately
	_ = store.Delete(ctx, key)

	var st OAuthState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("unmarshal oauth state: %w", err)
	}
	return &st, nil
}

// NewOAuthState creates a new bound OAuth state record with random 32-byte hex token.
func NewOAuthState(adminUserID, projectID, env, toolID, codeVerifier, redirectURI string) (*OAuthState, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate state token: %w", err)
	}
	return &OAuthState{
		Token:        hex.EncodeToString(b),
		AdminUserID:  adminUserID,
		ProjectID:    projectID,
		Environment:  env,
		ToolID:       toolID,
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

// OAuthEndpoints represents discovered endpoints for an OAuth authorization server.
type OAuthEndpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
	JWKSURI               string `json:"jwks_uri,omitempty"`
}

// DiscoverOAuthEndpoints fetches the OAuth/OIDC metadata document via SafeHTTPClient.
func DiscoverOAuthEndpoints(ctx context.Context, client *http.Client, issuerURL string) (*OAuthEndpoints, error) {
	if client == nil {
		client = NewSafeHTTPClient(SafeDialerConfig{}, 10*time.Second)
	}

	cleanIssuer := strings.TrimRight(issuerURL, "/")
	discoveryURLs := []string{
		cleanIssuer + "/.well-known/oauth-authorization-server",
		cleanIssuer + "/.well-known/openid-configuration",
	}

	var lastErr error
	for _, u := range discoveryURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("discovery endpoint %s returned status %d", u, resp.StatusCode)
			continue
		}

		var ep OAuthEndpoints
		if err := json.Unmarshal(body, &ep); err != nil {
			lastErr = err
			continue
		}

		if ep.AuthorizationEndpoint != "" && ep.TokenEndpoint != "" {
			return &ep, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthDiscovery, lastErr)
	}
	return nil, ErrOAuthDiscovery
}

// DCRRegistrationRequest represents client metadata sent for Dynamic Client Registration (RFC 7591).
type DCRRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// DCRResult represents client credentials issued by DCR.
type DCRResult struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// RegisterClientDCR performs Dynamic Client Registration (RFC 7591) via SafeHTTPClient.
func RegisterClientDCR(ctx context.Context, client *http.Client, registrationEndpoint string, clientName string, redirectURIs []string) (*DCRResult, error) {
	if client == nil {
		client = NewSafeHTTPClient(SafeDialerConfig{}, 15*time.Second)
	}

	dcrReq := DCRRegistrationRequest{
		ClientName:              clientName,
		RedirectURIs:            redirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_post",
	}

	bodyBytes, err := json.Marshal(dcrReq)
	if err != nil {
		return nil, fmt.Errorf("marshal dcr request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create dcr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request error: %v", ErrOAuthDCR, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrOAuthDCR, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: registration returned status %d: %s", ErrOAuthDCR, resp.StatusCode, string(respBody))
	}

	var res DCRResult
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %v", ErrOAuthDCR, err)
	}

	if res.ClientID == "" {
		return nil, fmt.Errorf("%w: missing client_id in response", ErrOAuthDCR)
	}

	return &res, nil
}

// EncryptStoredOAuthTokens encrypts OAuth token payload using AES-256-GCM (packing nonce + ciphertext in base64).
func EncryptStoredOAuthTokens(masterKey []byte, tokenPayload any) (string, error) {
	key := masterKey
	if len(key) != 32 {
		key = secrets.DeriveKey(string(masterKey))
	}
	tokenBytes, err := json.Marshal(tokenPayload)
	if err != nil {
		return "", fmt.Errorf("marshal tokens: %w", err)
	}
	ciphertext, nonce, err := secrets.Encrypt(key, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("encrypt tokens: %w", err)
	}
	packed := append(nonce, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(packed), nil
}

// DecryptStoredOAuthTokens decrypts an AES-256-GCM ciphertext containing OAuth tokens.
func DecryptStoredOAuthTokens(masterKey []byte, encoded string, dest any) error {
	key := masterKey
	if len(key) != 32 {
		key = secrets.DeriveKey(string(masterKey))
	}
	packed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode token ciphertext: %w", err)
	}
	if len(packed) < 12 {
		return errors.New("ciphertext too short")
	}
	nonce := packed[:12]
	ciphertext := packed[12:]
	plaintext, err := secrets.Decrypt(key, ciphertext, nonce)
	if err != nil {
		return fmt.Errorf("decrypt tokens: %w", err)
	}
	return json.Unmarshal(plaintext, dest)
}
