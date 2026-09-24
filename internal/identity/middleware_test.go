package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/snapshot"
)

func TestAuthMiddleware(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	registry := snapshot.NewRegistry()

	// Register project snapshot
	projectID := "proj_test"
	env := "production"
	snap := core.NewSnapshot(projectID, env, 1)
	snap.AuthConfig = &core.AuthConfig{
		Provider: "oidc",
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-app"},
		JWKSURL:  testURL,
	}
	registry.Swap(snap)

	// Protected handler that verifies context injection
	var handled bool
	var capturedID *core.Identity
	var capturedToken string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
		capturedID, _ = FromContext(r.Context())
		capturedToken, _ = RawTokenFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	handler := AuthMiddleware(registry, verifier)(nextHandler)

	t.Run("missing Authorization header returns 401", func(t *testing.T) {
		handled = false
		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var errResp core.ErrorResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
		assert.Equal(t, "unauthenticated", errResp.Code)
		assert.NotEmpty(t, errResp.RequestID)
		assert.Equal(t, errResp.RequestID, w.Header().Get("X-Request-Id"))
	})

	t.Run("invalid authorization header format returns 401", func(t *testing.T) {
		handled = false
		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		req.Header.Set("X-Project-ID", projectID)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("missing X-Project-ID header returns 400", func(t *testing.T) {
		handled = false
		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var errResp core.ErrorResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
		assert.Equal(t, "invalid_request", errResp.Code)
	})

	t.Run("unregistered project returns 401", func(t *testing.T) {
		handled = false
		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		req.Header.Set("X-Project-ID", "proj_nonexistent")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("expired token returns 401", func(t *testing.T) {
		handled = false
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			snap.AuthConfig.Issuer,
			"user-exp",
			snap.AuthConfig.Audience,
			time.Now().Add(-10*time.Minute),
			time.Time{},
			nil,
		)

		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var errResp core.ErrorResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
		assert.Equal(t, "unauthenticated", errResp.Code)
		assert.Contains(t, errResp.Message, "expired")
	})

	t.Run("valid token passes and populates context", func(t *testing.T) {
		handled = false
		capturedID = nil
		capturedToken = ""

		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			snap.AuthConfig.Issuer,
			"user-valid-99",
			snap.AuthConfig.Audience,
			time.Now().Add(1*time.Hour),
			time.Time{},
			map[string]any{
				"role": "admin",
				"user_metadata": map[string]any{
					"plan": "team",
				},
			},
		)

		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		req.Header.Set("X-Request-Id", "req_custom_123")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "req_custom_123", w.Header().Get("X-Request-Id"))
		require.NotNil(t, capturedID)
		assert.Equal(t, "user-valid-99", capturedID.Subject)
		assert.Equal(t, projectID, capturedID.Project)
		assert.Equal(t, env, capturedID.Environment)
		assert.Equal(t, "team", capturedID.Claims["user_metadata.plan"])
		assert.Equal(t, tokenStr, capturedToken)
	})

	t.Run("respects cancelled request context", func(t *testing.T) {
		handled = false
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer some-token")
		req.Header.Set("X-Project-ID", projectID)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.False(t, handled)
	})
}
