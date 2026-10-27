package configcode

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"gopkg.in/yaml.v3"
)

const (
	// CurrentVersion is the supported Beavend configuration schema version.
	CurrentVersion = "0.1"

	// MaskedSecretPlaceholder is the placeholder used when exporting sensitive values.
	MaskedSecretPlaceholder = "[SECRET_MASKED]"
)

var envVarPattern = regexp.MustCompile(`^\$\{[A-Za-z0-9_]+\}$|^env:[A-Za-z0-9_]+$`)

// ProjectDocument represents the complete declarative configuration of a Beavend project
// within a specific environment.
type ProjectDocument struct {
	BeavendVersion string             `json:"beavend_version" yaml:"beavend_version"`
	Project        ProjectMeta        `json:"project" yaml:"project"`
	Environment    string             `json:"environment" yaml:"environment"`
	Auth           *core.AuthConfig   `json:"auth,omitempty" yaml:"auth,omitempty"`
	Apps           []AppSpec          `json:"apps,omitempty" yaml:"apps,omitempty"`
	Prompts        []PromptSpec       `json:"prompts,omitempty" yaml:"prompts,omitempty"`
	Skills         []SkillSpec        `json:"skills,omitempty" yaml:"skills,omitempty"`
	Models         []ModelSpec        `json:"models,omitempty" yaml:"models,omitempty"`
	Tools          []ToolSpec         `json:"tools,omitempty" yaml:"tools,omitempty"`
	Policies       []PolicySpec       `json:"policies,omitempty" yaml:"policies,omitempty"`
	APIKeys        []AppAPIKeySpec    `json:"api_keys,omitempty" yaml:"api_keys,omitempty"`
	Knowledge      []KnowledgeDocSpec `json:"knowledge,omitempty" yaml:"knowledge,omitempty"`
}

// ProjectMeta holds top-level project metadata.
type ProjectMeta struct {
	ID             string   `json:"id" yaml:"id"`
	Name           string   `json:"name" yaml:"name"`
	Description    string   `json:"description,omitempty" yaml:"description,omitempty"`
	AllowedOrigins []string `json:"allowed_origins,omitempty" yaml:"allowed_origins,omitempty"`
	AllowedBundles []string `json:"allowed_bundles,omitempty" yaml:"allowed_bundles,omitempty"`
}

// AppSpec specifies an AI App configuration.
type AppSpec struct {
	ID             string                `json:"id" yaml:"id"`
	Name           string                `json:"name" yaml:"name"`
	Description    string                `json:"description,omitempty" yaml:"description,omitempty"`
	AccessPolicy   string                `json:"access_policy,omitempty" yaml:"access_policy,omitempty"`
	PromptID       string                `json:"prompt_id,omitempty" yaml:"prompt_id,omitempty"`
	PromptVersion  int                   `json:"prompt_version,omitempty" yaml:"prompt_version,omitempty"`
	ModelRoutingID string                `json:"model_routing_id,omitempty" yaml:"model_routing_id,omitempty"`
	ToolIDs        []string              `json:"tool_ids,omitempty" yaml:"tool_ids,omitempty"`
	SkillIDs       []string              `json:"skill_ids,omitempty" yaml:"skill_ids,omitempty"`
	Limits         []core.LimitRule      `json:"limits,omitempty" yaml:"limits,omitempty"`
	Cache          *core.CacheConfig     `json:"cache,omitempty" yaml:"cache,omitempty"`
	Guardrails     *core.GuardrailConfig `json:"guardrails,omitempty" yaml:"guardrails,omitempty"`
	Memory             *core.MemoryConfig             `json:"memory,omitempty" yaml:"memory,omitempty"`
	Search             *core.SearchConfig             `json:"search,omitempty" yaml:"search,omitempty"`
	ResponseSchema     string                         `json:"response_schema,omitempty" yaml:"response_schema,omitempty"`
	Auth               *core.AuthConfig               `json:"auth,omitempty" yaml:"auth,omitempty"`
	ModelRoutes        *core.ModelRoutes              `json:"model_routes,omitempty" yaml:"model_routes,omitempty"`
	APIKeys            []AppAPIKeySpec                `json:"api_keys,omitempty" yaml:"api_keys,omitempty"`
}

