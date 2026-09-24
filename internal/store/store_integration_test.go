//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/store"
	"github.com/beavend/beavend/internal/store/db"
)

func TestStore_Integration(t *testing.T) {
	dbURL := os.Getenv("BEAVEND_DATABASE_URL")
	if dbURL == "" {
		t.Skip("Skipping store integration test: BEAVEND_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Run migrations
	sqlDB, err := store.OpenDB(ctx, dbURL)
	require.NoError(t, err)
	defer sqlDB.Close()

	err = store.Migrate(ctx, sqlDB)
	require.NoError(t, err)

	// 2. Initialize Store
	st, err := store.NewStore(ctx, dbURL)
	require.NoError(t, err)
	defer st.Close()

	q := st.Queries()
	projectID := "proj-test-" + time.Now().Format("150405")
	envName := "production"

	// 3. Create Project & Environment
	proj, err := q.CreateProject(ctx, db.CreateProjectParams{
		ID:          projectID,
		Name:        "Test Project",
		Description: "Integration test project",
	})
	require.NoError(t, err)
	require.Equal(t, projectID, proj.ID)

	env, err := q.CreateEnvironment(ctx, db.CreateEnvironmentParams{
		ProjectID:   projectID,
		Name:        envName,
		Description: "Production environment",
	})
	require.NoError(t, err)
	require.Equal(t, envName, env.Name)

	// 4. Create Revision
	rev, err := q.IncrementProjectRevision(ctx, db.IncrementProjectRevisionParams{
		ProjectID:   projectID,
		Environment: envName,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rev)

	// 5. Create Prompt and Active Version
	promptID := "p-welcome"
	p, err := q.CreatePrompt(ctx, db.CreatePromptParams{
		ID:            promptID,
		ProjectID:     projectID,
		Environment:   envName,
		Name:          "welcome_prompt",
		Description:   "Greets user",
		ActiveVersion: 1,
	})
	require.NoError(t, err)
	require.Equal(t, promptID, p.ID)

	pv, err := q.CreatePromptVersion(ctx, db.CreatePromptVersionParams{
		ID:          "pv-welcome-1",
		PromptID:    promptID,
		ProjectID:   projectID,
		Environment: envName,
		Version:     1,
		Template:    "Hello {{name}}",
		Variables:   []byte(`["name"]`),
		IsActive:    true,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), pv.Version)

	// 6. Query Snapshot Prompts
	snapshotPrompts, err := q.GetSnapshotPrompts(ctx, db.GetSnapshotPromptsParams{
		ProjectID:   projectID,
		Environment: envName,
	})
	require.NoError(t, err)
	require.Len(t, snapshotPrompts, 1)
	require.Equal(t, "Hello {{name}}", snapshotPrompts[0].Template)

	// 7. Cleanup project
	err = q.DeleteProject(ctx, projectID)
	require.NoError(t, err)
}
