package identity

import (
	"context"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizer_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		provider      string
		tokenClaims   map[string]any
		issuer        string
		subject       string
		audience      []string
		wantUserID    string
		wantClaims    map[string]any
		wantStableKey string
	}{
		{
			name:     "Generic OIDC Token",
			provider: "oidc",
			issuer:   "https://auth.company.com",
			subject:  "usr_001",
			audience: []string{"ai-app-client"},
			tokenClaims: map[string]any{
				"email": "alice@company.com",
				"org": map[string]any{
					"id":   "org_99",
					"tier": "enterprise",
				},
			},
			wantUserID: "usr_001",
			wantClaims: map[string]any{
				"email":    "alice@company.com",
				"org.id":   "org_99",
				"org.tier": "enterprise",
			},
			wantStableKey: "proj_demo:https://auth.company.com:usr_001",
		},
		{
			name:     "Firebase Auth Token",
			provider: "firebase",
			issuer:   "https://securetoken.google.com/my-firebase-app",
			subject:  "firebase_uid_123",
			audience: []string{"my-firebase-app"},
			tokenClaims: map[string]any{
				"user_id": "firebase_uid_123",
				"email":   "bob@firebase.com",
				"firebase": map[string]any{
					"sign_in_provider": "google.com",
					"identities": map[string]any{
						"google.com": []any{"12345"},
					},
				},
			},
			wantUserID: "firebase_uid_123",
			wantClaims: map[string]any{
				"user_id":                   "firebase_uid_123",
				"sign_in_provider":          "google.com",
				"firebase.sign_in_provider": "google.com",
			},
			wantStableKey: "proj_demo:https://securetoken.google.com/my-firebase-app:firebase_uid_123",
		},
		{
			name:     "Supabase Auth Token",
			provider: "supabase",
			issuer:   "https://example.supabase.co/auth/v1",
			subject:  "uuid-supabase-789",
			audience: []string{"authenticated"},
			tokenClaims: map[string]any{
				"role":  "authenticated",
				"email": "charlie@supabase.io",
				"app_metadata": map[string]any{
					"provider":  "email",
					"providers": []any{"email"},
				},
				"user_metadata": map[string]any{
					"plan": "pro",
					"preferences": map[string]any{
						"theme": "dark",
					},
				},
			},
			wantUserID: "uuid-supabase-789",
			wantClaims: map[string]any{
				"role":                            "authenticated",
				"email":                           "charlie@supabase.io",
				"app_metadata.provider":           "email",
				"user_metadata.plan":              "pro",
				"user_metadata.preferences.theme": "dark",
			},
			wantStableKey: "proj_demo:https://example.supabase.co/auth/v1:uuid-supabase-789",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := jwt.NewBuilder().
				Issuer(tc.issuer).
				Subject(tc.subject).
				Audience(tc.audience).
				IssuedAt(time.Now())

			for k, v := range tc.tokenClaims {
				b.Claim(k, v)
			}

			tok, err := b.Build()
			require.NoError(t, err)

			normalizer := NewNormalizer(tc.provider)
			id, err := normalizer.Normalize(ctx, tok, "proj_demo", "production")
			require.NoError(t, err)
			require.NotNil(t, id)

			assert.Equal(t, "proj_demo", id.Project)
			assert.Equal(t, "production", id.Environment)
			assert.Equal(t, tc.issuer, id.Issuer)
			assert.Equal(t, tc.subject, id.Subject)
			assert.Equal(t, tc.wantStableKey, id.StableKey())

			// Verify expected flattened dot-notation claims
			for expectedKey, expectedVal := range tc.wantClaims {
				actualVal, ok := id.Claims[expectedKey]
				assert.True(t, ok, "claim %q should be present", expectedKey)
				assert.Equal(t, expectedVal, actualVal, "claim %q should match", expectedKey)
			}
		})
	}
}

func TestNormalizer_ClaimsImmutability(t *testing.T) {
	ctx := context.Background()

	originalMeta := map[string]any{
		"plan": "starter",
	}

	tok, err := jwt.NewBuilder().
		Issuer("https://auth.example.com").
		Subject("user-immut").
		Claim("user_metadata", originalMeta).
		Build()
	require.NoError(t, err)

	normalizer := NewNormalizer("generic")
	id, err := normalizer.Normalize(ctx, tok, "p1", "dev")
	require.NoError(t, err)

	// Mutate original source map
	originalMeta["plan"] = "hacked"

	// Verify normalized identity claims were not mutated
	assert.Equal(t, "starter", id.Claims["user_metadata.plan"])
	metaMap := id.Claims["user_metadata"].(map[string]any)
	assert.Equal(t, "starter", metaMap["plan"])
}
