package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatIdempotencyKey(t *testing.T) {
	key := FormatIdempotencyKey("proj_1", "prod", "app_chat", "usr_alice", "req_key_123")
	assert.Equal(t, "idem:proj_1:prod:app_chat:usr_alice:req_key_123", key)

	anonKey := FormatIdempotencyKey("proj_1", "prod", "app_chat", "", "req_key_123")
	assert.Equal(t, "idem:proj_1:prod:app_chat:anonymous:req_key_123", anonKey)
}

func TestMemoryIdempotencyStore_StateMachine(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	ctx := context.Background()

	hashA := "sha256_hash_payload_a"
	hashB := "sha256_hash_payload_b"

	// 1. First acquire succeeds
	rec, acquired, err := store.Acquire(ctx, "p1", "prod", "a1", "u1", "key1", "req1", hashA, 5*time.Minute)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, StatusInProgress, rec.Status)
	assert.Equal(t, "req1", rec.RequestID)
	assert.Equal(t, hashA, rec.PayloadHash)

	// 2. Duplicate concurrent acquire with same payload hash fails acquisition (returns existing in_progress state)
	recDup, acquiredDup, err := store.Acquire(ctx, "p1", "prod", "a1", "u1", "key1", "req2", hashA, 5*time.Minute)
	require.NoError(t, err)
	assert.False(t, acquiredDup)
	assert.Equal(t, StatusInProgress, recDup.Status)
	assert.Equal(t, "req1", recDup.RequestID)

	// 2b. Concurrent acquire with DIFFERENT payload hash returns ErrPayloadMismatch
	_, _, errMismatch := store.Acquire(ctx, "p1", "prod", "a1", "u1", "key1", "req3", hashB, 5*time.Minute)
	assert.ErrorIs(t, errMismatch, ErrPayloadMismatch)

	// 3. Complete request with wrong req_id fails
	payload := `{"id":"resp_123","message":{"content":"hello"}}`
	errBadReq := store.Complete(ctx, "p1", "prod", "a1", "u1", "key1", "req_rogue", payload, 5*time.Minute)
	assert.Error(t, errBadReq, "expected req_id mismatch error")

	// 3b. Complete request with matching req_id succeeds
	err = store.Complete(ctx, "p1", "prod", "a1", "u1", "key1", "req1", payload, 5*time.Minute)
	require.NoError(t, err)

	// 4. Subsequent acquire returns completed state with cached payload
	recDone, acquiredDone, err := store.Acquire(ctx, "p1", "prod", "a1", "u1", "key1", "req4", hashA, 5*time.Minute)
	require.NoError(t, err)
	assert.False(t, acquiredDone)
	assert.Equal(t, StatusCompleted, recDone.Status)
	assert.Equal(t, payload, recDone.Payload)

	// 5. Release key allows re-acquiring
	require.NoError(t, store.Release(ctx, "p1", "prod", "a1", "u1", "key1"))
	recRe, acquiredRe, err := store.Acquire(ctx, "p1", "prod", "a1", "u1", "key1", "req5", hashA, 5*time.Minute)
	require.NoError(t, err)
	assert.True(t, acquiredRe)
	assert.Equal(t, "req5", recRe.RequestID)
}
