package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexbase/cortexbase/internal/core"
)

func TestVerifier_ValidTokens(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/.well-known/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Provider: "oidc",
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-client-id"},
		JWKSURL:  testURL,
	}

	t.Run("valid RSA token", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			cfg.Issuer,
			"user-rsa-123",
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Now().Add(-5*time.Minute),
			map[string]any{"role": "member"},
		)

		token, err := verifier.Verify(ctx, tokenStr, cfg)
		require.NoError(t, err)
		assert.Equal(t, "user-rsa-123", token.Subject())
		assert.Equal(t, cfg.Issuer, token.Issuer())
	})

	t.Run("valid ECDSA token", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privECJWK,
			jwa.ES256,
			cfg.Issuer,
			"user-ec-456",
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Now().Add(-5*time.Minute),
			map[string]any{"plan": "pro"},
		)

		token, err := verifier.Verify(ctx, tokenStr, cfg)
		require.NoError(t, err)
		assert.Equal(t, "user-ec-456", token.Subject())
	})
}

func TestVerifier_ExpiredAndNotBefore(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Issuer:    "https://auth.example.com",
		Audience:  []string{"test-app"},
		JWKSURL:   testURL,
		ClockSkew: 5 * time.Second,
	}

	t.Run("expired token", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			cfg.Issuer,
			"user-expired",
			cfg.Audience,
			time.Now().Add(-10*time.Minute), // expired well beyond clock skew
			time.Time{},
			nil,
		)

		_, err := verifier.Verify(ctx, tokenStr, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTokenExpired)
	})

	t.Run("token not yet valid", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			cfg.Issuer,
			"user-future",
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Now().Add(10*time.Minute), // not valid for 10 minutes (beyond clock skew)
			nil,
		)

		_, err := verifier.Verify(ctx, tokenStr, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTokenNotYetValid)
	})
}

func TestVerifier_IssuerAndAudienceMismatch(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Issuer:   "https://auth.example.com",
		Audience: []string{"expected-aud"},
		JWKSURL:  testURL,
	}

	t.Run("wrong issuer", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			"https://wrong-issuer.com",
			"user-1",
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Time{},
			nil,
		)

		_, err := verifier.Verify(ctx, tokenStr, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidIssuer)
	})

	t.Run("wrong audience", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			cfg.Issuer,
			"user-1",
			[]string{"unexpected-aud"},
			time.Now().Add(1*time.Hour),
			time.Time{},
			nil,
		)

		_, err := verifier.Verify(ctx, tokenStr, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidAudience)
	})
}

func TestVerifier_AlgorithmSecurity(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-app"},
		JWKSURL:  testURL,
	}

	t.Run("reject alg none", func(t *testing.T) {
		// Construct unsigned token (alg none)
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":%q,"sub":"user-none","aud":%q,"exp":%d}`, cfg.Issuer, cfg.Audience[0], time.Now().Add(1*time.Hour).Unix())))
		noneToken := header + "." + payload + "."

		_, err := verifier.Verify(ctx, noneToken, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidAlgorithm)
	})

	t.Run("reject symmetric HMAC algorithm", func(t *testing.T) {
		tok, err := jwt.NewBuilder().
			Issuer(cfg.Issuer).
			Subject("user-hmac").
			Audience(cfg.Audience).
			Expiration(time.Now().Add(1 * time.Hour)).
			Build()
		require.NoError(t, err)

		hmacSecret := []byte("insecure-secret-key-32-bytes-long!")
		hmacBytes, err := jwt.Sign(tok, jwt.WithKey(jwa.HS256, hmacSecret))
		require.NoError(t, err)

		_, err = verifier.Verify(ctx, string(hmacBytes), cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidAlgorithm)
	})
}

