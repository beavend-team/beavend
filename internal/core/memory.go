package core

import "time"

// MemoryRecord represents a single user-scoped semantic memory unit.
type MemoryRecord struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Environment string         `json:"environment"`
	Issuer      string         `json:"issuer"`
	Subject     string         `json:"subject"`
	AIAppID     string         `json:"ai_app_id,omitempty"`
	Content     string         `json:"content"`
	Embedding   []float32      `json:"embedding,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
}

// MemoryFilter specifies criteria for recalling semantic memories.
type MemoryFilter struct {
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment,omitempty"`
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	AIAppID     string `json:"ai_app_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// MemorySearchQuery specifies criteria for vector similarity search over semantic memories.
type MemorySearchQuery struct {
	ProjectID   string    `json:"project_id"`
	Environment string    `json:"environment,omitempty"`
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"`
	AIAppID     string    `json:"ai_app_id,omitempty"`
	Vector      []float32 `json:"vector,omitempty"`
	Query       string    `json:"query,omitempty"`
	Limit       int       `json:"limit,omitempty"`
	Threshold   float32   `json:"threshold,omitempty"`
}

// MemorySearchResult wraps a retrieved MemoryRecord with its similarity score.
type MemorySearchResult struct {
	MemoryRecord
	Score float32 `json:"score"`
}

// Thread represents a server-side conversation thread session with message history and TTL.
type Thread struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Environment string         `json:"environment"`
	AIAppID     string         `json:"ai_app_id"`
	Issuer      string         `json:"issuer,omitempty"`
	Subject     string         `json:"subject,omitempty"`
	ExternalRef string         `json:"external_ref,omitempty"`
	Messages    []ChatMessage  `json:"messages"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	TTLSeconds  int            `json:"ttl_seconds"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	ExpiresAt   time.Time      `json:"expires_at"`
}