// AppAPIKeySpec specifies an API key for app access with claims delegation rules.
type AppAPIKeySpec struct {
	ID               string   `json:"id" yaml:"id"`
	AppID            string   `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	Name             string   `json:"name" yaml:"name"`
	KeyPrefix        string   `json:"key_prefix,omitempty" yaml:"key_prefix,omitempty"`
	MayAssertSubject bool     `json:"may_assert_subject,omitempty" yaml:"may_assert_subject,omitempty"`
	AllowedClaims    []string `json:"allowed_claims,omitempty" yaml:"allowed_claims,omitempty"`
}

// KnowledgeDocSpec specifies knowledge base document metadata.
type KnowledgeDocSpec struct {
	ID            string         `json:"id" yaml:"id"`
	AppID         string         `json:"app_id" yaml:"app_id"`
	Title         string         `json:"title" yaml:"title"`
	Description   string         `json:"description,omitempty" yaml:"description,omitempty"`
	SourceURL     string         `json:"source_url,omitempty" yaml:"source_url,omitempty"`
	Tags          []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
	Revision      int            `json:"revision,omitempty" yaml:"revision,omitempty"`
	Source        string         `json:"source,omitempty" yaml:"source,omitempty"`
	TotalPassages int            `json:"total_passages,omitempty" yaml:"total_passages,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// PromptSpec specifies a versioned prompt.
type PromptSpec struct {
	ID          string                   `json:"id" yaml:"id"`
	Name        string                   `json:"name" yaml:"name"`
	Description string                   `json:"description,omitempty" yaml:"description,omitempty"`
	Template    string                   `json:"template" yaml:"template"`
	Variables   []string                 `json:"variables,omitempty" yaml:"variables,omitempty"`
	Fragments   []core.PromptFragmentRef `json:"fragments,omitempty" yaml:"fragments,omitempty"`
	Version     int                      `json:"version,omitempty" yaml:"version,omitempty"`
}

// SkillSpec specifies a versioned agent skill.
type SkillSpec struct {
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name" yaml:"name"`
	Description   string   `json:"description,omitempty" yaml:"description,omitempty"`
	Source        string   `json:"source,omitempty" yaml:"source,omitempty"`
	InjectionMode string   `json:"injection_mode,omitempty" yaml:"injection_mode,omitempty"`
	Content       string   `json:"content" yaml:"content"`
	Variables     []string `json:"variables,omitempty" yaml:"variables,omitempty"`
	Version       int      `json:"version,omitempty" yaml:"version,omitempty"`
	Status        string   `json:"status,omitempty" yaml:"status,omitempty"`
	ChangeNote    string   `json:"change_note,omitempty" yaml:"change_note,omitempty"`
}

// ModelSpec specifies a model routing and its underlying provider configuration.
type ModelSpec struct {
	ID                  string        `json:"id" yaml:"id"`
	LogicalModel        string        `json:"logical_model" yaml:"logical_model"`
	Deployment          string        `json:"deployment" yaml:"deployment"`
	Adapter             string        `json:"adapter" yaml:"adapter"`
	FallbackDeployments []string      `json:"fallback_deployments,omitempty" yaml:"fallback_deployments,omitempty"`
	InputCostPer1M      *float64      `json:"input_cost_per_1m,omitempty" yaml:"input_cost_per_1m,omitempty"`
	CachedInputCostPer1M *float64     `json:"cached_input_cost_per_1m,omitempty" yaml:"cached_input_cost_per_1m,omitempty"`
	OutputCostPer1M     *float64      `json:"output_cost_per_1m,omitempty" yaml:"output_cost_per_1m,omitempty"`
	InputModalities     []string      `json:"input_modalities,omitempty" yaml:"input_modalities,omitempty"`
	OutputModalities    []string      `json:"output_modalities,omitempty" yaml:"output_modalities,omitempty"`
	Capabilities        []string      `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ProviderConfig      *ProviderSpec `json:"provider,omitempty" yaml:"provider,omitempty"`
}

// ProviderSpec specifies external LLM provider configuration with secret references.
type ProviderSpec struct {
	ProviderName string `json:"provider_name" yaml:"provider_name"`
	BaseURL      string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	APIKey       string `json:"api_key,omitempty" yaml:"api_key,omitempty"` // Can be "${VAR}", "env:VAR", or MaskedSecretPlaceholder
}

// ToolSpec specifies a callable tool.
type ToolSpec struct {
	ID               string                     `json:"id" yaml:"id"`
	Name             string                     `json:"name" yaml:"name"`
	Description      string                     `json:"description,omitempty" yaml:"description,omitempty"`
	Version          int                        `json:"version,omitempty" yaml:"version,omitempty"`
	SideEffect       string                     `json:"side_effect,omitempty" yaml:"side_effect,omitempty"`
	ParallelSafe     bool                       `json:"parallel_safe,omitempty" yaml:"parallel_safe,omitempty"`
	InputSchema      map[string]any             `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	IdentityBindings map[string]string          `json:"identity_bindings,omitempty" yaml:"identity_bindings,omitempty"`
	Executor         core.ExecutorConfig        `json:"executor" yaml:"executor"`
	DownstreamAuth   *core.DownstreamAuthConfig `json:"downstream_auth,omitempty" yaml:"downstream_auth,omitempty"`
	Confirmation     string                     `json:"confirmation,omitempty" yaml:"confirmation,omitempty"`
	Authorization    string                     `json:"authorization,omitempty" yaml:"authorization,omitempty"`
	Result           *core.ResultConfig         `json:"result,omitempty" yaml:"result,omitempty"`
	Cache            *core.CacheConfig          `json:"cache,omitempty" yaml:"cache,omitempty"`
}

// PolicySpec specifies a CEL authorization policy.
type PolicySpec struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Expression  string `json:"expression" yaml:"expression"`
}

