package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWKSManager_Caching(t *testing.T) {
	fixture := newTestKeyFixture(t)

	var fetchCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture.jwksSet)
	}))
	defer server.Close()

	mgr := NewJWKSManager(
		WithDefaultTTL(10 * time.Minute),
	)

	ctx := context.Background()

	// First fetch should hit the server
	set1, err := mgr.Get(ctx, server.URL)
	require.NoError(t, err)
	assert.NotNil(t, set1)
	assert.Equal(t, int64(1), atomic.LoadInt64(&fetchCount))

	// Second fetch should return cached set without touching network
	set2, err := mgr.Get(ctx, server.URL)
	require.NoError(t, err)
	assert.NotNil(t, set2)
	assert.Equal(t, int64(1), atomic.LoadInt64(&fetchCount), "should not fetch from server on cache hit")
}

func TestJWKSManager_SingleFlight(t *testing.T) {
	fixture := newTestKeyFixture(t)

	var fetchCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond) // Simulate network delay
		atomic.AddInt64(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture.jwksSet)
	}))
	defer server.Close()

	mgr := NewJWKSManager(
		WithDefaultTTL(10 * time.Minute),
	)

	ctx := context.Background()
	concurrency := 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			set, err := mgr.Get(ctx, server.URL)
			assert.NoError(t, err)
			assert.NotNil(t, set)
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(1), atomic.LoadInt64(&fetchCount), "concurrent calls to same URL must be deduplicated via single-flight")
}

func TestJWKSManager_Refresh(t *testing.T) {
	fixture := newTestKeyFixture(t)

	var fetchCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture.jwksSet)
	}))
	defer server.Close()

	mgr := NewJWKSManager(
		WithMinRefreshInterval(0), // No debounce for this test
	)
	ctx := context.Background()

	_, err := mgr.Get(ctx, server.URL)
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&fetchCount))

	// Refresh forces an update
	_, err = mgr.Refresh(ctx, server.URL)
	require.NoError(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&fetchCount))
}

func TestJWKSManager_SetManual(t *testing.T) {
	fixture := newTestKeyFixture(t)
	mgr := NewJWKSManager()

	testURL := "https://custom.auth.local/jwks.json"
	mgr.SetManual(testURL, fixture.jwksSet, 1*time.Hour)

	set, err := mgr.Get(context.Background(), testURL)
	require.NoError(t, err)
	assert.NotNil(t, set)

	key, found := set.LookupKeyID(fixture.rsaKeyID)
	assert.True(t, found)
	assert.NotNil(t, key)
}

func TestJWKSManager_InvalidURL(t *testing.T) {
	mgr := NewJWKSManager()
	ctx := context.Background()

	t.Run("empty url", func(t *testing.T) {
		_, err := mgr.Get(ctx, "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAuthConfigMissing)
	})

	t.Run("invalid scheme", func(t *testing.T) {
		_, err := mgr.Get(ctx, "ftp://example.com/jwks.json")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAuthConfigMissing)
	})
}
