package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// MockStage verifies Stage interface implementation.
type mockStage struct{}

func (m *mockStage) Handle(_ context.Context, _ *ReqState) error {
	return nil
}

// MockProvider verifies ProviderAdapter interface implementation.
type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{ID: "chat_123"}, nil
}

func (m *mockProvider) Stream(_ context.Context, _ ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Embed(_ context.Context, req EmbedRequest) (*EmbedResponse, error) {
	return &EmbedResponse{Model: req.Model}, nil
}

func (m *mockProvider) Health(_ context.Context) error {
	return nil
}

func TestInterfaceConformance(_ *testing.T) {
	var _ Stage = (*mockStage)(nil)
	var _ ProviderAdapter = (*mockProvider)(nil)
}

func TestIdentity_Keys(t *testing.T) {
	id := Identity{
		Project:     "proj_123",
		Environment: "production",
		Issuer:      "https://auth.example.com",
		Subject:     "user_456",
		Audience:    "my-app",
		Claims: map[string]any{
			"plan": "pro",
			"role": "admin",
		},
	}

	expectedStableKey := "proj_123:https://auth.example.com:user_456"
	if key := id.StableKey(); key != expectedStableKey {
		t.Fatalf("expected StableKey %q, got %q", expectedStableKey, key)
	}

	expectedUserKey := "https://auth.example.com:user_456"
	if key := id.UserKey(); key != expectedUserKey {
		t.Fatalf("expected UserKey %q, got %q", expectedUserKey, key)
	}

	val, ok := id.Claim("plan")
	if !ok || val != "pro" {
		t.Fatalf("expected plan claim 'pro', got %v", val)
	}

	strVal, ok := id.ClaimString("plan")
	if !ok || strVal != "pro" {
		t.Fatalf("expected ClaimString 'pro', got %q", strVal)
	}

	_, ok = id.ClaimBool("plan")
	if ok {
		t.Fatalf("expected ClaimBool for string claim to return false")
	}

	_, ok = id.Claim("nonexistent")
	if ok {
		t.Fatalf("expected nonexistent claim to return false")
	}

	if !id.HasAudience("my-app") {
		t.Fatalf("expected HasAudience('my-app') to be true")
	}
	if id.HasAudience("other-app") {
		t.Fatalf("expected HasAudience('other-app') to be false")
	}

	idMulti := Identity{
		Audience: "app1,app2",
		Claims: map[string]any{
			"aud": []string{"app1", "app2"},
		},
	}
	if !idMulti.HasAudience("app1") || !idMulti.HasAudience("app2") {
		t.Fatalf("expected HasAudience for slice to return true")
	}
}

func TestSnapshot_Accessors(t *testing.T) {
	s := NewSnapshot("proj_1", "dev", 1)

	app := &AIApp{ID: "app_1", Name: "Test App"}
	s.Apps[app.ID] = app

	model := &ModelRouting{ID: "m_1", LogicalModel: "fast"}
	s.Models[model.ID] = model

	tool := &ToolDefinition{ID: "t_1", Name: "search"}
	s.Tools[tool.ID] = tool

	policy := &PolicyDefinition{ID: "p_1", Name: "allow_all"}
	s.Policies[policy.ID] = policy

	prompt := &PromptVersion{ID: "pv_1", PromptID: "p_prompt", Version: 1, Template: "Hello {{name}}"}
	s.Prompts[prompt.ID] = prompt

	if got, ok := s.App("app_1"); !ok || got.Name != "Test App" {
		t.Errorf("App() failed: got %v, ok %v", got, ok)
	}
	if _, ok := s.App("app_unknown"); ok {
		t.Errorf("App() should return false for unknown id")
	}

	if got, ok := s.Model("m_1"); !ok || got.LogicalModel != "fast" {
		t.Errorf("Model() failed: got %v, ok %v", got, ok)
	}

	if got, ok := s.Tool("t_1"); !ok || got.Name != "search" {
		t.Errorf("Tool() failed: got %v, ok %v", got, ok)
	}

	if got, ok := s.Policy("p_1"); !ok || got.Name != "allow_all" {
		t.Errorf("Policy() failed: got %v, ok %v", got, ok)
	}

	if got, ok := s.Prompt("pv_1"); !ok || got.Template != "Hello {{name}}" {
		t.Errorf("Prompt() failed: got %v, ok %v", got, ok)
	}
}

func TestErrorResponse(t *testing.T) {
	err := &ErrorResponse{
		Code:       "rate_limited",
		Message:    "Quota exceeded",
		RetryAfter: 60,
	}
	if err.Error() != "Quota exceeded" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestLimitRule_JSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		max      int64
	}{
		{
			name:     "string 1m",
			input:    `{"resource":"inference","window":"1m","max":10}`,
			expected: time.Minute,
			max:      10,
		},
		{
			name:     "string 1d",
			input:    `{"resource":"tokens","window":"1d","max":50000}`,
			expected: 24 * time.Hour,
			max:      50000,
		},
		{
			name:     "number seconds 60",
			input:    `{"resource":"inference","window":60,"max":100}`,
			expected: 60 * time.Second,
			max:      100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rule LimitRule
			if err := json.Unmarshal([]byte(tc.input), &rule); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if rule.Window != tc.expected {
				t.Fatalf("expected window %v, got %v", tc.expected, rule.Window)
			}
			if rule.Max != tc.max {
				t.Fatalf("expected max %d, got %d", tc.max, rule.Max)
			}
		})
	}
}

