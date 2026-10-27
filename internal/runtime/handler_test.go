package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/media"
	"github.com/beavend/beavend/internal/memory"
	"github.com/beavend/beavend/internal/prompts"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime/stages"
	"github.com/beavend/beavend/internal/tools"
)

type mockSnapshotResolver struct {
	snapshots map[string]*core.Snapshot
}

func (m *mockSnapshotResolver) Get(projectID, env string) (*core.Snapshot, bool) {
	snap, ok := m.snapshots[projectID+":"+env]
	return snap, ok
}

type mockAdapter struct {
	chatFn   func(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
	streamFn func(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error)
}

func (m *mockAdapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &core.ChatResponse{
		ID: "chat_123",
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: "Hello world!",
		},
		Usage: core.UsageStats{TotalTokens: 10},
	}, nil
}

func (m *mockAdapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	ch := make(chan core.StreamEvent, 5)
	go func() {
		ch <- core.StreamEvent{Type: "delta", Delta: "Hello "}
		ch <- core.StreamEvent{Type: "delta", Delta: "world!"}
		ch <- core.StreamEvent{Type: "done"}
		close(ch)
	}()
	return ch, nil
}

func (m *mockAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}

func (m *mockAdapter) Health(_ context.Context) error {
	return nil
}

func setupTestEnvironment(t *testing.T) (*chi.Mux, *mockAdapter, *Handler) {
	resolver := &mockSnapshotResolver{
		snapshots: make(map[string]*core.Snapshot),
	}

	snap := core.NewSnapshot("proj_1", "development", 1)
	pv := &core.PromptVersion{
		ID:        "pv_1",
		PromptID:  "prompt_bot",
		Version:   1,
		Template:  "You are assistant for {{ project.id }}.",
		Variables: []string{"project.id"},
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	snap.Prompts[pv.PromptID] = pv

	app := &core.AIApp{
		ID:             "assistant",
		Name:           "Test Assistant",
		PromptID:       pv.PromptID,
		PromptVersion:  1,
		ModelRoutingID: "fast",
		ToolIDs:        []string{"cancel_order", "delete_account"},
	}
	snap.Apps[app.ID] = app

	snap.Tools["cancel_order"] = &core.ToolDefinition{
		ID:           "cancel_order",
		Name:         "cancel_order",
		SideEffect:   "write",
		Confirmation: "required",
	}
	snap.Tools["delete_account"] = &core.ToolDefinition{
		ID:           "delete_account",
		Name:         "delete_account",
		SideEffect:   "write",
		Confirmation: "required",
	}

	mRouting := &core.ModelRouting{
		ID:           "mr_fast",
		LogicalModel: "fast",
		Deployment:   "gpt-4o",
		Adapter:      "litellm",
	}
	snap.Models[mRouting.LogicalModel] = mRouting

	resolver.snapshots["proj_1:development"] = snap

	modelResolver := routing.NewResolver()
	adapter := &mockAdapter{}
	modelResolver.RegisterAdapter("litellm", adapter)

	promptStage, err := stages.NewPromptStage(prompts.NewEngine())
	require.NoError(t, err)

	routingStage, err := stages.NewModelRoutingStage(modelResolver)
	require.NoError(t, err)

	execStage, err := stages.NewExecutionStage(modelResolver)
	require.NoError(t, err)

	pipeline := NewPipeline(promptStage, routingStage, execStage)
	handler := NewHandler(resolver, pipeline)

	r := chi.NewRouter()
	r.Post("/v1/apps/{ai_app_id}/chat", handler.HandleChat)
	r.Post("/v1/apps/{ai_app_id}/stream", handler.HandleStream)
	r.Post("/v1/apps/{ai_app_id}/confirm", handler.HandleConfirm)
	r.Post("/v1/apps/{ai_app_id}/tool-results", handler.HandleToolResults)
	r.Post("/v1/apps/{ai_app_id}/answer", handler.HandleAnswer)

	return r, adapter, handler
}

func TestHandler_HandleChat(t *testing.T) {
	r, _, _ := setupTestEnvironment(t)

	t.Run("missing project id header returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var errResp core.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		assert.Equal(t, "invalid_argument", errResp.Code)
	})

	t.Run("unknown app returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/nonexistent/chat", strings.NewReader(`{}`))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		var errResp core.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		assert.Equal(t, "not_found", errResp.Code)
	})

	t.Run("successful chat completion", func(t *testing.T) {
		body := `{"messages":[{"role":"user","content":"Hi"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", bytes.NewBufferString(body))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var resp core.ChatResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "assistant", resp.AIAppID)
		assert.Equal(t, "Hello world!", resp.Message.Content)
		assert.Equal(t, 10, resp.Usage.TotalTokens)
	})
}

func TestHandler_HandleStream(t *testing.T) {
	r, _, _ := setupTestEnvironment(t)

	t.Run("successful stream completion returns SSE chunks", func(t *testing.T) {
		body := `{"messages":[{"role":"user","content":"Hi"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/stream", bytes.NewBufferString(body))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/event-stream; charset=utf-8", rec.Header().Get("Content-Type"))

		bodyStr := rec.Body.String()
		assert.Contains(t, bodyStr, "event: delta\ndata: {\"text\":\"Hello \"}\n\n")
		assert.Contains(t, bodyStr, "event: delta\ndata: {\"text\":\"world!\"}\n\n")
		assert.Contains(t, bodyStr, "event: done\n")
	})
}

func TestHandler_HandleConfirm(t *testing.T) {
	r, adapter, handler := setupTestEnvironment(t)

	// Setup a suspended state
	susp := &tools.SuspendedState{
		ProjectID:   "proj_1",
		Environment: "development",
		RequestID:   "req_confirm_test",
		AIAppID:     "assistant",
		Subject:     "usr_alice",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Cancel order 10"},
			{
				Role: "assistant",
				ToolCalls: []core.ToolCall{
					{ID: "call_cancel", Name: "cancel_order", Arguments: `{"order_id":10}`},
				},
			},
		},
		PendingToolCall: core.ToolCall{
			ID:        "call_cancel",
			Name:      "cancel_order",
			Arguments: `{"order_id":10}`,
		},
		ToolDefinitionID: "cancel_order",
		SnapshotRevision: 1,
		IsStreaming:      false,
	}

	ctx := context.Background()
	err := handler.ContinuationManager().Store().Save(ctx, susp, 5*time.Minute)
	require.NoError(t, err)

	token, err := handler.ContinuationManager().IssueConfirmationToken(susp, susp.PendingToolCall, 5*time.Minute)
	require.NoError(t, err)

	adapter.chatFn = func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
		lastMsg := req.Messages[len(req.Messages)-1]
		return &core.ChatResponse{
			ID: "chat_resumed",
			Message: core.ChatMessage{
				Role:    "assistant",
				Content: "Resumed with: " + lastMsg.Content,
			},
		}, nil
	}

	t.Run("successful approval resumes and executes", func(t *testing.T) {
		body := `{"confirmation_token":"` + token + `","approved":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		// Add identity context
		req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{Subject: "usr_alice"}))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp core.ChatResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "chat_resumed", resp.ID)
		assert.Contains(t, resp.Message.Content, "Resumed with:")
	})

	t.Run("invalid token returns 400", func(t *testing.T) {
		body := `{"confirmation_token":"invalid.token","approved":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("subject mismatch returns 403 forbidden", func(t *testing.T) {
		body := `{"confirmation_token":"` + token + `","approved":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{Subject: "usr_attacker"}))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		var errResp core.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		assert.Equal(t, "forbidden", errResp.Code)
	})
}

func TestHandler_MultiSuspendingToolBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("sequential confirmation re-suspends then resumes", func(t *testing.T) {
		r, adapter, handler := setupTestEnvironment(t)

		adapterCalls := 0
		var finalMessages []core.ChatMessage
		adapter.chatFn = func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			adapterCalls++
			finalMessages = req.Messages
			return &core.ChatResponse{
				ID: "resp_resumed_batch",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "All operations confirmed and executed.",
				},
			}, nil
		}

		call1 := core.ToolCall{ID: "call_cancel_1", Name: "cancel_order", Arguments: `{"order_id":10}`}
		call2 := core.ToolCall{ID: "call_delete_2", Name: "delete_account", Arguments: `{"account_id":99}`}

		susp := &tools.SuspendedState{
			ProjectID:        "proj_1",
			Environment:      "development",
			RequestID:        "req_multi_batch",
			AIAppID:          "assistant",
			Subject:          "usr_alice",
			BoundaryNonce:    "nonce_batch_test",
			SnapshotRevision: 1,
			PendingToolCall:  call1,
			PendingToolCalls: []core.ToolCall{call1, call2},
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Cancel order 10 and delete account"},
				{
					Role:      "assistant",
					ToolCalls: []core.ToolCall{call1, call2},
				},
			},
		}

		require.NoError(t, handler.ContinuationManager().Store().Save(ctx, susp, 5*time.Minute))
		token1, err := handler.ContinuationManager().IssueConfirmationToken(susp, call1, 5*time.Minute)
		require.NoError(t, err)

		// 1. Confirm call 1: should re-suspend for call 2, adapter NOT called yet
		body1 := `{"confirmation_token":"` + token1 + `","approved":true}`
		req1 := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body1))
		req1.Header.Set("X-Project-ID", "proj_1")
		req1.Header.Set("X-Environment", "development")
		req1 = req1.WithContext(identity.ContextWithIdentity(req1.Context(), &core.Identity{Subject: "usr_alice"}))

		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)

		require.Equal(t, http.StatusOK, rec1.Code)
		token2 := rec1.Header().Get("X-Confirmation-Token")
		require.NotEmpty(t, token2, "re-suspension should issue new confirmation token for remaining tool")
		assert.Equal(t, 0, adapterCalls, "upstream adapter must NOT be called while pending calls remain")

		var intermediateResp core.ChatResponse
		require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &intermediateResp))
		assert.Equal(t, "confirmation_required", intermediateResp.FinishReason)
		require.Len(t, intermediateResp.Message.ToolCalls, 1)
		assert.Equal(t, "call_delete_2", intermediateResp.Message.ToolCalls[0].ID)

		// Verify stored state in ContinuationManager
		stored, err := handler.ContinuationManager().Store().Get(ctx, "proj_1", "req_multi_batch")
		require.NoError(t, err)
		require.Len(t, stored.PendingToolCalls, 1)
		assert.Equal(t, "call_delete_2", stored.PendingToolCalls[0].ID)

		// 2. Confirm call 2: resolves all pending calls, upstream adapter IS called
		body2 := `{"confirmation_token":"` + token2 + `","approved":true}`
		req2 := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body2))
		req2.Header.Set("X-Project-ID", "proj_1")
		req2.Header.Set("X-Environment", "development")
		req2 = req2.WithContext(identity.ContextWithIdentity(req2.Context(), &core.Identity{Subject: "usr_alice"}))

		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)

		require.Equal(t, http.StatusOK, rec2.Code)
		assert.Equal(t, 1, adapterCalls, "upstream adapter must be called once all pending calls are resolved")

		var finalResp core.ChatResponse
		require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &finalResp))
		assert.Equal(t, "All operations confirmed and executed.", finalResp.Message.Content)

		// Verify both tool results are present in messages passed to upstream adapter
		var foundCall1, foundCall2 bool
		for _, m := range finalMessages {
			if m.Role == "tool" && m.ToolCallID == "call_cancel_1" {
				foundCall1 = true
			}
			if m.Role == "tool" && m.ToolCallID == "call_delete_2" {
				foundCall2 = true
			}
		}
		assert.True(t, foundCall1, "tool result for call 1 should be passed upstream")
		assert.True(t, foundCall2, "tool result for call 2 should be passed upstream")
	})

	t.Run("declining one call cancels all pending tool calls in batch", func(t *testing.T) {
		r, adapter, handler := setupTestEnvironment(t)

		adapterCalls := 0
		var finalMessages []core.ChatMessage
		adapter.chatFn = func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			adapterCalls++
			finalMessages = req.Messages
			return &core.ChatResponse{
				ID: "resp_declined",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Understood, cancelled batch operations.",
				},
			}, nil
		}

		call1 := core.ToolCall{ID: "call_cancel_1", Name: "cancel_order", Arguments: `{"order_id":10}`}
		call2 := core.ToolCall{ID: "call_delete_2", Name: "delete_account", Arguments: `{"account_id":99}`}

		susp := &tools.SuspendedState{
			ProjectID:        "proj_1",
			Environment:      "development",
			RequestID:        "req_declined_batch",
			AIAppID:          "assistant",
			Subject:          "usr_alice",
			BoundaryNonce:    "nonce_decline_test",
			SnapshotRevision: 1,
			PendingToolCall:  call1,
			PendingToolCalls: []core.ToolCall{call1, call2},
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Cancel order 10 and delete account"},
				{
					Role:      "assistant",
					ToolCalls: []core.ToolCall{call1, call2},
				},
			},
		}

		require.NoError(t, handler.ContinuationManager().Store().Save(ctx, susp, 5*time.Minute))
		token, err := handler.ContinuationManager().IssueConfirmationToken(susp, call1, 5*time.Minute)
		require.NoError(t, err)

		// Decline confirmation
		body := `{"confirmation_token":"` + token + `","approved":false}`
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(body))
		req.Header.Set("X-Project-ID", "proj_1")
		req.Header.Set("X-Environment", "development")
		req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{Subject: "usr_alice"}))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, adapterCalls, "upstream adapter must be called to handle decline")

		// Verify BOTH call1 and call2 received cancellation tool messages
		var call1Cancelled, call2Cancelled bool
		for _, m := range finalMessages {
			if m.Role == "tool" && m.ToolCallID == "call_cancel_1" {
				assert.Contains(t, m.Content, "User declined to confirm tool execution")
				call1Cancelled = true
			}
			if m.Role == "tool" && m.ToolCallID == "call_delete_2" {
				assert.Contains(t, m.Content, "User declined to confirm tool execution")
				call2Cancelled = true
			}
		}
		assert.True(t, call1Cancelled, "call 1 should receive cancelled tool message")
		assert.True(t, call2Cancelled, "call 2 should also receive cancelled tool message to prevent orphan calls")
	})
}

func TestHandler_HandleToolResults(t *testing.T) {
	r, adapter, handler := setupTestEnvironment(t)

	susp := &tools.SuspendedState{
		ProjectID:   "proj_1",
		Environment: "development",
		RequestID:   "req_client_tool",
		AIAppID:     "assistant",
		Subject:     "usr_bob",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Open camera"},
			{
				Role: "assistant",
				ToolCalls: []core.ToolCall{
					{ID: "call_cam", Name: "open_camera", Arguments: `{}`},
				},
			},
		},
		PendingToolCall: core.ToolCall{
			ID:        "call_cam",
			Name:      "open_camera",
			Arguments: `{}`,
		},
		SnapshotRevision: 1,
		IsStreaming:      false,
	}

	ctx := context.Background()
	err := handler.ContinuationManager().Store().Save(ctx, susp, 5*time.Minute)
	require.NoError(t, err)

	token, err := handler.ContinuationManager().IssueConfirmationToken(susp, susp.PendingToolCall, 5*time.Minute)
	require.NoError(t, err)

	adapter.chatFn = func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
		lastMsg := req.Messages[len(req.Messages)-1]
		return &core.ChatResponse{
			ID: "chat_from_client_tool",
			Message: core.ChatMessage{
				Role:    "assistant",
				Content: "Received: " + lastMsg.Content,
			},
		}, nil
	}

	body := `{"continuation_token":"` + token + `","call_id":"call_cam","result":{"status":"photo_taken"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/tool-results", bytes.NewBufferString(body))
	req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{Subject: "usr_bob"}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp core.ChatResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Message.Content, "photo_taken")
}

type mockUsageStore struct {
	mu       sync.Mutex
	recorded []*core.ReqState
}

func (m *mockUsageStore) RecordUsage(_ context.Context, state *core.ReqState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *state
	m.recorded = append(m.recorded, &clone)
	return nil
}

func (m *mockUsageStore) Recorded() []*core.ReqState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*core.ReqState, len(m.recorded))
	copy(out, m.recorded)
	return out
}

func TestHandler_UsageRecording(t *testing.T) {
	resolver := &mockSnapshotResolver{
		snapshots: make(map[string]*core.Snapshot),
	}
	snap := core.NewSnapshot("proj_1", "development", 1)
	snap.Apps["assistant"] = &core.AIApp{
		ID:             "assistant",
		ModelRoutingID: "fast",
	}
	snap.Models["fast"] = &core.ModelRouting{
		ID:           "fast",
		LogicalModel: "fast",
		Deployment:   "gpt-4o",
		Adapter:      "litellm",
	}
	resolver.snapshots["proj_1:development"] = snap

	adapter := &mockAdapter{
		chatFn: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				ID:      "chat_usage",
				Message: core.ChatMessage{Role: "assistant", Content: "Hello"},
				Usage:   core.UsageStats{InputTokens: 5, OutputTokens: 10, TotalTokens: 15},
			}, nil
		},
		streamFn: func(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
			ch := make(chan core.StreamEvent, 4)
			go func() {
				ch <- core.StreamEvent{Type: "delta", Delta: "Hello"}
				ch <- core.StreamEvent{Type: "usage", Usage: &core.UsageStats{InputTokens: 5, OutputTokens: 10, TotalTokens: 15}}
				ch <- core.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	modelResolver := routing.NewResolver()
	modelResolver.RegisterAdapter("litellm", adapter)

	routingStage, _ := stages.NewModelRoutingStage(modelResolver)
	execStage, _ := stages.NewExecutionStage(modelResolver)
	pipeline := NewPipeline(routingStage, execStage)

	usageStore := &mockUsageStore{}
	handler := NewHandler(resolver, pipeline, HandlerOptions{
		UsageStore: usageStore,
	})

	t.Run("HandleChat invokes usageStore", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", strings.NewReader(`{}`))
		req.Header.Set("X-Project-ID", "proj_1")
		rec := httptest.NewRecorder()

		handler.HandleChat(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Wait briefly for async record
		require.Eventually(t, func() bool {
			return len(usageStore.Recorded()) == 1
		}, 1*time.Second, 10*time.Millisecond)

		recorded := usageStore.Recorded()
		assert.Equal(t, "assistant", recorded[0].AIAppID)
	})

	t.Run("HandleStream invokes usageStore and captures streamed usage", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/stream", strings.NewReader(`{}`))
		req.Header.Set("X-Project-ID", "proj_1")
		rec := httptest.NewRecorder()

		handler.HandleStream(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		require.Eventually(t, func() bool {
			return len(usageStore.Recorded()) == 2
		}, 1*time.Second, 10*time.Millisecond)

		recorded := usageStore.Recorded()
		assert.Equal(t, 15, recorded[1].Usage.TotalTokens)
	})
}

func TestHandler_HandleCancel(t *testing.T) {
	_, _, handler := setupTestEnvironment(t)

	// Save a suspended state
	susp := &tools.SuspendedState{
		ProjectID:        "proj_1",
		Environment:      "development",
		RequestID:        "req_to_cancel",
		AIAppID:          "assistant",
		Subject:          "usr_bob",
		SnapshotRevision: 1,
	}
	ctx := context.Background()
	err := handler.ContinuationManager().Store().Save(ctx, susp, 5*time.Minute)
	require.NoError(t, err)

	token, err := handler.ContinuationManager().IssueConfirmationToken(susp, core.ToolCall{ID: "t1"}, 5*time.Minute)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Post("/v1/apps/{ai_app_id}/cancel", handler.HandleCancel)

	// 1. Cancel via continuation_token
	body := `{"continuation_token":"` + token + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/cancel", bytes.NewBufferString(body))
	req.Header.Set("X-Project-ID", "proj_1")
	req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{Subject: "usr_bob", Project: "proj_1"}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "cancelled", resp["status"])
	assert.Equal(t, "req_to_cancel", resp["request_id"])

	// State should now be deleted from store
	_, err = handler.ContinuationManager().Store().Get(ctx, "proj_1", "req_to_cancel")
	assert.ErrorIs(t, err, tools.ErrStateNotFound)
}

func TestHandler_HandleGetApp(t *testing.T) {
	_, _, handler := setupTestEnvironment(t)

	r := chi.NewRouter()
	r.Get("/v1/apps/{ai_app_id}", handler.HandleGetApp)

	// Valid app
	req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
	req.Header.Set("X-Project-ID", "proj_1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "assistant", resp["id"])
	assert.Equal(t, "Test Assistant", resp["name"])
	assert.Equal(t, "active", resp["status"])

	// Non-existent app
	reqBad := httptest.NewRequest(http.MethodGet, "/v1/apps/non_existent", nil)
	reqBad.Header.Set("X-Project-ID", "proj_1")
	recBad := httptest.NewRecorder()
	r.ServeHTTP(recBad, reqBad)
	assert.Equal(t, http.StatusNotFound, recBad.Code)
}

func TestHandler_HandleGetJob(t *testing.T) {
	resolver := &mockSnapshotResolver{snapshots: make(map[string]*core.Snapshot)}
	jobStore := media.NewInMemoryJobStore()

	handler := NewHandler(resolver, nil, HandlerOptions{
		JobStore: jobStore,
	})

	r := chi.NewRouter()
	r.Get("/v1/apps/{ai_app_id}/jobs/{job_id}", handler.HandleGetJob)

	ctx := context.Background()
	_ = jobStore.CreateJob(ctx, &media.MediaJob{
		ID:        "job_123",
		ProjectID: "proj_1",
		AIAppID:   "assistant",
		Status:    "completed",
		Result: &core.GenerateResponse{
			Items: []core.GeneratedItem{
				{Index: 0, URL: "http://localhost:8080/v1/media/med_123.mp4"},
			},
		},
	}, time.Hour)

	// Valid job query
	req := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant/jobs/job_123", nil)
	req.Header.Set("X-Project-ID", "proj_1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var job media.MediaJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &job))
	assert.Equal(t, "job_123", job.ID)
	assert.Equal(t, "completed", job.Status)
	assert.Len(t, job.Result.Items, 1)

	// Non-existent job
	reqBad := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant/jobs/job_nonexistent", nil)
	reqBad.Header.Set("X-Project-ID", "proj_1")
	recBad := httptest.NewRecorder()
	r.ServeHTTP(recBad, reqBad)
	assert.Equal(t, http.StatusNotFound, recBad.Code)

	// Wrong app
	reqWrongApp := httptest.NewRequest(http.MethodGet, "/v1/apps/other_app/jobs/job_123", nil)
	reqWrongApp.Header.Set("X-Project-ID", "proj_1")
	recWrongApp := httptest.NewRecorder()
	r.ServeHTTP(recWrongApp, reqWrongApp)
	assert.Equal(t, http.StatusNotFound, recWrongApp.Code)
}

func TestHandler_HandleGetMedia(t *testing.T) {
	tempDir := t.TempDir()
	signingKey := "test-secret-key-1234567890123456"
	mediaStore, err := media.NewLocalMediaStore(media.LocalMediaStoreConfig{
		BaseDir:    tempDir,
		SigningKey: signingKey,
		BaseURL:    "/v1/media",
	})
	require.NoError(t, err)

	handler := NewHandler(nil, nil, HandlerOptions{
		MediaStore: mediaStore,
	})

	r := chi.NewRouter()
	r.Get("/v1/media/{project_id}/{media_id}", handler.HandleGetMedia)

	ctx := context.Background()
	ref, err := mediaStore.Put(ctx, "proj_test", strings.NewReader("sample image content"), "image/png", "sample.png")
	require.NoError(t, err)

	// Valid signed URL request
	req := httptest.NewRequest(http.MethodGet, ref.URL, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "sample image content", rec.Body.String())

	// Invalid signature
	reqBadSig := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/media/proj_test/%s?exp=%d&sig=invalid", ref.MediaID, time.Now().Add(time.Hour).Unix()), nil)
	recBadSig := httptest.NewRecorder()
	r.ServeHTTP(recBadSig, reqBadSig)
	assert.Equal(t, http.StatusUnauthorized, recBadSig.Code)

	// Missing media
	sigURL, _ := mediaStore.SignedURL(ctx, "proj_test", "nonexistent_media", time.Hour)
	reqNotFound := httptest.NewRequest(http.MethodGet, sigURL, nil)
	recNotFound := httptest.NewRecorder()
	r.ServeHTTP(recNotFound, reqNotFound)
	assert.Equal(t, http.StatusNotFound, recNotFound.Code)
}

func TestHandler_Answer(t *testing.T) {
	r, adapter, handler := setupTestEnvironment(t)
	r.Post("/v1/apps/{ai_app_id}/answer", handler.HandleAnswer)

	adapter.chatFn = func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
		lastMsg := req.Messages[len(req.Messages)-1]
		return &core.ChatResponse{
			ID: "chat_resumed_answer",
			Message: core.ChatMessage{
				Role:    "assistant",
				Content: "Answering with: " + lastMsg.Content,
			},
		}, nil
	}

	contStore := tools.NewInMemorySuspendedStateStore()
	contMgr := tools.NewContinuationManager(contStore, []byte("01234567890123456789012345678901"))
	handler.continuationMgr = contMgr

	susp := &tools.SuspendedState{
		ProjectID:   "proj_1",
		Environment: "development",
		RequestID:   "req_answer_1",
		AIAppID:     "assistant",
		ThreadID:    "th_answer",
		Subject:     "usr_alice",
		Issuer:      "https://auth.test",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Need help"},
			{Role: "assistant", Content: "Window or aisle?"},
		},
		PendingToolCall: core.ToolCall{
			ID:        "call_ask",
			Name:      "ask_user",
			Arguments: `{"question":"Window or aisle?"}`,
		},
		BoundaryNonce: "bnonce123",
		CreatedAt:     time.Now().UTC(),
	}

	require.NoError(t, contStore.Save(context.Background(), susp, time.Minute))

	token, err := contMgr.IssueConfirmationToken(susp, susp.PendingToolCall, time.Minute)
	require.NoError(t, err)

	// 1. Successful answer resumption
	body := `{"confirmation_token":"` + token + `","answer":"I prefer window"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/answer", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Project-ID", "proj_1")
	req.Header.Set("X-Environment", "development")
	req = req.WithContext(identity.ContextWithIdentity(req.Context(), &core.Identity{
		Project: "proj_1",
		Subject: "usr_alice",
		Issuer:  "https://auth.test",
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp core.ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "chat_resumed_answer", resp.ID)
	assert.Contains(t, resp.Message.Content, "I prefer window")
	assert.Contains(t, resp.Message.Content, "bnonce123")

	// 2. Immediate replay of already-consumed token fails
	reqReplay := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/answer", bytes.NewBufferString(body))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("X-Project-ID", "proj_1")
	reqReplay.Header.Set("X-Environment", "development")
	reqReplay = reqReplay.WithContext(identity.ContextWithIdentity(reqReplay.Context(), &core.Identity{
		Project: "proj_1",
		Subject: "usr_alice",
		Issuer:  "https://auth.test",
	}))
	recReplay := httptest.NewRecorder()
	r.ServeHTTP(recReplay, reqReplay)
	assert.Equal(t, http.StatusNotFound, recReplay.Code)
}

func TestHandler_ResumptionHygiene(t *testing.T) {
	ctx := context.Background()

	promptEng := prompts.NewEngine()
	promptStage, err := stages.NewPromptStage(promptEng)
	require.NoError(t, err)

	threadStore := memory.NewMemoryThreadStore()
	contextStage := stages.NewContextStage(nil, threadStore)

	// Create thread with existing historical messages
	thread := &core.Thread{
		ID:          "th_hygiene",
		ProjectID:   "p1",
		Environment: "prod",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Initial message"},
			{Role: "assistant", Content: "Initial reply"},
		},
	}
	require.NoError(t, threadStore.CreateThread(ctx, thread))

	snap := core.NewSnapshot("p1", "prod", 1)
	snap.Prompts["prompt_sys"] = &core.PromptVersion{
		ID:       "prompt_sys",
		Template: "You are Beavend Assistant.",
	}
	snap.Apps["app_hygiene"] = &core.AIApp{
		ID:       "app_hygiene",
		PromptID: "prompt_sys",
	}

	t.Run("when is_resumed_turn is true context and prompt stages do NOT duplicate", func(t *testing.T) {
		resumedState := &core.ReqState{
			RequestID:   "req_resumed",
			ProjectID:   "p1",
			Environment: "prod",
			AIAppID:     "app_hygiene",
			Snapshot:    snap,
			AIApp:       snap.Apps["app_hygiene"],
			ChatRequest: &core.ChatRequest{
				ThreadID: "th_hygiene",
				Messages: []core.ChatMessage{
					{Role: "system", Content: "You are Beavend Assistant."},
					{Role: "user", Content: "Initial message"},
					{Role: "assistant", Content: "Initial reply"},
					{Role: "user", Content: "Followup turn"},
				},
			},
			Metadata: map[string]any{
				"is_resumed_turn": true,
			},
		}

		origMsgCount := len(resumedState.ChatRequest.Messages)

		// Run ContextStage
		require.NoError(t, contextStage.Handle(ctx, resumedState))
		assert.Equal(t, origMsgCount, len(resumedState.ChatRequest.Messages), "ContextStage should not duplicate thread history")

		// Run PromptStage
		require.NoError(t, promptStage.Handle(ctx, resumedState))
		assert.Equal(t, origMsgCount, len(resumedState.ChatRequest.Messages), "PromptStage should not duplicate system prompt")
	})

	t.Run("when is_resumed_turn is false context and prompt stages load history and system prompt", func(t *testing.T) {
		freshState := &core.ReqState{
			RequestID:   "req_fresh",
			ProjectID:   "p1",
			Environment: "prod",
			AIAppID:     "app_hygiene",
			Snapshot:    snap,
			AIApp:       snap.Apps["app_hygiene"],
			ChatRequest: &core.ChatRequest{
				ThreadID: "th_hygiene",
				Messages: []core.ChatMessage{
					{Role: "user", Content: "Followup turn"},
				},
			},
			Metadata: map[string]any{},
		}

		// Run ContextStage
		require.NoError(t, contextStage.Handle(ctx, freshState))
		// Thread history was prepended (2 historical messages + 1 fresh = 3)
		assert.Equal(t, 3, len(freshState.ChatRequest.Messages))

		// Run PromptStage
		require.NoError(t, promptStage.Handle(ctx, freshState))
		// System prompt was prepended (1 system + 3 messages = 4)
		assert.Equal(t, 4, len(freshState.ChatRequest.Messages))
		assert.Equal(t, "system", freshState.ChatRequest.Messages[0].Role)
	})
}



