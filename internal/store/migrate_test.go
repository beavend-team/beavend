package store_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cortexbase/cortexbase/internal/store"
)

func TestEmbeddedMigrations(t *testing.T) {
	files, err := fs.ReadDir(store.EmbedMigrations, "migrations")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	foundSchema := false
	for _, file := range files {
		if file.Name() == "00001_initial_schema.sql" {
			foundSchema = true
			content, err := fs.ReadFile(store.EmbedMigrations, "migrations/"+file.Name())
			require.NoError(t, err)
			require.Contains(t, string(content), "-- +goose Up")
			require.Contains(t, string(content), "CREATE TABLE IF NOT EXISTS projects")
			require.Contains(t, string(content), "-- +goose Down")
		}
	}
	require.True(t, foundSchema, "expected to find 00001_initial_schema.sql")
}
