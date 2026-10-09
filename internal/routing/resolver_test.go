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
		ModelRoutes: &core.ModelRoutes{
			Default: "fast",
			Rules: []core.ModelRouteRule{
				{
					When: core.RouteCondition{InputHas: []string{"image"}},
					Use:  "vision",
				},
				{
					When: core.RouteCondition{Metadata: map[string]string{"complexity": "high"}},
					Use:  "reasoning",
				},
			},
		},
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

func TestModelResolverMultiProviderFallback(t *testing.T) {
	snap := core.NewSnapshot("proj_multi", "prod", 1)
	snap.Models["chat"] = &core.ModelRouting{
		ID:           "mr_chat",
		LogicalModel: "chat",
		Deployment:   "gemini-2.5-flash",
		Adapter:      "litellm",
		FallbackDeployments: []string{
			"openai_compat/gpt-4o-mini",
			"groq:llama-3.3-70b-versatile",
			`{"provider":"together","deployment":"meta-llama/llama-3.3-70b"}`,
			"claude-3-haiku",
		},
	}
	snap.Apps["app_chat"] = &core.AIApp{
		ID:             "app_chat",
		ModelRoutingID: "chat",
	}

	resolver := NewResolver()
	litellmAdapter := &mockAdapter{name: "litellm"}
	openaiAdapter := &mockAdapter{name: "openai_compat"}
	groqAdapter := &mockAdapter{name: "groq"}
	togetherAdapter := &mockAdapter{name: "together"}

	resolver.RegisterAdapter("litellm", litellmAdapter)
	resolver.RegisterAdapter("openai_compat", openaiAdapter)
	resolver.RegisterAdapter("groq", groqAdapter)
	resolver.RegisterAdapter("together", togetherAdapter)

	// Primary route
	route, err := resolver.Resolve(snap, snap.Apps["app_chat"], nil)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if route.Deployment != "gemini-2.5-flash" || route.Adapter != litellmAdapter {
		t.Fatalf("expected primary to be gemini-2.5-flash on litellm, got %s on %v", route.Deployment, route.Adapter)
	}

	attempted := map[string]bool{route.Deployment: true}

	// 1. First fallback is on a DIFFERENT provider (openai_compat)
	fb1, err := resolver.ResolveFallback(route, attempted, snap)
	if err != nil {
		t.Fatalf("fb1 resolve failed: %v", err)
	}
	if fb1.Deployment != "gpt-4o-mini" {
		t.Fatalf("expected gpt-4o-mini, got %s", fb1.Deployment)
	}
	if fb1.Adapter != openaiAdapter {
		t.Fatalf("expected openai_compat adapter for fb1, got %v", fb1.Adapter)
	}
	if fb1.AdapterName != "openai_compat" {
		t.Fatalf("expected adapterName openai_compat, got %s", fb1.AdapterName)
	}
	attempted[fb1.Deployment] = true

	// 2. Second fallback is on yet another provider (groq via colon delimiter)
	fb2, err := resolver.ResolveFallback(route, attempted, snap)
	if err != nil {
		t.Fatalf("fb2 resolve failed: %v", err)
	}
	if fb2.Deployment != "llama-3.3-70b-versatile" {
		t.Fatalf("expected llama-3.3-70b-versatile, got %s", fb2.Deployment)
	}
	if fb2.Adapter != groqAdapter {
		t.Fatalf("expected groq adapter for fb2, got %v", fb2.Adapter)
	}
	if fb2.AdapterName != "groq" {
		t.Fatalf("expected adapterName groq, got %s", fb2.AdapterName)
	}
	attempted[fb2.Deployment] = true

	// 3. Third fallback is via JSON object format (together)
	fb3, err := resolver.ResolveFallback(route, attempted, snap)
	if err != nil {
		t.Fatalf("fb3 resolve failed: %v", err)
	}
	if fb3.Deployment != "meta-llama/llama-3.3-70b" {
		t.Fatalf("expected meta-llama/llama-3.3-70b, got %s", fb3.Deployment)
	}
	if fb3.Adapter != togetherAdapter {
		t.Fatalf("expected together adapter for fb3, got %v", fb3.Adapter)
	}
	attempted[fb3.Deployment] = true

	// 4. Fourth fallback has no prefix, so it falls back to default adapter (litellm)
	fb4, err := resolver.ResolveFallback(route, attempted, snap)
	if err != nil {
		t.Fatalf("fb4 resolve failed: %v", err)
	}
	if fb4.Deployment != "claude-3-haiku" {
		t.Fatalf("expected claude-3-haiku, got %s", fb4.Deployment)
	}
	if fb4.Adapter != litellmAdapter {
		t.Fatalf("expected default litellm adapter for fb4, got %v", fb4.Adapter)
	}
	attempted[fb4.Deployment] = true

	// 5. Exhausted
	_, err = resolver.ResolveFallback(route, attempted, snap)
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

func TestSelectLogicalModelTableDriven(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["text-model"] = &core.ModelRouting{LogicalModel: "text-model", Deployment: "gpt-4o-mini", Adapter: "litellm"}
	snap.Models["image-in-model"] = &core.ModelRouting{LogicalModel: "image-in-model", Deployment: "claude-3-5-sonnet", Adapter: "litellm", InputModalities: []string{"image"}}
	snap.Models["audio-in-model"] = &core.ModelRouting{LogicalModel: "audio-in-model", Deployment: "gemini-flash", Adapter: "litellm", InputModalities: []string{"audio"}}
	snap.Models["video-in-model"] = &core.ModelRouting{LogicalModel: "video-in-model", Deployment: "gemini-pro", Adapter: "litellm", InputModalities: []string{"video"}}
	snap.Models["image-out-model"] = &core.ModelRouting{LogicalModel: "image-out-model", Deployment: "dall-e-3", Adapter: "litellm", OutputModalities: []string{"image"}}
	snap.Models["audio-out-model"] = &core.ModelRouting{LogicalModel: "audio-out-model", Deployment: "tts-1", Adapter: "litellm", OutputModalities: []string{"audio"}}
	snap.Models["pro-model"] = &core.ModelRouting{LogicalModel: "pro-model", Deployment: "o3-mini", Adapter: "litellm"}
	snap.Models["urgent-model"] = &core.ModelRouting{LogicalModel: "urgent-model", Deployment: "gpt-4o", Adapter: "litellm"}

	app := &core.AIApp{
		ID: "app_multi",
		ModelRoutes: &core.ModelRoutes{
			Default: "text-model",
			Rules: []core.ModelRouteRule{
				{
					When: core.RouteCondition{InputHas: []string{"image"}},
					Use:  "image-in-model",
				},
				{
					When: core.RouteCondition{InputHas: []string{"audio"}},
					Use:  "audio-in-model",
				},
				{
					When: core.RouteCondition{InputHas: []string{"video"}},
					Use:  "video-in-model",
				},
				{
					When: core.RouteCondition{Output: []string{"image"}},
					Use:  "image-out-model",
				},
				{
					When: core.RouteCondition{Output: []string{"audio"}},
					Use:  "audio-out-model",
				},
				{
					When: core.RouteCondition{Metadata: map[string]string{"priority": "urgent"}},
					Use:  "urgent-model",
				},
				{
					When: core.RouteCondition{Expr: `meta.tier == "enterprise"`},
					Use:  "pro-model",
				},
			},
		},
	}

	tests := []struct {
		name          string
		app           *core.AIApp
		req           *core.ChatRequest
		id            *core.Identity
		meta          map[string]any
		expectedModel string
		expectErr     bool
	}{
		{
			name:          "default fallback when no rules match",
			app:           app,
			req:           &core.ChatRequest{Messages: []core.ChatMessage{{Role: "user", Content: "hello"}}},
			expectedModel: "text-model",
		},
		{
			name: "input modality image matches rule",
			app:  app,
			req: &core.ChatRequest{
				Attachments: []core.Attachment{{Type: "image", MimeType: "image/png"}},
			},
			expectedModel: "image-in-model",
		},
		{
			name: "input modality audio matches rule",
			app:  app,
			req: &core.ChatRequest{
				Attachments: []core.Attachment{{Type: "audio", MimeType: "audio/mp3"}},
			},
			expectedModel: "audio-in-model",
		},
		{
			name: "input modality video matches rule",
			app:  app,
			req: &core.ChatRequest{
				Attachments: []core.Attachment{{Type: "video", MimeType: "video/mp4"}},
			},
			expectedModel: "video-in-model",
		},
		{
			name: "output modality image matches rule",
			app:  app,
			req: &core.ChatRequest{
				OutputModalities: []string{"image"},
			},
			expectedModel: "image-out-model",
		},
		{
			name: "output modality audio matches rule",
			app:  app,
			req: &core.ChatRequest{
				OutputModalities: []string{"audio"},
			},
			expectedModel: "audio-out-model",
		},
		{
			name: "metadata condition matches",
			app:  app,
			req: &core.ChatRequest{
				Metadata: map[string]string{"priority": "urgent"},
			},
			expectedModel: "urgent-model",
		},
		{
			name: "rule order: image input matches first rule even if priority is urgent",
			app:  app,
			req: &core.ChatRequest{
				Attachments: []core.Attachment{{Type: "image", MimeType: "image/png"}},
				Metadata:    map[string]string{"priority": "urgent"},
			},
			expectedModel: "image-in-model",
		},
		{
			name: "CEL expression matches",
			app:  app,
			req: &core.ChatRequest{
				Metadata: map[string]string{"tier": "enterprise"},
			},
			expectedModel: "pro-model",
		},
		{
			name: "explicit override takes precedence over rules and default",
			app:  app,
			req: &core.ChatRequest{
				Model:            "urgent-model",
				Attachments:      []core.Attachment{{Type: "image"}},
				OutputModalities: []string{"audio"},
			},
			expectedModel: "urgent-model",
		},
		{
			name: "no match when no default and no rules match",
			app: &core.AIApp{
				ID: "app_no_match",
				ModelRoutes: &core.ModelRoutes{
					Rules: []core.ModelRouteRule{
						{
							When: core.RouteCondition{InputHas: []string{"image"}},
							Use:  "image-in-model",
						},
					},
				},
			},
			req:       &core.ChatRequest{Messages: []core.ChatMessage{{Role: "user", Content: "hi"}}},
			expectErr: true,
		},
	}

	resolver := NewResolver()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model, err := resolver.SelectLogicalModel(context.Background(), snap, tc.app, tc.req, tc.id, tc.meta)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got model %s", model)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if model != tc.expectedModel {
				t.Fatalf("expected model %s, got %s", tc.expectedModel, model)
			}
		})
	}
}

