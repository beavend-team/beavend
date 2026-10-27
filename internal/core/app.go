package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AIApp represents a configured AI capability exposed by an implementor's application.
type AIApp struct {
	ID             string          `json:"id" yaml:"id"`
	ProjectID      string          `json:"project_id" yaml:"project_id"`
	Environment    string          `json:"environment" yaml:"environment"`
	Name           string          `json:"name" yaml:"name"`
	Description    string          `json:"description" yaml:"description"`
	AccessPolicy   string          `json:"access_policy" yaml:"access_policy"`
	PromptID       string          `json:"prompt_id" yaml:"prompt_id"`
	PromptVersion  int             `json:"prompt_version" yaml:"prompt_version"`
	ModelRoutingID string          `json:"model_routing_id" yaml:"model_routing_id"`
	ModelRoutes    *ModelRoutes    `json:"model_routes,omitempty" yaml:"model_routes,omitempty"`
	ToolIDs        []string        `json:"tool_ids" yaml:"tool_ids"`
	SkillIDs       []string        `json:"skill_ids,omitempty" yaml:"skill_ids,omitempty"`
	Limits         []LimitRule     `json:"limits" yaml:"limits"`
	Cache          CacheConfig     `json:"cache" yaml:"cache"`
	Guardrails     GuardrailConfig `json:"guardrails" yaml:"guardrails"`
	Memory         MemoryConfig    `json:"memory" yaml:"memory"`
	Search         SearchConfig    `json:"search,omitempty" yaml:"search,omitempty"`
	ResponseSchema string          `json:"response_schema,omitempty" yaml:"response_schema,omitempty"`
	AuthConfig     *AuthConfig     `json:"auth,omitempty" yaml:"auth,omitempty"`
	CreatedAt      time.Time       `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" yaml:"updated_at"`
}

// ModelRoutes defines the routing rules and default logical model for an AI App.
type ModelRoutes struct {
	Default string           `json:"default" yaml:"default"`
	Rules   []ModelRouteRule `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// ModelRouteRule specifies a condition and the logical model to use when matched.
type ModelRouteRule struct {
	When RouteCondition `json:"when" yaml:"when"`
	Use  string         `json:"use" yaml:"use"`
}

// RouteCondition defines the matching criteria for a model route rule.
type RouteCondition struct {
	InputHas []string          `json:"input_has,omitempty" yaml:"input_has,omitempty"`
	Output   []string          `json:"output,omitempty" yaml:"output,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Expr     string            `json:"expr,omitempty" yaml:"expr,omitempty"`
}

