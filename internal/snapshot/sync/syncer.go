package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cortexbase/cortexbase/internal/snapshot"
)

// SyncerOptions configures the Syncer.
type SyncerOptions struct {
	PollInterval time.Duration
	DatabaseURL  string
	Logger       *slog.Logger
}

// Syncer manages real-time configuration revision synchronization using
// Redis pub/sub with PostgreSQL LISTEN/NOTIFY fallback and periodic polling backstop.
type Syncer struct {
	registry     *snapshot.Registry
	compiler     *snapshot.Compiler
	queries      snapshot.QuerySource
	redisClient  redis.UniversalClient
	databaseURL  string
	pollInterval time.Duration
	logger       *slog.Logger
}

// NewSyncer creates a new configuration revision Syncer.
func NewSyncer(
	registry *snapshot.Registry,
	compiler *snapshot.Compiler,
	queries snapshot.QuerySource,
	redisClient redis.UniversalClient,
	opts SyncerOptions,
) *Syncer {
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Syncer{
		registry:     registry,
		compiler:     compiler,
		queries:      queries,
		redisClient:  redisClient,
		databaseURL:  opts.DatabaseURL,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

// Start begins listening to pub/sub channels and initiates the periodic polling backstop.
// It blocks until the context is canceled.
func (s *Syncer) Start(ctx context.Context) error {
	errCh := make(chan error, 3)

	// Start Redis pub/sub listener if redis is provided
	if s.redisClient != nil {
		go func() {
			if err := s.listenPubSub(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error("pubsub listener stopped with error", "error", err)
				errCh <- err
			}
		}()
	}

	// Start PostgreSQL LISTEN/NOTIFY listener if database URL is provided
	if s.databaseURL != "" {
		pgListener := NewPostgresListener(s.databaseURL, s.HandleEvent, s.logger)
		go func() {
			if err := pgListener.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error("postgres listener stopped with error", "error", err)
				errCh <- err
			}
		}()
	}

	// Start Polling Backstop
	go func() {
		s.runPollingBackstop(ctx)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// HandleEvent processes an incoming RevisionEvent: compiles the snapshot and swaps it atomically.
// If compilation fails, the swap is rejected, an error is logged, and the previous snapshot is retained.
func (s *Syncer) HandleEvent(ctx context.Context, event RevisionEvent) error {
	// Check current snapshot revision to avoid redundant recompilation
	current, exists := s.registry.Get(event.ProjectID, event.Environment)
	if exists && current.Revision >= event.Revision {
		return nil
	}

	s.logger.Info("compiling new snapshot revision",
		"project_id", event.ProjectID,
		"env", event.Environment,
		"new_revision", event.Revision,
	)

	newSnap, err := s.compiler.Compile(ctx, event.ProjectID, event.Environment)
	if err != nil {
		s.logger.Error("failed to compile snapshot revision; retaining existing snapshot",
			"project_id", event.ProjectID,
			"env", event.Environment,
			"attempted_revision", event.Revision,
			"error", err,
		)
		return fmt.Errorf("compile snapshot: %w", err)
	}

	s.registry.Swap(newSnap)
	s.logger.Info("successfully swapped snapshot revision",
		"project_id", event.ProjectID,
		"env", event.Environment,
		"revision", newSnap.Revision,
	)

	return nil
}

func (s *Syncer) listenPubSub(ctx context.Context) error {
	pubsub := s.redisClient.PSubscribe(ctx, "cfg:*")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var event RevisionEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				s.logger.Error("invalid revision event payload", "channel", msg.Channel, "error", err)
				continue
			}

			if err := s.HandleEvent(ctx, event); err != nil {
				// Compilation failure logged in HandleEvent, keep listener running
				continue
			}
		}
	}
}

func (s *Syncer) runPollingBackstop(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.PollOnce(ctx)
		}
	}
}

// Bootstrap pre-compiles and loads all projects and environments from the database into the in-memory registry.
// This ensures that the runtime hot-path performs zero database queries starting from the very first incoming request.
func (s *Syncer) Bootstrap(ctx context.Context) error {
	if s.queries == nil {
		return nil
	}

	envs, err := s.queries.ListAllProjectEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("list project environments for bootstrap: %w", err)
	}

	for _, item := range envs {
		if err := s.HandleEvent(ctx, RevisionEvent{
			ProjectID:   item.ProjectID,
			Environment: item.Environment,
			Revision:    item.Revision,
		}); err != nil {
			s.logger.Error("failed to bootstrap snapshot for environment",
				"project_id", item.ProjectID,
				"env", item.Environment,
				"error", err,
			)
		}
	}

	return nil
}

// PollOnce inspects all active environments in the database against the in-memory registry
// to detect any missed pub/sub messages or newly created projects.
func (s *Syncer) PollOnce(ctx context.Context) {
	if s.queries == nil {
		return
	}

	envs, err := s.queries.ListAllProjectEnvironments(ctx)
	if err != nil {
		s.logger.Error("polling backstop failed to list environments", "error", err)
		return
	}

	for _, item := range envs {
		snap, exists := s.registry.Get(item.ProjectID, item.Environment)
		if !exists || item.Revision > snap.Revision {
			var currentRev int64
			if exists {
				currentRev = snap.Revision
			}
			s.logger.Info("polling backstop detected revision lag or new environment",
				"project_id", item.ProjectID,
				"env", item.Environment,
				"current_revision", currentRev,
				"db_revision", item.Revision,
			)

			_ = s.HandleEvent(ctx, RevisionEvent{
				ProjectID:   item.ProjectID,
				Environment: item.Environment,
				Revision:    item.Revision,
			})
		}
	}
}