func TestModelResolverOverridePolicyGating(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["standard"] = &core.ModelRouting{LogicalModel: "standard", Deployment: "gpt-4o-mini"}
	snap.Models["expensive"] = &core.ModelRouting{LogicalModel: "expensive", Deployment: "o1"}
	snap.Policies["allow_model_override"] = &core.PolicyDefinition{
		ID:         "allow_model_override",
		Expression: `claims.role == "admin"`,
	}

	app := &core.AIApp{
		ID: "app_1",
		ModelRoutes: &core.ModelRoutes{
			Default: "standard",
		},
	}

	resolver := NewResolver()

	// Denied override: non-admin user
	nonAdmin := &core.Identity{Claims: map[string]any{"role": "user"}}
	_, err := resolver.SelectLogicalModel(context.Background(), snap, app, &core.ChatRequest{Model: "expensive"}, nonAdmin, nil)
	if err == nil {
		t.Fatalf("expected override to be denied for non-admin")
	}

	// Allowed override: admin user
	admin := &core.Identity{Claims: map[string]any{"role": "admin"}}
	model, err := resolver.SelectLogicalModel(context.Background(), snap, app, &core.ChatRequest{Model: "expensive"}, admin, nil)
	if err != nil {
		t.Fatalf("expected override allowed for admin, got %v", err)
	}
	if model != "expensive" {
		t.Fatalf("expected expensive, got %s", model)
	}
}