func TestAIApp_AuthJSON(t *testing.T) {
	raw := `{
		"id": "app_1",
		"name": "Test App",
		"auth": {
			"provider": "firebase",
			"project_id": "demo-proj"
		}
	}`

	var app AIApp
	if err := json.Unmarshal([]byte(raw), &app); err != nil {
		t.Fatalf("failed to unmarshal AIApp: %v", err)
	}
	if app.AuthConfig == nil || app.AuthConfig.Provider != "firebase" {
		t.Fatalf("expected firebase auth config, got %+v", app.AuthConfig)
	}

	// Test auth_config alternate key
	rawAlt := `{
		"id": "app_2",
		"name": "Alt App",
		"auth_config": {
			"provider": "supabase"
		}
	}`
	var appAlt AIApp
	if err := json.Unmarshal([]byte(rawAlt), &appAlt); err != nil {
		t.Fatalf("failed to unmarshal AIApp: %v", err)
	}
	if appAlt.AuthConfig == nil || appAlt.AuthConfig.Provider != "supabase" {
		t.Fatalf("expected supabase auth config, got %+v", appAlt.AuthConfig)
	}
}

func TestAIApp_ModelRoutesJSON(t *testing.T) {
	raw := `{
		"id": "app_routes",
		"name": "Multimodal App",
		"model_routes": {
			"default": "fast",
			"rules": [
				{
					"when": {
						"input_has": ["image"],
						"output": ["text"],
						"metadata": {"complexity": "high"},
						"expr": "claims.role == 'admin'"
					},
					"use": "vision"
				}
			]
		}
	}`

	var app AIApp
	if err := json.Unmarshal([]byte(raw), &app); err != nil {
		t.Fatalf("failed to unmarshal AIApp with model_routes: %v", err)
	}

	if app.ModelRoutes == nil {
		t.Fatalf("expected ModelRoutes to be non-nil")
	}
	if app.ModelRoutes.Default != "fast" {
		t.Errorf("expected default 'fast', got %q", app.ModelRoutes.Default)
	}
	if len(app.ModelRoutes.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(app.ModelRoutes.Rules))
	}

	rule := app.ModelRoutes.Rules[0]
	if rule.Use != "vision" {
		t.Errorf("expected rule use 'vision', got %q", rule.Use)
	}
	if len(rule.When.InputHas) != 1 || rule.When.InputHas[0] != "image" {
		t.Errorf("expected input_has ['image'], got %v", rule.When.InputHas)
	}
	if len(rule.When.Output) != 1 || rule.When.Output[0] != "text" {
		t.Errorf("expected output ['text'], got %v", rule.When.Output)
	}
	if rule.When.Metadata["complexity"] != "high" {
		t.Errorf("expected metadata complexity 'high', got %v", rule.When.Metadata)
	}
	if rule.When.Expr != "claims.role == 'admin'" {
		t.Errorf("expected expr \"claims.role == 'admin'\", got %q", rule.When.Expr)
	}

	// Test round-trip serialization
	data, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("failed to marshal AIApp: %v", err)
	}
	var roundTrip AIApp
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal round-trip AIApp: %v", err)
	}
	if roundTrip.ModelRoutes == nil || roundTrip.ModelRoutes.Default != "fast" {
		t.Errorf("round-trip model routes mismatch: %+v", roundTrip.ModelRoutes)
	}
	if len(roundTrip.ModelRoutes.Rules) != 1 || roundTrip.ModelRoutes.Rules[0].Use != "vision" {
		t.Errorf("round-trip rules mismatch: %+v", roundTrip.ModelRoutes.Rules)
	}
}

func TestModelRouting_Modalities(t *testing.T) {
	routing := ModelRouting{
		ID:               "m_multimodal",
		LogicalModel:     "omni",
		Deployment:       "gemini-2.5-flash",
		Adapter:          "litellm",
		InputModalities:  []string{"text", "image", "audio"},
		OutputModalities: []string{"text", "audio"},
	}

	data, err := json.Marshal(routing)
	if err != nil {
		t.Fatalf("failed to marshal ModelRouting: %v", err)
	}

	var parsed ModelRouting
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal ModelRouting: %v", err)
	}

	if len(parsed.InputModalities) != 3 || parsed.InputModalities[1] != "image" {
		t.Errorf("expected 3 input modalities, got %v", parsed.InputModalities)
	}
	if len(parsed.OutputModalities) != 2 || parsed.OutputModalities[1] != "audio" {
		t.Errorf("expected 2 output modalities, got %v", parsed.OutputModalities)
	}
}

func TestChatRequest_OutputModalitiesAndAttachmentDigest(t *testing.T) {
	req := ChatRequest{
		Model:            "omni",
		OutputModalities: []string{"text", "image"},
		Attachments: []Attachment{
			{
				Type:     "audio",
				MimeType: "audio/mp3",
				URL:      "https://example.com/sound.mp3",
				Digest:   "sha256:1234567890abcdef",
			},
			{
				Type:     "video",
				MimeType: "video/mp4",
				URL:      "https://example.com/clip.mp4",
				Digest:   "sha256:fedcba0987654321",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal ChatRequest: %v", err)
	}

	var parsed ChatRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal ChatRequest: %v", err)
	}

	if len(parsed.OutputModalities) != 2 || parsed.OutputModalities[1] != "image" {
		t.Errorf("expected output modalities ['text', 'image'], got %v", parsed.OutputModalities)
	}
	if len(parsed.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(parsed.Attachments))
	}
	if parsed.Attachments[0].Type != "audio" || parsed.Attachments[0].Digest != "sha256:1234567890abcdef" {
		t.Errorf("unexpected audio attachment: %+v", parsed.Attachments[0])
	}
	if parsed.Attachments[1].Type != "video" || parsed.Attachments[1].Digest != "sha256:fedcba0987654321" {
		t.Errorf("unexpected video attachment: %+v", parsed.Attachments[1])
	}
}