// UnmarshalJSON handles both auth and auth_config JSON keys.
func (a *AIApp) UnmarshalJSON(data []byte) error {
	type rawApp AIApp
	var raw struct {
		rawApp
		AuthAlt *AuthConfig `json:"auth_config,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = AIApp(raw.rawApp)
	if a.AuthConfig == nil && raw.AuthAlt != nil {
		a.AuthConfig = raw.AuthAlt
	}
	return nil
}

// SearchConfig defines web search capabilities for an AI App via SearXNG.
type SearchConfig struct {
	Enabled    bool     `json:"enabled" yaml:"enabled"`
	MaxResults int      `json:"max_results,omitempty" yaml:"max_results,omitempty"`
	Categories []string `json:"categories,omitempty" yaml:"categories,omitempty"`
	Engines    []string `json:"engines,omitempty" yaml:"engines,omitempty"`
	TimeRange  string   `json:"time_range,omitempty" yaml:"time_range,omitempty"`
	SafeSearch int      `json:"safe_search,omitempty" yaml:"safe_search,omitempty"`
}

// AppAPIKey represents an authorized API key for direct server-to-server AI App access.
type AppAPIKey struct {
	ID               string     `json:"id" yaml:"id"`
	AppID            string     `json:"app_id" yaml:"app_id"`
	ProjectID        string     `json:"project_id" yaml:"project_id"`
	Environment      string     `json:"environment" yaml:"environment"`
	Name             string     `json:"name" yaml:"name"`
	KeyPrefix        string     `json:"key_prefix" yaml:"key_prefix"`
	KeyHash          string     `json:"-" yaml:"-"`
	MayAssertSubject bool       `json:"may_assert_subject" yaml:"may_assert_subject"`
	AllowedClaims    []string   `json:"allowed_claims,omitempty" yaml:"allowed_claims,omitempty"`
	CreatedAt        time.Time  `json:"created_at" yaml:"created_at"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty" yaml:"last_used_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

// LimitRule defines a rate or quota limit for a resource over a time window.
type LimitRule struct {
	Resource      string        `json:"resource" yaml:"resource"` // "inference", "tokens"
	Window        time.Duration `json:"window" yaml:"window"`
	Max           int64         `json:"max" yaml:"max"`
	TierCondition string        `json:"tier_condition,omitempty" yaml:"tier_condition,omitempty"` // CEL expression
	Scope         string        `json:"scope,omitempty" yaml:"scope,omitempty"`                   // "user" (default) or "project"
}

// UnmarshalJSON handles both integer nanoseconds/seconds and human duration strings like "1m", "1h", "1d".
func (r *LimitRule) UnmarshalJSON(data []byte) error {
	type rawLimitRule struct {
		Resource      string `json:"resource"`
		Window        any    `json:"window"`
		Max           int64  `json:"max"`
		TierCondition string `json:"tier_condition,omitempty"`
		Scope         string `json:"scope,omitempty"`
	}

	var raw rawLimitRule
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Resource = raw.Resource
	r.Max = raw.Max
	r.TierCondition = raw.TierCondition
	r.Scope = raw.Scope

	switch v := raw.Window.(type) {
	case string:
		d, err := ParseWindowDuration(v)
		if err != nil {
			return fmt.Errorf("parse limit window %q: %w", v, err)
		}
		r.Window = d
	case float64:
		// If small (e.g. <= 86400*365), treat as seconds; otherwise nanoseconds
		if v <= 31536000 {
			r.Window = time.Duration(v * float64(time.Second))
		} else {
			r.Window = time.Duration(v)
		}
	case nil:
		r.Window = 0
	default:
		return fmt.Errorf("unsupported window type: %T", raw.Window)
	}

	return nil
}

// MarshalJSON formats Window as a human-readable duration string.
func (r LimitRule) MarshalJSON() ([]byte, error) {
	type alias struct {
		Resource      string `json:"resource"`
		Window        string `json:"window"`
		Max           int64  `json:"max"`
		TierCondition string `json:"tier_condition,omitempty"`
	}

	return json.Marshal(alias{
		Resource:      r.Resource,
		Window:        r.Window.String(),
		Max:           r.Max,
		TierCondition: r.TierCondition,
	})
}

// ParseWindowDuration parses strings like "10s", "1m", "24h", "1d", "7d", "30d".
func ParseWindowDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Handle day suffix "d"
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		var days float64
		if _, err := fmt.Sscanf(numStr, "%f", &days); err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}

	return time.ParseDuration(s)
}

// CacheConfig specifies caching behavior for an AI App.
type CacheConfig struct {
	Enabled    bool     `json:"enabled" yaml:"enabled"`
	TTL        int      `json:"ttl" yaml:"ttl"`     // in seconds
	Scope      string   `json:"scope" yaml:"scope"` // "user", "shared"
	Modalities []string `json:"modalities,omitempty" yaml:"modalities,omitempty"`
}

// GuardrailConfig defines safety and guardrail behavior.
type GuardrailConfig struct {
	StreamMode       string   `json:"stream_mode" yaml:"stream_mode"` // "buffered", "passthrough_with_post_check"
	MaxInputBytes    int      `json:"max_input_bytes" yaml:"max_input_bytes"`
	MaxOutputBytes   int      `json:"max_output_bytes" yaml:"max_output_bytes"`
	PIIRedaction     bool     `json:"pii_redaction" yaml:"pii_redaction"`
	InjectionDefense bool     `json:"injection_defense" yaml:"injection_defense"`
	Rules            []string `json:"rules,omitempty" yaml:"rules,omitempty"`
	MaxDepth         int      `json:"max_depth,omitempty" yaml:"max_depth,omitempty"` // Max agent loop iterations (default: 5)
}

// MemoryConfig defines memory and conversation context settings for an AI App.
type MemoryConfig struct {
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	MaxHistory    int    `json:"max_history,omitempty" yaml:"max_history,omitempty"`       // Max message turns to retain in context
	MaxTokens     int    `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`         // Context window token budget for compaction
	VectorRecall  bool   `json:"vector_recall,omitempty" yaml:"vector_recall,omitempty"`   // Auto recall relevant memories
	RecallLimit   int    `json:"recall_limit,omitempty" yaml:"recall_limit,omitempty"`     // Number of memories to inject
	ProviderType  string `json:"provider_type,omitempty" yaml:"provider_type,omitempty"` // "postgres", "http", "local"
	HTTPURL       string `json:"http_url,omitempty" yaml:"http_url,omitempty"`             // For external implementor-hosted memory
	HTTPAuthToken string `json:"http_auth_token,omitempty" yaml:"http_auth_token,omitempty"`
	ThreadTTLSec  int    `json:"thread_ttl_sec,omitempty" yaml:"thread_ttl_sec,omitempty"` // Default TTL for threads in seconds
}
