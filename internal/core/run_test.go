package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryRunStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()

	run := &Run{
		ID:          "run_001",
		ProjectID:   "proj_test",
		Environment: "production",
		AIAppID:     "app_assistant",
		RequestID:   "req_001",
		Status:      RunStatusQueued,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]any{
			"caller": "user_123",
		},
	}

	// 1. Create run
	err := store.CreateRun(ctx, run)
	require.NoError(t, err)

	// 2. Duplicate create fails
	err = store.CreateRun(ctx, run)
	require.ErrorIs(t, err, ErrRunAlreadyExists)

	// 3. Get run by exact project, env, runID
	got, err := store.GetRun(ctx, "proj_test", "production", "run_001")
	require.NoError(t, err)
	assert.Equal(t, "run_001", got.ID)
	assert.Equal(t, RunStatusQueued, got.Status)
	assert.Equal(t, "user_123", got.Metadata["caller"])

	// 4. Get run by lookup (empty project/env)
	gotLookup, err := store.GetRun(ctx, "", "", "run_001")
	require.NoError(t, err)
	assert.Equal(t, "run_001", gotLookup.ID)

	// 5. Update status to running
	err = store.UpdateRunStatus(ctx, "proj_test", "production", "run_001", RunStatusRunning, nil, "", nil)
	require.NoError(t, err)
	gotRunning, err := store.GetRun(ctx, "proj_test", "production", "run_001")
	require.NoError(t, err)
	assert.Equal(t, RunStatusRunning, gotRunning.Status)
	assert.Nil(t, gotRunning.CompletedAt)

	// 6. Update status to completed with response and usage
	resp := &ChatResponse{
		ID:        "chatcmpl_001",
		RequestID: "req_001",
		AIAppID:   "app_assistant",
		Message: ChatMessage{
			Role:    "assistant",
			Content: "Hello world!",
		},
		Usage: UsageStats{
			TotalTokens: 42,
		},
	}
	usage := &UsageStats{TotalTokens: 42, InputTokens: 20, OutputTokens: 22}

	err = store.UpdateRunStatus(ctx, "proj_test", "production", "run_001", RunStatusCompleted, resp, "", usage)
	require.NoError(t, err)

	gotCompleted, err := store.GetRun(ctx, "proj_test", "production", "run_001")
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, gotCompleted.Status)
	assert.NotNil(t, gotCompleted.CompletedAt)
	require.NotNil(t, gotCompleted.Result)
	assert.Equal(t, "Hello world!", gotCompleted.Result.Message.Content)
	require.NotNil(t, gotCompleted.Usage)
	assert.Equal(t, 42, gotCompleted.Usage.TotalTokens)

	// 7. Non-existent run
	_, err = store.GetRun(ctx, "proj_test", "production", "non_existent")
	assert.ErrorIs(t, err, ErrRunNotFound)
}

func TestMemoryRunStore_Cancel(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()

	run := &Run{
		ID:          "run_cancel_test",
		ProjectID:   "p1",
		Environment: "dev",
		AIAppID:     "app1",
		Status:      RunStatusRunning,
	}
	require.NoError(t, store.CreateRun(ctx, run))

	// Cancel active run
	require.NoError(t, store.CancelRun(ctx, "p1", "dev", "run_cancel_test"))
	got, err := store.GetRun(ctx, "p1", "dev", "run_cancel_test")
	require.NoError(t, err)
	assert.Equal(t, RunStatusCancelled, got.Status)
	assert.NotNil(t, got.CompletedAt)

	// Cancelling an already terminal run is a no-op
	require.NoError(t, store.CancelRun(ctx, "p1", "dev", "run_cancel_test"))
	got2, err := store.GetRun(ctx, "p1", "dev", "run_cancel_test")
	require.NoError(t, err)
	assert.Equal(t, RunStatusCancelled, got2.Status)
}
