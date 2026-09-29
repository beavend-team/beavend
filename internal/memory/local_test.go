package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestLocalMemoryProvider_StoreAndRecall(t *testing.T) {
	ctx := context.Background()
	provider := NewLocalMemoryProvider()

	// 1. Store memory for user A
	err := provider.Store(ctx, core.MemoryRecord{
		ProjectID:   "proj1",
		Environment: "dev",
		Issuer:      "https://auth.example.com",
		Subject:     "user123",
		AIAppID:     "app1",
		Content:     "User prefers Python for data science",
	})
	require.NoError(t, err)

	// 2. Store memory for user B
	err = provider.Store(ctx, core.MemoryRecord{
		ProjectID:   "proj1",
		Environment: "dev",
		Issuer:      "https://auth.example.com",
		Subject:     "user456",
		AIAppID:     "app1",
		Content:     "User prefers Go for microservices",
	})
	require.NoError(t, err)

	// 3. Recall user A's memories
	memoriesA, err := provider.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj1",
		Issuer:    "https://auth.example.com",
		Subject:   "user123",
	})
	require.NoError(t, err)
	require.Len(t, memoriesA, 1)
	assert.Equal(t, "User prefers Python for data science", memoriesA[0].Content)

	// 4. Recall with non-matching subject
	empty, err := provider.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj1",
		Issuer:    "https://auth.example.com",
		Subject:   "user999",
	})
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestLocalMemoryProvider_Search(t *testing.T) {
	ctx := context.Background()
	provider := NewLocalMemoryProvider()

	// Store memories with embeddings
	_ = provider.Store(ctx, core.MemoryRecord{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
		Content:   "Favorite color is blue",
		Embedding: []float32{1.0, 0.0, 0.0},
	})
	_ = provider.Store(ctx, core.MemoryRecord{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
		Content:   "Favorite fruit is apple",
		Embedding: []float32{0.0, 1.0, 0.0},
	})

	// Search closest to [0.9, 0.1, 0.0]
	results, err := provider.Search(ctx, core.MemorySearchQuery{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
		Vector:    []float32{0.9, 0.1, 0.0},
		Limit:     2,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "Favorite color is blue", results[0].Content)
	assert.Greater(t, results[0].Score, results[1].Score)
}

func TestLocalMemoryProvider_TTLAndPurge(t *testing.T) {
	ctx := context.Background()
	provider := NewLocalMemoryProvider()

	past := time.Now().UTC().Add(-1 * time.Hour)
	future := time.Now().UTC().Add(1 * time.Hour)

	_ = provider.Store(ctx, core.MemoryRecord{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
		Content:   "Expired note",
		ExpiresAt: &past,
	})
	_ = provider.Store(ctx, core.MemoryRecord{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
		Content:   "Valid note",
		ExpiresAt: &future,
	})

	recalled, err := provider.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
	})
	require.NoError(t, err)
	require.Len(t, recalled, 1)
	assert.Equal(t, "Valid note", recalled[0].Content)

	// Delete by subject
	err = provider.DeleteBySubject(ctx, "proj1", "iss1", "sub1")
	require.NoError(t, err)

	recalledAfter, err := provider.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "sub1",
	})
	require.NoError(t, err)
	assert.Empty(t, recalledAfter)
}
