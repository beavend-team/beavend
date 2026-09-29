package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// LocalMemoryProvider provides an in-memory thread-safe implementation of core.MemoryProvider.
// Ideal for local development, tests, and ephemeral single-process execution.
type LocalMemoryProvider struct {
	mu      sync.RWMutex
	records map[string]core.MemoryRecord
}

// NewLocalMemoryProvider creates a new initialized in-memory memory provider.
func NewLocalMemoryProvider() *LocalMemoryProvider {
	return &LocalMemoryProvider{
		records: make(map[string]core.MemoryRecord),
	}
}

// Store saves a semantic memory record.
func (p *LocalMemoryProvider) Store(ctx context.Context, record core.MemoryRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if record.ID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		record.ID = "mem_" + hex.EncodeToString(b)
	}

	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	p.records[record.ID] = record
	return nil
}

// Recall retrieves memories matching the provided identity filter ordered by recency.
func (p *LocalMemoryProvider) Recall(ctx context.Context, filter core.MemoryFilter) ([]core.MemoryRecord, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now().UTC()
	var matches []core.MemoryRecord

	for _, rec := range p.records {
		if rec.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Environment != "" && rec.Environment != filter.Environment {
			continue
		}
		if rec.Issuer != filter.Issuer || rec.Subject != filter.Subject {
			continue
		}
		if filter.AIAppID != "" && rec.AIAppID != "" && rec.AIAppID != filter.AIAppID {
			continue
		}
		if rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			continue
		}
		matches = append(matches, rec)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})

	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}

	return matches, nil
}

// Search performs semantic vector similarity search over memories.
func (p *LocalMemoryProvider) Search(ctx context.Context, query core.MemorySearchQuery) ([]core.MemorySearchResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now().UTC()
	var results []core.MemorySearchResult

	for _, rec := range p.records {
		if rec.ProjectID != query.ProjectID {
			continue
		}
		if query.Environment != "" && rec.Environment != query.Environment {
			continue
		}
		if rec.Issuer != query.Issuer || rec.Subject != query.Subject {
			continue
		}
		if query.AIAppID != "" && rec.AIAppID != "" && rec.AIAppID != query.AIAppID {
			continue
		}
		if rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			continue
		}

		score := float32(1.0)
		if len(query.Vector) > 0 && len(rec.Embedding) > 0 {
			score = CosineSimilarity(query.Vector, rec.Embedding)
		}

		if query.Threshold > 0 && score < query.Threshold {
			continue
		}

		results = append(results, core.MemorySearchResult{
			MemoryRecord: rec,
			Score:        score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if query.Limit > 0 && len(results) > query.Limit {
		results = results[:query.Limit]
	}

	return results, nil
}

// DeleteBySubject purges all memories belonging to the given subject.
func (p *LocalMemoryProvider) DeleteBySubject(ctx context.Context, projectID, issuer, subject string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, rec := range p.records {
		if rec.ProjectID == projectID && rec.Issuer == issuer && rec.Subject == subject {
			delete(p.records, id)
		}
	}
	return nil
}
