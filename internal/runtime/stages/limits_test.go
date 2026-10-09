package stages

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/limits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockFailingLimitStore struct{}

func (m *mockFailingLimitStore) CheckAndDebit(_ context.Context, _ string, _ []core.LimitRule, _ int64) (bool, time.Duration, error) {
	return false, 0, errors.New("connection refused to redis")
}

func (m *mockFailingLimitStore) ReconcileTokens(_ context.Context, _ string, _, _ int64) error {
	return errors.New("connection refused to redis")
}

func TestLimitsStage_TableDriven(t *testing.T) {
	snap := core.NewSnapshot("proj-1", "production", 1)

	// App 1: No limits
	appNoLimits := &core.AIApp{
		ID:     "app-no-limits",
		Limits: nil,
	}
	snap.Apps[appNoLimits.ID] = appNoLimits

	// App 2: Inference limit (2 req per minute) and tokens limit (1000 per 24h)
	appWithLimits := &core.AIApp{
		ID: "app-with-limits",
		Limits: []core.LimitRule{
			{
				Resource: "inference",
				Window:   time.Minute,
				Max:      2,
			},
			{
				Resource: "tokens",
				Window:   24 * time.Hour,
				Max:      1000,
			},
			{
				Resource:      "inference",
				Window:        time.Minute,
				Max:           10, // Pro tier gets 10 per minute
				TierCondition: "claims.plan == 'pro'",
			},
		},
	}
	snap.Apps[appWithLimits.ID] = appWithLimits

	ctx := context.Background()

	t.Run("app without limits passes", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID:   "req-1",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-no-limits",
			Snapshot:    snap,
		}

		err = stage.Handle(ctx, state)
		assert.NoError(t, err)
		assert.Nil(t, state.Error)
	})

	t.Run("requests under limit pass and subsequent request exceeding limit is blocked", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		identity := &core.Identity{
			Issuer:  "https://auth.example.com",
			Subject: "user-free-1",
			Claims:  map[string]any{"plan": "free"},
		}

		maxTok := 200
		chatReq := &core.ChatRequest{
			Messages:  []core.ChatMessage{{Role: "user", Content: "Hi"}},
			MaxTokens: &maxTok,
		}

		// Request 1: passes
		state1 := &core.ReqState{
			RequestID:   "req-1",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-with-limits",
			Snapshot:    snap,
			Identity:    identity,
			ChatRequest: chatReq,
		}
		err = stage.Handle(ctx, state1)
		assert.NoError(t, err)
		assert.Nil(t, state1.Error)
		assert.Greater(t, state1.Metadata["estimated_tokens"].(int64), int64(200))

		// Request 2: passes (count = 2)
		state2 := &core.ReqState{
			RequestID:   "req-2",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-with-limits",
			Snapshot:    snap,
			Identity:    identity,
			ChatRequest: chatReq,
		}
		err = stage.Handle(ctx, state2)
		assert.NoError(t, err)
		assert.Nil(t, state2.Error)

		// Request 3: blocked by inference limit (2 per minute)
		state3 := &core.ReqState{
			RequestID:   "req-3",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-with-limits",
			Snapshot:    snap,
			Identity:    identity,
			ChatRequest: chatReq,
		}
		err = stage.Handle(ctx, state3)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)
		require.NotNil(t, state3.Error)
		assert.Equal(t, "rate_limited", state3.Error.Code)
		assert.Equal(t, "rate limit exceeded", state3.Error.Message)
		assert.Greater(t, state3.Error.RetryAfter, 0)
	})

	t.Run("tier resolution: pro user gets higher limit and does not get blocked after 2 requests", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		identityPro := &core.Identity{
			Issuer:  "https://auth.example.com",
			Subject: "user-pro-1",
			Claims:  map[string]any{"plan": "pro"},
		}

		maxTok := 50
		chatReq := &core.ChatRequest{
			Messages:  []core.ChatMessage{{Role: "user", Content: "Hi pro"}},
			MaxTokens: &maxTok,
		}

		// Perform 3 requests: pro user has max: 10, so all 3 pass!
		for i := 1; i <= 3; i++ {
			state := &core.ReqState{
				RequestID:   "req-pro",
				ProjectID:   "proj-1",
				Environment: "production",
				AIAppID:     "app-with-limits",
				Snapshot:    snap,
				Identity:    identityPro,
				ChatRequest: chatReq,
			}
			err = stage.Handle(ctx, state)
			assert.NoError(t, err, "request %d for pro user should pass", i)
			assert.Nil(t, state.Error)
		}
	})

	t.Run("store failure with fail closed returns unavailable error", func(t *testing.T) {
		failingStore := &mockFailingLimitStore{}
		stage, err := NewLimitsStage(failingStore, nil, &LimitsStageOptions{FailClosed: true})
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID:   "req-fail",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-with-limits",
			Snapshot:    snap,
		}

		err = stage.Handle(ctx, state)
		assert.Error(t, err)
		require.NotNil(t, state.Error)
		assert.Equal(t, "unavailable", state.Error.Code)
	})

	t.Run("store failure with fail open allows request", func(t *testing.T) {
		failingStore := &mockFailingLimitStore{}
		stage, err := NewLimitsStage(failingStore, nil, &LimitsStageOptions{FailClosed: false})
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID:   "req-fail-open",
			ProjectID:   "proj-1",
			Environment: "production",
			AIAppID:     "app-with-limits",
			Snapshot:    snap,
		}

		err = stage.Handle(ctx, state)
		assert.NoError(t, err)
		assert.Nil(t, state.Error)
	})

	t.Run("missing snapshot returns error", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID: "req-missing-snap",
			AIAppID:   "app-with-limits",
			Snapshot:  nil,
		}

		err = stage.Handle(ctx, state)
		assert.ErrorIs(t, err, ErrMissingSnapshot)
		require.NotNil(t, state.Error)
		assert.Equal(t, "unavailable", state.Error.Code)
	})

	t.Run("missing app id returns error", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID: "req-missing-appid",
			Snapshot:  snap,
		}

		err = stage.Handle(ctx, state)
		assert.ErrorIs(t, err, ErrMissingAIApp)
		require.NotNil(t, state.Error)
		assert.Equal(t, "invalid_request", state.Error.Code)
	})

	t.Run("unknown app returns not found", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		state := &core.ReqState{
			RequestID: "req-unknown-app",
			AIAppID:   "non-existent",
			Snapshot:  snap,
		}

		err = stage.Handle(ctx, state)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrMissingAIApp)
		require.NotNil(t, state.Error)
		assert.Equal(t, "not_found", state.Error.Code)
	})

	t.Run("project-level rate limit blocks across different users", func(t *testing.T) {
		store := limits.NewMemoryLimitStore(nil)
		stage, err := NewLimitsStage(store, nil, nil)
		require.NoError(t, err)

		snapProj := core.NewSnapshot("proj-shared", "production", 1)
		snapProj.Apps["app-proj-limit"] = &core.AIApp{
			ID: "app-proj-limit",
			Limits: []core.LimitRule{
				{
					Resource: "inference",
					Window:   10 * time.Second,
					Max:      1,
					Scope:    "project",
				},
			},
		}

		// User A makes request 1 -> allowed
		stateA := &core.ReqState{
			RequestID:   "req-a",
			ProjectID:   "proj-shared",
			Environment: "production",
			AIAppID:     "app-proj-limit",
			Snapshot:    snapProj,
			Identity:    &core.Identity{Subject: "user-a", Issuer: "test"},
		}
		err = stage.Handle(ctx, stateA)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), stateA.Metadata["ratelimit_limit"])

		// User B makes request 2 -> blocked by project-level limit
		stateB := &core.ReqState{
			RequestID:   "req-b",
			ProjectID:   "proj-shared",
			Environment: "production",
			AIAppID:     "app-proj-limit",
			Snapshot:    snapProj,
			Identity:    &core.Identity{Subject: "user-b", Issuer: "test"},
		}
		err = stage.Handle(ctx, stateB)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)
		require.NotNil(t, stateB.Error)
		assert.Equal(t, "rate_limited", stateB.Error.Code)
		assert.Contains(t, stateB.Error.Message, "project rate limit exceeded")
	})
}
