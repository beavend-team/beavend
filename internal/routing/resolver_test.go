package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

type mockAdapter struct {
	name string
}

func (m *mockAdapter) Chat(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{}, nil
}

func (m *mockAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *mockAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return &core.EmbedResponse{}, nil
}

func (m *mockAdapter) Health(_ context.Context) error {
	return nil
}

func setupTestSnapshot() *core.Snapshot {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["fast"] = &core.ModelRouting{
		ID:                  "mr_fast",
		LogicalModel:        "fast",
		Deployment:          "gemini-2.5-flash",
		Adapter:             "litellm",
		FallbackDeployments: []string{"gpt-4o-mini", "claude-3-haiku"},
	}
	snap.Models["reasoning"] = &core.ModelRouting{
		ID:           "mr_reasoning",
		LogicalModel: "reasoning",
		Deployment:   "claude-3-7-sonnet",
		Adapter:      "litellm",
	}
	snap.Models["vision"] = &core.ModelRouting{
		ID:           "mr_vision",
		LogicalModel: "vision",
		Deployment:   "gpt-4o",
		Adapter:      "openai_compat",
	}
	snap.Apps["app_support"] = &core.AIApp{
		ID:             "app_support",
		ModelRoutingID: "fast",
	}
	return snap
}

func TestModelResolverResolution(t *testing.T) {
	snap := setupTestSnapshot()
	resolver := NewResolver()
	litellmAdapter := &mockAdapter{name: "litellm"}
	openaiAdapter := &mockAdapter{name: "openai_compat"}

	resolver.RegisterAdapter("litellm", litellmAdapter)
	resolver.RegisterAdapter("openai_compat", openaiAdapter)

	// 1. Resolve from app configuration (default "fast")
	route, err := resolver.Resolve(snap, snap.Apps["app_support"], nil)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if route.Deployment != "gemini-2.5-flash" {
		t.Fatalf("expected deployment gemini-2.5-flash, got %s", route.Deployment)
	}
	if route.Adapter != litellmAdapter {
		t.Fatalf("expected litellm adapter")
	}

	// 2. Request model override to "reasoning"
	reqOverride := &core.ChatRequest{Model: "reasoning"}
	route, err = resolver.Resolve(snap, snap.Apps["app_support"], reqOverride)
	if err != nil {
		t.Fatalf("resolve override failed: %v", err)
	}
	if route.Deployment != "claude-3-7-sonnet" {
		t.Fatalf("expected claude-3-7-sonnet, got %s", route.Deployment)
	}

	// 3. Modality image attachment routes to "vision"
	reqImage := &core.ChatRequest{
		Attachments: []core.Attachment{
			{Type: "image", MimeType: "image/png"},
		},
	}
	route, err = resolver.Resolve(snap, snap.Apps["app_support"], reqImage)
	if err != nil {
		t.Fatalf("resolve image failed: %v", err)
	}
	if route.Deployment != "gpt-4o" {
		t.Fatalf("expected gpt-4o for vision, got %s", route.Deployment)
	}
	if route.Adapter != openaiAdapter {
		t.Fatalf("expected openai_compat adapter for vision")
	}

	// 4. Complexity high metadata routes to "reasoning"
	reqComplexity := &core.ChatRequest{
		Metadata: map[string]string{"complexity": "high"},
	}
	route, err = resolver.Resolve(snap, snap.Apps["app_support"], reqComplexity)
	if err != nil {
		t.Fatalf("resolve complexity failed: %v", err)
	}
	if route.Deployment != "claude-3-7-sonnet" {
		t.Fatalf("expected claude-3-7-sonnet, got %s", route.Deployment)
	}
}

func TestModelResolverFallback(t *testing.T) {
	snap := setupTestSnapshot()
	resolver := NewResolver()
	litellmAdapter := &mockAdapter{name: "litellm"}
	resolver.RegisterAdapter("litellm", litellmAdapter)

	route, err := resolver.Resolve(snap, snap.Apps["app_support"], nil)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	attempted := map[string]bool{
		route.Deployment: true, // gemini-2.5-flash failed
	}

	// First fallback
	fb1, err := resolver.ResolveFallback(route, attempted)
	if err != nil {
		t.Fatalf("fallback 1 failed: %v", err)
	}
	if fb1.Deployment != "gpt-4o-mini" {
		t.Fatalf("expected gpt-4o-mini, got %s", fb1.Deployment)
	}
	attempted[fb1.Deployment] = true

	// Second fallback
	fb2, err := resolver.ResolveFallback(route, attempted)
	if err != nil {
		t.Fatalf("fallback 2 failed: %v", err)
	}
	if fb2.Deployment != "claude-3-haiku" {
		t.Fatalf("expected claude-3-haiku, got %s", fb2.Deployment)
	}
	attempted[fb2.Deployment] = true

	// Third fallback - exhausted
	_, err = resolver.ResolveFallback(route, attempted)
	if !errors.Is(err, ErrNoAvailableDeployments) {
		t.Fatalf("expected ErrNoAvailableDeployments, got %v", err)
	}
}

func TestModelResolverErrors(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	resolver := NewResolver()

	// Missing model in snapshot
	_, err := resolver.Resolve(snap, &core.AIApp{ID: "app_1", ModelRoutingID: "nonexistent"}, nil)
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}

	// Model exists but adapter not registered
	snap.Models["test"] = &core.ModelRouting{LogicalModel: "test", Adapter: "custom_unregistered"}
	_, err = resolver.Resolve(snap, &core.AIApp{ID: "app_1", ModelRoutingID: "test"}, nil)
	if !errors.Is(err, ErrAdapterNotFound) {
		t.Fatalf("expected ErrAdapterNotFound, got %v", err)
	}

	// Explicit invalid model in ChatRequest
	_, err = resolver.Resolve(snap, &core.AIApp{ID: "app_1", ModelRoutingID: "test"}, &core.ChatRequest{Model: "invalid_model_override"})
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for invalid request model, got %v", err)
	}
}

func TestModelResolverCaseInsensitiveImageMime(t *testing.T) {
	snap := setupTestSnapshot()
	resolver := NewResolver()
	resolver.RegisterAdapter("openai_compat", &mockAdapter{name: "openai_compat"})

	req := &core.ChatRequest{
		Attachments: []core.Attachment{
			{MimeType: "IMAGE/JPEG"},
		},
	}
	route, err := resolver.Resolve(snap, snap.Apps["app_support"], req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if route.Deployment != "gpt-4o" {
		t.Fatalf("expected gpt-4o for vision route, got %s", route.Deployment)
	}
}
