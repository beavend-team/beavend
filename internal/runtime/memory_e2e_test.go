package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/control"
	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/memory"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/stages"
	"github.com/beavend/beavend/internal/snapshot"
)

type mockEchoProvider struct{}

func (m *mockEchoProvider) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	lastMsg := ""
	if len(req.Messages) > 0 {
		lastMsg = req.Messages[len(req.Messages)-1].Content
	}

	reply := "Echo: " + lastMsg
	// Check if conversation history had a secret number
	for _, msg := range req.Messages {
		if msg.Role == "user" && msg.Content == "My secret number is 42" {
			reply = "Remembered secret: 42"
		}
		if msg.Role == "system" && bytes.Contains([]byte(msg.Content), []byte("Spanish")) {
			reply = "Hola! Hablo Espanol."
		}
	}

	return &core.ChatResponse{
		ID:        "chat_mock",
		RequestID: "req_mock",
		CreatedAt: time.Now().Unix(),
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: reply,
		},
		FinishReason: "stop",
		Usage: core.UsageStats{
			InputTokens:  10,
			OutputTokens: 10,
			TotalTokens:  20,
		},
	}, nil
}

func (m *mockEchoProvider) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 2)
	go func() {
		defer close(ch)
		ch <- core.StreamEvent{Type: "delta", Delta: "Streaming reply"}
		ch <- core.StreamEvent{Type: "done"}
	}()
	return ch, nil
}

func (m *mockEchoProvider) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	return &core.EmbedResponse{}, nil
}

func (m *mockEchoProvider) Health(ctx context.Context) error {
	return nil
}

