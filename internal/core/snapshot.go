package core

import "time"

// Snapshot represents an immutable compiled project configuration in memory.
// It is swapped atomically and read without touching PostgreSQL on the request hot path.
type Snapshot struct {
	ProjectID        string                       `json:"project_id"`
	Environment      string                       `json:"environment"`
	Revision         int64                        `json:"revision"`
	CompiledAt       time.Time                    `json:"compiled_at"`
	Apps             map[string]*AIApp            `json:"apps"`
	Prompts          map[string]*PromptVersion    `json:"prompts"`
	Models           map[string]*ModelRouting     `json:"models"`
	Tools            map[string]*ToolDefinition   `json:"tools"`
	Policies         map[string]*PolicyDefinition `json:"policies"`
	CompiledPolicies map[string]any               `json:"-"` // Pre-compiled CEL programs
	CompiledSchemas  map[string]any               `json:"-"` // Pre-compiled JSON Schemas
}

// NewSnapshot initializes an empty Snapshot with allocated maps.
func NewSnapshot(projectID, environment string, revision int64) *Snapshot {
	return &Snapshot{
		ProjectID:        projectID,
		Environment:      environment,
		Revision:         revision,
		CompiledAt:       time.Now().UTC(),
		Apps:             make(map[string]*AIApp),
		Prompts:          make(map[string]*PromptVersion),
		Models:           make(map[string]*ModelRouting),
		Tools:            make(map[string]*ToolDefinition),
		Policies:         make(map[string]*PolicyDefinition),
		CompiledPolicies: make(map[string]any),
		CompiledSchemas:  make(map[string]any),
	}
}

// App returns the AIApp by ID if present.
func (s *Snapshot) App(id string) (*AIApp, bool) {
	if s == nil || s.Apps == nil {
		return nil, false
	}
	app, ok := s.Apps[id]
	return app, ok
}

// Model returns the ModelRouting by logical model ID if present.
func (s *Snapshot) Model(id string) (*ModelRouting, bool) {
	if s == nil || s.Models == nil {
		return nil, false
	}
	m, ok := s.Models[id]
	return m, ok
}

// Tool returns the ToolDefinition by ID if present.
func (s *Snapshot) Tool(id string) (*ToolDefinition, bool) {
	if s == nil || s.Tools == nil {
		return nil, false
	}
	t, ok := s.Tools[id]
	return t, ok
}

// Policy returns the PolicyDefinition by ID if present.
func (s *Snapshot) Policy(id string) (*PolicyDefinition, bool) {
	if s == nil || s.Policies == nil {
		return nil, false
	}
	p, ok := s.Policies[id]
	return p, ok
}

// Prompt returns the PromptVersion by Prompt ID if present.
func (s *Snapshot) Prompt(id string) (*PromptVersion, bool) {
	if s == nil || s.Prompts == nil {
		return nil, false
	}
	p, ok := s.Prompts[id]
	return p, ok
}
