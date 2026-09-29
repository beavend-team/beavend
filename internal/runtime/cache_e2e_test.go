package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/agent"
	"github.com/beavend/beavend/internal/runtime/stages"
)

type cacheMockAdapter struct {
	chatInvocations   atomic.Int64
	streamInvocations atomic.Int64
	responseContent   string
}

func (m *cacheMockAdapter) Chat(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
	callIdx := m.chatInvocations.Add(1)
	return &core.ChatResponse{
		ID:        fmt.Sprintf("resp_%d", callIdx),
		RequestID: "req_test",
		AIAppID:   "app-cache",
		CreatedAt: time.Now().Unix(),
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("%s (call #%d)", m.responseContent, callIdx),
		},
		FinishReason: "stop",
		Usage: core.UsageStats{
			InputTokens:  12,
			OutputTokens: 8,
			TotalTokens:  20,
		},
	}, nil
}

func (m *cacheMockAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	callIdx := m.streamInvocations.Add(1)
	ch := make(chan core.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- core.StreamEvent{
			Type:  "delta",
			Delta: fmt.Sprintf("%s (stream #%d)", m.responseContent, callIdx),
		}
		ch <- core.StreamEvent{
			Type: "usage",
			Usage: &core.UsageStats{
				InputTokens:  12,
				OutputTokens: 8,
				TotalTokens:  20,
			},
		}
		ch <- core.StreamEvent{
			Type: "done",
		}
	}()
	return ch, nil
}

func (m *cacheMockAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}

func (m *cacheMockAdapter) Health(_ context.Context) error {
	return nil
}

type cacheSnapshotResolver struct {
	snapshots map[string]*core.Snapshot
}

func (r *cacheSnapshotResolver) Get(projectID, env string) (*core.Snapshot, bool) {
	snap, ok := r.snapshots[projectID+":"+env]
	return snap, ok
}

