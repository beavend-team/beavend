//go:build integration

package sync_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/snapshot"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
)

func TestSyncer_RedisIntegration(t *testing.T) {
	redisURL := os.Getenv("BEAVEND_REDIS_URL")
	if redisURL == "" {
		t.Skip("Skipping Redis integration test: BEAVEND_REDIS_URL not set")
	}

	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err)

	client := redis.NewClient(opt)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, client.Ping(ctx).Err())

	reg := snapshot.NewRegistry()
	mock := &mockQuerySource{revision: 1}
	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	syncer := synckit.NewSyncer(reg, compiler, mock, client, synckit.SyncerOptions{
		PollInterval: 5 * time.Second,
	})

	go func() {
		_ = syncer.Start(ctx)
	}()

	// Wait briefly for subscription to establish
	time.Sleep(100 * time.Millisecond)

	// Publish revision 2
	publisher := synckit.NewRedisPublisher(client)
	mock.revision = 2
	err = publisher.Publish(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-redis-test",
		Environment: "prod",
		Revision:    2,
	})
	require.NoError(t, err)

	// Verify registry is updated within 500ms
	require.Eventually(t, func() bool {
		snap, ok := reg.Get("proj-redis-test", "prod")
		return ok && snap.Revision == 2
	}, 500*time.Millisecond, 20*time.Millisecond)
}

func TestSyncer_PostgresListenNotifyIntegration(t *testing.T) {
	dbURL := os.Getenv("BEAVEND_DATABASE_URL")
	if dbURL == "" {
		t.Skip("Skipping Postgres LISTEN/NOTIFY integration test: BEAVEND_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg := snapshot.NewRegistry()
	mock := &mockQuerySource{revision: 1}
	compiler, err := snapshot.NewCompiler(mock)
	require.NoError(t, err)

	syncer := synckit.NewSyncer(reg, compiler, mock, nil, synckit.SyncerOptions{
		DatabaseURL:  dbURL,
		PollInterval: 10 * time.Second,
	})

	go func() {
		_ = syncer.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	pool, err := synckit.NewPostgresPool(ctx, dbURL)
	if err != nil {
		t.Skip("Unable to create postgres pool for integration test: " + err.Error())
	}
	defer pool.Close()

	publisher := synckit.NewPostgresPublisher(pool)
	mock.revision = 2
	err = publisher.Publish(ctx, synckit.RevisionEvent{
		ProjectID:   "proj-pg-test",
		Environment: "prod",
		Revision:    2,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		snap, ok := reg.Get("proj-pg-test", "prod")
		return ok && snap.Revision == 2
	}, 1*time.Second, 50*time.Millisecond)
}
