package tools

import (
	"strings"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

func TestInjectIdentityBindings(t *testing.T) {
	id := &core.Identity{
		Project:     "proj_123",
		Environment: "prod",
		Issuer:      "https://auth.example.com",
		Subject:     "usr_real_alice",
		Claims: map[string]any{
			"tenant_id": "tenant_xyz",
			"role":      "admin",
		},
	}

	bindings := map[string]string{
		"user_id":   "identity.subject",
		"tenant":    "identity.claims.tenant_id",
		"issuer":    "identity.issuer",
		"env":       "identity.environment",
		"auth_role": "role",
	}

	// Model tries to supply its own attacker user_id
	args := map[string]any{
		"user_id": "attacker_supplied_bob",
		"limit":   float64(10),
	}

	injected := InjectIdentityBindings(args, bindings, id)

	// Confused deputy defense: user_id MUST be overwritten by the server!
	if injected["user_id"] != "usr_real_alice" {
		t.Fatalf("expected user_id to be overwritten by usr_real_alice, got %v", injected["user_id"])
	}

	if injected["tenant"] != "tenant_xyz" {
		t.Errorf("expected tenant_id tenant_xyz, got %v", injected["tenant"])
	}

	if injected["issuer"] != "https://auth.example.com" {
		t.Errorf("expected issuer to match, got %v", injected["issuer"])
	}

	if injected["env"] != "prod" {
		t.Errorf("expected env prod, got %v", injected["env"])
	}

	if injected["auth_role"] != "admin" {
		t.Errorf("expected auth_role admin, got %v", injected["auth_role"])
	}

	// Unbound args must remain untouched
	if injected["limit"] != float64(10) {
		t.Errorf("expected limit 10 preserved, got %v", injected["limit"])
	}
}

func TestValidateArguments(t *testing.T) {
	toolDef := &core.ToolDefinition{
		ID:   "calc",
		Name: "calc",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{"type": "string", "enum": []any{"add", "subtract"}},
				"amount":    map[string]any{"type": "number", "minimum": 0},
			},
			"required": []any{"operation", "amount"},
		},
	}

	// 1. Valid args
	validArgs := map[string]any{
		"operation": "add",
		"amount":    42,
	}
	if err := ValidateArguments(validArgs, nil, toolDef); err != nil {
		t.Fatalf("expected valid args to pass validation: %v", err)
	}

	// 2. Invalid args (missing required field)
	invalidArgs1 := map[string]any{
		"operation": "add",
	}
	if err := ValidateArguments(invalidArgs1, nil, toolDef); err == nil {
		t.Fatalf("expected error for missing required field, got nil")
	}

	// 3. Invalid enum value
	invalidArgs2 := map[string]any{
		"operation": "multiply",
		"amount":    10,
	}
	if err := ValidateArguments(invalidArgs2, nil, toolDef); err == nil {
		t.Fatalf("expected error for invalid enum value, got nil")
	}
}

func TestSanitizer(t *testing.T) {
	sanitizer := NewSanitizer()

	raw := `User data: Card: 4111-2222-3333-4444, SSN: 123-45-6789, Email: user@example.com, Secret: TOP_SECRET_DATA.`
	res := core.ToolResult{
		CallID:  "c_1",
		ToolID:  "get_profile",
		Content: raw,
		IsError: false,
	}

	cfg := core.ResultConfig{
		MaxBytes: 1000,
		Redact:   []string{"card_number", "ssn", "TOP_SECRET_[A-Z]+"},
	}

	sanitized := sanitizer.Sanitize(res, cfg)

	if strings.Contains(sanitized.Content, "4111-2222-3333-4444") {
		t.Errorf("credit card was not redacted: %s", sanitized.Content)
	}
	if !strings.Contains(sanitized.Content, "[REDACTED_CARD]") {
		t.Errorf("expected [REDACTED_CARD] in content: %s", sanitized.Content)
	}

	if strings.Contains(sanitized.Content, "123-45-6789") {
		t.Errorf("SSN was not redacted: %s", sanitized.Content)
	}
	if !strings.Contains(sanitized.Content, "[REDACTED_SSN]") {
		t.Errorf("expected [REDACTED_SSN] in content: %s", sanitized.Content)
	}

	if strings.Contains(sanitized.Content, "TOP_SECRET_DATA") {
		t.Errorf("custom regex secret was not redacted: %s", sanitized.Content)
	}

	// Check untrusted delimiters
	if !strings.HasPrefix(sanitized.Content, "<tool_result tool_id=\"get_profile\" call_id=\"c_1\" status=\"success\">") {
		t.Errorf("missing or invalid untrusted prefix delimiter: %s", sanitized.Content)
	}
	if !strings.HasSuffix(strings.TrimSpace(sanitized.Content), "</tool_result>") {
		t.Errorf("missing untrusted suffix delimiter: %s", sanitized.Content)
	}

	// Test truncation
	truncCfg := core.ResultConfig{
		MaxBytes: 20,
	}
	resTrunc := sanitizer.Sanitize(res, truncCfg)
	if !strings.Contains(resTrunc.Content, "...[truncated]") {
		t.Errorf("expected truncation indicator in content: %s", resTrunc.Content)
	}
}
