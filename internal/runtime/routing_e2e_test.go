package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/memory"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/stages"
	"github.com/beavend/beavend/internal/snapshot"
)

// routeMockAdapter records every request and replies with its own name so tests can see which model served a call.
type routeMockAdapter struct {
	name string
	mu   sync.Mutex
	reqs []core.ChatRequest
}

func (m *routeMockAdapter) Chat(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	return &core.ChatResponse{
		ID:           "resp_" + m.name,
		RequestID:    "req_mock",
		CreatedAt:    time.Now().Unix(),
		Message:      core.ChatMessage{Role: "assistant", Content: "served-by:" + m.name},
		FinishReason: "stop",
		Usage:        core.UsageStats{InputTokens: 5, OutputTokens: 5, TotalTokens: 10},
	}, nil
}

func (m *routeMockAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *routeMockAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return &core.EmbedResponse{}, nil
}

func (m *routeMockAdapter) Health(_ context.Context) error { return nil }

func (m *routeMockAdapter) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (m *routeMockAdapter) lastReq() core.ChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reqs[len(m.reqs)-1]
}

type routeE2EEnv struct {
	router   http.Handler
	adapters map[string]*routeMockAdapter
}

func setupRoutingE2E(t *testing.T, cacheEnabled bool) *routeE2EEnv {
	t.Helper()

	names := []string{"text", "vision", "audio", "imagegen", "big"}
	adapters := make(map[string]*routeMockAdapter, len(names))
	resolver := routing.NewResolver()

	snap := core.NewSnapshot("proj-route", "production", 1)
	for _, n := range names {
		a := &routeMockAdapter{name: n}
		adapters[n] = a
		resolver.RegisterAdapter("mock-"+n, a)
		in := []string{"text"}
		out := []string{"text"}
		switch n {
		case "vision":
			in = []string{"text", "image"}
		case "audio":
			in = []string{"text", "audio"}
		case "imagegen":
			out = []string{"image"}
		}
		snap.Models[n] = &core.ModelRouting{
			ID: n, LogicalModel: n, Deployment: n + "-deploy", Adapter: "mock-" + n,
			InputModalities: in, OutputModalities: out,
		}
	}

	app := &core.AIApp{
		ID: "app-route", ProjectID: "proj-route", Environment: "production",
		ModelRoutes: &core.ModelRoutes{
			Default: "text",
			Rules: []core.ModelRouteRule{
				{When: core.RouteCondition{InputHas: []string{"image"}}, Use: "vision"},
				{When: core.RouteCondition{InputHas: []string{"audio"}}, Use: "audio"},
				{When: core.RouteCondition{Output: []string{"image"}}, Use: "imagegen"},
				{When: core.RouteCondition{Metadata: map[string]string{"complexity": "high"}}, Use: "big"},
			},
		},
		Cache:  core.CacheConfig{Enabled: cacheEnabled, TTL: 60, Scope: "shared"},
		Memory: core.MemoryConfig{Enabled: true, ThreadTTLSec: 3600},
	}
	snap.Apps[app.ID] = app

	registry := snapshot.NewRegistry()
	registry.Swap(snap)

	cacheStore := cache.NewMemoryCacheStore()
	threadStore := memory.NewMemoryThreadStore()
	memEngine := memory.NewEngine(memory.EngineConfig{
		DefaultProvider: memory.NewLocalMemoryProvider(),
		ThreadStore:     threadStore,
	})

	routingStage, err := stages.NewModelRoutingStage(resolver)
	require.NoError(t, err)
	execStage, err := stages.NewExecutionStage(resolver, nil)
	require.NoError(t, err)
	routeSelect, err := stages.NewRouteSelectStage(resolver)
	require.NoError(t, err)

	pipeline := runtime.NewContextAwarePipeline(
		nil, nil, nil,
		stages.NewCacheStage(cacheStore),
		stages.NewContextStage(memEngine, threadStore),
		nil, routingStage, execStage, nil, routeSelect,
	)

	h := runtime.NewHandler(registry, pipeline, runtime.HandlerOptions{
		DefaultEnvironment: "production",
		CacheStore:         cacheStore,
		ThreadStore:        threadStore,
		MemoryEngine:       memEngine,
	})

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := identity.ContextWithIdentity(req.Context(), &core.Identity{
				Project: "proj-route", Environment: "production", Subject: "user_1", Issuer: "https://auth.beavend.local",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/v1/apps/{ai_app_id}/chat", h.HandleChat)
	r.Get("/v1/apps/{ai_app_id}/threads/{thread_id}", h.HandleGetThread)

	return &routeE2EEnv{router: r, adapters: adapters}
}

func (e *routeE2EEnv) chat(t *testing.T, payload map[string]any) core.ChatResponse {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/app-route/chat", bytes.NewReader(body))
	req.Header.Set("X-Project-ID", "proj-route")
	req.Header.Set("X-Environment", "production")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp core.ChatResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func userMsg(text string) []map[string]string {
	return []map[string]string{{"role": "user", "content": text}}
}

func TestE2E_ModelRouting_ByModality(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"text only uses default", map[string]any{"messages": userMsg("hello")}, "text"},
		{"image input", map[string]any{
			"messages":    userMsg("what is this"),
			"attachments": []map[string]any{{"type": "image", "mime_type": "image/png", "data": []byte{1, 2, 3}}},
		}, "vision"},
		{"audio input", map[string]any{
			"messages":    userMsg("transcribe"),
			"attachments": []map[string]any{{"type": "audio", "mime_type": "audio/wav", "data": []byte{4, 5, 6}}},
		}, "audio"},
		{"image output", map[string]any{
			"messages":          userMsg("draw a cat"),
			"output_modalities": []string{"image"},
		}, "imagegen"},
		{"metadata rule", map[string]any{
			"messages": userMsg("hard question"),
			"metadata": map[string]string{"complexity": "high"},
		}, "big"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := setupRoutingE2E(t, false)
			resp := env.chat(t, tc.payload)
			assert.Equal(t, "served-by:"+tc.want, resp.Message.Content)
			for name, a := range env.adapters {
				if name == tc.want {
					assert.Equal(t, 1, a.calls(), name)
				} else {
					assert.Equal(t, 0, a.calls(), name)
				}
			}
		})
	}
}

