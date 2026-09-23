package core

// ModelRouting maps a logical model alias to a concrete provider deployment and adapter.
type ModelRouting struct {
	ID                  string   `json:"id" yaml:"id"`
	ProjectID           string   `json:"project_id" yaml:"project_id"`
	Environment         string   `json:"environment" yaml:"environment"`
	LogicalModel        string   `json:"logical_model" yaml:"logical_model"` // e.g. "fast", "reasoning", "vision"
	Deployment          string   `json:"deployment" yaml:"deployment"`       // e.g. "gemini-2.5-flash", "claude-3-7-sonnet"
	Adapter             string   `json:"adapter" yaml:"adapter"`             // "litellm", "openai_compat"
	FallbackDeployments []string `json:"fallback_deployments,omitempty" yaml:"fallback_deployments,omitempty"`
}
