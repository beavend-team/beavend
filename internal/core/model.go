package core

// ModelPrice defines per-token rates in USD per 1 Million tokens, as well as per-unit media generation pricing.
type ModelPrice struct {
	InputCostPer1M      float64  `json:"input_cost_per_1m" yaml:"input_cost_per_1m"`
	OutputCostPer1M     float64  `json:"output_cost_per_1m" yaml:"output_cost_per_1m"`
	PerImageCost        *float64 `json:"per_image_cost,omitempty" yaml:"per_image_cost,omitempty"`
	PerAudioSecondCost  *float64 `json:"per_audio_second_cost,omitempty" yaml:"per_audio_second_cost,omitempty"`
	PerVideoSecondCost  *float64 `json:"per_video_second_cost,omitempty" yaml:"per_video_second_cost,omitempty"`
}

// ModelRouting maps a logical model alias to a concrete provider deployment and adapter.
type ModelRouting struct {
	ID                  string   `json:"id" yaml:"id"`
	ProjectID           string   `json:"project_id" yaml:"project_id"`
	Environment         string   `json:"environment" yaml:"environment"`
	LogicalModel        string   `json:"logical_model" yaml:"logical_model"` // e.g. "fast", "reasoning", "vision"
	Deployment          string   `json:"deployment" yaml:"deployment"`       // e.g. "gemini-2.5-flash", "claude-3-7-sonnet"
	Adapter             string   `json:"adapter" yaml:"adapter"`             // "litellm", "openai_compat"
	FallbackDeployments []string `json:"fallback_deployments,omitempty" yaml:"fallback_deployments,omitempty"`
	InputCostPer1M      *float64 `json:"input_cost_per_1m,omitempty" yaml:"input_cost_per_1m,omitempty"`
	OutputCostPer1M     *float64 `json:"output_cost_per_1m,omitempty" yaml:"output_cost_per_1m,omitempty"`
	PerImageCost        *float64 `json:"per_image_cost,omitempty" yaml:"per_image_cost,omitempty"`
	PerAudioSecondCost  *float64 `json:"per_audio_second_cost,omitempty" yaml:"per_audio_second_cost,omitempty"`
	PerVideoSecondCost  *float64 `json:"per_video_second_cost,omitempty" yaml:"per_video_second_cost,omitempty"`
	InputModalities     []string `json:"input_modalities,omitempty" yaml:"input_modalities,omitempty"`
	OutputModalities    []string `json:"output_modalities,omitempty" yaml:"output_modalities,omitempty"`
}