func setupCacheTestRouter(
	t *testing.T,
	cacheStore core.CacheStore,
	adapter *cacheMockAdapter,
	appConfig core.AIApp,
) http.Handler {
	t.Helper()

	snap := core.NewSnapshot("proj-cache", "production", 1)
	snap.Apps[appConfig.ID] = &appConfig
	snap.Models["fast"] = &core.ModelRouting{
		ID:           "fast",
		LogicalModel: "fast",
		Deployment:   "mock-deploy",
		Adapter:      "mock",
	}

	reg := &cacheSnapshotResolver{
		snapshots: map[string]*core.Snapshot{
			"proj-cache:production": snap,
		},
	}

	resolver := routing.NewResolver()
	resolver.RegisterAdapter("mock", adapter)

	loop := agent.NewLoop(agent.LoopConfig{
		CacheStore: cacheStore,
	})

	routingStage, _ := stages.NewModelRoutingStage(resolver)
	execStage, _ := stages.NewExecutionStage(resolver, loop)
	cacheStage := stages.NewCacheStage(cacheStore)

	pipeline := runtime.NewFullStandardPipeline(nil, nil, nil, nil, routingStage, execStage, nil, cacheStage)

	runtimeHandler := runtime.NewHandler(reg, pipeline, runtime.HandlerOptions{
		DefaultEnvironment: "production",
		CacheStore:         cacheStore,
	})

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			sub := req.Header.Get("X-User-Subject")
			if sub == "" {
				sub = "user_default"
			}
			ctx := identity.ContextWithIdentity(req.Context(), &core.Identity{
				Project:     "proj-cache",
				Environment: "production",
				Subject:     sub,
				Issuer:      "https://auth.beavend.local",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	r.Post("/v1/apps/{ai_app_id}/chat", runtimeHandler.HandleChat)
	r.Post("/v1/apps/{ai_app_id}/stream", runtimeHandler.HandleStream)

	return r
}

func TestE2E_ResponseCache_UnaryHitAndMiss(t *testing.T) {
	cacheStore := cache.NewMemoryCacheStore()
	adapter := &cacheMockAdapter{responseContent: "The answer is 42."}

	app := core.AIApp{
		ID:             "app-cache",
		ProjectID:      "proj-cache",
		Environment:    "production",
		ModelRoutingID: "fast",
		Cache: core.CacheConfig{
			Enabled: true,
			Scope:   "user",
			TTL:     300,
		},
	}

	router := setupCacheTestRouter(t, cacheStore, adapter, app)

	reqPayload := map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "What is the meaning of life?"},
		},
	}
	bodyBytes, _ := json.Marshal(reqPayload)

	// 1. Initial Request -> Cache Miss
	req1 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	req1.Header.Set("X-Project-ID", "proj-cache")
	req1.Header.Set("X-Environment", "production")
	req1.Header.Set("X-User-Subject", "alice")
	rec1 := httptest.NewRecorder()

	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "MISS", rec1.Header().Get("X-Cache"))
	assert.Equal(t, int64(1), adapter.chatInvocations.Load())

	var resp1 core.ChatResponse
	err := json.Unmarshal(rec1.Body.Bytes(), &resp1)
	require.NoError(t, err)
	assert.Contains(t, resp1.Message.Content, "call #1")

	// 2. Second identical request for Alice -> Cache Hit!
	req2 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	req2.Header.Set("X-Project-ID", "proj-cache")
	req2.Header.Set("X-Environment", "production")
	req2.Header.Set("X-User-Subject", "alice")
	rec2 := httptest.NewRecorder()

	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Cache"))
	assert.Equal(t, int64(1), adapter.chatInvocations.Load(), "LLM must NOT be called on cache hit")

	var resp2 core.ChatResponse
	err = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.Contains(t, resp2.Message.Content, "call #1", "Must return identical cached content")

	// 3. Different request for Alice -> Cache Miss!
	diffPayload := map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "What is the weather?"},
		},
	}
	diffBytes, _ := json.Marshal(diffPayload)

	req3 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(diffBytes))
	req3.Header.Set("X-Project-ID", "proj-cache")
	req3.Header.Set("X-Environment", "production")
	req3.Header.Set("X-User-Subject", "alice")
	rec3 := httptest.NewRecorder()

	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
	assert.Equal(t, "MISS", rec3.Header().Get("X-Cache"))
	assert.Equal(t, int64(2), adapter.chatInvocations.Load())
}

func TestE2E_ResponseCache_UserIsolation(t *testing.T) {
	cacheStore := cache.NewMemoryCacheStore()
	adapter := &cacheMockAdapter{responseContent: "Secret response"}

	app := core.AIApp{
		ID:             "app-cache",
		ProjectID:      "proj-cache",
		Environment:    "production",
		ModelRoutingID: "fast",
		Cache: core.CacheConfig{
			Enabled: true,
			Scope:   "user", // User-isolated cache
			TTL:     300,
		},
	}

	router := setupCacheTestRouter(t, cacheStore, adapter, app)

	bodyBytes, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "Tell me a secret"},
		},
	})

	// Alice queries -> MISS -> call #1
	reqAlice := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	reqAlice.Header.Set("X-Project-ID", "proj-cache")
	reqAlice.Header.Set("X-Environment", "production")
	reqAlice.Header.Set("X-User-Subject", "alice")
	recAlice := httptest.NewRecorder()
	router.ServeHTTP(recAlice, reqAlice)
	assert.Equal(t, "MISS", recAlice.Header().Get("X-Cache"))
	assert.Equal(t, int64(1), adapter.chatInvocations.Load())

	// Bob sends EXACT SAME query -> Because of user isolation, Bob must get a MISS!
	reqBob := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	reqBob.Header.Set("X-Project-ID", "proj-cache")
	reqBob.Header.Set("X-Environment", "production")
	reqBob.Header.Set("X-User-Subject", "bob")
	recBob := httptest.NewRecorder()
	router.ServeHTTP(recBob, reqBob)
	assert.Equal(t, "MISS", recBob.Header().Get("X-Cache"))
	assert.Equal(t, int64(2), adapter.chatInvocations.Load(), "Bob must trigger model call #2")
}

