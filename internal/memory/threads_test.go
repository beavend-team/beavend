package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestMemoryThreadStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryThreadStore()

	// 1. Create a thread
	th := &core.Thread{
		ID:          "th_100",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "assistant",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_alice",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello assistant!"},
			{Role: "assistant", Content: "Hello Alice! How can I help you today?"},
		},
		TTLSeconds: 3600,
	}

	err := store.CreateThread(ctx, th)
	require.NoError(t, err)

	// 2. Retrieve thread
	fetched, err := store.GetThread(ctx, "proj1", "prod", "th_100")
	require.NoError(t, err)
	assert.Equal(t, "th_100", fetched.ID)
	assert.Len(t, fetched.Messages, 2)
	assert.Equal(t, "Hello Alice! How can I help you today?", fetched.Messages[1].Content)

	// 3. Append messages
	err = store.AppendMessages(ctx, "proj1", "prod", "th_100", []core.ChatMessage{
		{Role: "user", Content: "What is my name?"},
		{Role: "assistant", Content: "Your name is Alice."},
	}, 1*time.Hour)
	require.NoError(t, err)

	updated, err := store.GetThread(ctx, "proj1", "prod", "th_100")
	require.NoError(t, err)
	assert.Len(t, updated.Messages, 4)
	assert.Equal(t, "Your name is Alice.", updated.Messages[3].Content)

	// 4. Delete thread by ID
	err = store.DeleteThread(ctx, "proj1", "prod", "th_100")
	require.NoError(t, err)

	_, err = store.GetThread(ctx, "proj1", "prod", "th_100")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestMemoryThreadStore_SubjectPurge(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryThreadStore()

	// Alice threads
	_ = store.CreateThread(ctx, &core.Thread{
		ID:          "th_a1",
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "alice",
	})
	_ = store.CreateThread(ctx, &core.Thread{
		ID:          "th_a2",
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "alice",
	})
	// Bob thread
	_ = store.CreateThread(ctx, &core.Thread{
		ID:          "th_b1",
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "bob",
	})

	err := store.DeleteThreadsBySubject(ctx, "proj1", "prod", "iss1", "alice")
	require.NoError(t, err)

	_, err = store.GetThread(ctx, "proj1", "prod", "th_a1")
	assert.ErrorIs(t, err, ErrThreadNotFound)

	_, err = store.GetThread(ctx, "proj1", "prod", "th_a2")
	assert.ErrorIs(t, err, ErrThreadNotFound)

	bobThread, err := store.GetThread(ctx, "proj1", "prod", "th_b1")
	require.NoError(t, err)
	assert.Equal(t, "th_b1", bobThread.ID)
}

func TestMemoryThreadStore_TTLExpiration(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryThreadStore()

	past := time.Now().UTC().Add(-10 * time.Minute)
	_ = store.CreateThread(ctx, &core.Thread{
		ID:          "th_expired",
		ProjectID:   "proj1",
		Environment: "dev",
		ExpiresAt:   past,
	})

	_, err := store.GetThread(ctx, "proj1", "dev", "th_expired")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}