func TestContextAndMemory_E2E(t *testing.T) {
	ctx := context.Background()

	// 1. Setup in-memory config snapshot
	registry := snapshot.NewRegistry()
	snap := core.NewSnapshot("proj-mem", "development", 1)

	app := &core.AIApp{
		ID:             "app-assistant",
		ProjectID:      "proj-mem",
		Environment:    "development",
		Name:           "Assistant",
		ModelRoutingID: "model-echo",
		Memory: core.MemoryConfig{
			Enabled:      true,
			VectorRecall: true,
			RecallLimit:  3,
			ThreadTTLSec: 3600,
		},
	}
	snap.Apps["app-assistant"] = app

	routingRule := &core.ModelRouting{
		ID:           "model-echo",
		ProjectID:    "proj-mem",
		Environment:  "development",
		LogicalModel: "echo-model",
		Deployment:   "mock-deploy",
		Adapter:      "mock",
	}
	snap.Models["model-echo"] = routingRule
	snap.Models["echo-model"] = routingRule
	registry.Swap(snap)

	// 2. Setup memory engine and thread store
	localMem := memory.NewLocalMemoryProvider()
	threadStore := memory.NewMemoryThreadStore()
	memEngine := memory.NewEngine(memory.EngineConfig{
		DefaultProvider: localMem,
		ThreadStore:     threadStore,
	})

	// 3. Setup resolver and pipeline
	modelResolver := routing.NewResolver()
	modelResolver.RegisterAdapter("mock", &mockEchoProvider{})

	authzStage, _ := stages.NewAuthZStage(nil)
	contextStage := stages.NewContextStage(memEngine, threadStore)
	routingStage, _ := stages.NewModelRoutingStage(modelResolver)
	execStage, _ := stages.NewExecutionStage(modelResolver, nil)

	pipeline := runtime.NewPipeline(authzStage, contextStage, routingStage, execStage)

	handler := runtime.NewHandler(registry, pipeline, runtime.HandlerOptions{
		DefaultEnvironment: "development",
		ThreadStore:        threadStore,
		MemoryEngine:       memEngine,
	})

	r := chi.NewRouter()
	r.Route("/v1/apps", func(apps chi.Router) {
		apps.Post("/{ai_app_id}/chat", handler.HandleChat)
		apps.Get("/{ai_app_id}/threads/{thread_id}", handler.HandleGetThread)
		apps.Delete("/{ai_app_id}/threads/{thread_id}", handler.HandleDeleteThread)
		apps.Post("/{ai_app_id}/threads", handler.HandleCreateThread)
	})

	// ------------------------------------------------------------------
	// Test Step 1: Multi-turn Conversation with Server-side Thread
	// ------------------------------------------------------------------
	// Turn 1: Client sends "My secret number is 42" with thread_id: "th_user_conversation"
	chatPayload1 := map[string]any{
		"thread_id": "th_user_conversation",
		"messages": []map[string]string{
			{"role": "user", "content": "My secret number is 42"},
		},
	}
	body1, _ := json.Marshal(chatPayload1)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/apps/app-assistant/chat", bytes.NewReader(body1))
	req1.Header.Set("X-Project-ID", "proj-mem")
	req1.Header.Set("X-Environment", "development")
	req1.Header.Set("Content-Type", "application/json")

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	var resp1 core.ChatResponse
	err := json.Unmarshal(w1.Body.Bytes(), &resp1)
	require.NoError(t, err)
	assert.Equal(t, "th_user_conversation", resp1.ThreadID)

	// Turn 2: Client sends query "What is my secret number?" with same thread_id
	chatPayload2 := map[string]any{
		"thread_id": "th_user_conversation",
		"messages": []map[string]string{
			{"role": "user", "content": "What is my secret number?"},
		},
	}
	body2, _ := json.Marshal(chatPayload2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/apps/app-assistant/chat", bytes.NewReader(body2))
	req2.Header.Set("X-Project-ID", "proj-mem")
	req2.Header.Set("X-Environment", "development")
	req2.Header.Set("Content-Type", "application/json")

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var resp2 core.ChatResponse
	err = json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	// Assistant remembered the prior turn from thread history!
	assert.Equal(t, "Remembered secret: 42", resp2.Message.Content)

	// ------------------------------------------------------------------
	// Test Step 2: Thread Inspection and Thread Deletion
	// ------------------------------------------------------------------
	getThreadReq := httptest.NewRequest(http.MethodGet, "/v1/apps/app-assistant/threads/th_user_conversation", nil)
	getThreadReq.Header.Set("X-Project-ID", "proj-mem")
	getThreadReq.Header.Set("X-Environment", "development")
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, getThreadReq)
	require.Equal(t, http.StatusOK, wGet.Code)

	var th core.Thread
	err = json.Unmarshal(wGet.Body.Bytes(), &th)
	require.NoError(t, err)
	assert.Equal(t, "th_user_conversation", th.ID)
	// Should have collected turns:
	assert.NotEmpty(t, th.Messages)

	// Delete Thread
	delThreadReq := httptest.NewRequest(http.MethodDelete, "/v1/apps/app-assistant/threads/th_user_conversation", nil)
	delThreadReq.Header.Set("X-Project-ID", "proj-mem")
	delThreadReq.Header.Set("X-Environment", "development")
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, delThreadReq)
	require.Equal(t, http.StatusOK, wDel.Code)

	// Verify 404 after deletion
	wGet2 := httptest.NewRecorder()
	r.ServeHTTP(wGet2, getThreadReq)
	assert.Equal(t, http.StatusNotFound, wGet2.Code)

	// ------------------------------------------------------------------
	// Test Step 3: Semantic Memory Recall
	// ------------------------------------------------------------------
	err = localMem.Store(ctx, core.MemoryRecord{
		ProjectID:   "proj-mem",
		Environment: "development",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_carlos",
		AIAppID:     "app-assistant",
		Content:     "User prefers to speak in Spanish",
	})
	require.NoError(t, err)

	// Request with authenticated Identity
	carlosID := &core.Identity{
		Project:     "proj-mem",
		Environment: "development",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_carlos",
	}

	chatPayload3 := map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "Hello there!"},
		},
	}
	body3, _ := json.Marshal(chatPayload3)
	req3 := httptest.NewRequest(http.MethodPost, "/v1/apps/app-assistant/chat", bytes.NewReader(body3))
	req3.Header.Set("X-Project-ID", "proj-mem")
	req3.Header.Set("X-Environment", "development")
	req3.Header.Set("Content-Type", "application/json")
	req3 = req3.WithContext(core.ContextWithIdentity(req3.Context(), carlosID))

	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	var resp3 core.ChatResponse
	err = json.Unmarshal(w3.Body.Bytes(), &resp3)
	require.NoError(t, err)
	// Provider saw the injected memory context and replied in Spanish!
	assert.Equal(t, "Hola! Hablo Espanol.", resp3.Message.Content)

	// ------------------------------------------------------------------
	// Test Step 4: GDPR Privacy Purge Hook
	// ------------------------------------------------------------------
	// Also create a thread for Carlos
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          "th_carlos",
		ProjectID:   "proj-mem",
		Environment: "development",
		Issuer:      "https://auth.company.com",
		Subject:     "usr_carlos",
	})

	ctrlService := control.NewService(nil, nil, audit.NewInMemoryAuditor(), []byte("01234567890123456789012345678901"))
	ctrlService.SetMemoryProvider(localMem)
	ctrlService.SetThreadStore(threadStore)
	ctrlHandler := control.NewHandler(ctrlService)

	adminRouter := chi.NewRouter()
	adminRouter.Delete("/v1/admin/subjects/{issuer}/{subject}", ctrlHandler.HandlePrivacyPurgeSubject)

	purgeReq := httptest.NewRequest(http.MethodDelete, "/v1/admin/subjects/https%3A%2F%2Fauth.company.com/usr_carlos", nil)
	purgeReq.Header.Set("X-Project-ID", "proj-mem")
	purgeReq.Header.Set("X-Environment", "development")
	wPurge := httptest.NewRecorder()
	adminRouter.ServeHTTP(wPurge, purgeReq)
	require.Equal(t, http.StatusOK, wPurge.Code)

	// Verify memories purged
	recalledCarlos, err := localMem.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj-mem",
		Issuer:    "https://auth.company.com",
		Subject:   "usr_carlos",
	})
	require.NoError(t, err)
	assert.Empty(t, recalledCarlos)

	// Verify thread purged
	_, err = threadStore.GetThread(ctx, "proj-mem", "development", "th_carlos")
	assert.ErrorIs(t, err, memory.ErrThreadNotFound)
}
