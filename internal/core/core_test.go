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