func TestE2E_ModelRouting_CacheIsolation(t *testing.T) {
	env := setupRoutingE2E(t, true)

	// Identical messages, different routes must never share a cache entry.
	plain := map[string]any{"messages": userMsg("same prompt")}
	high := map[string]any{"messages": userMsg("same prompt"), "metadata": map[string]string{"complexity": "high"}}
	img1 := map[string]any{
		"messages":    userMsg("same prompt"),
		"attachments": []map[string]any{{"type": "image", "mime_type": "image/png", "data": []byte{1, 1, 1}}},
	}
	img2 := map[string]any{
		"messages":    userMsg("same prompt"),
		"attachments": []map[string]any{{"type": "image", "mime_type": "image/png", "data": []byte{2, 2, 2}}},
	}

	assert.Equal(t, "served-by:text", env.chat(t, plain).Message.Content)
	assert.Equal(t, "served-by:big", env.chat(t, high).Message.Content)
	assert.Equal(t, "served-by:vision", env.chat(t, img1).Message.Content)
	assert.Equal(t, "served-by:vision", env.chat(t, img2).Message.Content)
	assert.Equal(t, 2, env.adapters["vision"].calls(), "different attachment digests must not collide in cache")

	// Repeats hit the cache: no new provider calls.
	assert.Equal(t, "served-by:text", env.chat(t, plain).Message.Content)
	assert.Equal(t, "served-by:big", env.chat(t, high).Message.Content)
	assert.Equal(t, "served-by:vision", env.chat(t, img1).Message.Content)
	assert.Equal(t, 1, env.adapters["text"].calls())
	assert.Equal(t, 1, env.adapters["big"].calls())
	assert.Equal(t, 2, env.adapters["vision"].calls())
}

func TestE2E_ModelRouting_ThreadContinuityAcrossModels(t *testing.T) {
	env := setupRoutingE2E(t, false)

	// Turn 1 routes to the vision model.
	r1 := env.chat(t, map[string]any{
		"thread_id":   "th_multi",
		"messages":    userMsg("here is my photo"),
		"attachments": []map[string]any{{"type": "image", "mime_type": "image/png", "data": []byte{9, 9, 9}}},
	})
	assert.Equal(t, "served-by:vision", r1.Message.Content)

	// Turn 2 is text-only and routes to the default text model on the same thread.
	r2 := env.chat(t, map[string]any{
		"thread_id": "th_multi",
		"messages":  userMsg("what did I just send?"),
	})
	assert.Equal(t, "served-by:text", r2.Message.Content)
	assert.Equal(t, "th_multi", r2.ThreadID)

	// The text model must see earlier turns, and must not receive raw media it cannot handle.
	last := env.adapters["text"].lastReq()
	var sawFirst bool
	for _, m := range last.Messages {
		if m.Content == "here is my photo" {
			sawFirst = true
		}
		for _, p := range m.Parts {
			assert.NotEqual(t, core.PartTypeMedia, p.Type, "text-only model must not receive media parts")
		}
	}
	assert.True(t, sawFirst, "history from the vision turn must be preserved")

	// Thread stores both turns.
	req := httptest.NewRequest(http.MethodGet, "/v1/apps/app-route/threads/th_multi", nil)
	req.Header.Set("X-Project-ID", "proj-route")
	req.Header.Set("X-Environment", "production")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var th core.Thread
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &th))
	assert.GreaterOrEqual(t, len(th.Messages), 4)
}
