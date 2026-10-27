package memory

import (
	"sort"

	"github.com/beavend/beavend/internal/core"
)

const (
	// DefaultRRFConstant is the standard constant (60) used to balance rank significance in Reciprocal Rank Fusion.
	DefaultRRFConstant = 60
)

// HybridRRF merges keyword and vector search results using Reciprocal Rank Fusion.
// Formula: score = sum(1.0 / (k + rank)), where rank is 1-indexed.
// Ties are broken deterministically by document ID ascending.
func HybridRRF(keywordResults, vectorResults []core.SearchResult, k int) []core.SearchResult {
	if k <= 0 {
		k = DefaultRRFConstant
	}

	type docScore struct {
		result core.SearchResult
		score  float64
	}

	scores := make(map[string]*docScore)

	// Process keyword ranks (1-indexed)
	for idx, res := range keywordResults {
		rank := idx + 1
		contribution := 1.0 / float64(k+rank)
		if entry, ok := scores[res.ID]; ok {
			entry.score += contribution
		} else {
			scores[res.ID] = &docScore{
				result: res,
				score:  contribution,
			}
		}
	}

	// Process vector ranks (1-indexed)
	for idx, res := range vectorResults {
		rank := idx + 1
		contribution := 1.0 / float64(k+rank)
		if entry, ok := scores[res.ID]; ok {
			entry.score += contribution
			// If keyword didn't have content or metadata but vector does, enrich
			if entry.result.Content == "" && res.Content != "" {
				entry.result.Content = res.Content
			}
			if len(entry.result.Metadata) == 0 && len(res.Metadata) > 0 {
				entry.result.Metadata = res.Metadata
			}
		} else {
			scores[res.ID] = &docScore{
				result: res,
				score:  contribution,
			}
		}
	}

	merged := make([]core.SearchResult, 0, len(scores))
	for _, entry := range scores {
		item := entry.result
		item.Score = entry.score
		merged = append(merged, item)
	}

	// Sort descending by RRF score; break ties by ID ascending
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score == merged[j].Score {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].Score > merged[j].Score
	})

	return merged
}
