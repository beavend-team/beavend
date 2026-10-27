package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestAuthMiddleware(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	registry := newMockSnapshotResolver()

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
	snap.Apps["assistant"] = &core.AIApp{
		ID:         "assistant",
		Name:       "Assistant",
		AuthConfig: snap.AuthConfig,
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

	t.Run("allow anonymous request when configured", func(t *testing.T) {
		handled = false
		anonSnap := core.NewSnapshot("proj_anon", "production", 1)
		anonSnap.Apps["public"] = &core.AIApp{
			ID:   "public",
			Name: "Public",
			AuthConfig: &core.AuthConfig{
				Provider:       "jwt",
				AllowAnonymous: true,
			},
		}
		registry.Swap(anonSnap)

		req := httptest.NewRequest(http.MethodGet, "/v1/apps/public", nil)
		req.Header.Set("X-Project-ID", "proj_anon")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "anonymous", capturedID.Subject)
		assert.Equal(t, "proj_anon", capturedID.Project)
	})

	t.Run("valid app api key authenticates successfully", func(t *testing.T) {
		handled = false
		appKeyStr := "bv_app_sec_test_secret_1234567890abcdef"
		h := sha256.Sum256([]byte(appKeyStr))
		keyHash := hex.EncodeToString(h[:])

		snapWithKey := core.NewSnapshot("proj_key", "production", 1)
		snapWithKey.AppAPIKeys[keyHash] = &core.AppAPIKey{
			ID:          "key_1",
			AppID:       "my_app",
			ProjectID:   "proj_key",
			Environment: "production",
			Name:        "Test Backend Key",
			KeyPrefix:   "bv_app_sec_test",
			KeyHash:     keyHash,
			CreatedAt:   time.Now().UTC(),
		}
		registry.Swap(snapWithKey)

		// Create chi router to test URLParam matching
		r := chi.NewRouter()
		r.Use(AuthMiddleware(registry, verifier))
		r.Post("/v1/apps/{ai_app_id}/chat", nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/my_app/chat", nil)
		req.Header.Set("Authorization", "Bearer "+appKeyStr)
		req.Header.Set("X-Project-ID", "proj_key")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "my_app", capturedID.Subject)
		assert.Equal(t, "beavend:app_key", capturedID.Issuer)
		assert.Equal(t, "my_app", capturedID.Claims["app_id"])
		assert.Equal(t, "key_1", capturedID.Claims["key_id"])
		assert.Equal(t, true, capturedID.Claims["machine"])
		assert.Empty(t, capturedToken, "RawToken must never be populated from AppAPIKey")
	})

	t.Run("app api key asserting subject rejected when MayAssertSubject is false", func(t *testing.T) {
		handled = false
		appKeyStr := "bv_app_sec_test_secret_1234567890abcdef"
		r := chi.NewRouter()
		r.Use(AuthMiddleware(registry, verifier))
		r.Post("/v1/apps/{ai_app_id}/chat", nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/my_app/chat", nil)
		req.Header.Set("Authorization", "Bearer "+appKeyStr)
		req.Header.Set("X-Project-ID", "proj_key")
		req.Header.Set("X-Environment", "production")
		req.Header.Set("X-Beavend-Subject", "usr_eve")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("app api key asserting subject permitted when MayAssertSubject is true with claim filtering", func(t *testing.T) {
		handled = false
		capturedID = nil
		capturedToken = ""

		keyToken := "bv_app_sec_assert_subject_allowed_key_9999"
		h := sha256.Sum256([]byte(keyToken))
		hStr := hex.EncodeToString(h[:])

		snapAssert := core.NewSnapshot("proj_assert", "production", 1)
		snapAssert.Apps["app_bridge"] = &core.AIApp{
			ID:          "app_bridge",
			ProjectID:   "proj_assert",
			Environment: "production",
			AuthConfig: &core.AuthConfig{
				Issuer: "https://my-company.auth0.com/",
			},
		}
		snapAssert.AppAPIKeys[hStr] = &core.AppAPIKey{
			ID:               "key_assert_1",
			AppID:            "app_bridge",
			ProjectID:        "proj_assert",
			Environment:      "production",
			Name:             "Backend Key With Subject Assertion",
			KeyPrefix:        "bv_app_sec_assert",
			KeyHash:          hStr,
			MayAssertSubject: true,
			AllowedClaims:    []string{"tier", "organization_id", "email"},
			CreatedAt:        time.Now().UTC(),
		}
		registry.Swap(snapAssert)

		r := chi.NewRouter()
		r.Use(AuthMiddleware(registry, verifier))
		r.Post("/v1/apps/{ai_app_id}/chat", nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_bridge/chat", nil)
		req.Header.Set("Authorization", "Bearer "+keyToken)
		req.Header.Set("X-Project-ID", "proj_assert")
		req.Header.Set("X-Environment", "production")
		req.Header.Set("X-Beavend-Subject", "usr_alice_123")
		req.Header.Set("X-Beavend-Claims", `{"tier":"enterprise","organization_id":"org_456","admin":true,"scope":"superuser","unallowed_field":"hack"}`)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "usr_alice_123", capturedID.Subject)
		assert.Equal(t, "https://my-company.auth0.com/", capturedID.Issuer)
		assert.Equal(t, "enterprise", capturedID.Claims["tier"])
		assert.Equal(t, "org_456", capturedID.Claims["organization_id"])
		assert.Nil(t, capturedID.Claims["admin"], "reserved claim 'admin' must be stripped")
		assert.NotEqual(t, "superuser", capturedID.Claims["scope"], "reserved claim 'scope' must be stripped")
		assert.Nil(t, capturedID.Claims["unallowed_field"], "unallowed claim must be stripped")
		assert.Empty(t, capturedToken, "RawToken must never be populated from AppAPIKey")
	})

	t.Run("app api key asserting subject with malformed X-Beavend-Claims returns 400", func(t *testing.T) {
		handled = false
		keyToken := "bv_app_sec_assert_subject_allowed_key_9999"

		r := chi.NewRouter()
		r.Use(AuthMiddleware(registry, verifier))
		r.Post("/v1/apps/{ai_app_id}/chat", nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_bridge/chat", nil)
		req.Header.Set("Authorization", "Bearer "+keyToken)
		req.Header.Set("X-Project-ID", "proj_assert")
		req.Header.Set("X-Environment", "production")
		req.Header.Set("X-Beavend-Subject", "usr_alice_123")
		req.Header.Set("X-Beavend-Claims", `{malformed:json:bad}`)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "malformed X-Beavend-Claims JSON header")
	})

	t.Run("app api key scoped to different app returns 403", func(t *testing.T) {
		handled = false
		appKeyStr := "bv_app_sec_test_secret_1234567890abcdef"

		r := chi.NewRouter()
		r.Use(AuthMiddleware(registry, verifier))
		r.Post("/v1/apps/{ai_app_id}/chat", nextHandler)

		// Request for different_app with my_app's key
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/different_app/chat", nil)
		req.Header.Set("Authorization", "Bearer "+appKeyStr)
		req.Header.Set("X-Project-ID", "proj_key")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid app api key returns 401", func(t *testing.T) {
		handled = false
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/my_app/chat", nil)
		req.Header.Set("Authorization", "Bearer bv_app_invalid_key_value")
		req.Header.Set("X-Project-ID", "proj_key")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unauthenticated request with X-Playground header without admin credentials is rejected", func(t *testing.T) {
		handled = false
		capturedID = nil
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", nil)
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		req.Header.Set("X-Playground", "true")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Nil(t, capturedID)
	})

	t.Run("playground request with X-Admin-Token populates admin identity", func(t *testing.T) {
		handled = false
		capturedID = nil

		adminValidator := AdminTokenValidatorFunc(func(tokenStr string) (*AdminClaims, error) {
			if tokenStr == "valid-admin-session-token" {
				return &AdminClaims{
					UserID: "admin_123",
					Email:  "admin@example.com",
					Role:   "owner",
				}, nil
			}
			return nil, assert.AnError
		})

		handlerWithAdmin := AuthMiddleware(registry, verifier, WithAdminTokenValidator(adminValidator))(nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", nil)
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		req.Header.Set("X-Playground", "true")
		req.Header.Set("X-Admin-Token", "valid-admin-session-token")
		w := httptest.NewRecorder()

		handlerWithAdmin.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "admin_123", capturedID.Subject)
		assert.Equal(t, "beavend:admin", capturedID.Issuer)
		assert.Equal(t, "admin@example.com", capturedID.Claims["email"])
		assert.Equal(t, "owner", capturedID.Claims["role"])
	})

	t.Run("admin bearer token authorizes as admin directly", func(t *testing.T) {
		handled = false
		capturedID = nil

		adminValidator := AdminTokenValidatorFunc(func(tokenStr string) (*AdminClaims, error) {
			if tokenStr == "admin-token-xyz" {
				return &AdminClaims{
					UserID: "admin_456",
					Email:  "dev@example.com",
					Role:   "editor",
				}, nil
			}
			return nil, assert.AnError
		})

		handlerWithAdmin := AuthMiddleware(registry, verifier, WithAdminTokenValidator(adminValidator))(nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", nil)
		req.Header.Set("Authorization", "Bearer admin-token-xyz")
		req.Header.Set("X-Project-ID", projectID)
		req.Header.Set("X-Environment", env)
		w := httptest.NewRecorder()

		handlerWithAdmin.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "admin_456", capturedID.Subject)
		assert.Equal(t, "beavend:admin", capturedID.Issuer)
		assert.Equal(t, "dev@example.com", capturedID.Claims["email"])
		assert.Equal(t, "editor", capturedID.Claims["role"])
	})

	t.Run("playground request with simulated JSON token populates claims when admin token is provided", func(t *testing.T) {
		handled = false
		capturedID = nil

		// Create snapshot with no auth provider configured
		unauthSnap := core.NewSnapshot("proj_unauth", "production", 1)
		unauthSnap.AuthConfig = nil
		registry.Swap(unauthSnap)

		adminValidator := AdminTokenValidatorFunc(func(tokenStr string) (*AdminClaims, error) {
			if tokenStr == "admin-key" {
				return &AdminClaims{UserID: "admin_root", Email: "admin@root.com", Role: "owner"}, nil
			}
			return nil, assert.AnError
		})
		handlerWithAdmin := AuthMiddleware(registry, verifier, WithAdminTokenValidator(adminValidator))(nextHandler)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", nil)
		req.Header.Set("Authorization", `Bearer {"sub":"test_user_789","plan":"pro","tier":"gold"}`)
		req.Header.Set("X-Project-ID", "proj_unauth")
		req.Header.Set("X-Environment", "production")
		req.Header.Set("X-Playground", "true")
		req.Header.Set("X-Admin-Token", "admin-key")
		w := httptest.NewRecorder()

		handlerWithAdmin.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "test_user_789", capturedID.Subject)
		assert.Equal(t, "beavend:playground_simulated", capturedID.Issuer)
		assert.Equal(t, "pro", capturedID.Claims["plan"])
		assert.Equal(t, "gold", capturedID.Claims["tier"])
	})

	t.Run("simulated JSON token without admin credentials is rejected", func(t *testing.T) {
		handled = false
		capturedID = nil

		unauthSnap := core.NewSnapshot("proj_unauth_2", "production", 1)
		unauthSnap.AuthConfig = nil
		registry.Swap(unauthSnap)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", nil)
		req.Header.Set("Authorization", `Bearer {"sub":"malicious_user","role":"admin"}`)
		req.Header.Set("X-Project-ID", "proj_unauth_2")
		req.Header.Set("X-Environment", "production")
		req.Header.Set("X-Playground", "true")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.False(t, handled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Nil(t, capturedID)
	})

	t.Run("app-level anonymous provider allows requests without token", func(t *testing.T) {
		handled = false
		capturedID = nil

		anonSnap := core.NewSnapshot("proj_anon", "production", 1)
		// Project requires strict auth
		anonSnap.AuthConfig = &core.AuthConfig{
			Provider: "oidc",
			Issuer:   "https://strict.auth.com",
			JWKSURL:  "https://strict.auth.com/jwks.json",
		}
		// But app has anonymous override
		anonSnap.Apps["public-bot"] = &core.AIApp{
			ID:   "public-bot",
			Name: "Public Bot",
			AuthConfig: &core.AuthConfig{
				Provider: "anonymous",
			},
		}
		registry.Swap(anonSnap)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/public-bot/chat", nil)
		req.Header.Set("X-Project-ID", "proj_anon")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "anonymous", capturedID.Subject)
		assert.Equal(t, "anonymous", capturedID.Issuer)
		assert.Equal(t, true, capturedID.Claims["anonymous"])
	})

	t.Run("app-level auth override uses app's specific provider", func(t *testing.T) {
		handled = false
		capturedID = nil

		appAuthSnap := core.NewSnapshot("proj_custom_app", "production", 1)
		// Project has dummy auth
		appAuthSnap.AuthConfig = &core.AuthConfig{
			Provider: "oidc",
			Issuer:   "https://project.auth.com",
			JWKSURL:  "https://project.auth.com/jwks.json",
		}
		// App overrides with fixture JWKS
		appAuthSnap.Apps["custom-app"] = &core.AIApp{
			ID:   "custom-app",
			Name: "Custom App",
			AuthConfig: &core.AuthConfig{
				Provider: "oidc",
				Issuer:   "https://auth.example.com",
				Audience: []string{"custom-app-aud"},
				JWKSURL:  testURL,
			},
		}
		registry.Swap(appAuthSnap)

		tok := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			"https://auth.example.com",
			"custom_user",
			[]string{"custom-app-aud"},
			time.Now().Add(1*time.Hour),
			time.Time{},
			map[string]any{"plan": "enterprise"},
		)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/custom-app/chat", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Project-ID", "proj_custom_app")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "custom_user", capturedID.Subject)
		assert.Equal(t, "enterprise", capturedID.Claims["plan"])
	})

	t.Run("app-level anonymous provider allows unauthenticated request", func(t *testing.T) {
		handled = false
		capturedID = nil

		anonSnap := core.NewSnapshot("proj_anon", "production", 1)
		anonSnap.Apps["anon-app"] = &core.AIApp{
			ID:   "anon-app",
			Name: "Anon App",
			AuthConfig: &core.AuthConfig{
				Provider: "anonymous",
			},
		}
		registry.Swap(anonSnap)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/anon-app/chat", nil)
		req.Header.Set("X-Project-ID", "proj_anon")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "anonymous", capturedID.Subject)
		assert.Equal(t, true, capturedID.Claims["anonymous"])
	})

	t.Run("app-level oidc provider with allow_anonymous allows unauthenticated request", func(t *testing.T) {
		handled = false
		capturedID = nil

		anonSnap := core.NewSnapshot("proj_anon_override", "production", 1)
		anonSnap.Apps["anon-override-app"] = &core.AIApp{
			ID:   "anon-override-app",
			Name: "Anon Override App",
			AuthConfig: &core.AuthConfig{
				Provider:       "oidc",
				Issuer:         "https://auth.example.com",
				Audience:       []string{"test-app"},
				JWKSURL:        testURL,
				AllowAnonymous: true,
			},
		}
		registry.Swap(anonSnap)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/anon-override-app/chat", nil)
		req.Header.Set("X-Project-ID", "proj_anon_override")
		req.Header.Set("X-Environment", "production")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handled)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, capturedID)
		assert.Equal(t, "anonymous", capturedID.Subject)
		assert.Equal(t, true, capturedID.Claims["anonymous"])
	})
}
