package stages

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
)

func TestPromptStage_Handle(t *testing.T) {
	ctx := context.Background()
	stage, err := NewPromptStage(prompts.NewEngine())
	require.NoError(t, err)

	t.Run("nil state", func(t *testing.T) {
		err := stage.Handle(ctx, nil)
		require.Error(t, err)
	})

	t.Run("missing snapshot", func(t *testing.T) {
		state := &core.ReqState{
			AIAppID: "assistant",
		}
		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrMissingSnapshot)
		assert.Equal(t, "unavailable", state.Error.Code)
	})

	t.Run("missing app in snapshot", func(t *testing.T) {
		snap := core.NewSnapshot("proj_1", "dev", 1)
		state := &core.ReqState{
			Snapshot: snap,
			AIAppID:  "nonexistent",
		}
		err := stage.Handle(ctx, state)
		require.Error(t, err)
		assert.Equal(t, "not_found", state.Error.Code)
	})

	t.Run("app with no prompt configured passes through", func(t *testing.T) {
		snap := core.NewSnapshot("proj_1", "dev", 1)
		app := &core.AIApp{
			ID: "simple_app",
		}
		snap.Apps[app.ID] = app

		state := &core.ReqState{
			Snapshot: snap,
			AIAppID:  app.ID,
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{
					{Role: "user", Content: "Hello"},
				},
			},
		}

		err := stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Len(t, state.ChatRequest.Messages, 1)
		assert.Equal(t, "user", state.ChatRequest.Messages[0].Role)
	})

	t.Run("app references missing prompt", func(t *testing.T) {
		snap := core.NewSnapshot("proj_1", "dev", 1)
		app := &core.AIApp{
			ID:       "support",
			PromptID: "prompt_missing",
		}
		snap.Apps[app.ID] = app

		state := &core.ReqState{
			Snapshot: snap,
			AIAppID:  app.ID,
		}

		err := stage.Handle(ctx, state)
		require.ErrorIs(t, err, ErrPromptNotFound)
		assert.Equal(t, "not_found", state.Error.Code)
	})

	t.Run("successful prompt render and injection", func(t *testing.T) {
		snap := core.NewSnapshot("proj_prod", "production", 42)
		pv := &core.PromptVersion{
			ID:        "pv_01",
			PromptID:  "p_support",
			Version:   2,
			Template:  "You are {{ app.name }} helping user {{ user.id }} with plan {{ claims.plan }} in {{ project.env }}.",
			Variables: []string{"app.name", "user.id", "claims.plan", "project.env"},
			IsActive:  true,
			CreatedAt: time.Now(),
		}
		snap.Prompts[pv.PromptID] = pv

		app := &core.AIApp{
			ID:            "app_support",
			Name:          "Customer Support Bot",
			PromptID:      pv.PromptID,
			PromptVersion: pv.Version,
		}
		snap.Apps[app.ID] = app

		identity := &core.Identity{
			Project:     snap.ProjectID,
			Environment: snap.Environment,
			Subject:     "usr_999",
			Issuer:      "https://auth.mycorp.com",
			Claims: map[string]any{
				"plan": "enterprise",
			},
		}

		state := &core.ReqState{
			ProjectID:   snap.ProjectID,
			Environment: snap.Environment,
			Snapshot:    snap,
			AIAppID:     app.ID,
			Identity:    identity,
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{
					{Role: "user", Content: "Where is my invoice?"},
				},
			},
		}

		err := stage.Handle(ctx, state)
		require.NoError(t, err)

		require.Len(t, state.ChatRequest.Messages, 2)
		assert.Equal(t, "system", state.ChatRequest.Messages[0].Role)
		assert.Equal(t, "You are Customer Support Bot helping user usr_999 with plan enterprise in production.", state.ChatRequest.Messages[0].Content)
		assert.Equal(t, "user", state.ChatRequest.Messages[1].Role)
		assert.Equal(t, "Where is my invoice?", state.ChatRequest.Messages[1].Content)

		// Calling stage again replaces the system message in place without duplicating
		err = stage.Handle(ctx, state)
		require.NoError(t, err)
		assert.Len(t, state.ChatRequest.Messages, 2, "must not duplicate system message")
	})

	t.Run("cancelled context halts stage", func(t *testing.T) {
		cancCtx, cancel := context.WithCancel(context.Background())
		cancel()

		err := stage.Handle(cancCtx, &core.ReqState{})
		require.ErrorIs(t, err, context.Canceled)
	})
}
