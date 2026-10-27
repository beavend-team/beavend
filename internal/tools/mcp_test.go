package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMCPExecutor_StdioTransport(t *testing.T) {
	ctx := context.Background()

	t.Run("stdio disabled by default rejects execution", func(t *testing.T) {
		toolDef := core.ToolDefinition{
			ID:   "mcp_stdio_echo",
			Name: "echo_tool",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "cat",
			},
		}
		exec := NewMCPExecutor(toolDef, nil, nil, WithMCPStdio(false, []string{"cat"}))
		res, err := exec.Execute(ctx, core.ToolCall{ID: "c1", Name: "echo_tool", Arguments: `{}`}, core.Identity{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMCPStdioDisabled))
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "mcp stdio transport is disabled")
	})

	t.Run("unapproved binary rejected by allowlist", func(t *testing.T) {
		toolDef := core.ToolDefinition{
			ID:   "mcp_stdio_bad",
			Name: "rm_tool",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "/bin/rm",
			},
		}
		exec := NewMCPExecutor(toolDef, nil, nil, WithMCPStdio(true, []string{"/usr/bin/git", "node"}))
		res, err := exec.Execute(ctx, core.ToolCall{ID: "c2", Name: "rm_tool", Arguments: `{}`}, core.Identity{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMCPStdioNotAllowed))
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "not in the operator allowlist")
	})

	t.Run("command hijack via basename or path traversal rejected", func(t *testing.T) {
		allowlist := []string{"/opt/beavend/bin/safe-tool", "cat"}

		// 1. Attacker attempts to run /tmp/evil/safe-tool matching base name of /opt/beavend/bin/safe-tool
		toolDef1 := core.ToolDefinition{
			ID:   "mcp_evil_path",
			Name: "evil_tool",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "/tmp/evil/safe-tool",
			},
		}
		exec1 := NewMCPExecutor(toolDef1, nil, nil, WithMCPStdio(true, allowlist))
		res1, err1 := exec1.Execute(ctx, core.ToolCall{ID: "c_evil_1", Name: "evil_tool", Arguments: `{}`}, core.Identity{})
		require.Error(t, err1)
		assert.True(t, errors.Is(err1, ErrMCPStdioNotAllowed))
		assert.True(t, res1.IsError)

		// 2. Attacker attempts bare name "safe-tool" when allowlist specifies full path
		toolDef2 := core.ToolDefinition{
			ID:   "mcp_bare_path",
			Name: "bare_tool",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "safe-tool",
			},
		}
		exec2 := NewMCPExecutor(toolDef2, nil, nil, WithMCPStdio(true, allowlist))
		res2, err2 := exec2.Execute(ctx, core.ToolCall{ID: "c_evil_2", Name: "bare_tool", Arguments: `{}`}, core.Identity{})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, ErrMCPStdioNotAllowed))
		assert.True(t, res2.IsError)

		// 3. Attacker attempts path traversal /tmp/cat or ../cat when allowlist specifies bare "cat"
		toolDef3 := core.ToolDefinition{
			ID:   "mcp_traversal_cat",
			Name: "traversal_tool",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "/tmp/cat",
			},
		}
		exec3 := NewMCPExecutor(toolDef3, nil, nil, WithMCPStdio(true, allowlist))
		res3, err3 := exec3.Execute(ctx, core.ToolCall{ID: "c_evil_3", Name: "traversal_tool", Arguments: `{}`}, core.Identity{})
		require.Error(t, err3)
		assert.True(t, errors.Is(err3, ErrMCPStdioNotAllowed))
		assert.True(t, res3.IsError)
	})

	t.Run("allowed binary executes via stdio and returns result", func(t *testing.T) {
		// Use echo or cat as allowed command. For standard JSON-RPC, cat echoes back the jsonrpc payload!
		toolDef := core.ToolDefinition{
			ID:   "mcp_stdio_cat",
			Name: "echo_cat",
			Executor: core.ExecutorConfig{
				Type:      "mcp",
				Transport: "stdio",
				Command:   "cat",
			},
		}
		exec := NewMCPExecutor(toolDef, nil, nil, WithMCPStdio(true, []string{"cat"}))

		// If cat echoes stdin, JSON-RPC formatMCPResponse formats it
		call := core.ToolCall{
			ID:        "c_stdio_1",
			Name:      "echo_cat",
			Arguments: `{"arg":"hello"}`,
		}
		res, err := exec.Execute(ctx, call, core.Identity{})
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Equal(t, "stdio", res.Metadata["transport"])
	})
}