func TestE2E_ResponseCache_StreamingReplay(t *testing.T) {
	cacheStore := cache.NewMemoryCacheStore()
	adapter := &cacheMockAdapter{responseContent: "Streaming acceleration."}

	app := core.AIApp{
		ID:             "app-cache-stream",
		ProjectID:      "proj-cache",
		Environment:    "production",
		ModelRoutingID: "fast",
		Cache: core.CacheConfig{
			Enabled: true,
			Scope:   "shared",
			TTL:     300,
		},
	}

	router := setupCacheTestRouter(t, cacheStore, adapter, app)

	bodyBytes, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "Stream fast"},
		},
		"stream": true,
	})

	// 1. Initial Stream -> MISS
	req1 := httptest.NewRequest("POST", "/v1/apps/app-cache-stream/stream", bytes.NewReader(bodyBytes))
	req1.Header.Set("X-Project-ID", "proj-cache")
	req1.Header.Set("X-Environment", "production")
	rec1 := httptest.NewRecorder()

	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "MISS", rec1.Header().Get("X-Cache"))
	assert.Equal(t, int64(1), adapter.streamInvocations.Load())
	assert.Contains(t, rec1.Body.String(), "Streaming acceleration.")

	// Wait for async stream writing to finalize cache entry
	time.Sleep(50 * time.Millisecond)

	// 2. Second Stream -> HIT!
	req2 := httptest.NewRequest("POST", "/v1/apps/app-cache-stream/stream", bytes.NewReader(bodyBytes))
	req2.Header.Set("X-Project-ID", "proj-cache")
	req2.Header.Set("X-Environment", "production")
	rec2 := httptest.NewRecorder()

	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Cache"))
	assert.Equal(t, int64(1), adapter.streamInvocations.Load(), "Model Stream must NOT be called on cache hit")
	assert.Contains(t, rec2.Body.String(), "Streaming acceleration.")
	assert.True(t, strings.Contains(rec2.Body.String(), "event: delta") || strings.Contains(rec2.Body.String(), "data: {"))
}

func TestE2E_ResponseCache_PrivacyPurge(t *testing.T) {
	cacheStore := cache.NewMemoryCacheStore()
	adapter := &cacheMockAdapter{responseContent: "Alice profile"}

	app := core.AIApp{
		ID:             "app-cache",
		ProjectID:      "proj-cache",
		Environment:    "production",
		ModelRoutingID: "fast",
		Cache: core.CacheConfig{
			Enabled: true,
			Scope:   "user",
			TTL:     300,
		},
	}

	router := setupCacheTestRouter(t, cacheStore, adapter, app)

	bodyBytes, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "Show my profile"},
		},
	})

	// 1. Alice creates cache entry
	req1 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	req1.Header.Set("X-Project-ID", "proj-cache")
	req1.Header.Set("X-Environment", "production")
	req1.Header.Set("X-User-Subject", "alice")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, "MISS", rec1.Header().Get("X-Cache"))

	// Verify hit
	req2 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	req2.Header.Set("X-Project-ID", "proj-cache")
	req2.Header.Set("X-Environment", "production")
	req2.Header.Set("X-User-Subject", "alice")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Cache"))

	// 2. Privacy Purge Alice
	err := cacheStore.DeleteSubjectKeys(context.Background(), "proj-cache", "production", "alice")
	require.NoError(t, err)

	// 3. Subsequent request for Alice -> Cache MISS again!
	req3 := httptest.NewRequest("POST", "/v1/apps/app-cache/chat", bytes.NewReader(bodyBytes))
	req3.Header.Set("X-Project-ID", "proj-cache")
	req3.Header.Set("X-Environment", "production")
	req3.Header.Set("X-User-Subject", "alice")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	assert.Equal(t, "MISS", rec3.Header().Get("X-Cache"))
}
