package core

import "time"

const (
	// SkillInjectionAlways injects the full skill instructions directly into the system prompt.
	SkillInjectionAlways = "always"
	// SkillInjectionOnDemand lists skill metadata in the system prompt and exposes the built-in load_skill tool.
	SkillInjectionOnDemand = "on_demand"
)

// SkillVersion represents an immutable version of an agent skill in the active snapshot.
type SkillVersion struct {
	ID            string    `json:"id" yaml:"id"`
	SkillID       string    `json:"skill_id" yaml:"skill_id"`
	Name          string    `json:"name" yaml:"name"`
	Description   string    `json:"description" yaml:"description"`
	Source        string    `json:"source" yaml:"source"`
	InjectionMode string    `json:"injection_mode" yaml:"injection_mode"`
	Version       int       `json:"version" yaml:"version"`
	Content       string    `json:"content" yaml:"content"`
	Variables     []string  `json:"variables" yaml:"variables"`
	IsActive      bool      `json:"is_active" yaml:"is_active"`
	CreatedAt     time.Time `json:"created_at" yaml:"created_at"`
}