func TestMCPOAuth_PKCEAndState(t *testing.T) {
	ctx := context.Background()

	// 1. PKCE generation
	pkce, err := GeneratePKCE()
	require.NoError(t, err)
	assert.NotEmpty(t, pkce.CodeVerifier)
	assert.NotEmpty(t, pkce.Challenge)
	assert.Equal(t, "S256", pkce.Method)

	// 2. State generation and single-use consumption
	cacheStore := cache.NewMemoryCacheStore()
	st, err := NewOAuthState("admin_1", "proj_x", "prod", "tool_mcp", pkce.CodeVerifier, "https://app.com/cb")
	require.NoError(t, err)
	assert.NotEmpty(t, st.Token)

	err = SaveOAuthState(ctx, cacheStore, st, 5*time.Minute)
	require.NoError(t, err)

	consumed, err := ConsumeOAuthState(ctx, cacheStore, st.Token)
	require.NoError(t, err)
	assert.Equal(t, "admin_1", consumed.AdminUserID)
	assert.Equal(t, "proj_x", consumed.ProjectID)
	assert.Equal(t, pkce.CodeVerifier, consumed.CodeVerifier)

	// Second consume must fail (single-use token)
	_, err = ConsumeOAuthState(ctx, cacheStore, st.Token)
	require.ErrorIs(t, err, ErrOAuthInvalidState)

	// 3. Token encryption and decryption
	masterKey := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	payload := map[string]string{
		"access_token":  "gho_1234567890",
		"refresh_token": "ghr_abcdefghij",
	}

	encrypted, err := EncryptStoredOAuthTokens(masterKey, payload)
	require.NoError(t, err)
	assert.NotEmpty(t, encrypted)
	assert.NotContains(t, encrypted, "gho_1234567890")

	var decrypted map[string]string
	err = DecryptStoredOAuthTokens(masterKey, encrypted, &decrypted)
	require.NoError(t, err)
	assert.Equal(t, "gho_1234567890", decrypted["access_token"])
	assert.Equal(t, "ghr_abcdefghij", decrypted["refresh_token"])
}

func TestMCPOAuth_DiscoveryAndDCR(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			resp := map[string]any{
				"issuer":                 "http://" + r.Host,
				"authorization_endpoint": "http://" + r.Host + "/oauth/authorize",
				"token_endpoint":         "http://" + r.Host + "/oauth/token",
				"registration_endpoint":  "http://" + r.Host + "/oauth/register",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/oauth/register":
			if r.Method != http.MethodPost {
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			resp := map[string]any{
				"client_id":     "dcr_client_12345",
				"client_secret": "dcr_secret_67890",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// 1. Discovery
	endpoints, err := DiscoverOAuthEndpoints(ctx, ts.Client(), ts.URL)
	require.NoError(t, err)
	assert.Contains(t, endpoints.AuthorizationEndpoint, "/oauth/authorize")
	assert.Contains(t, endpoints.TokenEndpoint, "/oauth/token")
	assert.Contains(t, endpoints.RegistrationEndpoint, "/oauth/register")

	// 2. DCR
	dcrRes, err := RegisterClientDCR(ctx, ts.Client(), endpoints.RegistrationEndpoint, "Beavend", []string{"http://localhost:8080/callback"})
	require.NoError(t, err)
	assert.Equal(t, "dcr_client_12345", dcrRes.ClientID)
	assert.Equal(t, "dcr_secret_67890", dcrRes.ClientSecret)
}

func TestMCP_ProgressiveDiscovery(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/list" {
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      "discovery_tools_list",
				"result": map[string]any{
					"tools": []map[string]any{
						{"name": "fetch_user", "description": "Fetch user profile information"},
						{"name": "update_user", "description": "Update user account preferences"},
						{"name": "create_invoice", "description": "Generate billing invoices for customers"},
						{"name": "query_inventory", "description": "Search warehouse inventory stocks"},
						{"name": "send_email", "description": "Send customer notification email"},
						{"name": "delete_cache", "description": "Flush temporary cache keys"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "unknown method", http.StatusBadRequest)
	}))
	defer ts.Close()

	// 1. Discover all remote tools
	discovered, err := DiscoverMCPTools(ctx, ts.Client(), ts.URL, nil)
	require.NoError(t, err)
	require.Len(t, discovered, 6)

	// 2. Filter on demand matching user query (top <= 5)
	userQuery := "I need to check user account and billing invoice"
	filtered := FilterOnDemandTools(discovered, userQuery, 3)
	require.Len(t, filtered, 3)

	names := []string{filtered[0].Name, filtered[1].Name, filtered[2].Name}
	// "fetch_user", "update_user", "create_invoice" should score highest
	assert.Contains(t, names, "fetch_user")
	assert.Contains(t, names, "create_invoice")
}

