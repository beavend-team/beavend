package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
)

func TestContinuationManager_IssueAndValidateToken(t *testing.T) {
	key := []byte("secret-key-1234567890123456789012")
	cm := NewContinuationManager(NewInMemorySuspendedStateStore(), key)

	state := &SuspendedState{
		ProjectID: "proj_abc",
		RequestID: "req_xyz",
		Subject:   "usr_alice",
	}

	toolCall := core.ToolCall{
		ID:        "call_transfer",
		Name:      "transfer_funds",
		Arguments: `{"amount":100,"to":"bob"}`,
	}

	token, err := cm.IssueConfirmationToken(state, toolCall, 5*time.Minute)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	if token == "" || !strings.Contains(token, ".") {
		t.Fatalf("invalid token format: %q", token)
	}

	// Validate valid token
	claims, err := cm.ValidateConfirmationToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.ProjectID != "proj_abc" || claims.RequestID != "req_xyz" || claims.Subject != "usr_alice" {
		t.Errorf("claims mismatch: %+v", claims)
	}

	if claims.ArgsHash != ComputeArgsHash(toolCall.Arguments) {
		t.Errorf("args hash mismatch")
	}

	// 1. Tampered signature test
	tamperedToken := token[:len(token)-5] + "XXXXX"
	_, err = cm.ValidateConfirmationToken(tamperedToken)
	if err == nil {
		t.Fatalf("expected signature error for tampered token, got nil")
	}
	if !errors.Is(err, ErrTokenSignatureInvalid) {
		t.Errorf("expected ErrTokenSignatureInvalid, got %v", err)
	}

	// 2. Expired token test
	expiredToken, err := cm.IssueConfirmationToken(state, toolCall, -1*time.Second)
	if err != nil {
		t.Fatalf("failed to issue expired token: %v", err)
	}
	_, err = cm.ValidateConfirmationToken(expiredToken)
	if err == nil {
		t.Fatalf("expected expired error, got nil")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestContinuationManager_ValidateResumption(t *testing.T) {
	key := []byte("secret-key-1234567890123456789012")
	store := NewInMemorySuspendedStateStore()
	cm := NewContinuationManager(store, key)
	ctx := context.Background()

	state := &SuspendedState{
		ProjectID: "proj_1",
		RequestID: "req_1",
		Subject:   "usr_alice",
		PendingToolCall: core.ToolCall{
			ID:        "t_1",
			Name:      "delete_account",
			Arguments: `{"confirm":true}`,
		},
	}

	// Save state in store
	if err := store.Save(ctx, state, 5*time.Minute); err != nil {
		t.Fatalf("save state failed: %v", err)
	}

	token, err := cm.IssueConfirmationToken(state, state.PendingToolCall, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}

	// 1. Successful resumption with matching user (atomically consumes state)
	callerAlice := &core.Identity{Subject: "usr_alice"}
	retrievedState, claims, err := cm.ValidateResumption(ctx, token, callerAlice)
	if err != nil {
		t.Fatalf("resumption failed: %v", err)
	}
	if retrievedState.RequestID != "req_1" || claims.Subject != "usr_alice" {
		t.Errorf("state mismatch: %+v", retrievedState)
	}

	// 1b. Immediate replay of the consumed token must fail (TOCTOU defense)
	_, _, err = cm.ValidateResumption(ctx, token, callerAlice)
	if err == nil || !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("expected ErrStateNotFound on replay, got %v", err)
	}

	// 2. Caller mismatch (attacker tries to resume someone else's request)
	_ = store.Save(ctx, state, 5*time.Minute)
	callerEve := &core.Identity{Subject: "usr_eve"}
	_, _, err = cm.ValidateResumption(ctx, token, callerEve)
	if err == nil {
		t.Fatalf("expected subject mismatch error, got nil")
	}
	if !errors.Is(err, ErrSubjectMismatch) {
		t.Errorf("expected ErrSubjectMismatch, got %v", err)
	}

	// 3. Arguments tampered in stored state
	stateTampered := &SuspendedState{
		ProjectID: "proj_1",
		RequestID: "req_1",
		Subject:   "usr_alice",
		PendingToolCall: core.ToolCall{
			ID:        "t_1",
			Name:      "delete_account",
			Arguments: `{"confirm":false,"tampered":true}`,
		},
	}
	_ = store.Save(ctx, stateTampered, 5*time.Minute)
	_, _, err = cm.ValidateResumption(ctx, token, callerAlice)
	if err == nil {
		t.Fatalf("expected args hash mismatch error, got nil")
	}
	if !errors.Is(err, ErrArgsHashMismatch) {
		t.Errorf("expected ErrArgsHashMismatch, got %v", err)
	}

	// 4. Missing / deleted state
	_ = store.Delete(ctx, "proj_1", "req_1")
	_, _, err = cm.ValidateResumption(ctx, token, callerAlice)
	if err == nil {
		t.Fatalf("expected state not found error, got nil")
	}
	if !errors.Is(err, ErrStateNotFound) {
		t.Errorf("expected ErrStateNotFound, got %v", err)
	}

	// 5. Anonymous caller cannot resume authenticated user's state
	_ = store.Save(ctx, state, 5*time.Minute)
	_, _, err = cm.ValidateResumption(ctx, token, nil)
	if err == nil || !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("expected ErrSubjectMismatch for nil callerID, got %v", err)
	}
	_, _, err = cm.ValidateResumption(ctx, token, &core.Identity{Subject: ""})
	if err == nil || !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("expected ErrSubjectMismatch for empty subject caller, got %v", err)
	}

	// 6. Project isolation check
	_ = store.Save(ctx, state, 5*time.Minute)
	callerDiffProject := &core.Identity{Subject: "usr_alice", Project: "proj_other"}
	_, _, err = cm.ValidateResumption(ctx, token, callerDiffProject)
	if err == nil || !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("expected ErrSubjectMismatch for project mismatch, got %v", err)
	}
}

