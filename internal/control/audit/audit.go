package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/store/db"
)

// Entry defines an immutable audit log record.
type Entry struct {
	ProjectID    string `json:"project_id"`
	Environment  string `json:"environment"`
	UserID       string `json:"user_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Payload      any    `json:"payload"`
}

// Auditor defines the interface for recording administrative audit log events.
type Auditor interface {
	Record(ctx context.Context, entry Entry) error
}

// StoreAuditor persists audit entries directly into PostgreSQL via sqlc Queries.
type StoreAuditor struct {
	queries *db.Queries
}

// NewStoreAuditor constructs a PostgreSQL-backed auditor.
func NewStoreAuditor(queries *db.Queries) *StoreAuditor {
	return &StoreAuditor{queries: queries}
}

// Record saves the audit event to the database.
func (a *StoreAuditor) Record(ctx context.Context, entry Entry) error {
	if a.queries == nil {
		slog.WarnContext(ctx, "audit queries not initialized, skipping persistence",
			slog.String("action", entry.Action),
			slog.String("resource", entry.ResourceType),
		)
		return nil
	}

	payloadBytes, err := json.Marshal(entry.Payload)
	if err != nil {
		payloadBytes = []byte("{}")
	}

	_, err = a.queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ProjectID:    entry.ProjectID,
		Environment:  entry.Environment,
		UserID:       entry.UserID,
		Action:       entry.Action,
		ResourceType: entry.ResourceType,
		ResourceID:   entry.ResourceID,
		Payload:      payloadBytes,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to write audit log entry",
			slog.String("action", entry.Action),
			slog.String("resource_id", entry.ResourceID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("create audit log entry: %w", err)
	}

	return nil
}

// InMemoryAuditor stores audit events in memory (used for unit tests and local mock mode).
type InMemoryAuditor struct {
	mu      sync.RWMutex
	entries []EntryWithTime
}

// EntryWithTime wraps Entry with recording timestamp.
type EntryWithTime struct {
	Entry
	Timestamp time.Time
}

// NewInMemoryAuditor creates an in-memory auditor.
func NewInMemoryAuditor() *InMemoryAuditor {
	return &InMemoryAuditor{
		entries: make([]EntryWithTime, 0),
	}
}

// Record adds an entry to in-memory history.
func (m *InMemoryAuditor) Record(_ context.Context, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, EntryWithTime{
		Entry:     entry,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

// Entries returns a copy of all recorded entries.
func (m *InMemoryAuditor) Entries() []EntryWithTime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]EntryWithTime, len(m.entries))
	copy(out, m.entries)
	return out
}
