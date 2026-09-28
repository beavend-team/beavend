package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
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

func setupTestEnvironment(t *testing.T) (*chi.Mux, *mockSnapshotResolver, *mockAdapter, *Handler) {
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
		ToolIDs:        []string{"cancel_order"},
	}
	snap.Apps[app.ID] = app

	snap.Tools["cancel_order"] = &core.ToolDefinition{
		ID:           "cancel_order",
		Name:         "cancel_order",
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

	return r, resolver, adapter, handler
}

func TestHandler_HandleChat(t *testing.T) {
	r, _, _, _ := setupTestEnvironment(t)

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
	r, _, _, _ := setupTestEnvironment(t)

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
	r, _, adapter, handler := setupTestEnvironment(t)

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
}

func TestHandler_HandleToolResults(t *testing.T) {
	r, _, adapter, handler := setupTestEnvironment(t)

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
