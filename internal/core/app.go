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
	ToolIDs        []string        `json:"tool_ids" yaml:"tool_ids"`
	Limits         []LimitRule     `json:"limits" yaml:"limits"`
	Cache          CacheConfig     `json:"cache" yaml:"cache"`
	Guardrails     GuardrailConfig `json:"guardrails" yaml:"guardrails"`
	Memory         MemoryConfig    `json:"memory" yaml:"memory"`
	ResponseSchema string          `json:"response_schema,omitempty" yaml:"response_schema,omitempty"`
	CreatedAt      time.Time       `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" yaml:"updated_at"`
}

// LimitRule defines a rate or quota limit for a resource over a time window.
type LimitRule struct {
	Resource      string        `json:"resource" yaml:"resource"` // "inference", "tokens"
	Window        time.Duration `json:"window" yaml:"window"`
	Max           int64         `json:"max" yaml:"max"`
	TierCondition string        `json:"tier_condition,omitempty" yaml:"tier_condition,omitempty"` // CEL expression
}

// UnmarshalJSON handles both integer nanoseconds/seconds and human duration strings like "1m", "1h", "1d".
func (r *LimitRule) UnmarshalJSON(data []byte) error {
	type rawLimitRule struct {
		Resource      string `json:"resource"`
		Window        any    `json:"window"`
		Max           int64  `json:"max"`
		TierCondition string `json:"tier_condition,omitempty"`
	}

	var raw rawLimitRule
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Resource = raw.Resource
	r.Max = raw.Max
	r.TierCondition = raw.TierCondition

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
	Enabled bool   `json:"enabled" yaml:"enabled"`
	TTL     int    `json:"ttl" yaml:"ttl"`     // in seconds
	Scope   string `json:"scope" yaml:"scope"` // "user", "shared"
}

// GuardrailConfig defines safety and guardrail behavior.
type GuardrailConfig struct {
	StreamMode       string   `json:"stream_mode" yaml:"stream_mode"` // "buffered", "passthrough_with_post_check"
	MaxInputBytes    int      `json:"max_input_bytes" yaml:"max_input_bytes"`
	MaxOutputBytes   int      `json:"max_output_bytes" yaml:"max_output_bytes"`
	PIIRedaction     bool     `json:"pii_redaction" yaml:"pii_redaction"`
	InjectionDefense bool     `json:"injection_defense" yaml:"injection_defense"`
	Rules            []string `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// MemoryConfig defines memory and conversation context settings for an AI App.
type MemoryConfig struct {
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	MaxHistory    int    `json:"max_history,omitempty" yaml:"max_history,omitempty"`       // Max message turns to retain in context
	VectorRecall  bool   `json:"vector_recall,omitempty" yaml:"vector_recall,omitempty"`   // Auto recall relevant memories
	RecallLimit   int    `json:"recall_limit,omitempty" yaml:"recall_limit,omitempty"`     // Number of memories to inject
	ProviderType  string `json:"provider_type,omitempty" yaml:"provider_type,omitempty"` // "postgres", "http", "local"
	HTTPURL       string `json:"http_url,omitempty" yaml:"http_url,omitempty"`             // For external implementor-hosted memory
	HTTPAuthToken string `json:"http_auth_token,omitempty" yaml:"http_auth_token,omitempty"`
	ThreadTTLSec  int    `json:"thread_ttl_sec,omitempty" yaml:"thread_ttl_sec,omitempty"` // Default TTL for threads in seconds
}
