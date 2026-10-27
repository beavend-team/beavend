package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrProposalNotFound indicates the requested proposal does not exist.
	ErrProposalNotFound = errors.New("document memory proposal not found")
	// ErrInvalidProposal indicates invalid proposal attributes.
	ErrInvalidProposal = errors.New("invalid document memory proposal")
	// ErrMemoryNotFound indicates the requested document memory does not exist.
	ErrMemoryNotFound = errors.New("document memory not found")
	// ErrMemoryTooLarge indicates memory value exceeds 2048 bytes.
	ErrMemoryTooLarge = errors.New("document memory value exceeds maximum allowed size of 2048 bytes")
)

// DocumentMemoryStore manages document memories, proposals, and revision histories.
type DocumentMemoryStore interface {
	CreateProposal(ctx context.Context, proposal *core.DocumentMemoryProposal) error
	GetProposal(ctx context.Context, projectID, env, proposalID string) (*core.DocumentMemoryProposal, error)
	ListProposals(ctx context.Context, projectID, env, status string) ([]*core.DocumentMemoryProposal, error)
	ApproveProposal(ctx context.Context, projectID, env, proposalID, reviewedBy string) (*core.DocumentMemory, error)
	RejectProposal(ctx context.Context, projectID, env, proposalID, reviewedBy, reason string) error
	GetMemory(ctx context.Context, projectID, env, scope, targetID, key string) (*core.DocumentMemory, error)
	UndoMemory(ctx context.Context, projectID, env, scope, targetID, key string) (*core.DocumentMemory, error)
}

// MemoryDocumentStore provides an in-memory thread-safe implementation of DocumentMemoryStore.
type MemoryDocumentStore struct {
	mu          sync.RWMutex
	redisClient redis.UniversalClient
	proposals   map[string]*core.DocumentMemoryProposal // id -> proposal
	memories    map[string]*core.DocumentMemory         // scope:target:key -> memory
	history     map[string][]*core.DocumentMemory       // scope:target:key -> list of previous versions
}

// NewMemoryDocumentStore creates a new MemoryDocumentStore.
func NewMemoryDocumentStore(r redis.UniversalClient) *MemoryDocumentStore {
	return &MemoryDocumentStore{
		redisClient: r,
		proposals:   make(map[string]*core.DocumentMemoryProposal),
		memories:    make(map[string]*core.DocumentMemory),
		history:     make(map[string][]*core.DocumentMemory),
	}
}

func memoryKey(scope, targetID, key string) string {
	return fmt.Sprintf("%s:%s:%s", scope, targetID, key)
}

func generateID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func (s *MemoryDocumentStore) CreateProposal(ctx context.Context, p *core.DocumentMemoryProposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p == nil {
		return ErrInvalidProposal
	}
	if p.TargetScope != core.DocumentMemoryScopeProject &&
		p.TargetScope != core.DocumentMemoryScopeApp &&
		p.TargetScope != core.DocumentMemoryScopeUser {
		return fmt.Errorf("%w: unknown target_scope %q", ErrInvalidProposal, p.TargetScope)
	}
	if p.Key == "" {
		return fmt.Errorf("%w: missing key", ErrInvalidProposal)
	}
	if len(p.Value) > core.MaxDocumentMemoryBytes {
		return ErrMemoryTooLarge
	}

	if p.ID == "" {
		p.ID = generateID("prop")
	}
	p.Status = core.ProposalStatusPending
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}

	copied := *p
	s.proposals[p.ID] = &copied
	return nil
}

func (s *MemoryDocumentStore) GetProposal(ctx context.Context, projectID, env, proposalID string) (*core.DocumentMemoryProposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.proposals[proposalID]
	if !ok || p.ProjectID != projectID || p.Environment != env {
		return nil, ErrProposalNotFound
	}

	copied := *p
	return &copied, nil
}

func (s *MemoryDocumentStore) ListProposals(ctx context.Context, projectID, env, status string) ([]*core.DocumentMemoryProposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*core.DocumentMemoryProposal
	for _, p := range s.proposals {
		if p.ProjectID == projectID && p.Environment == env {
			if status == "" || p.Status == status {
				copied := *p
				result = append(result, &copied)
			}
		}
	}
	return result, nil
}

