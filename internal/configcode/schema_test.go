package configcode

import (
	"os"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchema_YAMLRoundTrip(t *testing.T) {
	doc := &ProjectDocument{
		BeavendVersion: CurrentVersion,
		Project: ProjectMeta{
			ID:             "proj_ecommerce",
			Name:           "E-Commerce Store",
			Description:    "Production AI Store Assistant",
			AllowedOrigins: []string{"https://store.example.com"},
		},
		Environment: "production",
		Auth: &core.AuthConfig{
			Provider: "firebase",
			Issuer:   "https://securetoken.google.com/my-project",
			Audience: []string{"my-project"},
		},
		Apps: []AppSpec{
			{
				ID:             "support_agent",
				Name:           "Support Agent",
				AccessPolicy:   "authenticated",
				PromptID:       "support_prompt",
				ModelRoutingID: "fast",
				ToolIDs:        []string{"check_order"},
				Limits: []core.LimitRule{
					{
						Resource: "inference",
						Window:   60 * time.Second,
						Max:      10,
					},
				},
				Cache: &core.CacheConfig{
					Enabled: true,
					TTL:     300,
					Scope:   "user",
				},
			},
		},
		Prompts: []PromptSpec{
			{
				ID:       "support_prompt",
				Name:     "Support Prompt",
				Template: "You are a helpful customer support agent for {{.user_name}}.",
			},
		},
		Models: []ModelSpec{
			{
				ID:           "fast",
				LogicalModel: "fast",
				Deployment:   "gpt-4o-mini",
				Adapter:      "litellm",
				ProviderConfig: &ProviderSpec{
					ProviderName: "litellm",
					BaseURL:      "http://localhost:4000",
					APIKey:       "${LITELLM_API_KEY}",
				},
			},
		},
		Tools: []ToolSpec{
			{
				ID:   "check_order",
				Name: "Check Order",
				Executor: core.ExecutorConfig{
					Type:   "http",
					Method: "GET",
					URL:    "https://api.example.com/orders/{order_id}",
				},
				Confirmation: "none",
			},
		},
		Policies: []PolicySpec{
			{
				ID:         "authenticated",
				Name:       "Authenticated Users",
				Expression: "claims.email_verified == true",
			},
		},
	}

	yamlBytes, err := doc.ToYAML()
	require.NoError(t, err)
	assert.Contains(t, string(yamlBytes), "beavend_version: \"0.1\"")
	assert.Contains(t, string(yamlBytes), "proj_ecommerce")
	assert.Contains(t, string(yamlBytes), "${LITELLM_API_KEY}")

	parsed, err := FromYAML(yamlBytes)
	require.NoError(t, err)
	assert.Equal(t, doc.BeavendVersion, parsed.BeavendVersion)
	assert.Equal(t, doc.Project.ID, parsed.Project.ID)
	assert.Equal(t, doc.Environment, parsed.Environment)
	require.Len(t, parsed.Apps, 1)
	assert.Equal(t, "support_agent", parsed.Apps[0].ID)
	require.Len(t, parsed.Prompts, 1)
	assert.Equal(t, "support_prompt", parsed.Prompts[0].ID)
	require.Len(t, parsed.Models, 1)
	assert.Equal(t, "fast", parsed.Models[0].ID)
	require.Len(t, parsed.Tools, 1)
	assert.Equal(t, "check_order", parsed.Tools[0].ID)
	require.Len(t, parsed.Policies, 1)
	assert.Equal(t, "authenticated", parsed.Policies[0].ID)
}

func TestSchema_Validation(t *testing.T) {
	tests := []struct {
		name    string
		doc     *ProjectDocument
		wantErr string
	}{
		{
			name: "missing version",
			doc: &ProjectDocument{
				Project:     ProjectMeta{ID: "p1"},
				Environment: "dev",
			},
			wantErr: "missing beavend_version",
		},
		{
			name: "wrong version",
			doc: &ProjectDocument{
				BeavendVersion: "99.0",
				Project:        ProjectMeta{ID: "p1"},
				Environment:    "dev",
			},
			wantErr: "unsupported beavend_version",
		},
		{
			name: "missing project id",
			doc: &ProjectDocument{
				BeavendVersion: CurrentVersion,
				Environment:    "dev",
			},
			wantErr: "project.id is required",
		},
		{
			name: "missing environment",
			doc: &ProjectDocument{
				BeavendVersion: CurrentVersion,
				Project:        ProjectMeta{ID: "p1"},
			},
			wantErr: "environment is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.doc.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSchema_SecretResolution(t *testing.T) {
	os.Setenv("TEST_KEY", "secret_value_123")
	defer os.Unsetenv("TEST_KEY")

	assert.True(t, IsEnvVarRef("${TEST_KEY}"))
	assert.True(t, IsEnvVarRef("env:TEST_KEY"))
	assert.False(t, IsEnvVarRef("plain_password"))

	assert.Equal(t, "secret_value_123", ResolveSecret("${TEST_KEY}"))
	assert.Equal(t, "secret_value_123", ResolveSecret("env:TEST_KEY"))
	assert.Equal(t, "raw_secret", ResolveSecret("raw_secret"))
	assert.Equal(t, "[SECRET_MASKED]", MaskSecret("raw_secret", ""))
	assert.Equal(t, "${TEST_KEY}", MaskSecret("raw_secret", "TEST_KEY"))
}
