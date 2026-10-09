package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
)

func TestSearXNGClient_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("expected path /search, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Fatalf("expected format=json, got %s", r.URL.Query().Get("format"))
		}
		if r.URL.Query().Get("q") != "golang 1.24" {
			t.Fatalf("expected q=golang 1.24, got %s", r.URL.Query().Get("q"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"query": "golang 1.24",
			"number_of_results": 2,
			"results": [
				{
					"title": "Go 1.24 Release Notes",
					"url": "https://go.dev/doc/go1.24",
					"content": "Go 1.24 introduces generic type aliases and performance improvements.",
					"engine": "google",
					"score": 1.5
				},
				{
					"title": "Go Blog",
					"url": "https://go.dev/blog",
					"content": "Official Go announcements and deep dives.",
					"engine": "bing",
					"score": 1.2
				}
			]
		}`))
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	res, err := client.Search(context.Background(), "golang 1.24", []string{"general"}, nil, "year", 1, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != "success" {
		t.Errorf("expected status 'success', got %s", res.Status)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Results))
	}
	if res.Results[0].Title != "Go 1.24 Release Notes" {
		t.Errorf("unexpected first title: %s", res.Results[0].Title)
	}
}

func TestSearXNGClient_EmptyQuery(t *testing.T) {
	client := NewSearXNGClient("http://localhost:8081", true, 5*time.Second, nil)
	res, err := client.Search(context.Background(), "   ", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "error" || res.ErrorCode != ErrCodeSearXNGEmptyQuery {
		t.Errorf("expected empty query error, got %+v", res)
	}
}

func TestSearXNGClient_Disabled(t *testing.T) {
	client := NewSearXNGClient("http://localhost:8081", false, 5*time.Second, nil)
	res, err := client.Search(context.Background(), "test", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "disabled" || res.ErrorCode != ErrCodeSearXNGDisabled {
		t.Errorf("expected disabled status, got %+v", res)
	}
}

func TestSearXNGClient_ConnectionRefused(t *testing.T) {
	// Connect to non-listening local port
	client := NewSearXNGClient("http://127.0.0.1:59999", true, 200*time.Millisecond, nil)
	res, err := client.Search(context.Background(), "test query", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error (should be handled gracefully): %v", err)
	}
	if res.Status != "unavailable" || res.ErrorCode != ErrCodeSearXNGUnreachable {
		t.Errorf("expected unavailable status on connection error, got %+v", res)
	}
}

func TestSearXNGClient_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 50*time.Millisecond, ts.Client())
	res, err := client.Search(context.Background(), "test", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "unavailable" || res.ErrorCode != ErrCodeSearXNGTimeout {
		t.Errorf("expected timeout status, got %+v", res)
	}
}

func TestSearXNGClient_HTTP500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Upstream search engine error"))
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	res, err := client.Search(context.Background(), "test", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "error" || res.ErrorCode != ErrCodeSearXNGHTTPError {
		t.Errorf("expected http error status, got %+v", res)
	}
}

func TestSearXNGClient_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{ "query": "asdfghjklllkjhgfdsa", "number_of_results": 0, "results": [] }`))
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	res, err := client.Search(context.Background(), "asdfghjklllkjhgfdsa", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "no_results" || res.ErrorCode != ErrCodeSearXNGNoResults {
		t.Errorf("expected no_results status, got %+v", res)
	}
}

func TestSearXNGClient_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>Not JSON</html>"))
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	res, err := client.Search(context.Background(), "test", nil, nil, "", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "error" || res.ErrorCode != ErrCodeSearXNGMalformedPayload {
		t.Errorf("expected malformed response error, got %+v", res)
	}
}

func TestSearXNGClient_CheckHealth(t *testing.T) {
	// 1. Healthy server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	health := client.CheckHealth(context.Background())
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %+v", health)
	}

	// 2. Unreachable server
	unreachableClient := NewSearXNGClient("http://127.0.0.1:59998", true, 200*time.Millisecond, nil)
	unreachableHealth := unreachableClient.CheckHealth(context.Background())
	if unreachableHealth.Status != "unreachable" {
		t.Errorf("expected unreachable, got %+v", unreachableHealth)
	}
}

func TestSearXNGExecutor_Execute(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"query": "beavend ai",
			"number_of_results": 1,
			"results": [{
				"title": "Beavend AI",
				"url": "https://beavend.dev",
				"content": "Open-source self-hosted AI backend runtime."
			}]
		}`))
	}))
	defer ts.Close()

	client := NewSearXNGClient(ts.URL, true, 5*time.Second, ts.Client())
	exec := NewSearXNGExecutor(client, core.SearchConfig{
		Enabled:    true,
		MaxResults: 3,
	})

	call := core.ToolCall{
		ID:        "call_123",
		Name:      "web_search",
		Arguments: `{"query": "beavend ai"}`,
	}

	result, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("expected IsError=false, got true: %s", result.Content)
	}

	var parsed SearXNGSearchResult
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("failed to unmarshal content: %v", err)
	}
	if parsed.Status != "success" || len(parsed.Results) != 1 {
		t.Errorf("unexpected parsed result: %+v", parsed)
	}
}
