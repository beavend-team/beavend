package identity

import (
	"context"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexbase/cortexbase/internal/core"
	"github.com/cortexbase/cortexbase/internal/snapshot"
)

func TestProvider_ValidateToken(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()
	testURL := "https://auth.example.com/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	verifier := NewVerifier(mgr)
	registry := snapshot.NewRegistry()

	projectID := "proj_prov"
	env := "development"
	snap := core.NewSnapshot(projectID, env, 1)
	snap.AuthConfig = &core.AuthConfig{
		Provider: "oidc",
		Issuer:   "https://auth.example.com",
		Audience: []string{"app-1"},
		JWKSURL:  testURL,
	}
	registry.Swap(snap)

	provider := NewProvider(registry, verifier, env)

	// Ensure Provider conforms to core.AuthProvider interface
	var _ core.AuthProvider = provider

	ctx := context.Background()

	t.Run("valid token", func(t *testing.T) {
		tokenStr := buildAndSignToken(t,
			fixture.privJWK,
			jwa.RS256,
			snap.AuthConfig.Issuer,
			"user_provider_123",
			snap.AuthConfig.Audience,
			time.Now().Add(1*time.Hour),
			time.Time{},
			map[string]any{"plan": "enterprise"},
		)

		id, err := provider.ValidateToken(ctx, projectID, tokenStr)
		require.NoError(t, err)
		require.NotNil(t, id)
		assert.Equal(t, projectID, id.Project)
		assert.Equal(t, env, id.Environment)
		assert.Equal(t, "user_provider_123", id.Subject)
		assert.Equal(t, "enterprise", id.Claims["plan"])
	})

	t.Run("unknown project", func(t *testing.T) {
		_, err := provider.ValidateToken(ctx, "proj_unknown", "some_token")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProjectNotFound)
	})
}
