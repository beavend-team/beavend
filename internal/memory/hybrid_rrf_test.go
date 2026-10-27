package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestHybridRRF(t *testing.T) {
	t.Run("calculates RRF scores with default k=60", func(t *testing.T) {
		kw := []core.SearchResult{
			{ID: "doc_a", Content: "keyword top doc"},
			{ID: "doc_b", Content: "keyword second doc"},
			{ID: "doc_c", Content: "keyword third doc"},
		}

		vec := []core.SearchResult{
			{ID: "doc_b", Content: "vector top doc"},
			{ID: "doc_d", Content: "vector second doc"},
			{ID: "doc_a", Content: "vector third doc"},
		}

		// RRF formula: sum(1.0 / (k + rank))
		// For k = 60:
		// doc_a: rank 1 in kw (1/61 = 0.016393), rank 3 in vec (1/63 = 0.015873) -> total = 0.032266
		// doc_b: rank 2 in kw (1/62 = 0.016129), rank 1 in vec (1/61 = 0.016393) -> total = 0.032522
		// doc_c: rank 3 in kw (1/63 = 0.015873), not in vec -> total = 0.015873
		// doc_d: not in kw, rank 2 in vec (1/62 = 0.016129) -> total = 0.016129
		// Expected rank order: doc_b, doc_a, doc_d, doc_c

		merged := HybridRRF(kw, vec, 60)
		require.Len(t, merged, 4)

		assert.Equal(t, "doc_b", merged[0].ID)
		assert.InDelta(t, 0.032522, merged[0].Score, 0.0001)

		assert.Equal(t, "doc_a", merged[1].ID)
		assert.InDelta(t, 0.032266, merged[1].Score, 0.0001)

		assert.Equal(t, "doc_d", merged[2].ID)
		assert.InDelta(t, 0.016129, merged[2].Score, 0.0001)

		assert.Equal(t, "doc_c", merged[3].ID)
		assert.InDelta(t, 0.015873, merged[3].Score, 0.0001)
	})

	t.Run("breaks ties deterministically by ID ascending", func(t *testing.T) {
		kw := []core.SearchResult{
			{ID: "doc_z", Content: "content z"},
			{ID: "doc_m", Content: "content m"},
		}
		vec := []core.SearchResult{
			{ID: "doc_b", Content: "content b"},
			{ID: "doc_a", Content: "content a"},
		}

		// doc_z: 1/61
		// doc_b: 1/61
		// Tied for rank 1
		merged := HybridRRF(kw, vec, 60)
		require.Len(t, merged, 4)

		// doc_b and doc_z have equal scores: doc_b comes first alphabetically
		assert.Equal(t, "doc_b", merged[0].ID)
		assert.Equal(t, "doc_z", merged[1].ID)
		assert.Equal(t, merged[0].Score, merged[1].Score)

		// doc_a and doc_m have equal scores: doc_a comes first alphabetically
		assert.Equal(t, "doc_a", merged[2].ID)
		assert.Equal(t, "doc_m", merged[3].ID)
		assert.Equal(t, merged[2].Score, merged[3].Score)
	})

	t.Run("handles empty slices gracefully", func(t *testing.T) {
		assert.Empty(t, HybridRRF(nil, nil, 60))

		kwOnly := []core.SearchResult{{ID: "only_kw", Content: "text"}}
		res := HybridRRF(kwOnly, nil, 60)
		require.Len(t, res, 1)
		assert.Equal(t, "only_kw", res[0].ID)
	})
}
