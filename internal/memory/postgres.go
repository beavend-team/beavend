package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/store/db"
)

// PGVectorMemoryProvider implements core.MemoryProvider using PostgreSQL with pgvector.
type PGVectorMemoryProvider struct {
	queries *db.Queries
}

// NewPGVectorMemoryProvider constructs a new PostgreSQL pgvector memory provider.
func NewPGVectorMemoryProvider(queries *db.Queries) *PGVectorMemoryProvider {
	return &PGVectorMemoryProvider{
		queries: queries,
	}
}

// Store persists a semantic memory record into PostgreSQL with pgvector embeddings.
func (p *PGVectorMemoryProvider) Store(ctx context.Context, record core.MemoryRecord) error {
	if p.queries == nil {
		return fmt.Errorf("pgvector memory provider: uninitialized queries")
	}

	if record.ID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		record.ID = "mem_" + hex.EncodeToString(b)
	}

	metadataBytes, err := json.Marshal(record.Metadata)
	if err != nil {
		metadataBytes = []byte("{}")
	}

	var expiresAt pgtype.Timestamptz
	if record.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{
			Time:  *record.ExpiresAt,
			Valid: true,
		}
	}

	_, err = p.queries.CreateSemanticMemory(ctx, db.CreateSemanticMemoryParams{
		ID:          record.ID,
		ProjectID:   record.ProjectID,
		Environment: record.Environment,
		AiAppID:     record.AIAppID,
		Issuer:      record.Issuer,
		Subject:     record.Subject,
		Content:     record.Content,
		Embedding:   ToPGVector(record.Embedding),
		Metadata:    metadataBytes,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return fmt.Errorf("pgvector store: %w", err)
	}

	return nil
}

// Recall retrieves memories matching the provided identity filter ordered by recency.
func (p *PGVectorMemoryProvider) Recall(ctx context.Context, filter core.MemoryFilter) ([]core.MemoryRecord, error) {
	if p.queries == nil {
		return nil, fmt.Errorf("pgvector memory provider: uninitialized queries")
	}

	limit := int32(10)
	if filter.Limit > 0 {
		limit = int32(filter.Limit)
	}

	rows, err := p.queries.RecallSemanticMemories(ctx, db.RecallSemanticMemoriesParams{
		ProjectID:   filter.ProjectID,
		Environment: filter.Environment,
		Issuer:      filter.Issuer,
		Subject:     filter.Subject,
		AiAppID:     filter.AIAppID,
		Limit:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("pgvector recall: %w", err)
	}

	records := make([]core.MemoryRecord, 0, len(rows))
	for _, row := range rows {
		var meta map[string]any
		if len(row.Metadata) > 0 {
			_ = json.Unmarshal(row.Metadata, &meta)
		}

		var exp *time.Time
		if row.ExpiresAt.Valid {
			t := row.ExpiresAt.Time
			exp = &t
		}

		records = append(records, core.MemoryRecord{
			ID:          row.ID,
			ProjectID:   row.ProjectID,
			Environment: row.Environment,
			Issuer:      row.Issuer,
			Subject:     row.Subject,
			AIAppID:     row.AiAppID,
			Content:     row.Content,
			Embedding:   FromPGVector(row.Embedding),
			Metadata:    meta,
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
			ExpiresAt:   exp,
		})
	}

	return records, nil
}

// Search performs semantic vector similarity search using PostgreSQL pgvector cosine distance.
func (p *PGVectorMemoryProvider) Search(ctx context.Context, query core.MemorySearchQuery) ([]core.MemorySearchResult, error) {
	if p.queries == nil {
		return nil, fmt.Errorf("pgvector memory provider: uninitialized queries")
	}

	limit := int32(10)
	if query.Limit > 0 {
		limit = int32(query.Limit)
	}

	rows, err := p.queries.SearchSemanticMemories(ctx, db.SearchSemanticMemoriesParams{
		ProjectID:   query.ProjectID,
		Environment: query.Environment,
		Issuer:      query.Issuer,
		Subject:     query.Subject,
		AiAppID:     query.AIAppID,
		Embedding:   ToPGVector(query.Vector),
		Limit:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("pgvector search: %w", err)
	}

	results := make([]core.MemorySearchResult, 0, len(rows))
	for _, row := range rows {
		score := float32(row.Similarity)
		if query.Threshold > 0 && score < query.Threshold {
			continue
		}

		var meta map[string]any
		if len(row.Metadata) > 0 {
			_ = json.Unmarshal(row.Metadata, &meta)
		}

		var exp *time.Time
		if row.ExpiresAt.Valid {
			t := row.ExpiresAt.Time
			exp = &t
		}

		results = append(results, core.MemorySearchResult{
			MemoryRecord: core.MemoryRecord{
				ID:          row.ID,
				ProjectID:   row.ProjectID,
				Environment: row.Environment,
				Issuer:      row.Issuer,
				Subject:     row.Subject,
				AIAppID:     row.AiAppID,
				Content:     row.Content,
				Embedding:   FromPGVector(row.Embedding),
				Metadata:    meta,
				CreatedAt:   row.CreatedAt.Time,
				UpdatedAt:   row.UpdatedAt.Time,
				ExpiresAt:   exp,
			},
			Score: score,
		})
	}

	return results, nil
}

// DeleteBySubject removes all semantic memories for the given issuer and subject.
func (p *PGVectorMemoryProvider) DeleteBySubject(ctx context.Context, projectID, issuer, subject string) error {
	if p.queries == nil {
		return fmt.Errorf("pgvector memory provider: uninitialized queries")
	}

	err := p.queries.DeleteSemanticMemoriesBySubject(ctx, db.DeleteSemanticMemoriesBySubjectParams{
		ProjectID: projectID,
		Issuer:    issuer,
		Subject:   subject,
	})
	if err != nil {
		return fmt.Errorf("pgvector delete by subject: %w", err)
	}
	return nil
}
