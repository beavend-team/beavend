package control

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/configcode"
	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/control/auth"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/beavend/beavend/internal/tools"
)

// Service coordinates administrative business logic, database queries,
// configuration snapshot invalidations, and audit log generation.
type Service struct {
	queries        *db.Queries
	publisher      synckit.Publisher
	auditor        audit.Auditor
	policyCompiler *policy.Compiler
	masterKey      []byte
	cacheStore     core.CacheStore
	memoryProvider core.MemoryProvider
	threadStore    core.ThreadStore
	configEngine   *configcode.Engine
	searxngClient  *tools.SearXNGClient
}

// NewService constructs a new control plane service.
func NewService(
	queries *db.Queries,
	publisher synckit.Publisher,
	auditor audit.Auditor,
	masterKey []byte,
) *Service {
	compiler, _ := policy.NewCompiler()
	var engine *configcode.Engine
	if queries != nil {
		engine = configcode.NewEngine(queries, publisher, masterKey)
	}
	return &Service{
		queries:        queries,
		publisher:      publisher,
		auditor:        auditor,
		policyCompiler: compiler,
		masterKey:      masterKey,
		configEngine:   engine,
	}
}

// SetConfigEngine sets or overrides the configcode Engine.
func (s *Service) SetConfigEngine(engine *configcode.Engine) {
	s.configEngine = engine
}

// ConfigEngine returns the configured configcode Engine.
func (s *Service) ConfigEngine() *configcode.Engine {
	return s.configEngine
}

// SetSearXNGClient sets the SearXNG client for search status and test executions.
func (s *Service) SetSearXNGClient(client *tools.SearXNGClient) {
	s.searxngClient = client
}

// SearXNGClient returns the configured SearXNG client.
func (s *Service) SearXNGClient() *tools.SearXNGClient {
	return s.searxngClient
}

// SetCacheStore sets the cache store for subject cache purges.
func (s *Service) SetCacheStore(store core.CacheStore) {
	s.cacheStore = store
}

// PurgeSubjectCache purges cached responses and tool results for a subject.
func (s *Service) PurgeSubjectCache(ctx context.Context, projectID, env, subject string) error {
	if s.cacheStore == nil || subject == "" {
		return nil
	}
	if purger, ok := s.cacheStore.(cache.PurgeableCacheStore); ok {
		return purger.DeleteSubjectKeys(ctx, projectID, env, subject)
	}
	return nil
}

// SetMemoryProvider sets the memory provider for privacy subject purging.
func (s *Service) SetMemoryProvider(provider core.MemoryProvider) {
	s.memoryProvider = provider
}

// SetThreadStore sets the thread store for privacy subject purging.
func (s *Service) SetThreadStore(store core.ThreadStore) {
	s.threadStore = store
}

// PurgeSubjectMemory purges semantic memories and conversation threads for a subject.
func (s *Service) PurgeSubjectMemory(ctx context.Context, projectID, env, issuer, subject string) error {
	var errs []error

	if s.memoryProvider != nil {
		if err := s.memoryProvider.DeleteBySubject(ctx, projectID, issuer, subject); err != nil {
			errs = append(errs, fmt.Errorf("memory provider purge: %w", err))
		}
	} else if s.queries != nil {
		if err := s.queries.DeleteSemanticMemoriesBySubject(ctx, db.DeleteSemanticMemoriesBySubjectParams{
			ProjectID: projectID,
			Issuer:    issuer,
			Subject:   subject,
		}); err != nil {
			errs = append(errs, fmt.Errorf("database semantic memories purge: %w", err))
		}
	}

	if s.threadStore != nil {
		if err := s.threadStore.DeleteThreadsBySubject(ctx, projectID, env, issuer, subject); err != nil {
			errs = append(errs, fmt.Errorf("thread store purge: %w", err))
		}
	} else if s.queries != nil {
		if err := s.queries.DeleteConversationThreadsBySubject(ctx, db.DeleteConversationThreadsBySubjectParams{
			ProjectID:   projectID,
			Environment: env,
			Issuer:      issuer,
			Subject:     subject,
		}); err != nil {
			errs = append(errs, fmt.Errorf("database conversation threads purge: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("purge subject memory errors: %v", errs)
	}
	return nil
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
