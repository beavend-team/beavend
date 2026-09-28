package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
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

	// 1. Successful resumption with matching user
	callerAlice := &core.Identity{Subject: "usr_alice"}
	retrievedState, claims, err := cm.ValidateResumption(ctx, token, callerAlice)
	if err != nil {
		t.Fatalf("resumption failed: %v", err)
	}
	if retrievedState.RequestID != "req_1" || claims.Subject != "usr_alice" {
		t.Errorf("state mismatch: %+v", retrievedState)
	}

	// 2. Caller mismatch (attacker tries to resume someone else's request)
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
