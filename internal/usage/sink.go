package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/beavend/beavend/internal/store/db"
)

// Sink defines the destination interface for flushed logs and usage batches.
type Sink interface {
	WriteBatch(ctx context.Context, logs []LogEvent, usage []Event) error
}

// InMemorySink collects written batches in memory for testing or standalone modes.
type InMemorySink struct {
	mu         sync.Mutex
	logs       []LogEvent
	usage      []Event
	writeCalls int
}

// NewInMemorySink constructs an InMemorySink.
func NewInMemorySink() *InMemorySink {
	return &InMemorySink{
		logs:  make([]LogEvent, 0),
		usage: make([]Event, 0),
	}
}

// WriteBatch stores the events in memory.
func (s *InMemorySink) WriteBatch(_ context.Context, logs []LogEvent, usage []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCalls++
	s.logs = append(s.logs, logs...)
	s.usage = append(s.usage, usage...)
	return nil
}

// Logs returns a copy of captured log events.
func (s *InMemorySink) Logs() []LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LogEvent, len(s.logs))
	copy(out, s.logs)
	return out
}

// Usage returns a copy of captured usage events.
func (s *InMemorySink) Usage() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.usage))
	copy(out, s.usage)
	return out
}

// CallCount returns how many times WriteBatch was invoked.
func (s *InMemorySink) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeCalls
}

// PostgresSink persists batches into PostgreSQL partitioned tables.
type PostgresSink struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPostgresSink constructs a PostgresSink.
func NewPostgresSink(pool *pgxpool.Pool, queries *db.Queries) *PostgresSink {
	return &PostgresSink{
		pool:    pool,
		queries: queries,
	}
}

// WriteBatch persists log events and usage events in a single transaction.
func (s *PostgresSink) WriteBatch(ctx context.Context, logs []LogEvent, usage []Event) error {
	if len(logs) == 0 && len(usage) == 0 {
		return nil
	}

	if s.pool == nil || s.queries == nil {
		return fmt.Errorf("postgres sink: uninitialized pool or queries")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	qtx := s.queries.WithTx(tx)

	for _, log := range logs {
		var toolCallsBytes []byte
		if len(log.ToolCalls) == 0 {
			toolCallsBytes = []byte("[]")
		} else {
			var err error
			toolCallsBytes, err = json.Marshal(log.ToolCalls)
			if err != nil {
				toolCallsBytes = []byte("[]")
			}
		}

		var metaBytes []byte
		if len(log.Metadata) == 0 {
			metaBytes = []byte("{}")
		} else {
			var err error
			metaBytes, err = json.Marshal(log.Metadata)
			if err != nil {
				metaBytes = []byte("{}")
			}
		}

		params := db.InsertRequestLogParams{
			ID:              log.ID,
			RequestID:       log.RequestID,
			ProjectID:       log.ProjectID,
			Environment:     log.Environment,
			AiAppID:         log.AIAppID,
			UserID:          log.UserID,
			UserSubject:     log.UserSubject,
			UserIssuer:      log.UserIssuer,
			LogicalModel:    log.LogicalModel,
			Provider:        log.Provider,
			ModelDeployment: log.ModelDeployment,
			Status:          log.Status,
			StatusCode:      int32(log.StatusCode),
			LatencyMs:       log.LatencyMS,
			InputTokens:     int32(log.InputTokens),
			OutputTokens:    int32(log.OutputTokens),
			TotalTokens:     int32(log.TotalTokens),
			CostUsd:         floatToNumeric(log.CostUSD),
			IsStreaming:     log.IsStreaming,
			ToolCalls:       toolCallsBytes,
			ErrorCode:       log.ErrorCode,
			ErrorMessage:    log.ErrorMessage,
			Metadata:        metaBytes,
			CreatedAt:       timeToTimestamptz(log.CreatedAt),
		}

		if err := qtx.InsertRequestLog(ctx, params); err != nil {
			return fmt.Errorf("insert request log %s: %w", log.ID, err)
		}
	}

	for _, u := range usage {
		params := db.InsertUsageEventParams{
			ID:           u.ID,
			RequestID:    u.RequestID,
			ProjectID:    u.ProjectID,
			Environment:  u.Environment,
			AiAppID:      u.AIAppID,
			UserID:       u.UserID,
			LogicalModel: u.LogicalModel,
			Provider:     u.Provider,
			InputTokens:  int32(u.InputTokens),
			OutputTokens: int32(u.OutputTokens),
			TotalTokens:  int32(u.TotalTokens),
			CostUsd:      floatToNumeric(u.CostUSD),
			CreatedAt:    timeToTimestamptz(u.CreatedAt),
		}

		if err := qtx.InsertUsageEvent(ctx, params); err != nil {
			return fmt.Errorf("insert usage event %s: %w", u.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit batch transaction: %w", err)
	}

	return nil
}

func floatToNumeric(val float64) pgtype.Numeric {
	if math.IsNaN(val) || math.IsInf(val, 0) || val < 0 {
		val = 0.0
	}
	var num pgtype.Numeric
	_ = num.Scan(strconv.FormatFloat(val, 'f', 6, 64))
	return num
}

func timeToTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		t = time.Now()
	}
	return pgtype.Timestamptz{
		Time:  t,
		Valid: true,
	}
}
