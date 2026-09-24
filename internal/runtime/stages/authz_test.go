package stages

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
)

func TestAuthZStage_Handle(t *testing.T) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(t, err)

	stage, err := NewAuthZStage(evaluator)
	require.NoError(t, err)

	compiler := evaluator.Compiler()
	prgPro, err := compiler.Compile(`claims.plan == "pro"`)
	require.NoError(t, err)

	snap := core.NewSnapshot("proj-1", "prod", 1)
	snap.CompiledPolicies["pro_only"] = prgPro
	snap.Policies["pro_only"] = &core.PolicyDefinition{
		ID:         "pro_only",
		Name:       "Pro Only",
		Expression: `claims.plan == "pro"`,
	}

	snap.Apps["app_open"] = &core.AIApp{
		ID:           "app_open",
		AccessPolicy: "",
	}
	snap.Apps["app_pro"] = &core.AIApp{
		ID:           "app_pro",
		AccessPolicy: "pro_only",
	}
	snap.Apps["app_inline"] = &core.AIApp{
		ID:           "app_inline",
		AccessPolicy: `claims.role == "admin"`,
	}
	snap.Apps["app_bad_expr"] = &core.AIApp{
		ID:           "app_bad_expr",
		AccessPolicy: `10 / claims.zero == 1`,
	}

	ctx := context.Background()

	t.Run("missing snapshot returns ErrMissingSnapshot", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_1",
			AIAppID:   "app_open",
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrMissingSnapshot)
	})

	t.Run("missing app returns ErrMissingAIApp", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_2",
			Snapshot:  snap,
			AIAppID:   "non_existent_app",
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrMissingAIApp)
	})

	t.Run("open app without policy allows access", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_3",
			Snapshot:  snap,
			AIAppID:   "app_open",
			Identity:  &core.Identity{Claims: map[string]any{"plan": "free"}},
		}
		err := stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Nil(t, state.Error)
	})

	t.Run("named policy allowed when claims match", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_4",
			Snapshot:  snap,
			AIAppID:   "app_pro",
			Identity:  &core.Identity{Claims: map[string]any{"plan": "pro"}},
		}
		err := stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Nil(t, state.Error)
	})

	t.Run("named policy denied when claims do not match", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_5",
			Snapshot:  snap,
			AIAppID:   "app_pro",
			Identity:  &core.Identity{Claims: map[string]any{"plan": "free"}},
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrForbidden)
		require.NotNil(t, state.Error)
		assert.Equal(t, "forbidden", state.Error.Code)
		assert.Contains(t, state.Error.Message, "access denied by policy")
		assert.Equal(t, "req_5", state.Error.RequestID)
	})

	t.Run("inline policy allowed", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_6",
			Snapshot:  snap,
			AIAppID:   "app_inline",
			Identity:  &core.Identity{Claims: map[string]any{"role": "admin"}},
		}
		err := stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Nil(t, state.Error)
	})

	t.Run("inline policy denied", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_7",
			Snapshot:  snap,
			AIAppID:   "app_inline",
			Identity:  &core.Identity{Claims: map[string]any{"role": "viewer"}},
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrForbidden)
		require.NotNil(t, state.Error)
		assert.Equal(t, "forbidden", state.Error.Code)
	})

	t.Run("policy evaluation runtime error denies with 403 sanitized message", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_8",
			Snapshot:  snap,
			AIAppID:   "app_bad_expr",
			Identity:  &core.Identity{Claims: map[string]any{"zero": 0}},
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrForbidden)
		require.NotNil(t, state.Error)
		assert.Equal(t, "forbidden", state.Error.Code)
		assert.Equal(t, "access denied by policy", state.Error.Message)
		assert.Contains(t, err.Error(), "division by zero")
	})

	t.Run("context cancelled returns error immediately", func(t *testing.T) {
		cancCtx, cancel := context.WithCancel(ctx)
		cancel()

		state := &core.ReqState{
			RequestID: "req_9",
			Snapshot:  snap,
			AIAppID:   "app_pro",
			Identity:  &core.Identity{Claims: map[string]any{"plan": "pro"}},
		}
		err := stage.Handle(cancCtx, state)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("app object already on state syncs AIAppID", func(t *testing.T) {
		state := &core.ReqState{
			RequestID: "req_10",
			Snapshot:  snap,
			AIApp:     snap.Apps["app_open"],
			Identity:  &core.Identity{Claims: map[string]any{"plan": "free"}},
		}
		err := stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Equal(t, "app_open", state.AIAppID)
	})

	t.Run("nil req state returns error", func(t *testing.T) {
		err := stage.Handle(ctx, nil)
		require.Error(t, err)
	})
}

func TestAuthZStage_AuthorizeTool(t *testing.T) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(t, err)

	stage, err := NewAuthZStage(evaluator)
	require.NoError(t, err)

	compiler := evaluator.Compiler()
	prgAdmin, err := compiler.Compile(`claims.role == "admin"`)
	require.NoError(t, err)

	snap := core.NewSnapshot("proj-1", "prod", 1)
	snap.CompiledPolicies["admin_tool"] = prgAdmin

	toolOpen := &core.ToolDefinition{
		ID:            "weather",
		Authorization: "",
	}
	toolRestricted := &core.ToolDefinition{
		ID:            "delete_db",
		Authorization: "admin_tool",
	}

	ctx := context.Background()
	adminID := &core.Identity{Claims: map[string]any{"role": "admin"}}
	userID := &core.Identity{Claims: map[string]any{"role": "user"}}

	// Unrestricted tool
	allowed, err := stage.AuthorizeTool(ctx, snap, toolOpen, userID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// Restricted tool with matching claims
	allowed, err = stage.AuthorizeTool(ctx, snap, toolRestricted, adminID, nil)
	require.NoError(t, err)
	assert.True(t, allowed)

	// Restricted tool with non-matching claims
	allowed, err = stage.AuthorizeTool(ctx, snap, toolRestricted, userID, nil)
	require.NoError(t, err)
	assert.False(t, allowed)

	// Nil tool returns error
	_, err = stage.AuthorizeTool(ctx, snap, nil, adminID, nil)
	require.Error(t, err)

	// Nil snapshot on restricted tool returns ErrMissingSnapshot
	_, err = stage.AuthorizeTool(ctx, nil, toolRestricted, adminID, nil)
	require.ErrorIs(t, err, ErrMissingSnapshot)

	// Cancelled context returns error
	cancCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = stage.AuthorizeTool(cancCtx, snap, toolRestricted, adminID, nil)
	require.ErrorIs(t, err, context.Canceled)
}
