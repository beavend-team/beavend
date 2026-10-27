package core

import "time"

// PromptFragmentRef represents an ordered modular fragment within a layered prompt bundle.
type PromptFragmentRef struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Content string `json:"content" yaml:"content"`
	Order   int    `json:"order" yaml:"order"`
}

// PromptVersion represents an immutable version of a prompt template.
type PromptVersion struct {
	ID        string              `json:"id" yaml:"id"`
	PromptID  string              `json:"prompt_id" yaml:"prompt_id"`
	Version   int                 `json:"version" yaml:"version"`
	Template  string              `json:"template" yaml:"template"`
	Fragments []PromptFragmentRef `json:"fragments,omitempty" yaml:"fragments,omitempty"`
	Variables []string            `json:"variables" yaml:"variables"`
	IsActive  bool                `json:"is_active" yaml:"is_active"`
	CreatedAt time.Time           `json:"created_at" yaml:"created_at"`
}
