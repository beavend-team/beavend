package core

import "time"

const (
	// DocumentMemoryScopeProject represents project-wide document memory.
	DocumentMemoryScopeProject = "project"
	// DocumentMemoryScopeApp represents application-wide document memory.
	DocumentMemoryScopeApp = "app"
	// DocumentMemoryScopeUser represents user-scoped document memory.
	DocumentMemoryScopeUser = "user"

	// MaxDocumentMemoryBytes is the maximum allowed size (2048 bytes) for compiled document memories.
	MaxDocumentMemoryBytes = 2048
)

// DocumentMemory represents a compiled or cached memory snippet.
type DocumentMemory struct {
	ID          string    `json:"id" yaml:"id"`
	ProjectID   string    `json:"project_id" yaml:"project_id"`
	Environment string    `json:"environment" yaml:"environment"`
	Scope       string    `json:"scope" yaml:"scope"`                   // "project", "app", "user"
	TargetID    string    `json:"target_id,omitempty" yaml:"target_id,omitempty"` // app_id or user_id
	Key         string    `json:"key" yaml:"key"`
	Value       string    `json:"value" yaml:"value"`
	Version     int       `json:"version" yaml:"version"`
	UpdatedAt   time.Time `json:"updated_at" yaml:"updated_at"`
}

const (
	// ProposalStatusPending indicates a proposed memory update awaits admin review.
	ProposalStatusPending = "pending"
	// ProposalStatusApproved indicates the proposed memory was approved and applied.
	ProposalStatusApproved = "approved"
	// ProposalStatusRejected indicates the proposed memory was rejected.
	ProposalStatusRejected = "rejected"
)

// DocumentMemoryProposal represents a proposal submitted by an agent to modify document memory.
type DocumentMemoryProposal struct {
	ID          string     `json:"id" yaml:"id"`
	ProjectID   string     `json:"project_id" yaml:"project_id"`
	Environment string     `json:"environment" yaml:"environment"`
	TargetScope string     `json:"target_scope" yaml:"target_scope"` // "project", "app", "user"
	TargetID    string     `json:"target_id,omitempty" yaml:"target_id,omitempty"`
	Key         string     `json:"key" yaml:"key"`
	Value       string     `json:"value" yaml:"value"`
	Reason      string     `json:"reason" yaml:"reason"`
	Status      string     `json:"status" yaml:"status"` // "pending", "approved", "rejected"
	ProposedBy  string     `json:"proposed_by" yaml:"proposed_by"`
	CreatedAt   time.Time  `json:"created_at" yaml:"created_at"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty" yaml:"reviewed_at,omitempty"`
	ReviewedBy  string     `json:"reviewed_by,omitempty" yaml:"reviewed_by,omitempty"`
}
