package stages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
)

func TestCacheStage_Disabled(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	snap.Apps["app1"] = &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: false},
	}

	state := &core.ReqState{
		RequestID:   "req_1",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Hello"}},
		},
		Metadata: make(map[string]any),
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)
	assert.Nil(t, state.ChatResponse)
	assert.Equal(t, false, state.Metadata["cache_hit"])
}

func TestCacheStage_Miss(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	snap.Apps["app1"] = &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "user", TTL: 300},
	}

	state := &core.ReqState{
		RequestID:   "req_1",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		Identity: &core.Identity{
			Subject: "user_alice",
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "What is 2+2?"}},
		},
		Metadata: make(map[string]any),
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)
	assert.Nil(t, state.ChatResponse)
	assert.Equal(t, false, state.Metadata["cache_hit"])
	assert.NotEmpty(t, state.Metadata["cache_key"])
}

func TestCacheStage_HitUnary(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	app := &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "user", TTL: 300},
	}
	snap.Apps["app1"] = app

	req := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "What is 2+2?"}},
	}

	// Pre-populate cache
	hash, err := cache.ComputeRequestHash(req, app)
	require.NoError(t, err)

	key := cache.ResponseCacheKey("proj1", "prod", "user", "user_alice", hash)
	cachedResp := core.ChatResponse{
		ID:        "chat_old",
		RequestID: "req_old",
		AIAppID:   "app1",
		CreatedAt: 123456,
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: "2+2 equals 4.",
		},
		FinishReason: "stop",
		Usage: core.UsageStats{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}
	b, err := json.Marshal(cachedResp)
	require.NoError(t, err)

	err = store.Set(ctx, key, b, time.Minute)
	require.NoError(t, err)

	state := &core.ReqState{
		RequestID:   "req_new_2",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		Identity: &core.Identity{
			Subject: "user_alice",
		},
		ChatRequest: req,
		Metadata:    make(map[string]any),
	}

	err = stage.Handle(ctx, state)
	require.NoError(t, err)

	require.NotNil(t, state.ChatResponse)
	assert.Equal(t, true, state.Metadata["cache_hit"])
	assert.Equal(t, "chatcmpl_cached_req_new_2", state.ChatResponse.ID)
	assert.Equal(t, "req_new_2", state.ChatResponse.RequestID)
	assert.Equal(t, "2+2 equals 4.", state.ChatResponse.Message.Content)
	assert.Equal(t, 15, state.Usage.TotalTokens)
	require.NotNil(t, state.Usage.CostUSD)
	assert.Equal(t, float64(0), *state.Usage.CostUSD)
}

func TestCacheStage_HitStreamingReplay(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	app := &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "shared", TTL: 300},
	}
	snap.Apps["app1"] = app

	req := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Explain gravity in one sentence."}},
	}

	hash, err := cache.ComputeRequestHash(req, app)
	require.NoError(t, err)

	key := cache.ResponseCacheKey("proj1", "prod", "shared", "", hash)
	cachedResp := core.ChatResponse{
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: "Gravity is the fundamental force of attraction between masses.",
		},
		FinishReason: "stop",
		Usage: core.UsageStats{
			TotalTokens: 20,
		},
	}
	b, _ := json.Marshal(cachedResp)
	_ = store.Set(ctx, key, b, time.Minute)

	state := &core.ReqState{
		RequestID:   "req_stream_1",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		IsStreaming: true,
		ChatRequest: req,
		Metadata:    make(map[string]any),
	}

	err = stage.Handle(ctx, state)
	require.NoError(t, err)
	require.NotNil(t, state.ChatResponse)
	assert.Equal(t, true, state.Metadata["cache_hit"])

	// Collect stream events
	require.NotNil(t, state.StreamChan)
	var events []core.StreamEvent
	for ev := range state.StreamChan {
		events = append(events, ev)
	}

	require.Len(t, events, 3, "expected delta, usage, done events")
	assert.Equal(t, "delta", events[0].Type)
	assert.Equal(t, "Gravity is the fundamental force of attraction between masses.", events[0].Delta)
	assert.Equal(t, "usage", events[1].Type)
	assert.Equal(t, "done", events[2].Type)
}