func TestVerifier_KeyRotationSingleFlight(t *testing.T) {
	// Initial key
	fixture := newTestKeyFixture(t)

	// Rotated new key
	rotatedPrivRSA, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rotatedJWK, err := jwk.FromRaw(rotatedPrivRSA)
	require.NoError(t, err)
	rotatedKeyID := "rotated-key-2"
	require.NoError(t, rotatedJWK.Set(jwk.KeyIDKey, rotatedKeyID))
	require.NoError(t, rotatedJWK.Set(jwk.AlgorithmKey, jwa.RS256))
	rotatedPubJWK, err := jwk.PublicKeyOf(rotatedJWK)
	require.NoError(t, err)

	var serverFetchCount int64
	var currentSet atomic.Value
	currentSet.Store(fixture.jwksSet)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&serverFetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(currentSet.Load().(jwk.Set))
	}))
	defer server.Close()

	mgr := NewJWKSManager(
		WithDefaultTTL(1*time.Hour),
		WithMinRefreshInterval(0),
	)
	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-app"},
		JWKSURL:  server.URL,
	}

	// 1. First token signed with initial key (populates cache)
	tokenStr1 := buildAndSignToken(t,
		fixture.privJWK,
		jwa.RS256,
		cfg.Issuer,
		"user-1",
		cfg.Audience,
		time.Now().Add(1*time.Hour),
		time.Time{},
		nil,
	)

	token1, err := verifier.Verify(ctx, tokenStr1, cfg)
	require.NoError(t, err)
	assert.Equal(t, "user-1", token1.Subject())
	assert.Equal(t, int64(1), atomic.LoadInt64(&serverFetchCount))

	// 2. Rotate keys on the remote server: add rotated key to the remote JWKS
	newRemoteSet := jwk.NewSet()
	require.NoError(t, newRemoteSet.AddKey(fixture.pubJWK))
	require.NoError(t, newRemoteSet.AddKey(rotatedPubJWK))
	currentSet.Store(newRemoteSet)

	// 3. Issue token signed with rotated key (unknown kid in current in-memory cache)
	tokenStr2 := buildAndSignToken(t,
		rotatedJWK,
		jwa.RS256,
		cfg.Issuer,
		"user-rotated",
		cfg.Audience,
		time.Now().Add(1*time.Hour),
		time.Time{},
		nil,
	)

	// 4. Verifier should detect unknown kid, trigger singleflight refresh, and succeed
	token2, err := verifier.Verify(ctx, tokenStr2, cfg)
	require.NoError(t, err)
	assert.Equal(t, "user-rotated", token2.Subject())
	assert.Equal(t, int64(2), atomic.LoadInt64(&serverFetchCount), "unknown kid must trigger jwks refresh")

	// 5. Subsequent request with the same rotated kid should be cached (zero network)
	token3, err := verifier.Verify(ctx, tokenStr2, cfg)
	require.NoError(t, err)
	assert.Equal(t, "user-rotated", token3.Subject())
	assert.Equal(t, int64(2), atomic.LoadInt64(&serverFetchCount), "subsequent verification with known kid must not call remote server")
}

func TestVerifier_SubjectAndIssuerRequired(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()

	cfg := &core.AuthConfig{
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-app"},
		JWKSURL:  testURL,
	}

	t.Run("missing subject", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			cfg.Issuer,
			"", // empty subject
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Time{},
			nil,
		)

		_, err := verifier.Verify(ctx, tokenStr, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTokenFormat)
		assert.Contains(t, err.Error(), "subject")
	})

	t.Run("missing issuer", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			"", // empty issuer
			"user-1",
			cfg.Audience,
			time.Now().Add(1*time.Hour),
			time.Time{},
			nil,
		)

		// Verification with empty issuer expectation
		cfgNoIss := &core.AuthConfig{
			JWKSURL: testURL,
		}
		_, err := verifier.Verify(ctx, tokenStr, cfgNoIss)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTokenFormat)
		assert.Contains(t, err.Error(), "issuer")
	})
}

func TestVerifier_KeyAlgorithmAndUsageChecks(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"

	// Key with specific alg and use
	keyWithAlg, err := jwk.PublicKeyOf(fixture.privJWK)
	require.NoError(t, err)
	require.NoError(t, keyWithAlg.Set(jwk.KeyIDKey, "key-with-alg"))
	require.NoError(t, keyWithAlg.Set(jwk.AlgorithmKey, jwa.RS256))
	require.NoError(t, keyWithAlg.Set(jwk.KeyUsageKey, "sig"))

	// Key with encryption usage (not signature)
	encKey, err := jwk.PublicKeyOf(fixture.privJWK)
	require.NoError(t, err)
	require.NoError(t, encKey.Set(jwk.KeyIDKey, "enc-key"))
	require.NoError(t, encKey.Set(jwk.AlgorithmKey, jwa.RS256))
	require.NoError(t, encKey.Set(jwk.KeyUsageKey, "enc"))

	set := jwk.NewSet()
	require.NoError(t, set.AddKey(keyWithAlg))
	require.NoError(t, set.AddKey(encKey))
	mgr.SetManual(testURL, set, 1*time.Hour)

	verifier := NewVerifier(mgr)
	ctx := context.Background()
	cfg := &core.AuthConfig{
		Issuer:   "https://auth.example.com",
		Audience: []string{"test-app"},
		JWKSURL:  testURL,
	}

	t.Run("key usage enc rejected for signature verification", func(t *testing.T) {
		// Sign using privJWK but with kid "enc-key"
		tok, err := jwt.NewBuilder().
			Issuer(cfg.Issuer).
			Subject("user-enc").
			Audience(cfg.Audience).
			Expiration(time.Now().Add(1 * time.Hour)).
			Build()
		require.NoError(t, err)

		keyCopy, err := fixture.privJWK.Clone()
		require.NoError(t, err)
		require.NoError(t, keyCopy.Set(jwk.KeyIDKey, "enc-key"))
		signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, keyCopy))
		require.NoError(t, err)

		_, err = verifier.Verify(ctx, string(signed), cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidAlgorithm)
		assert.Contains(t, err.Error(), "key usage is \"enc\"")
	})
}
