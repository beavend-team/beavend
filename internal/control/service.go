package control

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/control/auth"
	"github.com/beavend/beavend/internal/policy"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store/db"
)

// Service coordinates administrative business logic, database queries,
// configuration snapshot invalidations, and audit log generation.
type Service struct {
	queries        *db.Queries
	publisher      synckit.Publisher
	auditor        audit.Auditor
	policyCompiler *policy.Compiler
	masterKey      []byte
}

// NewService constructs a new control plane service.
func NewService(
	queries *db.Queries,
	publisher synckit.Publisher,
	auditor audit.Auditor,
	masterKey []byte,
) *Service {
	compiler, _ := policy.NewCompiler()
	return &Service{
		queries:        queries,
		publisher:      publisher,
		auditor:        auditor,
		policyCompiler: compiler,
		masterKey:      masterKey,
	}
}

// InvalidateSnapshot bumps the project revision in PostgreSQL and publishes
// a RevisionEvent to notify runtime nodes to atomically compile a new snapshot.
func (s *Service) InvalidateSnapshot(ctx context.Context, projectID, environment string) (int64, error) {
	if s.queries == nil {
		return 0, nil
	}

	rev, err := s.queries.IncrementProjectRevision(ctx, db.IncrementProjectRevisionParams{
		ProjectID:   projectID,
		Environment: environment,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to increment project revision in database",
			"project_id", projectID,
			"env", environment,
			"error", err,
		)
		return 0, fmt.Errorf("increment project revision: %w", err)
	}

	if s.publisher != nil {
		event := synckit.RevisionEvent{
			ProjectID:   projectID,
			Environment: environment,
			Revision:    rev,
		}
		if err := s.publisher.Publish(ctx, event); err != nil {
			slog.WarnContext(ctx, "failed to publish snapshot invalidation notification",
				"project_id", projectID,
				"env", environment,
				"revision", rev,
				"error", err,
			)
		} else {
			slog.InfoContext(ctx, "published snapshot revision invalidation notification",
				"project_id", projectID,
				"env", environment,
				"revision", rev,
			)
		}
	}

	return rev, nil
}

// RecordAudit logs a mutation action performed by an administrator.
func (s *Service) RecordAudit(
	ctx context.Context,
	projectID, environment, action, resourceType, resourceID string,
	payload any,
) {
	if s.auditor == nil {
		return
	}

	userID := "system"
	if u, ok := auth.GetAdminUser(ctx); ok && u != nil {
		userID = u.ID
	}

	_ = s.auditor.Record(ctx, audit.Entry{
		ProjectID:    projectID,
		Environment:  environment,
		UserID:       userID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Payload:      payload,
	})
}