func TestComputeArgsHash_Normalization(t *testing.T) {
	h1 := ComputeArgsHash("")
	h2 := ComputeArgsHash("   ")
	h3 := ComputeArgsHash("{}")
	h4 := ComputeArgsHash("{  }")

	if h1 != h2 || h2 != h3 || h3 != h4 {
		t.Fatalf("expected all empty/whitespace variants to yield same hash: %s vs %s vs %s vs %s", h1, h2, h3, h4)
	}
}

func TestInMemorySuspendedStateStore_Consume(t *testing.T) {
	store := NewInMemorySuspendedStateStore()
	ctx := context.Background()

	state := &SuspendedState{
		ProjectID:   "proj_test",
		Environment: "prod",
		RequestID:   "req_atom_1",
		Subject:     "usr_bob",
	}

	require.NoError(t, store.Save(ctx, state, 5*time.Minute))

	// First consume should return the state and delete it atomically
	consumed, err := store.Consume(ctx, "proj_test", "prod", "req_atom_1")
	require.NoError(t, err)
	assert.Equal(t, "req_atom_1", consumed.RequestID)

	// Second consume must return ErrStateNotFound
	_, err = store.Consume(ctx, "proj_test", "prod", "req_atom_1")
	assert.ErrorIs(t, err, ErrStateNotFound)
}