func (s *MemoryDocumentStore) ApproveProposal(ctx context.Context, projectID, env, proposalID, reviewedBy string) (*core.DocumentMemory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[proposalID]
	if !ok || p.ProjectID != projectID || p.Environment != env {
		return nil, ErrProposalNotFound
	}

	now := time.Now().UTC()
	p.Status = core.ProposalStatusApproved
	p.ReviewedAt = &now
	p.ReviewedBy = reviewedBy

	mKey := memoryKey(p.TargetScope, p.TargetID, p.Key)
	var newVersion int = 1
	if existing, ok := s.memories[mKey]; ok {
		newVersion = existing.Version + 1
		// Save to revision history
		s.history[mKey] = append(s.history[mKey], existing)
	}

	mem := &core.DocumentMemory{
		ID:          generateID("mem"),
		ProjectID:   projectID,
		Environment: env,
		Scope:       p.TargetScope,
		TargetID:    p.TargetID,
		Key:         p.Key,
		Value:       p.Value,
		Version:     newVersion,
		UpdatedAt:   now,
	}
	s.memories[mKey] = mem

	// Invalidate/update Redis cache for user-scoped memories
	if p.TargetScope == core.DocumentMemoryScopeUser && s.redisClient != nil && p.TargetID != "" {
		redisKey := fmt.Sprintf("mem:user:%s:%s:%s", projectID, env, p.TargetID)
		// Retrieve all user memories for this user
		var userMems []*core.DocumentMemory
		for _, m := range s.memories {
			if m.Scope == core.DocumentMemoryScopeUser && m.TargetID == p.TargetID {
				userMems = append(userMems, m)
			}
		}
		data, _ := json.Marshal(userMems)
		_ = s.redisClient.Set(ctx, redisKey, data, 24*time.Hour).Err()
	}

	copied := *mem
	return &copied, nil
}

func (s *MemoryDocumentStore) RejectProposal(ctx context.Context, projectID, env, proposalID, reviewedBy, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[proposalID]
	if !ok || p.ProjectID != projectID || p.Environment != env {
		return ErrProposalNotFound
	}

	now := time.Now().UTC()
	p.Status = core.ProposalStatusRejected
	p.ReviewedAt = &now
	p.ReviewedBy = reviewedBy
	if reason != "" {
		p.Reason = reason
	}
	return nil
}

func (s *MemoryDocumentStore) GetMemory(ctx context.Context, projectID, env, scope, targetID, key string) (*core.DocumentMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mKey := memoryKey(scope, targetID, key)
	mem, ok := s.memories[mKey]
	if !ok || mem.ProjectID != projectID || mem.Environment != env {
		return nil, ErrMemoryNotFound
	}

	copied := *mem
	return &copied, nil
}

func (s *MemoryDocumentStore) UndoMemory(ctx context.Context, projectID, env, scope, targetID, key string) (*core.DocumentMemory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mKey := memoryKey(scope, targetID, key)
	historyList := s.history[mKey]
	if len(historyList) == 0 {
		// No previous revision: delete current memory
		delete(s.memories, mKey)
		return nil, nil
	}

	// Restore the most recent previous revision
	lastIdx := len(historyList) - 1
	prev := historyList[lastIdx]
	s.history[mKey] = historyList[:lastIdx]
	s.memories[mKey] = prev

	// Update Redis cache if user scope
	if scope == core.DocumentMemoryScopeUser && s.redisClient != nil && targetID != "" {
		redisKey := fmt.Sprintf("mem:user:%s:%s:%s", projectID, env, targetID)
		var userMems []*core.DocumentMemory
		for _, m := range s.memories {
			if m.Scope == core.DocumentMemoryScopeUser && m.TargetID == targetID {
				userMems = append(userMems, m)
			}
		}
		data, _ := json.Marshal(userMems)
		_ = s.redisClient.Set(ctx, redisKey, data, 24*time.Hour).Err()
	}

	copied := *prev
	return &copied, nil
}
