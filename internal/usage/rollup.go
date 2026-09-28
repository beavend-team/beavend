package usage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beavend/beavend/internal/store/db"
)

// AggregatedUsageRow represents summary metrics for a grouped aggregation bucket.
type AggregatedUsageRow struct {
	ProjectID    string
	Environment  string
	AIAppID      string
	UserID       string
	LogicalModel string
	Provider     string
	RequestCount int64
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CostUSD      float64
	ErrorCount   int64
}

// RollupStore defines database operations required for aggregating usage events into rollups.
type RollupStore interface {
	AggregateWindow(ctx context.Context, start, end time.Time) ([]AggregatedUsageRow, error)
	UpsertRollup(ctx context.Context, row AggregatedUsageRow, granularity string, bucketStart time.Time) error
}

// PostgresRollupStore implements RollupStore backed by sqlc queries.
type PostgresRollupStore struct {
	queries *db.Queries
}

// NewPostgresRollupStore constructs a PostgresRollupStore.
func NewPostgresRollupStore(queries *db.Queries) *PostgresRollupStore {
	return &PostgresRollupStore{queries: queries}
}

// AggregateWindow aggregates usage events within [start, end).
func (s *PostgresRollupStore) AggregateWindow(ctx context.Context, start, end time.Time) ([]AggregatedUsageRow, error) {
	if s.queries == nil {
		return nil, fmt.Errorf("postgres rollup store: uninitialized queries")
	}

	params := db.AggregateUsageEventsForWindowParams{
		CreatedAt:   timeToTimestamptz(start),
		CreatedAt_2: timeToTimestamptz(end),
	}

	rows, err := s.queries.AggregateUsageEventsForWindow(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage events for window [%v, %v): %w", start, end, err)
	}

	result := make([]AggregatedUsageRow, len(rows))
	for i, r := range rows {
		result[i] = AggregatedUsageRow{
			ProjectID:    r.ProjectID,
			Environment:  r.Environment,
			AIAppID:      r.AiAppID,
			UserID:       r.UserID,
			LogicalModel: r.LogicalModel,
			Provider:     r.Provider,
			RequestCount: r.RequestCount,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			TotalTokens:  r.TotalTokens,
			CostUSD:      numericToFloat(r.CostUsd),
		}
	}

	return result, nil
}

// UpsertRollup inserts or updates a usage rollup bucket.
func (s *PostgresRollupStore) UpsertRollup(ctx context.Context, row AggregatedUsageRow, granularity string, bucketStart time.Time) error {
	if s.queries == nil {
		return fmt.Errorf("postgres rollup store: uninitialized queries")
	}

	params := db.UpsertUsageRollupParams{
		ProjectID:    row.ProjectID,
		Environment:  row.Environment,
		AiAppID:      row.AIAppID,
		UserID:       row.UserID,
		LogicalModel: row.LogicalModel,
		Provider:     row.Provider,
		Granularity:  granularity,
		BucketStart:  timeToTimestamptz(bucketStart),
		RequestCount: row.RequestCount,
		InputTokens:  row.InputTokens,
		OutputTokens: row.OutputTokens,
		TotalTokens:  row.TotalTokens,
		CostUsd:      floatToNumeric(row.CostUSD),
		ErrorCount:   row.ErrorCount,
	}

	if err := s.queries.UpsertUsageRollup(ctx, params); err != nil {
		return fmt.Errorf("upsert usage rollup: %w", err)
	}

	return nil
}

func numericToFloat(num pgtype.Numeric) float64 {
	f, err := num.Float64Value()
	if err == nil && f.Valid {
		return f.Float64
	}
	return 0.0
}

// RollupAggregatorConfig configures the periodic rollup aggregator.
type RollupAggregatorConfig struct {
	Interval time.Duration
}

// DefaultRollupAggregatorConfig provides default rollup settings.
func DefaultRollupAggregatorConfig() RollupAggregatorConfig {
	return RollupAggregatorConfig{
		Interval: 1 * time.Minute,
	}
}

// RollupAggregator is a background service that aggregates raw usage_events into usage_rollups.
type RollupAggregator struct {
	store   RollupStore
	cfg     RollupAggregatorConfig
	stopCh  chan struct{}
	started atomic.Bool
	mu      sync.Mutex
}

// NewRollupAggregator creates a new RollupAggregator.
func NewRollupAggregator(store RollupStore, cfg RollupAggregatorConfig) *RollupAggregator {
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Minute
	}
	return &RollupAggregator{
		store:  store,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// AggregateHourly aggregates usage events for the hour window containing targetTime.
func (a *RollupAggregator) AggregateHourly(ctx context.Context, targetTime time.Time) error {
	hourStart := targetTime.UTC().Truncate(time.Hour)
	hourEnd := hourStart.Add(time.Hour)

	rows, err := a.store.AggregateWindow(ctx, hourStart, hourEnd)
	if err != nil {
		return fmt.Errorf("aggregate hourly: %w", err)
	}

	for _, row := range rows {
		if err := a.store.UpsertRollup(ctx, row, "hour", hourStart); err != nil {
			return fmt.Errorf("upsert hourly rollup: %w", err)
		}
	}

	return nil
}

// AggregateDaily aggregates usage events for the day window containing targetTime.
func (a *RollupAggregator) AggregateDaily(ctx context.Context, targetTime time.Time) error {
	t := targetTime.UTC()
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := a.store.AggregateWindow(ctx, dayStart, dayEnd)
	if err != nil {
		return fmt.Errorf("aggregate daily: %w", err)
	}

	for _, row := range rows {
		if err := a.store.UpsertRollup(ctx, row, "day", dayStart); err != nil {
			return fmt.Errorf("upsert daily rollup: %w", err)
		}
	}

	return nil
}

// RunOnce performs an aggregation run for current time and recent lookback window.
func (a *RollupAggregator) RunOnce(ctx context.Context) error {
	now := time.Now().UTC()

	// Aggregate current hour and previous hour (to capture any slightly delayed events)
	if err := a.AggregateHourly(ctx, now); err != nil {
		return err
	}
	if err := a.AggregateHourly(ctx, now.Add(-1*time.Hour)); err != nil {
		return err
	}

	// Aggregate today's daily rollup
	if err := a.AggregateDaily(ctx, now); err != nil {
		return err
	}

	return nil
}

// Start runs the periodic aggregation loop until ctx is cancelled.
func (a *RollupAggregator) Start(ctx context.Context) {
	if !a.started.CompareAndSwap(false, true) {
		return
	}
	defer a.started.Store(false)

	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()

	// Initial run on startup
	if err := a.RunOnce(ctx); err != nil {
		slog.WarnContext(ctx, "initial rollup aggregation encountered error", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			if err := a.RunOnce(ctx); err != nil {
				slog.WarnContext(ctx, "periodic rollup aggregation failed", "error", err)
			}
		}
	}
}

// Stop halts the periodic aggregation loop.
func (a *RollupAggregator) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
}
