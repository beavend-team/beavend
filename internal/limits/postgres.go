package limits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/store/db"
)

// PostgresLimitStoreOptions configures PostgresLimitStore.
type PostgresLimitStoreOptions struct {
	FailClosed bool
	NowFunc    func() time.Time
}

// PostgresLimitStore is a database-backed rate limit store using PostgreSQL.
type PostgresLimitStore struct {
	pool       *pgxpool.Pool
	failClosed bool
	nowFunc    func() time.Time
}

// NewPostgresLimitStore creates a new PostgresLimitStore.
func NewPostgresLimitStore(pool *pgxpool.Pool, opts *PostgresLimitStoreOptions) *PostgresLimitStore {
	failClosed := true
	nowFn := time.Now
	if opts != nil {
		failClosed = opts.FailClosed
		if opts.NowFunc != nil {
			nowFn = opts.NowFunc
		}
	}

	return &PostgresLimitStore{
		pool:       pool,
		failClosed: failClosed,
		nowFunc:    nowFn,
	}
}

// CheckAndDebit evaluates multiple limit rules atomically within a database transaction.
func (s *PostgresLimitStore) CheckAndDebit(ctx context.Context, key string, rules []core.LimitRule, requestedTokens int64) (bool, time.Duration, error) {
	if len(rules) == 0 {
		return true, 0, nil
	}

	if s.pool == nil {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: nil postgres pool", ErrLimitStoreUnavailable)
		}
		slog.WarnContext(ctx, "nil postgres pool, failing open")
		return true, 0, nil
	}

	now := s.nowFunc().UTC()
	nowSec := now.Unix()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: begin tx: %w", ErrLimitStoreUnavailable, err)
		}
		slog.WarnContext(ctx, "postgres begin tx failed, failing open", "error", err)
		return true, 0, nil
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe if already committed

	queries := db.New(tx)
	var maxRetry time.Duration
	allowed := true

	type ruleDebit struct {
		res         string
		windowSec   int32
		bucketStart time.Time
		cost        int64
		expiresAt   time.Time
	}
	debits := make([]ruleDebit, len(rules))

	// First pass: check all rules
	for i, r := range rules {
		windowSec := int64(r.Window.Seconds())
		if windowSec <= 0 {
			windowSec = 1
		}
		cost := int64(1)
		if r.Resource == "tokens" {
			cost = requestedTokens
		}

		bucketStartSec := (nowSec / windowSec) * windowSec
		bucketStart := time.Unix(bucketStartSec, 0).UTC()
		prevBucketStart := time.Unix(bucketStartSec-windowSec, 0).UTC()
		expiresAt := bucketStart.Add(time.Duration(windowSec*2) * time.Second)

		debits[i] = ruleDebit{
			res:         r.Resource,
			windowSec:   int32(windowSec),
			bucketStart: bucketStart,
			cost:        cost,
			expiresAt:   expiresAt,
		}

		currVal, err := queries.GetRateLimitCounter(ctx, db.GetRateLimitCounterParams{
			Key:           key,
			Resource:      r.Resource,
			WindowSeconds: int32(windowSec),
			BucketStart:   pgtype.Timestamptz{Time: bucketStart, Valid: true},
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			if s.failClosed {
				return false, 0, fmt.Errorf("%w: get current counter: %w", ErrLimitStoreUnavailable, err)
			}
			return true, 0, nil
		}

		prevVal, err := queries.GetRateLimitCounter(ctx, db.GetRateLimitCounterParams{
			Key:           key,
			Resource:      r.Resource,
			WindowSeconds: int32(windowSec),
			BucketStart:   pgtype.Timestamptz{Time: prevBucketStart, Valid: true},
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			if s.failClosed {
				return false, 0, fmt.Errorf("%w: get previous counter: %w", ErrLimitStoreUnavailable, err)
			}
			return true, 0, nil
		}

		elapsed := nowSec % windowSec
		ruleAllowed, retry := EstimateSlidingWindow(prevVal, currVal, cost, r.Max, windowSec, elapsed)
		if !ruleAllowed {
			allowed = false
			if retry > maxRetry {
				maxRetry = retry
			}
		}
	}

	if !allowed {
		return false, maxRetry, nil
	}

	// Second pass: debit all rules
	for _, d := range debits {
		_, err := queries.UpsertRateLimitCounter(ctx, db.UpsertRateLimitCounterParams{
			Key:           key,
			Resource:      d.res,
			WindowSeconds: d.windowSec,
			BucketStart:   pgtype.Timestamptz{Time: d.bucketStart, Valid: true},
			CurrentCount:  d.cost,
			ExpiresAt:     pgtype.Timestamptz{Time: d.expiresAt, Valid: true},
		})
		if err != nil {
			if s.failClosed {
				return false, 0, fmt.Errorf("%w: upsert counter: %w", ErrLimitStoreUnavailable, err)
			}
			return true, 0, nil
		}
	}

	if err := tx.Commit(ctx); err != nil {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: commit tx: %w", ErrLimitStoreUnavailable, err)
		}
		return true, 0, nil
	}

	return true, 0, nil
}

// ReconcileTokens adjusts token counters by delta in PostgreSQL.
func (s *PostgresLimitStore) ReconcileTokens(ctx context.Context, key string, estimatedTokens, actualTokens int64) error {
	delta := actualTokens - estimatedTokens
	if delta == 0 {
		return nil
	}

	if s.pool == nil {
		if s.failClosed {
			return fmt.Errorf("%w: nil postgres pool", ErrLimitStoreUnavailable)
		}
		return nil
	}

	now := s.nowFunc().UTC()

	// Find active token windows for this user key
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT window_seconds
		FROM rate_limit_counters
		WHERE key = $1 AND resource = 'tokens' AND expires_at > $2
	`, key, now)
	if err != nil {
		if s.failClosed {
			return fmt.Errorf("%w: query token windows: %w", ErrLimitStoreUnavailable, err)
		}
		return nil
	}
	defer rows.Close()

	var windows []int32
	for rows.Next() {
		var w int32
		if err := rows.Scan(&w); err != nil {
			continue
		}
		windows = append(windows, w)
	}

	queries := db.New(s.pool)
	nowSec := now.Unix()

	for _, w := range windows {
		windowSec := int64(w)
		bucketStartSec := (nowSec / windowSec) * windowSec
		bucketStart := time.Unix(bucketStartSec, 0).UTC()

		_ = queries.AdjustRateLimitCounter(ctx, db.AdjustRateLimitCounterParams{
			Key:           key,
			Resource:      "tokens",
			WindowSeconds: w,
			BucketStart:   pgtype.Timestamptz{Time: bucketStart, Valid: true},
			CurrentCount:  delta,
		})
	}

	return nil
}
