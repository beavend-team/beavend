package snapshot_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

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
}
