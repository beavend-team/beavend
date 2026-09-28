package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestExtractVariables(t *testing.T) {
	tests := []struct {
		name     string
		template string
		expected []string
	}{
		{
			name:     "empty template",
			template: "",
			expected: nil,
		},
		{
			name:     "no variables",
			template: "You are a helpful assistant.",
			expected: nil,
		},
		{
			name:     "single variable",
			template: "Hello {{name}}!",
			expected: []string{"name"},
		},
		{
			name:     "multiple variables with whitespace and duplicates",
			template: "User {{ user_id }} has role {{ user.role }}. Welcome back {{ user_id }}!",
			expected: []string{"user_id", "user.role"},
		},
		{
			name:     "nested dot notation",
			template: "Plan: {{ claims.tier.name }} on {{ project.env }}",
			expected: []string{"claims.tier.name", "project.env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := ExtractVariables(tt.template)
			assert.Equal(t, tt.expected, vars)
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		vars        map[string]any
		opts        RenderOptions
		expected    string
		expectError bool
	}{
		{
			name:     "simple interpolation",
			template: "Hello, {{ name }}! Your score is {{ score }}.",
			vars: map[string]any{
				"name":  "Alice",
				"score": 100,
			},
			expected: "Hello, Alice! Your score is 100.",
		},
		{
			name:     "nested path resolution",
			template: "User: {{ user.name }} ({{ user.email }}), Active: {{ user.active }}",
			vars: map[string]any{
				"user": map[string]any{
					"name":   "Bob",
					"email":  "bob@example.com",
					"active": true,
				},
			},
			expected: "User: Bob (bob@example.com), Active: true",
		},
		{
			name:     "direct flat key precedence",
			template: "Value: {{ user.profile }}",
			vars: map[string]any{
				"user.profile": "DirectValue",
				"user": map[string]any{
					"profile": "NestedValue",
				},
			},
			expected: "Value: DirectValue",
		},
		{
			name:     "missing variable blank by default",
			template: "Welcome {{ name }} from {{ country }}!",
			vars: map[string]any{
				"name": "Charlie",
			},
			expected: "Welcome Charlie from !",
		},
		{
			name:     "missing variable keep placeholder",
			template: "Welcome {{ name }} from {{ country }}!",
			vars: map[string]any{
				"name": "Charlie",
			},
			opts: RenderOptions{
				KeepMissingPlaceholders: true,
			},
			expected: "Welcome Charlie from {{ country }}!",
		},
		{
			name:     "strict missing variable returns error",
			template: "Welcome {{ name }} from {{ country }}!",
			vars: map[string]any{
				"name": "Charlie",
			},
			opts: RenderOptions{
				Strict: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.opts)
			out, err := engine.Render(tt.template, tt.vars)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, out)
			}
		})
	}
}

func TestBuildVariableContext(t *testing.T) {
	snap := &core.Snapshot{
		ProjectID:   "proj_test",
		Environment: "production",
	}

	app := &core.AIApp{
		ID:          "app_support",
		Name:        "Support Assistant",
		Description: "Customer support AI",
	}

	id := &core.Identity{
		Project:     "proj_test",
		Environment: "production",
		Subject:     "usr_12345",
		Issuer:      "https://auth.example.com",
		Claims: map[string]any{
			"email": "user@example.com",
			"name":  "John Doe",
			"role":  "member",
			"plan":  "pro",
		},
	}

	metadata := map[string]any{
		"locale":     "en-US",
		"session_id": "sess_abc",
	}

	ctx := BuildVariableContext(snap, app, id, metadata)

	// Verify snapshot context
	assert.Equal(t, "proj_test", ctx["project_id"])
	assert.Equal(t, "production", ctx["environment"])

	// Verify app context
	assert.Equal(t, "app_support", ctx["app_id"])
	assert.Equal(t, "Support Assistant", ctx["app_name"])

	// Verify identity context
	assert.Equal(t, "usr_12345", ctx["user_id"])
	assert.Equal(t, "usr_12345", ctx["subject"])
	assert.Equal(t, "https://auth.example.com", ctx["issuer"])
	assert.Equal(t, "user@example.com", ctx["email"])
	assert.Equal(t, "John Doe", ctx["name"])
	assert.Equal(t, "member", ctx["role"])
	assert.Equal(t, "pro", ctx["plan"])

	// Verify metadata
	assert.Equal(t, "en-US", ctx["locale"])
	assert.Equal(t, "sess_abc", ctx["session_id"])

	// Render end-to-end with this context
	template := "Assistant {{ app.name }} helping {{ user.name }} ({{ user.id }}) with plan {{ user.claims.plan }} in {{ project.env }}."
	out, err := Render(template, ctx)
	require.NoError(t, err)
	assert.Equal(t, "Assistant Support Assistant helping John Doe (usr_12345) with plan pro in production.", out)
}
