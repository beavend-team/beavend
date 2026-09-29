package sync_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/snapshot"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store/db"
)

type mockQuerySource struct {
	revision int64
	failRev  bool
	failComp bool
	apps     []db.GetSnapshotAppsRow
	prompts  []db.GetSnapshotPromptsRow
	policies []db.GetSnapshotPoliciesRow
	tools    []db.GetSnapshotToolsRow
	models   []db.ModelRoutings
}

func (m *mockQuerySource) GetSnapshotRevision(_ context.Context, _ db.GetSnapshotRevisionParams) (int64, error) {
	if m.failRev {
		return 0, errors.New("database connection lost")
	}
	return m.revision, nil
}

func (m *mockQuerySource) GetSnapshotEnvironment(_ context.Context, _ db.GetSnapshotEnvironmentParams) ([]byte, error) {
	return nil, nil
}

func (m *mockQuerySource) GetSnapshotApps(_ context.Context, _ db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error) {
	if m.failComp {
		return nil, errors.New("db query error")
	}
	return m.apps, nil
}

func (m *mockQuerySource) GetSnapshotPrompts(_ context.Context, _ db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error) {
	return m.prompts, nil
}

func (m *mockQuerySource) GetSnapshotPolicies(_ context.Context, _ db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error) {
	return m.policies, nil
}

func (m *mockQuerySource) GetSnapshotTools(_ context.Context, _ db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error) {
	return m.tools, nil
}

func (m *mockQuerySource) GetSnapshotModelRoutings(_ context.Context, _ db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error) {
	return m.models, nil
}

func (m *mockQuerySource) ListAllProjectEnvironments(_ context.Context) ([]db.ListAllProjectEnvironmentsRow, error) {
	if m.failRev {
		return nil, errors.New("db error")
	}
	return []db.ListAllProjectEnvironmentsRow{
		{ProjectID: "proj-poll", Environment: "prod", Revision: m.revision},
	}, nil
}

func TestSyncer_HandleEvent_Success(t *testing.T) {
	ctx := context.Background()
	reg := snapshot.NewRegistry()
	mock := &mockQuerySource{revision: 1}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	syncer := synckit.NewSyncer(reg, compiler, mock, nil, synckit.SyncerOptions{
		PollInterval: 10 * time.Millisecond,
	})

	// Initial event
	err = syncer.HandleEvent(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-1",
		Environment: "prod",
		Revision:    1,
	})
	require.NoError(t, err)

	snap, ok := reg.Get("proj-1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(1), snap.Revision)

	// Update to revision 2
	mock.revision = 2
	err = syncer.HandleEvent(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-1",
		Environment: "prod",
		Revision:    2,
	})
	require.NoError(t, err)

	snap, ok = reg.Get("proj-1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(2), snap.Revision)
}

func TestSyncer_HandleEvent_CompilationFailureRetainsExisting(t *testing.T) {
	ctx := context.Background()
	reg := snapshot.NewRegistry()

	// Initial valid revision 1
	mock := &mockQuerySource{revision: 1}
	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	syncer := synckit.NewSyncer(reg, compiler, mock, nil, synckit.SyncerOptions{})
	err = syncer.HandleEvent(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-1",
		Environment: "prod",
		Revision:    1,
	})
	require.NoError(t, err)

	initialSnap, ok := reg.Get("proj-1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(1), initialSnap.Revision)

	// Revision 2 has a broken query or invalid configuration
	mock.revision = 2
	mock.failComp = true

	err = syncer.HandleEvent(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-1",
		Environment: "prod",
		Revision:    2,
	})
	require.Error(t, err)

	// Invariant: Failed compilation must retain existing snapshot
	currentSnap, ok := reg.Get("proj-1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(1), currentSnap.Revision, "must retain previous valid snapshot")
}

func TestSyncer_PollingBackstop(t *testing.T) {
	ctx := context.Background()
	reg := snapshot.NewRegistry()

	mock := &mockQuerySource{
		revision: 1,
		prompts: []db.GetSnapshotPromptsRow{
			{
				ID:        "p1",
				PromptID:  "welcome",
				Version:   1,
				Template:  "Hello",
				IsActive:  true,
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	// Manually seed registry with rev 1
	seedSnap := core.NewSnapshot("proj-poll", "prod", 1)
	reg.Swap(seedSnap)

	syncer := synckit.NewSyncer(reg, compiler, mock, nil, synckit.SyncerOptions{})

	// Simulate control plane incrementing revision in DB without pubsub
	mock.revision = 3

	// Run polling backstop check
	syncer.PollOnce(ctx)

	// Verify registry was updated to revision 3
	updatedSnap, ok := reg.Get("proj-poll", "prod")
	require.True(t, ok)
	require.Equal(t, int64(3), updatedSnap.Revision)
}

func TestSyncer_Bootstrap(t *testing.T) {
	ctx := context.Background()
	reg := snapshot.NewRegistry()

	mock := &mockQuerySource{
		revision: 2,
		prompts: []db.GetSnapshotPromptsRow{
			{
				ID:        "p1",
				PromptID:  "welcome",
				Version:   1,
				Template:  "Hello Bootstrap",
				IsActive:  true,
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
		},
	}

	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	syncer := synckit.NewSyncer(reg, compiler, mock, nil, synckit.SyncerOptions{})

	// Bootstrap should load the environment from the database on startup
	err = syncer.Bootstrap(ctx)
	require.NoError(t, err)

	snap, ok := reg.Get("proj-poll", "prod")
	require.True(t, ok)
	require.Equal(t, int64(2), snap.Revision)
	p, ok := snap.Prompt("welcome")
	require.True(t, ok)
	require.Equal(t, "Hello Bootstrap", p.Template)
}
