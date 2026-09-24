package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const PostgresNotificationChannel = "beavend_revisions"

// PostgresPublisher broadcasts revision invalidation events via PostgreSQL NOTIFY.
type PostgresPublisher struct {
	pool *pgxpool.Pool
}

// NewPostgresPublisher creates a new PostgreSQL NOTIFY publisher.
func NewPostgresPublisher(pool *pgxpool.Pool) *PostgresPublisher {
	return &PostgresPublisher{pool: pool}
}

// NewPostgresPool creates a pgxpool.Pool helper for PostgreSQL connections.
func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, databaseURL)
}

// Publish broadcasts the revision event via pg_notify.
func (p *PostgresPublisher) Publish(ctx context.Context, event RevisionEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal revision event: %w", err)
	}

	_, err = p.pool.Exec(ctx, "SELECT pg_notify($1, $2)", PostgresNotificationChannel, string(data))
	if err != nil {
		return fmt.Errorf("execute pg_notify: %w", err)
	}

	return nil
}

// MultiPublisher publishes revision events to multiple publishers (e.g. Redis + Postgres).
type MultiPublisher struct {
	publishers []Publisher
}

// NewMultiPublisher initializes a MultiPublisher.
func NewMultiPublisher(publishers ...Publisher) *MultiPublisher {
	var valid []Publisher
	for _, p := range publishers {
		if p != nil {
			valid = append(valid, p)
		}
	}
	return &MultiPublisher{publishers: valid}
}

// Publish sends the event to all configured publishers.
func (m *MultiPublisher) Publish(ctx context.Context, event RevisionEvent) error {
	for _, p := range m.publishers {
		if err := p.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
