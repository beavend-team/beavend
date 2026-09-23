package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cortexbase/cortexbase/internal/store/db"
)

// Store encapsulates the database pool and generated queries.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewStore initializes a new Store with a pgx connection pool.
func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pgx pool: %w", err)
	}

	return &Store{
		pool:    pool,
		queries: db.New(pool),
	}, nil
}

// Pool returns the underlying pgxpool.Pool.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Queries returns the sqlc generated queries.
func (s *Store) Queries() *db.Queries {
	return s.queries
}

// Close closes the underlying pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