func TestModelResolverCapabilityFilteredFallback(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["multimodal-primary"] = &core.ModelRouting{
		LogicalModel:        "multimodal-primary",
		Deployment:          "primary-dep",
		Adapter:             "litellm",
		InputModalities:     []string{"text", "image"},
		FallbackDeployments: []string{"text-only-fallback", "capable-fallback"},
	}
	snap.Models["text-only-fallback"] = &core.ModelRouting{
		LogicalModel:    "text-only-fallback",
		Deployment:      "text-dep",
		Adapter:         "litellm",
		InputModalities: []string{"text"}, // Lacks image!
	}
	snap.Models["capable-fallback"] = &core.ModelRouting{
		LogicalModel:    "capable-fallback",
		Deployment:      "capable-dep",
		Adapter:         "litellm",
		InputModalities: []string{"text", "image"}, // Supports image!
	}

	resolver := NewResolver()
	litellmAdapter := &mockAdapter{name: "litellm"}
	resolver.RegisterAdapter("litellm", litellmAdapter)

	primaryRoute := &ResolvedRoute{
		ModelRouting: snap.Models["multimodal-primary"],
		Adapter:      litellmAdapter,
		Deployment:   "primary-dep",
		AdapterName:  "litellm",
		Snapshot:     snap,
	}

	attempted := map[string]bool{"primary-dep": true}

	// Fallback should skip text-only-fallback and resolve capable-fallback directly
	fb, err := resolver.ResolveFallback(primaryRoute, attempted, snap)
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if fb.Deployment != "capable-dep" {
		t.Fatalf("expected capable-dep (skipping text-only-fallback), got %s", fb.Deployment)
	}
}

