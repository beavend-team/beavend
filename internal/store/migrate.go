package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	// Register standard database/sql driver for pgx
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var EmbedMigrations embed.FS

// Migrate runs all pending database migrations against the provided db.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("run goose migrations: %w", err)
	}

	return nil
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.DownContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("rollback goose migration: %w", err)
	}

	return nil
}

// OpenDB opens a standard sql.DB connection for migrations and administration.
func OpenDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
