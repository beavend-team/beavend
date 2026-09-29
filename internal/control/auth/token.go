package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	CookieSessionName = "beavend_admin_session"
	DefaultTokenTTL   = 24 * time.Hour
)

// AdminUser represents an authenticated administrative user.
type AdminUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TokenClaims represents the claims contained inside a signed admin session token.
type TokenClaims struct {
	UserID    string `json:"sub"`
	Email     string `json:"email"`
	Role      Role   `json:"role"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// TokenManager manages token generation and verification using HMAC-SHA256.
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager creates a new TokenManager with given secret and TTL.
func NewTokenManager(secret []byte, ttl time.Duration) *TokenManager {
	if len(secret) == 0 {
		secret = []byte("default-beavend-admin-insecure-secret-key-change-me")
	}
	if ttl == 0 {
		ttl = DefaultTokenTTL
	}
	return &TokenManager{
		secret: secret,
		ttl:    ttl,
	}
}

// GenerateToken creates a signed session token string for the given admin user.
func (m *TokenManager) GenerateToken(u *AdminUser) (string, error) {
	now := time.Now().UTC()
	claims := TokenClaims{
		UserID:    u.ID,
		Email:     u.Email,
		Role:      u.Role,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.ttl).Unix(),
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	sig := m.sign(payloadB64)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s", payloadB64, sigB64), nil
}

// ValidateToken parses, verifies HMAC signature, and checks expiration of a session token string.
func (m *TokenManager) ValidateToken(tokenStr string) (*TokenClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 2 {
		return nil, errors.New("malformed session token format")
	}

	payloadB64, sigB64 := parts[0], parts[1]
	expectedSig := m.sign(payloadB64)
	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.New("invalid payload encoding")
	}

	var claims TokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	now := time.Now().UTC().Unix()
	if claims.ExpiresAt < now {
		return nil, errors.New("session token expired")
	}

	return &claims, nil
}

// SetSessionCookie sets the HTTP-only session cookie on the response.
func (m *TokenManager) SetSessionCookie(w http.ResponseWriter, tokenStr string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieSessionName,
		Value:    tokenStr,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie instructs the browser to delete the session cookie.
func (m *TokenManager) ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieSessionName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *TokenManager) sign(data string) []byte {
	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(data))
	return h.Sum(nil)
}