func TestCacheStage_ScopeIsolation(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	app := &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "user", TTL: 300},
	}
	snap.Apps["app1"] = app

	req := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Secret query"}},
	}

	hash, _ := cache.ComputeRequestHash(req, app)
	// Populate cache for Alice
	keyAlice := cache.ResponseCacheKey("proj1", "prod", "user", "alice", hash)
	_ = store.Set(ctx, keyAlice, []byte(`{"message":{"role":"assistant","content":"Alice secret"}}`), time.Minute)

	// Bob sends same request
	stateBob := &core.ReqState{
		RequestID:   "req_bob",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		Identity:    &core.Identity{Subject: "bob"},
		ChatRequest: req,
		Metadata:    make(map[string]any),
	}

	err := stage.Handle(ctx, stateBob)
	require.NoError(t, err)
	assert.Nil(t, stateBob.ChatResponse, "Bob must NOT see Alice's cached response")
	assert.Equal(t, false, stateBob.Metadata["cache_hit"])
}

func TestCacheStage_CrossRouteIsolation(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	app := &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "shared", TTL: 300},
	}
	snap.Apps["app1"] = app

	req := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Calculate distance"}},
	}

	// Response cached under route "fast-model"
	hashFast, err := cache.ComputeRequestHash(req, app, "fast-model")
	require.NoError(t, err)
	keyFast := cache.ResponseCacheKey("proj1", "prod", "shared", "", hashFast)
	_ = store.Set(ctx, keyFast, []byte(`{"message":{"role":"assistant","content":"Fast route answer"}}`), time.Minute)

	// Incoming request is routed to "reasoning-model"
	stateReasoning := &core.ReqState{
		RequestID:     "req_reasoning",
		ProjectID:     "proj1",
		Environment:   "prod",
		AIAppID:       "app1",
		Snapshot:      snap,
		SelectedModel: "reasoning-model",
		ChatRequest:   req,
		Metadata:      make(map[string]any),
	}

	err = stage.Handle(ctx, stateReasoning)
	require.NoError(t, err)
	assert.Nil(t, stateReasoning.ChatResponse, "Cross-route cache hit must NOT occur")
	assert.Equal(t, false, stateReasoning.Metadata["cache_hit"])

	// Incoming request routed to "fast-model" gets a cache hit
	stateFast := &core.ReqState{
		RequestID:     "req_fast",
		ProjectID:     "proj1",
		Environment:   "prod",
		AIAppID:       "app1",
		Snapshot:      snap,
		SelectedModel: "fast-model",
		ChatRequest:   req,
		Metadata:      make(map[string]any),
	}

	err = stage.Handle(ctx, stateFast)
	require.NoError(t, err)
	require.NotNil(t, stateFast.ChatResponse)
	assert.Equal(t, true, stateFast.Metadata["cache_hit"])
	assert.Equal(t, "Fast route answer", stateFast.ChatResponse.Message.Content)
}

func TestCacheStage_ModalityBypass(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryCacheStore()
	stage := NewCacheStage(store)

	snap := core.NewSnapshot("proj1", "prod", 1)
	// App does not enable image modality caching
	app := &core.AIApp{
		ID:    "app1",
		Cache: core.CacheConfig{Enabled: true, Scope: "shared", TTL: 300}, // Modalities empty
	}
	snap.Apps["app1"] = app

	reqImage := &core.ChatRequest{
		Messages:         []core.ChatMessage{{Role: "user", Content: "Draw a cat"}},
		OutputModalities: []string{"image"},
	}

	state := &core.ReqState{
		RequestID:   "req_img",
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		Snapshot:    snap,
		ChatRequest: reqImage,
		Metadata:    make(map[string]any),
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)
	assert.Nil(t, state.ChatResponse)
	assert.Equal(t, false, state.Metadata["cache_hit"])
}
