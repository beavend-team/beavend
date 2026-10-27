package core

import (
	"context"
	"strings"
	"time"
)

type contextKey string

const (
	snapshotContextKey contextKey = "beavend_snapshot"
	appIDContextKey    contextKey = "beavend_app_id"
)

// ContextWithSnapshot returns a new context containing the Snapshot.
func ContextWithSnapshot(ctx context.Context, snap *Snapshot) context.Context {
	return context.WithValue(ctx, snapshotContextKey, snap)
}

// SnapshotFromContext extracts the Snapshot from context if present.
func SnapshotFromContext(ctx context.Context) (*Snapshot, bool) {
	if ctx == nil {
		return nil, false
	}
	snap, ok := ctx.Value(snapshotContextKey).(*Snapshot)
	return snap, ok
}

// ContextWithAppID returns a new context containing the AIAppID.
func ContextWithAppID(ctx context.Context, appID string) context.Context {
	return context.WithValue(ctx, appIDContextKey, appID)
}

// AppIDFromContext extracts the AIAppID from context if present.
func AppIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	appID, ok := ctx.Value(appIDContextKey).(string)
	return appID, ok && appID != ""
}

// ProviderConfig defines decrypted credentials and endpoint settings for an upstream model provider.
type ProviderConfig struct {
	ID           string `json:"id" yaml:"id"`
	ProjectID    string `json:"project_id" yaml:"project_id"`
	Environment  string `json:"environment" yaml:"environment"`
	ProviderName string `json:"provider_name" yaml:"provider_name"`
	BaseURL      string `json:"base_url" yaml:"base_url"`
	APIKey       string `json:"api_key,omitempty" yaml:"-"`
}

// Snapshot represents an immutable compiled project configuration in memory.
// It is swapped atomically and read without touching PostgreSQL on the request hot path.
type Snapshot struct {
	ProjectID        string                       `json:"project_id"`
	Environment      string                       `json:"environment"`
	Revision         int64                        `json:"revision"`
	CompiledAt       time.Time                    `json:"compiled_at"`
	AuthConfig       *AuthConfig                  `json:"auth_config,omitempty"`
	Apps             map[string]*AIApp            `json:"apps"`
	Prompts          map[string]*PromptVersion    `json:"prompts"`
	Skills           map[string]*SkillVersion     `json:"skills,omitempty"`
	Models           map[string]*ModelRouting     `json:"models"`
	Tools            map[string]*ToolDefinition   `json:"tools"`
	Policies         map[string]*PolicyDefinition `json:"policies"`
	AppAPIKeys       map[string]*AppAPIKey        `json:"app_api_keys,omitempty"` // Keyed by key_hash
	Providers        map[string]*ProviderConfig   `json:"providers,omitempty"`    // Keyed by provider_name (lowercase)
	DocumentMemories map[string]*DocumentMemory   `json:"document_memories,omitempty"`
	KnowledgeCatalog map[string][]KnowledgeDocMeta `json:"knowledge_catalog,omitempty"` // Keyed by app_id
	CompiledPolicies map[string]any               `json:"-"`                      // Pre-compiled CEL programs
	CompiledSchemas  map[string]any               `json:"-"`                      // Pre-compiled JSON Schemas
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
		Skills:           make(map[string]*SkillVersion),
		Models:           make(map[string]*ModelRouting),
		Tools:            make(map[string]*ToolDefinition),
		Policies:         make(map[string]*PolicyDefinition),
		AppAPIKeys:       make(map[string]*AppAPIKey),
		Providers:        make(map[string]*ProviderConfig),
		DocumentMemories: make(map[string]*DocumentMemory),
		KnowledgeCatalog: make(map[string][]KnowledgeDocMeta),
		CompiledPolicies: make(map[string]any),
		CompiledSchemas:  make(map[string]any),
	}
}

// Provider returns the ProviderConfig by provider name if present.
func (s *Snapshot) Provider(name string) (*ProviderConfig, bool) {
	if s == nil || s.Providers == nil {
		return nil, false
	}
	p, ok := s.Providers[name]
	if !ok {
		for k, v := range s.Providers {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
	}
	return p, ok
}

// AppAPIKeyByHash returns the AppAPIKey by key hash if present.
func (s *Snapshot) AppAPIKeyByHash(hash string) (*AppAPIKey, bool) {
	if s == nil || s.AppAPIKeys == nil {
		return nil, false
	}
	key, ok := s.AppAPIKeys[hash]
	return key, ok
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

// Skill returns the SkillVersion by Skill ID or Name if present.
func (s *Snapshot) Skill(id string) (*SkillVersion, bool) {
	if s == nil || s.Skills == nil {
		return nil, false
	}
	sk, ok := s.Skills[id]
	return sk, ok
}

// Auth returns the AuthConfig if present.
func (s *Snapshot) Auth() (*AuthConfig, bool) {
	if s == nil || s.AuthConfig == nil {
		return nil, false
	}
	return s.AuthConfig, true
}

// CompiledPolicy returns the pre-compiled policy program by ID or expression if present.
func (s *Snapshot) CompiledPolicy(id string) (any, bool) {
	if s == nil || s.CompiledPolicies == nil {
		return nil, false
	}
	p, ok := s.CompiledPolicies[id]
	return p, ok
}

// DocumentMemory returns a compiled document memory by scope, target, and key if present.
func (s *Snapshot) DocumentMemory(scope, targetID, key string) (*DocumentMemory, bool) {
	if s == nil || s.DocumentMemories == nil {
		return nil, false
	}
	memKey := scope + ":" + targetID + ":" + key
	m, ok := s.DocumentMemories[memKey]
	return m, ok
}

// DocumentMemoriesForApp returns all project-level and app-level document memories compiled for an app.
func (s *Snapshot) DocumentMemoriesForApp(appID string) []*DocumentMemory {
	if s == nil || s.DocumentMemories == nil {
		return nil
	}
	var res []*DocumentMemory
	for _, mem := range s.DocumentMemories {
		if mem.Scope == DocumentMemoryScopeProject || (mem.Scope == DocumentMemoryScopeApp && mem.TargetID == appID) {
			res = append(res, mem)
		}
	}
	return res
}

// KnowledgeDocs returns compiled knowledge document metadata for a given app.
func (s *Snapshot) KnowledgeDocs(appID string) []KnowledgeDocMeta {
	if s == nil || s.KnowledgeCatalog == nil {
		return nil
	}
	return s.KnowledgeCatalog[appID]
}
