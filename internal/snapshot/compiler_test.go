package snapshot_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/snapshot"
	"github.com/beavend/beavend/internal/store/db"
)

type mockQuerySource struct {
	revision   int64
	authConfig []byte
	apps       []db.GetSnapshotAppsRow
	prompts    []db.GetSnapshotPromptsRow
	policies   []db.GetSnapshotPoliciesRow
	tools      []db.GetSnapshotToolsRow
	models     []db.ModelRoutings
}

func (m *mockQuerySource) GetSnapshotRevision(_ context.Context, _ db.GetSnapshotRevisionParams) (int64, error) {
	return m.revision, nil
}

func (m *mockQuerySource) GetSnapshotEnvironment(_ context.Context, _ db.GetSnapshotEnvironmentParams) ([]byte, error) {
	return m.authConfig, nil
}

func (m *mockQuerySource) GetSnapshotApps(_ context.Context, _ db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error) {
	return m.apps, nil
}

func (m *mockQuerySource) GetSnapshotPrompts(_ context.Context, _ db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error) {
	return m.prompts, nil
}

func (m *mockQuerySource) GetSnapshotPolicies(_ context.Context, _ db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error) {
	return m.policies, nil
}

func (m *mockQuerySource) GetSnapshotTools(_ context.Context, _ db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error) {
	return m.tools, nil
}

func (m *mockQuerySource) GetSnapshotModelRoutings(_ context.Context, _ db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error) {
	return m.models, nil
}

func (m *mockQuerySource) GetSnapshotAppAPIKeys(_ context.Context, _ db.GetSnapshotAppAPIKeysParams) ([]db.AppApiKeys, error) {
	return nil, nil
}

func (m *mockQuerySource) ListAllProjectEnvironments(_ context.Context) ([]db.ListAllProjectEnvironmentsRow, error) {
	return nil, nil
}

