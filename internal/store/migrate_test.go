package store_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/store"
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

	foundModelRoutes := false
	for _, file := range files {
		if file.Name() == "00009_model_routes.sql" {
			foundModelRoutes = true
			content, err := fs.ReadFile(store.EmbedMigrations, "migrations/"+file.Name())
			require.NoError(t, err)
			require.Contains(t, string(content), "-- +goose Up")
			require.Contains(t, string(content), "ALTER TABLE ai_apps ADD COLUMN IF NOT EXISTS model_routes JSONB")
			require.Contains(t, string(content), "ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS input_modalities")
			require.Contains(t, string(content), "ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS output_modalities")
			require.Contains(t, string(content), "-- +goose Down")
		}
	}
	require.True(t, foundModelRoutes, "expected to find 00009_model_routes.sql")
}
