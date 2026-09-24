package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
)

func TestCompiler_Compile(t *testing.T) {
	compiler, err := policy.NewCompiler()
	require.NoError(t, err)
	require.NotNil(t, compiler)

	tests := []struct {
		name        string
		expression  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid boolean comparison",
			expression:  `claims.plan == "pro"`,
			expectError: false,
		},
		{
			name:        "valid boolean logic and membership",
			expression:  `claims.ai_access == true && claims.plan in ["pro", "team"]`,
			expectError: false,
		},
		{
			name:        "valid identity check",
			expression:  `identity.issuer == "https://securetoken.google.com/demo"`,
			expectError: false,
		},
		{
			name:        "valid meta check",
			expression:  `meta.app_id == "chat_app"`,
			expectError: false,
		},
		{
			name:        "empty expression",
			expression:  "",
			expectError: true,
			errorMsg:    "empty expression",
		},
		{
			name:        "whitespace only",
			expression:  "   ",
			expectError: true,
			errorMsg:    "empty expression",
		},
		{
			name:        "syntax error",
			expression:  `claims.plan === "pro"`,
			expectError: true,
			errorMsg:    "compile cel",
		},
		{
			name:        "non-boolean return string",
			expression:  `claims.plan`,
			expectError: true,
			errorMsg:    "expression must evaluate to bool, got dyn",
		},
		{
			name:        "non-boolean return integer",
			expression:  `1 + 2`,
			expectError: true,
			errorMsg:    "expression must evaluate to bool, got int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prg, err := compiler.Compile(tt.expression)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, prg)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, prg)
			}
		})
	}
}