func TestContinuationManager_ValidateResumption9Claims(t *testing.T) {
	store := NewInMemorySuspendedStateStore()
	hmacKey := []byte("testing-hmac-key-for-claims-validation-32b")
	cm := NewContinuationManager(store, hmacKey)
	ctx := context.Background()

	state := &SuspendedState{
		ProjectID:        "proj_claims",
		Environment:      "staging",
		AIAppID:          "app_claims",
		RequestID:        "req_claims_1",
		Issuer:           "https://auth.company.com",
		Subject:          "usr_charlie",
		BoundaryNonce:    "nonce_secure_boundary",
		PendingToolCalls: []core.ToolCall{
			{ID: "call_a", Name: "tool_a", Arguments: `{"arg":"1"}`},
			{ID: "call_b", Name: "tool_b", Arguments: `{"arg":"2"}`},
		},
		PendingToolCall: core.ToolCall{
			ID:        "call_a",
			Name:      "tool_a",
			Arguments: `{"arg":"1"}`,
		},
	}

	require.NoError(t, store.Save(ctx, state, 5*time.Minute))

	// Issue token bound to call_b
	tokenB, err := cm.IssueConfirmationToken(state, state.PendingToolCalls[1], 5*time.Minute)
	require.NoError(t, err)

	callerOk := &core.Identity{
		Project:     "proj_claims",
		Environment: "staging",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_charlie",
	}

	// Successful validation with all 9 claims matching
	retrievedState, claims, err := cm.ValidateResumption(ctx, tokenB, callerOk)
	require.NoError(t, err)
	assert.Equal(t, "call_b", claims.ToolID)
	assert.Equal(t, "staging", claims.Environment)
	assert.Equal(t, "app_claims", claims.AIAppID)
	assert.Equal(t, "https://auth.company.com", claims.Issuer)
	assert.Equal(t, "usr_charlie", claims.Subject)
	assert.Equal(t, "proj_claims", claims.ProjectID)
	assert.Equal(t, "req_claims_1", claims.RequestID)
	assert.Equal(t, state.BoundaryNonce, retrievedState.BoundaryNonce)

	// Replaying consumed token immediately fails (atomic TOCTOU defense)
	_, _, err = cm.ValidateResumption(ctx, tokenB, callerOk)
	assert.ErrorIs(t, err, ErrStateNotFound)

	// Re-save state for remaining claim tests
	require.NoError(t, store.Save(ctx, state, 5*time.Minute))

	// Environment mismatch rejection
	callerBadEnv := &core.Identity{
		Project:     "proj_claims",
		Environment: "production",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_charlie",
	}
	_, _, err = cm.ValidateResumption(ctx, tokenB, callerBadEnv)
	assert.ErrorIs(t, err, ErrSubjectMismatch)

	// Issuer mismatch rejection
	callerBadIssuer := &core.Identity{
		Project:     "proj_claims",
		Environment: "staging",
		Issuer:      "https://rogue.issuer.com",
		Subject:     "usr_charlie",
	}
	_, _, err = cm.ValidateResumption(ctx, tokenB, callerBadIssuer)
	assert.ErrorIs(t, err, ErrSubjectMismatch)
}

func TestRedisSuspendedStateStore_AESGCMEncryption(t *testing.T) {
	key := []byte("aes-256-gcm-master-key-32bytes!!")
	store := &RedisSuspendedStateStore{encKey: key}

	state := SuspendedState{
		ProjectID:        "proj_enc",
		Environment:      "prod",
		RequestID:        "req_enc_123",
		AIAppID:          "app_enc",
		Subject:          "usr_enc",
		BoundaryNonce:    "nonce_123",
		SnapshotRevision: 42,
	}

	data, err := json.Marshal(state)
	require.NoError(t, err)

	aad := []byte(suspendedAAD("proj_enc", "prod", "req_enc_123"))
	ciphertext, nonce, err := secrets.EncryptWithAAD(key, data, aad)
	require.NoError(t, err)

	payload := append(nonce, ciphertext...)

	// Successfully decode encrypted payload with matching AAD
	decoded, err := store.decodeState(payload, "proj_enc", "prod", "req_enc_123")
	require.NoError(t, err)
	assert.Equal(t, "proj_enc", decoded.ProjectID)
	assert.Equal(t, "usr_enc", decoded.Subject)
	assert.Equal(t, "nonce_123", decoded.BoundaryNonce)

	// Tampered ciphertext fails decryption
	tampered := make([]byte, len(payload))
	copy(tampered, payload)
	tampered[len(tampered)-1] ^= 0xFF
	_, err = store.decodeState(tampered, "proj_enc", "prod", "req_enc_123")
	assert.Error(t, err, "expected decryption error for tampered ciphertext")
}
