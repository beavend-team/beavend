package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestHTTPMemoryProvider(t *testing.T) {
	var receivedAuthHeader string
	var lastPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		receivedAuthHeader = r.Header.Get("Authorization")

		switch r.URL.Path {
		case "/store":
			var rec core.MemoryRecord
			_ = json.NewDecoder(r.Body).Decode(&rec)
			w.WriteHeader(http.StatusCreated)
		case "/recall":
			var filter core.MemoryFilter
			_ = json.NewDecoder(r.Body).Decode(&filter)
			resp := []core.MemoryRecord{
				{
					ID:      "mem_1",
					Content: "HTTP memory recalled",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/search":
			var query core.MemorySearchQuery
			_ = json.NewDecoder(r.Body).Decode(&query)
			resp := []core.MemorySearchResult{
				{
					MemoryRecord: core.MemoryRecord{
						ID:      "mem_1",
						Content: "HTTP search result",
					},
					Score: 0.95,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/delete-by-subject":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	provider, err := NewHTTPMemoryProvider(HTTPMemoryProviderConfig{
		EndpointURL: server.URL,
		AuthToken:   "secret-token-123",
		Client:      server.Client(),
	})
	require.NoError(t, err)

	// Test Store
	err = provider.Store(ctx, core.MemoryRecord{
		Content: "New semantic note",
	})
	require.NoError(t, err)
	assert.Equal(t, "/store", lastPath)
	assert.Equal(t, "Bearer secret-token-123", receivedAuthHeader)

	// Test Recall
	recalled, err := provider.Recall(ctx, core.MemoryFilter{
		ProjectID: "p1",
		Subject:   "s1",
	})
	require.NoError(t, err)
	require.Len(t, recalled, 1)
	assert.Equal(t, "HTTP memory recalled", recalled[0].Content)

	// Test Search
	searched, err := provider.Search(ctx, core.MemorySearchQuery{
		ProjectID: "p1",
		Vector:    []float32{0.1, 0.2},
	})
	require.NoError(t, err)
	require.Len(t, searched, 1)
	assert.Equal(t, "HTTP search result", searched[0].Content)
	assert.InDelta(t, 0.95, searched[0].Score, 1e-4)

	// Test DeleteBySubject
	err = provider.DeleteBySubject(ctx, "p1", "iss1", "sub1")
	require.NoError(t, err)
	assert.Equal(t, "/delete-by-subject", lastPath)
}
