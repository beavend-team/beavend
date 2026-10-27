package core

import "time"

const (
	// SkillInjectionAlways injects the full skill instructions directly into the system prompt.
	SkillInjectionAlways = "always"
	// SkillInjectionOnDemand lists skill metadata in the system prompt and exposes the built-in load_skill tool.
	SkillInjectionOnDemand = "on_demand"

	// Skill governance statuses
	SkillStatusActive   = "active"
	SkillStatusStaged   = "staged"
	SkillStatusArchived = "archived"
)

// Skill represents the governed parent entity for an agent skill.
type Skill struct {
	ID            string    `json:"id" yaml:"id"`
	ProjectID     string    `json:"project_id" yaml:"project_id"`
	Environment   string    `json:"environment" yaml:"environment"`
	Name          string    `json:"name" yaml:"name"`
	Description   string    `json:"description" yaml:"description"`
	Source        string    `json:"source" yaml:"source"`
	InjectionMode string    `json:"injection_mode" yaml:"injection_mode"`
	ActiveVersion int       `json:"active_version" yaml:"active_version"`
	Status        string    `json:"status" yaml:"status"` // "active", "staged", "archived"
	ChangeNote    string    `json:"change_note,omitempty" yaml:"change_note,omitempty"`
	CreatedAt     time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" yaml:"updated_at"`
}

// CanPromote returns whether the skill is currently in a staged state and eligible for activation.
func (s *Skill) CanPromote() bool {
	return s.Status == SkillStatusStaged
}

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
	Status        string    `json:"status,omitempty" yaml:"status,omitempty"` // "active", "staged", "archived"
	ChangeNote    string    `json:"change_note,omitempty" yaml:"change_note,omitempty"`
	IsActive      bool      `json:"is_active" yaml:"is_active"`
	CreatedAt     time.Time `json:"created_at" yaml:"created_at"`
}
