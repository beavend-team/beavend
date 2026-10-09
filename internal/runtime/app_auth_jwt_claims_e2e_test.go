package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/prompts"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/stages"
)

type e2eMockAdapter struct {
	reply string
}

func (m *e2eMockAdapter) Chat(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		ID:        "chat_resp_123",
		RequestID: "req_test",
		AIAppID:   "app-pro",
		CreatedAt: time.Now().Unix(),
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: m.reply,
		},
		FinishReason: "stop",
		Usage: core.UsageStats{
			InputTokens:  10,
			OutputTokens: 10,
			TotalTokens:  20,
		},
	}, nil
}

func (m *e2eMockAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 2)
	ch <- core.StreamEvent{Type: "delta", Delta: m.reply}
	ch <- core.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

func (m *e2eMockAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return &core.EmbedResponse{}, nil
}

func (m *e2eMockAdapter) Health(_ context.Context) error {
	return nil
}

type testSnapshotResolver struct {
	snap *core.Snapshot
}

func (r *testSnapshotResolver) Get(projectID, env string) (*core.Snapshot, bool) {
	return r.snap, true
}

func TestAppAuthAndJWTClaims_E2E(t *testing.T) {
	evaluator, err := policy.NewEvaluator(nil)
	require.NoError(t, err)

	prgPro, err := evaluator.Compiler().Compile(`claims.plan in ['pro', 'team'] && claims.email_verified == true`)
	require.NoError(t, err)

	snap := core.NewSnapshot("proj-e2e", "production", 1)
	snap.CompiledPolicies["pro_required"] = prgPro
	snap.Policies["pro_required"] = &core.PolicyDefinition{
		ID:         "pro_required",
		Name:       "Pro Plan Required",
		Expression: `claims.plan in ['pro', 'team'] && claims.email_verified == true`,
	}

	snap.Prompts["prompt-1"] = &core.PromptVersion{
		ID:        "prompt-1",
		PromptID:  "prompt-1",
		Version:   1,
		Template:  "You are a helpful assistant.",
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	snap.Models["fast"] = &core.ModelRouting{
		ID:           "fast",
		LogicalModel: "fast",
		Deployment:   "gpt-4o-mini",
		Adapter:      "mock",
	}

	// App with CEL policy requiring pro plan in JWT claims
	snap.Apps["pro-app"] = &core.AIApp{
		ID:             "pro-app",
		ProjectID:      "proj-e2e",
		Environment:    "production",
		Name:           "Pro App",
		AccessPolicy:   "pro_required",
		PromptID:       "prompt-1",
		PromptVersion:  1,
		ModelRoutingID: "fast",
		AuthConfig: &core.AuthConfig{
			Provider: "firebase",
		},
	}

	// Public anonymous app
	snap.Apps["anon-app"] = &core.AIApp{
		ID:             "anon-app",
		ProjectID:      "proj-e2e",
		Environment:    "production",
		Name:           "Anon App",
		PromptID:       "prompt-1",
		PromptVersion:  1,
		ModelRoutingID: "fast",
		AuthConfig: &core.AuthConfig{
			Provider: "anonymous",
		},
	}

	resolver := &testSnapshotResolver{snap: snap}
	cacheStore := cache.NewMemoryCacheStore()

	// Assemble Pipeline
	authzStage, err := stages.NewAuthZStage(evaluator)
	require.NoError(t, err)

	promptEngine := prompts.NewEngine()
	promptStage, err := stages.NewPromptStage(promptEngine)
	require.NoError(t, err)

	modelAdapter := &e2eMockAdapter{reply: "Welcome to Beavend Pro!"}
	routingResolver := routing.NewResolver()
	routingResolver.RegisterAdapter("mock", modelAdapter)
	routingStage, err := stages.NewModelRoutingStage(routingResolver)
	require.NoError(t, err)

	execStage, err := stages.NewExecutionStage(routingResolver)
	require.NoError(t, err)

	pipeline := runtime.NewPipeline(
		authzStage,
		promptStage,
		routingStage,
		execStage,
	)

	handler := runtime.NewHandler(resolver, pipeline, runtime.HandlerOptions{
		DefaultEnvironment: "production",
		CacheStore:         cacheStore,
	})

	t.Run("pro user with valid JWT claims succeeds", func(t *testing.T) {
		chatPayload := map[string]any{
			"messages": []map[string]string{
				{"role": "user", "content": "Hello!"},
			},
		}
		bodyBytes, _ := json.Marshal(chatPayload)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/pro-app/chat", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Project-ID", "proj-e2e")
		req.Header.Set("X-Environment", "production")

		aliceID := &core.Identity{
			Project:     "proj-e2e",
			Environment: "production",
			Subject:     "alice",
			Claims: map[string]any{
				"sub":            "alice",
				"plan":           "pro",
				"email_verified": true,
			},
		}
		ctx := core.ContextWithIdentity(req.Context(), aliceID)
		ctx = core.ContextWithSnapshot(ctx, snap)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.HandleChat(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp core.ChatResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Welcome to Beavend Pro!", resp.Message.Content)
	})

	t.Run("free user denied by CEL policy based on claims", func(t *testing.T) {
		chatPayload := map[string]any{
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
		}
		bodyBytes, _ := json.Marshal(chatPayload)

		req := httptest.NewRequest(http.MethodPost, "/v1/apps/pro-app/chat", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Project-ID", "proj-e2e")
		req.Header.Set("X-Environment", "production")

		bobID := &core.Identity{
			Project:     "proj-e2e",
			Environment: "production",
			Subject:     "bob_free",
			Claims: map[string]any{
				"sub":            "bob_free",
				"plan":           "free",
				"email_verified": true,
			},
		}
		ctx := core.ContextWithIdentity(req.Context(), bobID)
		ctx = core.ContextWithSnapshot(ctx, snap)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.HandleChat(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
