package core

import "context"

// Generator is an optional capability interface on ProviderAdapter implementations
// covering image, audio, and video media generation (ADR-017 §6).
// Discovered via type assertion so existing chat/embed adapters remain unaffected.
type Generator interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
}

// GenerateRequest represents parameters for image, audio, or video generation.
type GenerateRequest struct {
	Model     string         `json:"model" yaml:"model"`         // Concrete provider deployment name
	Modality  string         `json:"modality" yaml:"modality"`   // "image", "audio", "video"
	Prompt    string         `json:"prompt" yaml:"prompt"`       // Input text prompt describing desired generation
	Voice     string         `json:"voice,omitempty" yaml:"voice,omitempty"`         // Voice identifier for speech synthesis
	Size      string         `json:"size,omitempty" yaml:"size,omitempty"`           // Resolution dimension, e.g. "1024x1024"
	Quality   string         `json:"quality,omitempty" yaml:"quality,omitempty"`     // "standard", "hd"
	Format    string         `json:"format,omitempty" yaml:"format,omitempty"`       // Desired output format, e.g. "png", "mp3", "mp4"
	Duration  float64        `json:"duration,omitempty" yaml:"duration,omitempty"`   // Duration in seconds (for audio/video)
	Count     int            `json:"count,omitempty" yaml:"count,omitempty"`         // Number of items to generate (default: 1)
	Metadata  map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// GeneratedItem represents a single generated media item.
type GeneratedItem struct {
	Index        int       `json:"index"`
	Media        *MediaRef `json:"media,omitempty"`         // Media reference after saving to MediaStore or signed URL
	URL          string    `json:"url,omitempty"`           // Upstream URL if returned directly
	Data         []byte    `json:"data,omitempty"`          // Raw bytes if returned inline (e.g. b64_json or audio stream)
	MimeType     string    `json:"mime_type,omitempty"`
	RevisedPrompt string   `json:"revised_prompt,omitempty"` // Model-revised prompt (e.g. DALL-E 3)
}

// GenerateResponse represents the result of media generation.
type GenerateResponse struct {
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	Modality  string          `json:"modality"`
	Items     []GeneratedItem `json:"items"`
	Usage     UsageStats      `json:"usage"`
	CreatedAt int64           `json:"created_at"`
}