func TestEvaluator_TableDriven(t *testing.T) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(t, err)

	type testCase struct {
		name        string
		expression  string
		identity    *core.Identity
		meta        map[string]any
		expected    bool
		expectError bool
	}

	tests := []testCase{
		// 1. Equality and Inequality
		{
			name:       "eq matching string",
			expression: `claims.plan == "pro"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			expected: true,
		},
		{
			name:       "eq non-matching string",
			expression: `claims.plan == "pro"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "free"},
			},
			expected: false,
		},
		{
			name:       "neq matching",
			expression: `claims.plan != "banned"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			expected: true,
		},
		{
			name:       "neq non-matching",
			expression: `claims.plan != "banned"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "banned"},
			},
			expected: false,
		},

		// 2. List Membership ('in' and 'not_in')
		{
			name:       "in list allowed",
			expression: `claims.role in ["admin", "editor", "owner"]`,
			identity: &core.Identity{
				Claims: map[string]any{"role": "editor"},
			},
			expected: true,
		},
		{
			name:       "in list disallowed",
			expression: `claims.role in ["admin", "editor", "owner"]`,
			identity: &core.Identity{
				Claims: map[string]any{"role": "viewer"},
			},
			expected: false,
		},
		{
			name:       "not in list",
			expression: `!(claims.plan in ["free", "trial"])`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "enterprise"},
			},
			expected: true,
		},

		// 3. Numeric Comparisons (gte, lte, cross-type float64 vs int)
		{
			name:       "gte passing int",
			expression: `claims.tier_level >= 2`,
			identity: &core.Identity{
				Claims: map[string]any{"tier_level": 3},
			},
			expected: true,
		},
		{
			name:       "gte passing float64 from JSON unmarshaling",
			expression: `claims.tier_level >= 2`,
			identity: &core.Identity{
				Claims: map[string]any{"tier_level": float64(3.0)},
			},
			expected: true,
		},
		{
			name:       "gte failing float64",
			expression: `claims.tier_level >= 2`,
			identity: &core.Identity{
				Claims: map[string]any{"tier_level": float64(1.5)},
			},
			expected: false,
		},
		{
			name:       "lte passing",
			expression: `claims.violation_count <= 3`,
			identity: &core.Identity{
				Claims: map[string]any{"violation_count": 0},
			},
			expected: true,
		},

		// 4. Optional syntax support
		{
			name:       "optional chaining missing claim with default fallback",
			expression: `claims.?non_existent.orValue("default") == "default"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			expected: true,
		},
		{
			name:       "optional chaining existing claim",
			expression: `claims.?plan.orValue("free") == "pro"`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			expected: true,
		},

		// 4. List Comprehension Macros (exists, all)
		{
			name:       "list macro exists match",
			expression: `claims.roles.exists(r, r in ["admin", "owner"])`,
			identity: &core.Identity{
				Claims: map[string]any{"roles": []any{"member", "admin"}},
			},
			expected: true,
		},
		{
			name:       "list macro exists non-match",
			expression: `claims.roles.exists(r, r in ["admin", "owner"])`,
			identity: &core.Identity{
				Claims: map[string]any{"roles": []any{"member", "guest"}},
			},
			expected: false,
		},
		{
			name:       "list macro all match",
			expression: `claims.scopes.all(s, s.startsWith("ai:"))`,
			identity: &core.Identity{
				Claims: map[string]any{"scopes": []any{"ai:chat", "ai:image"}},
			},
			expected: true,
		},

		// 5. Regex Matching
		{
			name:       "regex domain match",
			expression: `claims.email.matches(".*@corp[.]internal")`,
			identity: &core.Identity{
				Claims: map[string]any{"email": "alice@corp.internal"},
			},
			expected: true,
		},
		{
			name:       "regex non-matching",
			expression: `claims.email.matches(".*@corp[.]internal")`,
			identity: &core.Identity{
				Claims: map[string]any{"email": "bob@gmail.com"},
			},
			expected: false,
		},

		// 5. Map Key Presence (has / in)
		{
			name:       "has key check true",
			expression: `has(claims.ai_access) && claims.ai_access == true`,
			identity: &core.Identity{
				Claims: map[string]any{"ai_access": true},
			},
			expected: true,
		},
		{
			name:       "has key check false when key missing",
			expression: `has(claims.ai_access) && claims.ai_access == true`,
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			expected: false,
		},
		{
			name:       "in map check true",
			expression: `'feature_x' in claims && claims.feature_x == "enabled"`,
			identity: &core.Identity{
				Claims: map[string]any{"feature_x": "enabled"},
			},
			expected: true,
		},

		// 6. Identity attributes
		{
			name:       "identity subject check",
			expression: `identity.subject == "usr_special"`,
			identity: &core.Identity{
				Subject: "usr_special",
			},
			expected: true,
		},
		{
			name:       "identity issuer check",
			expression: `identity.issuer == "https://firebase.google.com"`,
			identity: &core.Identity{
				Issuer: "https://firebase.google.com",
			},
			expected: true,
		},

		// 7. Request metadata
		{
			name:       "metadata app_id check",
			expression: `meta.app_id == "chat_app"`,
			identity:   &core.Identity{},
			meta:       map[string]any{"app_id": "chat_app"},
			expected:   true,
		},

		// 8. Complex composition
		{
			name:       "complex rule pass",
			expression: `(claims.plan in ["pro", "enterprise"] || claims.is_beta == true) && identity.environment == "prod"`,
			identity: &core.Identity{
				Environment: "prod",
				Claims: map[string]any{
					"plan":    "free",
					"is_beta": true,
				},
			},
			expected: true,
		},

		// 9. Edge Cases: nil Identity
		{
			name:        "nil identity empty claims check",
			expression:  `identity.subject == ""`,
			identity:    nil,
			expected:    true,
			expectError: false,
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prg, err := evaluator.Compiler().Compile(tt.expression)
			require.NoError(t, err)

			res, err := evaluator.EvaluateProgram(ctx, prg, tt.identity, tt.meta)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, res)
			}
		})
	}
}

func TestEvaluator_SnapshotIntegration(t *testing.T) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(t, err)

	compiler := evaluator.Compiler()
	prgAdmin, err := compiler.Compile(`claims.role == "admin"`)
	require.NoError(t, err)

	snap := core.NewSnapshot("proj-1", "prod", 1)
	snap.CompiledPolicies["admin_only"] = prgAdmin
	snap.Policies["editor_or_admin"] = &core.PolicyDefinition{
		ID:         "editor_or_admin",
		Name:       "Editor or Admin",
		Expression: `claims.role in ["admin", "editor"]`,
	}

	ctx := context.Background()
	adminID := &core.Identity{
		Claims: map[string]any{"role": "admin"},
	}
	editorID := &core.Identity{
		Claims: map[string]any{"role": "editor"},
	}
	viewerID := &core.Identity{
		Claims: map[string]any{"role": "viewer"},
	}

	// 1. Compiled policy evaluation
	allowed, err := evaluator.EvaluatePolicy(ctx, snap, "admin_only", adminID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = evaluator.EvaluatePolicy(ctx, snap, "admin_only", editorID, nil)
	require.NoError(t, err)
	assert.False(t, allowed)

	// 2. Fallback to Snapshot.Policies definition
	allowed, err = evaluator.EvaluatePolicy(ctx, snap, "editor_or_admin", editorID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = evaluator.EvaluatePolicy(ctx, snap, "editor_or_admin", viewerID, nil)
	require.NoError(t, err)
	assert.False(t, allowed)

	// 3. Inline expression evaluation
	allowed, err = evaluator.EvaluatePolicy(ctx, snap, `claims.role == "viewer"`, viewerID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// 4. Empty policy evaluates to true (open access)
	allowed, err = evaluator.EvaluatePolicy(ctx, snap, "", viewerID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// 5. Invalid policy expression returns error
	_, err = evaluator.EvaluatePolicy(ctx, snap, `unknown ==== invalid`, viewerID, nil)
	require.Error(t, err)

	// 6. SnapshotEvaluator wrapper satisfies core.PolicyEvaluator
	var coreEval core.PolicyEvaluator = policy.NewSnapshotEvaluator(snap, evaluator)
	allowed, err = coreEval.Evaluate(ctx, "admin_only", adminID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// 7. ContextWithSnapshot helper
	ctxWithSnap := policy.ContextWithSnapshot(ctx, snap)
	snapFound, ok := policy.SnapshotFromContext(ctxWithSnap)
	assert.True(t, ok)
	assert.Equal(t, snap, snapFound)

	allowed, err = evaluator.Evaluate(ctxWithSnap, "admin_only", adminID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// 8. Test Snapshot.CompiledPolicy helper
	prgGot, exists := snap.CompiledPolicy("admin_only")
	assert.True(t, exists)
	assert.NotNil(t, prgGot)

	_, exists = snap.CompiledPolicy("non_existent")
	assert.False(t, exists)
}

func BenchmarkEvaluator_EvaluateProgram(b *testing.B) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(b, err)

	prg, err := evaluator.Compiler().Compile(`claims.ai_access == true && claims.plan in ["pro", "team"]`)
	require.NoError(b, err)

	id := &core.Identity{
		Project:     "test-project",
		Environment: "prod",
		Issuer:      "https://auth.example.com",
		Subject:     "usr_12345",
		Claims: map[string]any{
			"ai_access": true,
			"plan":      "pro",
		},
	}
	meta := map[string]any{
		"app_id": "chat_bot",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		allowed, err := evaluator.EvaluateProgram(ctx, prg, id, meta)
		if err != nil || !allowed {
			b.Fatalf("unexpected eval failure: allowed=%v, err=%v", allowed, err)
		}
	}
}