// Validate verifies the structural and version integrity of the document.
func (doc *ProjectDocument) Validate() error {
	if doc.BeavendVersion == "" {
		return fmt.Errorf("missing beavend_version (expected %q)", CurrentVersion)
	}
	if doc.BeavendVersion != CurrentVersion {
		return fmt.Errorf("unsupported beavend_version %q (supported: %q)", doc.BeavendVersion, CurrentVersion)
	}
	if strings.TrimSpace(doc.Project.ID) == "" {
		return fmt.Errorf("project.id is required")
	}
	if strings.TrimSpace(doc.Environment) == "" {
		return fmt.Errorf("environment is required")
	}
	for _, sk := range doc.Skills {
		if strings.TrimSpace(sk.ID) == "" {
			return fmt.Errorf("skill.id is required")
		}
		if strings.TrimSpace(sk.Name) == "" {
			return fmt.Errorf("skill %s: name is required", sk.ID)
		}
		if strings.TrimSpace(sk.Content) == "" {
			return fmt.Errorf("skill %s: content is required", sk.ID)
		}
		if sk.InjectionMode != "" && sk.InjectionMode != core.SkillInjectionAlways && sk.InjectionMode != core.SkillInjectionOnDemand {
			return fmt.Errorf("skill %s: invalid injection_mode %q (must be %q or %q)", sk.ID, sk.InjectionMode, core.SkillInjectionAlways, core.SkillInjectionOnDemand)
		}
	}
	return nil
}

// ToYAML serializes the project document to formatted YAML.
func (doc *ProjectDocument) ToYAML() ([]byte, error) {
	return yaml.Marshal(doc)
}

// FromYAML parses a project document from YAML.
func FromYAML(data []byte) (*ProjectDocument, error) {
	var doc ProjectDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// IsEnvVarRef checks if a secret string is an environment reference like "${MY_KEY}" or "env:MY_KEY".
func IsEnvVarRef(val string) bool {
	return envVarPattern.MatchString(strings.TrimSpace(val))
}

// ResolveSecret resolves an environment reference or placeholder.
// If the value is "${VAR}" or "env:VAR", it resolves the environment variable.
// If not found in the environment, it returns the raw value or empty string.
func ResolveSecret(val string) string {
	trimmed := strings.TrimSpace(val)
	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		envName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "${"), "}")
		if v, ok := os.LookupEnv(envName); ok {
			return v
		}
	} else if strings.HasPrefix(trimmed, "env:") {
		envName := strings.TrimPrefix(trimmed, "env:")
		if v, ok := os.LookupEnv(envName); ok {
			return v
		}
	}
	return val
}

// MaskSecret returns MaskedSecretPlaceholder if the string is not empty and not an env ref.
func MaskSecret(val string, envRefName string) string {
	if val == "" {
		return ""
	}
	if envRefName != "" {
		return fmt.Sprintf("${%s}", envRefName)
	}
	return MaskedSecretPlaceholder
}
