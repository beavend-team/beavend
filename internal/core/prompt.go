package core

import "time"

// PromptVersion represents an immutable version of a prompt template.
type PromptVersion struct {
	ID        string    `json:"id" yaml:"id"`
	PromptID  string    `json:"prompt_id" yaml:"prompt_id"`
	Version   int       `json:"version" yaml:"version"`
	Template  string    `json:"template" yaml:"template"`
	Variables []string  `json:"variables" yaml:"variables"`
	IsActive  bool      `json:"is_active" yaml:"is_active"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
}