func TestCompiler_ValidSnapshot(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockQuerySource{
		revision: 5,
		prompts: []db.GetSnapshotPromptsRow{
			{
				ID:        "pv1",
				PromptID:  "p_chat",
				Version:   1,
				Template:  "System prompt: {{role}}",
				Variables: []byte(`["role"]`),
				IsActive:  true,
				CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
			},
		},
		policies: []db.GetSnapshotPoliciesRow{
			{
				ID:          "pol1",
				PolicyID:    "premium_only",
				Name:        "Premium Policy",
				Expression:  `claims.plan == "pro"`,
				IsActive:    true,
				CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				Description: "Only for pro users",
			},
		},
		tools: []db.GetSnapshotToolsRow{
			{
				ID:               "tv1",
				ToolID:           "get_weather",
				Name:             "Get Weather",
				SideEffect:       "read",
				InputSchema:      []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
				IdentityBindings: []byte(`{"user_id":"claims.sub"}`),
				Executor:         []byte(`{"type":"http","method":"GET","url":"https://api.weather.com"}`),
				IsActive:         true,
			},
		},
		models: []db.ModelRoutings{
			{
				ID:                  "m1",
				LogicalModel:        "fast",
				Deployment:          "gemini-2.5-flash",
				Adapter:             "litellm",
				FallbackDeployments: []byte(`["gpt-4o-mini"]`),
			},
		},
		apps: []db.GetSnapshotAppsRow{
			{
				ID:             "av1",
				AppID:          "weather_bot",
				Name:           "Weather Assistant",
				AccessPolicy:   "premium_only",
				PromptID:       "p_chat",
				PromptVersion:  1,
				ModelRoutingID: "fast",
				ToolIds:        []byte(`["get_weather"]`),
				Limits:         []byte(`[{"resource":"inference","window":60000000000,"max":10}]`),
				CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	snap, err := compiler.Compile(context.Background(), "test-proj", "prod")
	require.NoError(t, err)
	require.NotNil(t, snap)

	// Verify revision
	require.Equal(t, int64(5), snap.Revision)

	// Verify app
	app, ok := snap.App("weather_bot")
	require.True(t, ok)
	require.Equal(t, "Weather Assistant", app.Name)
	require.Equal(t, "fast", app.ModelRoutingID)
	require.Equal(t, []string{"get_weather"}, app.ToolIDs)
	require.Len(t, app.Limits, 1)

	// Verify pre-compiled policy
	require.Contains(t, snap.CompiledPolicies, "premium_only")
	require.NotNil(t, snap.CompiledPolicies["premium_only"])

	// Verify pre-compiled JSON schema
	require.Contains(t, snap.CompiledSchemas, "get_weather")
	require.NotNil(t, snap.CompiledSchemas["get_weather"])

	// Verify prompt
	prompt, ok := snap.Prompt("p_chat")
	require.True(t, ok)
	require.Equal(t, []string{"role"}, prompt.Variables)

	// Verify model routing
	model, ok := snap.Model("fast")
	require.True(t, ok)
	require.Equal(t, "gemini-2.5-flash", model.Deployment)
	require.Equal(t, []string{"gpt-4o-mini"}, model.FallbackDeployments)
}

func TestCompiler_InvalidCELPolicy(t *testing.T) {
	mock := &mockQuerySource{
		revision: 1,
		policies: []db.GetSnapshotPoliciesRow{
			{
				ID:         "pol-bad",
				PolicyID:   "syntax_error_policy",
				Expression: "claims.plan ==== invalid syntax",
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	_, err = compiler.Compile(context.Background(), "test-proj", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile cel policy")
}

func TestCompiler_InvalidJSONSchema(t *testing.T) {
	mock := &mockQuerySource{
		revision: 1,
		tools: []db.GetSnapshotToolsRow{
			{
				ID:          "tv-bad",
				ToolID:      "broken_tool",
				InputSchema: []byte(`{"type":"invalid_type_def"}`),
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	_, err = compiler.Compile(context.Background(), "test-proj", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile json schema")
}

func TestCompiler_MissingReferencedTool(t *testing.T) {
	mock := &mockQuerySource{
		revision: 1,
		apps: []db.GetSnapshotAppsRow{
			{
				ID:      "av1",
				AppID:   "orphan_tool_app",
				ToolIds: []byte(`["non_existent_tool"]`),
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	_, err = compiler.Compile(context.Background(), "test-proj", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "references non-existent tool")
}

func TestCompiler_MissingReferencedPrompt(t *testing.T) {
	mock := &mockQuerySource{
		revision: 1,
		apps: []db.GetSnapshotAppsRow{
			{
				ID:       "av1",
				AppID:    "orphan_prompt_app",
				PromptID: "missing_prompt_id",
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	_, err = compiler.Compile(context.Background(), "test-proj", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "references non-existent prompt")
}

func TestCompiler_ToolCELAuthorization(t *testing.T) {
	t.Run("invalid tool CEL policy fails compilation", func(t *testing.T) {
		mock := &mockQuerySource{
			revision: 1,
			tools: []db.GetSnapshotToolsRow{
				{
					ID:            "tv-bad-auth",
					ToolID:        "tool_with_bad_auth",
					Authorization: "claims.plan ==== invalid syntax",
				},
			},
		}

		compiler, err := snapshot.NewCompiler(mock)
		require.NoError(t, err)

		_, err = compiler.Compile(context.Background(), "test-proj", "prod")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authorization policy")
	})

	t.Run("valid inline tool CEL policy pre-compiles successfully", func(t *testing.T) {
		mock := &mockQuerySource{
			revision: 1,
			tools: []db.GetSnapshotToolsRow{
				{
					ID:            "tv-good-auth",
					ToolID:        "tool_with_good_auth",
					Authorization: `claims.role == "admin"`,
				},
			},
		}

		compiler, err := snapshot.NewCompiler(mock)
		require.NoError(t, err)

		snap, err := compiler.Compile(context.Background(), "test-proj", "prod")
		require.NoError(t, err)
		require.NotNil(t, snap)

		prg, ok := snap.CompiledPolicy(`claims.role == "admin"`)
		require.True(t, ok)
		require.NotNil(t, prg)
	})

	t.Run("auth config compiles successfully into snapshot", func(t *testing.T) {
		mock := &mockQuerySource{
			revision:   1,
			authConfig: []byte(`{"provider":"firebase","project_id":"my-firebase-app"}`),
		}

		compiler, err := snapshot.NewCompiler(mock)
		require.NoError(t, err)

		snap, err := compiler.Compile(context.Background(), "test-proj", "prod")
		require.NoError(t, err)
		require.NotNil(t, snap)
		require.NotNil(t, snap.AuthConfig)
		require.Equal(t, "firebase", snap.AuthConfig.Provider)
		require.Equal(t, "my-firebase-app", snap.AuthConfig.ProjectID)
	})

	t.Run("app auth compiles successfully", func(t *testing.T) {
		mock := &mockQuerySource{
			revision: 1,
			models: []db.ModelRoutings{
				{
					ID:           "m1",
					LogicalModel: "fast",
					Deployment:   "gemini-2.5-flash",
					Adapter:      "litellm",
				},
			},
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:          "app-with-auth",
					Name:           "App With Auth",
					ModelRoutingID: "fast",
					AuthConfig:     []byte(`{"provider":"oidc","issuer":"https://app.auth.com"}`),
					CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
				},
				{
					AppID:          "app-with-anon",
					Name:           "App With Anon",
					ModelRoutingID: "fast",
					AuthConfig:     []byte(`{"allow_anonymous":true}`),
					CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
				},
			},
		}

		compiler, err := snapshot.NewCompiler(mock)
		require.NoError(t, err)

		snap, err := compiler.Compile(context.Background(), "test-proj", "prod")
		require.NoError(t, err)
		require.NotNil(t, snap)

		app, ok := snap.App("app-with-auth")
		require.True(t, ok)
		require.NotNil(t, app.AuthConfig)
		require.Equal(t, "oidc", app.AuthConfig.Provider)
		require.Equal(t, "https://app.auth.com", app.AuthConfig.Issuer)

		anonApp, ok := snap.App("app-with-anon")
		require.True(t, ok)
		require.NotNil(t, anonApp.AuthConfig)
		require.True(t, anonApp.AuthConfig.AllowAnonymous)
	})
}

func TestCompiler_ModelRoutesValidation(t *testing.T) {
	now := time.Now().UTC()

	baseModels := []db.ModelRoutings{
		{
			ID:               "m_text",
			LogicalModel:     "fast",
			Deployment:       "gemini-2.5-flash",
			Adapter:          "litellm",
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		},
		{
			ID:               "m_vision",
			LogicalModel:     "vision",
			Deployment:       "claude-3-7-sonnet",
			Adapter:          "litellm",
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
		},
		{
			ID:               "m_omni",
			LogicalModel:     "omni",
			Deployment:       "gpt-4o",
			Adapter:          "openai_compat",
			InputModalities:  []string{"text", "image", "audio"},
			OutputModalities: []string{"text", "audio"},
		},
	}

	tests := []struct {
		name        string
		models      []db.ModelRoutings
		apps        []db.GetSnapshotAppsRow
		expectErr   bool
		errContains string
		verifySnap  func(t *testing.T, snap *core.Snapshot)
	}{
		{
			name:   "valid routes with input_has and cel expr",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "app1",
					Name:        "App 1",
					ModelRoutes: []byte(`{"default":"fast","rules":[{"when":{"input_has":["image"],"expr":"meta.priority == 'high'"},"use":"vision"}]}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr: false,
			verifySnap: func(t *testing.T, snap *core.Snapshot) {
				app, ok := snap.App("app1")
				require.True(t, ok)
				require.NotNil(t, app.ModelRoutes)
				require.Equal(t, "fast", app.ModelRoutes.Default)
				require.Len(t, app.ModelRoutes.Rules, 1)
				require.Equal(t, "vision", app.ModelRoutes.Rules[0].Use)
				require.Equal(t, []string{"image"}, app.ModelRoutes.Rules[0].When.InputHas)

				// CEL expr precompiled
				require.Contains(t, snap.CompiledPolicies, "meta.priority == 'high'")
			},
		},
		{
			name:   "legacy app without model_routes uses ModelRoutingID",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:          "legacy_app",
					Name:           "Legacy App",
					ModelRoutingID: "fast",
					CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr: false,
			verifySnap: func(t *testing.T, snap *core.Snapshot) {
				app, ok := snap.App("legacy_app")
				require.True(t, ok)
				require.NotNil(t, app.ModelRoutes)
				require.Equal(t, "fast", app.ModelRoutes.Default)
			},
		},
		{
			name:   "missing default model route",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:     "bad_app",
					Name:      "Bad App",
					CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "missing default model route",
		},
		{
			name:   "references non-existent default model",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "bad_app",
					Name:        "Bad App",
					ModelRoutes: []byte(`{"default":"non_existent_model"}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "references non-existent default model",
		},
		{
			name:   "route rule references non-existent model",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "bad_app",
					Name:        "Bad App",
					ModelRoutes: []byte(`{"default":"fast","rules":[{"when":{"input_has":["image"]},"use":"phantom_model"}]}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "references non-existent model \"phantom_model\"",
		},
		{
			name:   "route rule target does not support input modality",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "bad_app",
					Name:        "Bad App",
					ModelRoutes: []byte(`{"default":"fast","rules":[{"when":{"input_has":["audio"]},"use":"vision"}]}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "does not support requested input modality \"audio\"",
		},
		{
			name:   "route rule target does not support output modality",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "bad_app",
					Name:        "Bad App",
					ModelRoutes: []byte(`{"default":"fast","rules":[{"when":{"output":["audio"]},"use":"vision"}]}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "does not support requested output modality \"audio\"",
		},
		{
			name:   "invalid CEL expression in rule",
			models: baseModels,
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:       "bad_app",
					Name:        "Bad App",
					ModelRoutes: []byte(`{"default":"fast","rules":[{"when":{"expr":"claims.role ===="},"use":"fast"}]}`),
					CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "invalid route rule cel expression",
		},
		{
			name: "fallback does not support required input modality",
			models: []db.ModelRoutings{
				{
					ID:                  "m_vision",
					LogicalModel:        "vision",
					Deployment:          "claude-3-7-sonnet",
					Adapter:             "litellm",
					InputModalities:     []string{"text", "image"},
					OutputModalities:    []string{"text"},
					FallbackDeployments: []byte(`["fast"]`),
				},
				{
					ID:               "m_text",
					LogicalModel:     "fast",
					Deployment:       "gemini-2.5-flash",
					Adapter:          "litellm",
					InputModalities:  []string{"text"}, // lacks "image"
					OutputModalities: []string{"text"},
				},
			},
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:          "app1",
					Name:           "App 1",
					ModelRoutingID: "vision",
					CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "fallback fast does not support required input modality \"image\"",
		},
		{
			name: "fallback does not support required output modality",
			models: []db.ModelRoutings{
				{
					ID:                  "m_omni",
					LogicalModel:        "omni",
					Deployment:          "gpt-4o",
					Adapter:             "litellm",
					InputModalities:     []string{"text"},
					OutputModalities:    []string{"text", "audio"},
					FallbackDeployments: []byte(`["fast"]`),
				},
				{
					ID:               "m_text",
					LogicalModel:     "fast",
					Deployment:       "gemini-2.5-flash",
					Adapter:          "litellm",
					InputModalities:  []string{"text"},
					OutputModalities: []string{"text"}, // lacks "audio"
				},
			},
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:          "app1",
					Name:           "App 1",
					ModelRoutingID: "omni",
					CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr:   true,
			errContains: "fallback fast does not support required output modality \"audio\"",
		},
		{
			name: "fallback with equal or superset capabilities succeeds",
			models: []db.ModelRoutings{
				{
					ID:                  "m_vision",
					LogicalModel:        "vision",
					Deployment:          "claude-3-7-sonnet",
					Adapter:             "litellm",
					InputModalities:     []string{"text", "image"},
					OutputModalities:    []string{"text"},
					FallbackDeployments: []byte(`["omni"]`),
				},
				{
					ID:               "m_omni",
					LogicalModel:     "omni",
					Deployment:       "gpt-4o",
					Adapter:          "openai_compat",
					InputModalities:  []string{"text", "image", "audio"}, // superset
					OutputModalities: []string{"text", "audio"},          // superset
				},
			},
			apps: []db.GetSnapshotAppsRow{
				{
					AppID:          "app1",
					Name:           "App 1",
					ModelRoutingID: "vision",
					CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
				},
			},
			expectErr: false,
			verifySnap: func(t *testing.T, snap *core.Snapshot) {
				require.NotNil(t, snap)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockQuerySource{
				revision: 1,
				models:   tc.models,
				apps:     tc.apps,
			}

			compiler, err := snapshot.NewCompiler(mock)
			require.NoError(t, err)

			snap, err := compiler.Compile(context.Background(), "test-proj", "prod")
			if tc.expectErr {
				require.Error(t, err)
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
				if tc.verifySnap != nil {
					tc.verifySnap(t, snap)
				}
			}
		})
	}
}
