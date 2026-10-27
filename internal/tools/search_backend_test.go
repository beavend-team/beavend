package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSearchCacheKey_ScopeIsolation(t *testing.T) {
	baseParams := SearchParams{
		Query:      "golang microservices",
		AIAppID:    "app_1",
		ProjectID:  "proj_1",
		Env:        "prod",
		SafeSearch: 1,
		MaxResults: 5,
		Categories: []string{"general"},
		Engines:    []string{"google", "duckduckgo"},
	}

	key1 := ComputeSearchCacheKey(baseParams)
	require.NotEmpty(t, key1)

	// Same query with different app ID must produce different cache key
	diffAppParams := baseParams
	diffAppParams.AIAppID = "app_2"
	key2 := ComputeSearchCacheKey(diffAppParams)
	assert.NotEqual(t, key1, key2, "cache key must differ across AI Apps")

	// Different environment must produce different cache key
	diffEnvParams := baseParams
	diffEnvParams.Env = "staging"
	key3 := ComputeSearchCacheKey(diffEnvParams)
	assert.NotEqual(t, key1, key3, "cache key must differ across environments")

	// Different safe search policy must produce different cache key
	diffSafeParams := baseParams
	diffSafeParams.SafeSearch = 2
	key4 := ComputeSearchCacheKey(diffSafeParams)
	assert.NotEqual(t, key1, key4, "cache key must differ across safe search levels")

	// Case normalization: same query in uppercase should match
	upperParams := baseParams
	upperParams.Query = "GOLANG MICROSERVICES"
	key5 := ComputeSearchCacheKey(upperParams)
	assert.Equal(t, key1, key5, "cache key should be normalized for casing")
}

func TestSearchBackends(t *testing.T) {
	ctx := context.Background()

	t.Run("exa search backend", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "test-exa-key", r.Header.Get("x-api-key"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"title": "Exa Result 1",
						"url":   "https://exa.ai/1",
						"text":  "Exa content text",
						"score": 0.95,
					},
				},
			})
		}))
		defer ts.Close()

		backend := NewSearchBackend("exa", &http.Client{Timeout: 5 * time.Second})
		res, err := backend.Search(ctx, SearchParams{
			Query:   "test query",
			BaseURL: ts.URL,
			APIKey:  "test-exa-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "success", res.Status)
		require.Len(t, res.Results, 1)
		assert.Equal(t, "Exa Result 1", res.Results[0].Title)
		assert.Equal(t, "exa", res.Results[0].Engine)
	})

	t.Run("firecrawl search backend", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-fc-key", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"title":    "FC Result",
						"url":      "https://fc.dev/1",
						"markdown": "# Markdown Content",
					},
				},
			})
		}))
		defer ts.Close()

		backend := NewSearchBackend("firecrawl", &http.Client{Timeout: 5 * time.Second})
		res, err := backend.Search(ctx, SearchParams{
			Query:   "test query",
			BaseURL: ts.URL,
			APIKey:  "test-fc-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "success", res.Status)
		require.Len(t, res.Results, 1)
		assert.Equal(t, "FC Result", res.Results[0].Title)
		assert.Equal(t, "# Markdown Content", res.Results[0].Content)
	})

	t.Run("http_json generic search backend", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"title":   "Generic HTTP Title",
						"url":     "https://custom.internal/res",
						"snippet": "Custom snippet text",
					},
				},
			})
		}))
		defer ts.Close()

		backend := NewSearchBackend("http_json", &http.Client{Timeout: 5 * time.Second})
		res, err := backend.Search(ctx, SearchParams{
			Query:   "search term",
			BaseURL: ts.URL,
		})
		require.NoError(t, err)
		assert.Equal(t, "success", res.Status)
		require.Len(t, res.Results, 1)
		assert.Equal(t, "Generic HTTP Title", res.Results[0].Title)
	})
}