type mockGeneratorAdapter struct {
	mockAdapter
}

func (m *mockGeneratorAdapter) Generate(_ context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	return &core.GenerateResponse{Model: req.Model}, nil
}

func TestResolver_ResolveGenerator(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["image-gen"] = &core.ModelRouting{
		LogicalModel: "image-gen",
		Deployment:   "dall-e-3",
		Adapter:      "litellm",
	}
	snap.Models["text-only"] = &core.ModelRouting{
		LogicalModel: "text-only",
		Deployment:   "gpt-4o",
		Adapter:      "mock",
	}

	resolver := NewResolver()
	genAdapter := &mockGeneratorAdapter{mockAdapter: mockAdapter{name: "litellm"}}
	plainAdapter := &mockAdapter{name: "mock"}
	resolver.RegisterAdapter("litellm", genAdapter)
	resolver.RegisterAdapter("mock", plainAdapter)

	// Resolve image-gen succeeds and implements Generator
	gen, dep, err := resolver.ResolveGenerator(context.Background(), "image-gen", snap)
	if err != nil {
		t.Fatalf("ResolveGenerator failed: %v", err)
	}
	if dep != "dall-e-3" {
		t.Errorf("expected deployment dall-e-3, got %s", dep)
	}
	if gen == nil {
		t.Fatal("expected non-nil Generator")
	}

	// Resolve text-only fails because plainAdapter does not implement Generator
	_, _, err = resolver.ResolveGenerator(context.Background(), "text-only", snap)
	if err == nil {
		t.Fatal("expected error resolving model without Generator adapter")
	}
}

func TestResolver_FallbackModalitiesOutput(t *testing.T) {
	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Models["image-primary"] = &core.ModelRouting{
		LogicalModel:        "image-primary",
		Deployment:          "flux-pro",
		Adapter:             "litellm",
		OutputModalities:    []string{"image"},
		FallbackDeployments: []string{"text-dep", "dall-e-3"},
	}
	snap.Models["text-dep"] = &core.ModelRouting{
		LogicalModel:     "text-dep",
		Deployment:       "text-model",
		Adapter:          "litellm",
		OutputModalities: []string{"text"}, // lacks image!
	}
	snap.Models["dall-e-3"] = &core.ModelRouting{
		LogicalModel:     "dall-e-3",
		Deployment:       "dall-e-3-dep",
		Adapter:          "litellm",
		OutputModalities: []string{"image"}, // supports image!
	}

	resolver := NewResolver()
	litellmAdapter := &mockAdapter{name: "litellm"}
	resolver.RegisterAdapter("litellm", litellmAdapter)

	primaryRoute := &ResolvedRoute{
		ModelRouting: snap.Models["image-primary"],
		Adapter:      litellmAdapter,
		Deployment:   "flux-pro",
		AdapterName:  "litellm",
		Snapshot:     snap,
	}

	attempted := map[string]bool{"flux-pro": true}

	fb, err := resolver.ResolveFallback(primaryRoute, attempted, snap)
	if err != nil {
		t.Fatalf("ResolveFallback failed: %v", err)
	}
	if fb.Deployment != "dall-e-3-dep" {
		t.Errorf("expected fallback to dall-e-3-dep, got %s", fb.Deployment)
	}
}

