package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

func TestMCPExecutor_Success_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}

		var req mcpRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "tools/call" {
			http.Error(w, "expected tools/call", http.StatusBadRequest)
			return
		}
		if req.Params.Name != "get_weather" {
			http.Error(w, "expected tool name get_weather", http.StatusBadRequest)
			return
		}
		if req.Params.Arguments["city"] != "Tokyo" {
			http.Error(w, "expected city Tokyo", http.StatusBadRequest)
			return
		}

		resp := mcpRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: &mcpCallResult{
				Content: []mcpContentItem{
					{Type: "text", Text: "Sunny, 22°C"},
				},
				IsError: false,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	toolDef := core.ToolDefinition{
		ID:   "mcp_weather",
		Name: "get_weather",
		Executor: core.ExecutorConfig{
			Type: "mcp",
			URL:  ts.URL,
		},
	}

	exec := NewMCPExecutor(toolDef, ts.Client(), nil)
	call := core.ToolCall{
		ID:        "call_123",
		Name:      "get_weather",
		Arguments: `{"city": "Tokyo"}`,
	}

	result, err := exec.Execute(context.Background(), call, core.Identity{Subject: "user_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected result not to be error, got: %s", result.Content)
	}
	if result.Content != "Sunny, 22°C" {
		t.Fatalf("expected content 'Sunny, 22°C', got %q", result.Content)
	}
	if result.Metadata["protocol"] != "mcp-jsonrpc-2.0" {
		t.Fatalf("expected protocol metadata, got %v", result.Metadata)
	}
}

func TestMCPExecutor_Success_SSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcpRPCResponse{
			JSONRPC: "2.0",
			ID:      "call_sse",
			Result: &mcpCallResult{
				Content: []mcpContentItem{
					{Type: "text", Text: "Result over SSE stream"},
				},
			},
		}
		data, _ := json.Marshal(resp)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: " + string(data) + "\n\n"))
	}))
	defer ts.Close()

	toolDef := core.ToolDefinition{
		ID:   "mcp_sse",
		Name: "test_sse",
		Executor: core.ExecutorConfig{
			Type: "mcp",
			URL:  ts.URL,
		},
	}

	exec := NewMCPExecutor(toolDef, ts.Client(), nil)
	call := core.ToolCall{ID: "call_sse", Name: "test_sse", Arguments: `{}`}

	result, err := exec.Execute(context.Background(), call, core.Identity{Subject: "user_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "Result over SSE stream" {
		t.Fatalf("unexpected SSE content: %q", result.Content)
	}
}

func TestMCPExecutor_ProtocolError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcpRPCResponse{
			JSONRPC: "2.0",
			ID:      "call_err",
			Error: &mcpRPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	toolDef := core.ToolDefinition{
		ID:   "mcp_err",
		Name: "unknown_method",
		Executor: core.ExecutorConfig{
			Type: "mcp",
			URL:  ts.URL,
		},
	}

	exec := NewMCPExecutor(toolDef, ts.Client(), nil)
	call := core.ToolCall{ID: "call_err", Name: "unknown_method", Arguments: `{}`}

	result, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if !result.IsError {
		t.Fatalf("expected result.IsError to be true")
	}
	if !strings.Contains(result.Content, "Method not found") {
		t.Fatalf("expected content to contain error message, got %q", result.Content)
	}
}

func TestMCPExecutor_RetriesOn500(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att == 1 {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		resp := mcpRPCResponse{
			JSONRPC: "2.0",
			ID:      "call_retry",
			Result: &mcpCallResult{
				Content: []mcpContentItem{
					{Type: "text", Text: "Recovered"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	toolDef := core.ToolDefinition{
		ID:   "mcp_retry",
		Name: "test_retry",
		Executor: core.ExecutorConfig{
			Type:    "mcp",
			URL:     ts.URL,
			Retries: 2,
		},
	}

	exec := NewMCPExecutor(toolDef, ts.Client(), nil)
	call := core.ToolCall{ID: "call_retry", Name: "test_retry", Arguments: `{}`}

	result, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "Recovered" {
		t.Fatalf("expected 'Recovered', got %q", result.Content)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}
