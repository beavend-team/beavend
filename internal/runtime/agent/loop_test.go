package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/tools"
)

type mockProvider struct {
	chatFunc   func(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
	streamFunc func(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error)
}

func (m *mockProvider) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockProvider) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockProvider) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}

func (m *mockProvider) Health(_ context.Context) error {
	return nil
}

type mockExecutor struct {
	execFunc func(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error)
}

func (m *mockExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, call, id)
	}
	return core.ToolResult{
		CallID:  call.ID,
		Content: "mock result",
	}, nil
}

type mockRegistry struct {
	executor core.Executor
}

func (m *mockRegistry) GetExecutor(_ core.ToolDefinition) (core.Executor, error) {
	return m.executor, nil
}

func TestLoop_Unary_DirectAnswer(t *testing.T) {
	loop := NewLoop(LoopConfig{})

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Apps["app_1"] = &core.AIApp{
		ID: "app_1",
	}

	state := &core.ReqState{
		RequestID: "req_1",
		ProjectID: "proj_1",
		AIAppID:   "app_1",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_1"],
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Hello"}},
		},
	}

	provider := &mockProvider{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				ID: "resp_1",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Hello world!",
				},
			}, nil
		},
	}

	err := loop.Run(context.Background(), state, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.ChatResponse == nil || state.ChatResponse.Message.Content != "Hello world!" {
		t.Fatalf("unexpected chat response: %+v", state.ChatResponse)
	}
}

func TestLoop_Unary_WithToolExecution(t *testing.T) {
	var step int
	provider := &mockProvider{
		chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			step++
			if step == 1 {
				// Step 1: Model requests a tool call
				return &core.ChatResponse{
					ID: "resp_step1",
					Message: core.ChatMessage{
						Role: "assistant",
						ToolCalls: []core.ToolCall{
							{
								ID:        "call_calc",
								Name:      "calculate",
								Arguments: `{"op":"add","a":2,"b":3}`,
							},
						},
					},
				}, nil
			}
			// Step 2: Model receives tool result and provides final answer
			lastMsg := req.Messages[len(req.Messages)-1]
			if lastMsg.Role != "tool" || !strings.Contains(lastMsg.Content, "result: 5") {
				return nil, fmt.Errorf("expected tool result message, got: %+v", lastMsg)
			}
			return &core.ChatResponse{
				ID: "resp_step2",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "The answer is 5.",
				},
			}, nil
		},
	}

	exec := &mockExecutor{
		execFunc: func(_ context.Context, call core.ToolCall, _ core.Identity) (core.ToolResult, error) {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  "calculate",
				Content: "result: 5",
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"calculate"},
	}
	snap.Tools["calculate"] = &core.ToolDefinition{
		ID:         "calculate",
		Name:       "calculate",
		SideEffect: "read",
	}

	loop := NewLoop(LoopConfig{
		ExecutorRegistry: &mockRegistry{executor: exec},
	})

	state := &core.ReqState{
		RequestID: "req_tool",
		ProjectID: "proj_1",
		AIAppID:   "app_1",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_1"],
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Add 2 and 3"}},
		},
	}

	err := loop.Run(context.Background(), state, provider)
	if err != nil {
		t.Fatalf("unexpected loop error: %v", err)
	}

	if state.ChatResponse == nil || state.ChatResponse.Message.Content != "The answer is 5." {
		t.Errorf("unexpected final response: %+v", state.ChatResponse)
	}
}

func TestLoop_SuspendsForConfirmation(t *testing.T) {
	provider := &mockProvider{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				ID: "resp_write",
				Message: core.ChatMessage{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{
							ID:        "call_delete",
							Name:      "delete_database",
							Arguments: `{"db":"prod"}`,
						},
					},
				},
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"delete_database"},
	}
	snap.Tools["delete_database"] = &core.ToolDefinition{
		ID:           "delete_database",
		Name:         "delete_database",
		SideEffect:   "write",
		Confirmation: "required",
	}

	continuationMgr := tools.NewContinuationManager(tools.NewInMemorySuspendedStateStore(), []byte("test-key-32-bytes-long-12345678"))
	loop := NewLoop(LoopConfig{
		ContinuationMgr: continuationMgr,
	})

	state := &core.ReqState{
		RequestID: "req_suspend",
		ProjectID: "proj_1",
		AIAppID:   "app_1",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_1"],
		Identity:  &core.Identity{Subject: "usr_alice"},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Drop the DB"}},
		},
	}

	err := loop.Run(context.Background(), state, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.ChatResponse == nil {
		t.Fatalf("expected suspended chat response, got nil")
	}

	if state.ChatResponse.FinishReason != "confirmation_required" {
		t.Errorf("expected finish_reason confirmation_required, got %s", state.ChatResponse.FinishReason)
	}

	token := state.ChatRequest.Metadata["confirmation_token"]
	if token == "" {
		t.Fatalf("expected confirmation token in metadata")
	}

	// Verify state saved in store
	savedState, err := continuationMgr.Store().Get(context.Background(), "proj_1", "req_suspend")
	if err != nil {
		t.Fatalf("failed to retrieve saved state: %v", err)
	}
	if savedState.PendingToolCall.Name != "delete_database" {
		t.Errorf("unexpected saved tool call: %+v", savedState.PendingToolCall)
	}
}

func TestLoop_Streaming_WithToolAndConfirmation(t *testing.T) {
	provider := &mockProvider{
		streamFunc: func(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
			ch := make(chan core.StreamEvent, 3)
			go func() {
				ch <- core.StreamEvent{Type: "delta", Delta: "I will transfer funds."}
				ch <- core.StreamEvent{
					Type: "tool_call",
					ToolCall: &core.ToolCall{
						ID:        "t_wire",
						Name:      "wire_transfer",
						Arguments: `{"amount":5000}`,
					},
				}
				ch <- core.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"wire_transfer"},
	}
	snap.Tools["wire_transfer"] = &core.ToolDefinition{
		ID:           "wire_transfer",
		Name:         "wire_transfer",
		SideEffect:   "write",
		Confirmation: "required",
	}

	continuationMgr := tools.NewContinuationManager(tools.NewInMemorySuspendedStateStore(), []byte("test-key-32-bytes-long-12345678"))
	loop := NewLoop(LoopConfig{
		ContinuationMgr: continuationMgr,
	})

	state := &core.ReqState{
		RequestID:   "req_stream_suspend",
		ProjectID:   "proj_1",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		IsStreaming: true,
		StreamChan:  make(chan core.StreamEvent, 10),
		Identity:    &core.Identity{Subject: "usr_alice"},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Send 5000"}},
		},
	}

	err := loop.Run(context.Background(), state, provider)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}

	// Drain stream events
	var (
		gotConfirmation bool
		tokenReceived   string
	)
	for len(state.StreamChan) > 0 {
		ev := <-state.StreamChan
		if ev.Type == "confirmation" {
			gotConfirmation = true
			tokenReceived = ev.ConfirmationToken
		}
	}

	if !gotConfirmation || tokenReceived == "" {
		t.Fatalf("expected confirmation event with token, got none")
	}
}

func TestLoop_CELPolicyDenial(t *testing.T) {
	policyCompiler, err := policy.NewCompiler()
	if err != nil {
		t.Fatalf("failed to create policy compiler: %v", err)
	}
	policyEvaluator, err := policy.NewEvaluator(policyCompiler)
	if err != nil {
		t.Fatalf("failed to create policy evaluator: %v", err)
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Policies["admin_only"] = &core.PolicyDefinition{
		ID:         "admin_only",
		Expression: `identity.claims.role == "admin"`,
	}
	// Compile CEL program into snapshot
	compiled, err := policyCompiler.Compile(snap.Policies["admin_only"].Expression)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}
	snap.CompiledPolicies["admin_only"] = compiled

	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"restricted_tool"},
	}
	snap.Tools["restricted_tool"] = &core.ToolDefinition{
		ID:            "restricted_tool",
		Name:          "restricted_tool",
		Authorization: "admin_only",
	}

	var step int
	provider := &mockProvider{
		chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			step++
			if step == 1 {
				return &core.ChatResponse{
					ID: "resp_1",
					Message: core.ChatMessage{
						Role: "assistant",
						ToolCalls: []core.ToolCall{
							{ID: "c_1", Name: "restricted_tool"},
						},
					},
				}, nil
			}
			lastMsg := req.Messages[len(req.Messages)-1]
			return &core.ChatResponse{
				ID: "resp_2",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: fmt.Sprintf("Tool rejected with: %s", lastMsg.Content),
				},
			}, nil
		},
	}

	loop := NewLoop(LoopConfig{
		PolicyEvaluator: policyEvaluator,
	})

	// User is a non-admin
	state := &core.ReqState{
		RequestID: "req_cel",
		ProjectID: "proj_1",
		AIAppID:   "app_1",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_1"],
		Identity: &core.Identity{
			Subject: "usr_regular",
			Claims:  map[string]any{"role": "user"},
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Run restricted tool"}},
		},
	}

	err = loop.Run(context.Background(), state, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(state.ChatResponse.Message.Content, "authorization denied") {
		t.Errorf("expected denial message in content: %s", state.ChatResponse.Message.Content)
	}
}

func TestLoop_EntitlementToolAuthorization(t *testing.T) {
	policyCompiler, err := policy.NewCompiler()
	require.NoError(t, err)
	policyEvaluator, err := policy.NewEvaluator(policyCompiler)
	require.NoError(t, err)

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Policies["enterprise_tool_policy"] = &core.PolicyDefinition{
		ID:         "enterprise_tool_policy",
		Expression: `has(claims.plan) && claims.plan == "enterprise"`,
	}
	compiled, err := policyCompiler.Compile(snap.Policies["enterprise_tool_policy"].Expression)
	require.NoError(t, err)
	snap.CompiledPolicies["enterprise_tool_policy"] = compiled

	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"enterprise_tool"},
	}
	snap.Tools["enterprise_tool"] = &core.ToolDefinition{
		ID:            "enterprise_tool",
		Name:          "enterprise_tool",
		Authorization: "enterprise_tool_policy",
		Executor: core.ExecutorConfig{
			Type: "client_side",
		},
	}

	loop := NewLoop(LoopConfig{
		PolicyEvaluator: policyEvaluator,
	})

	t.Run("denies tool execution when required claim missing", func(t *testing.T) {
		var step int
		provider := &mockProvider{
			chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
				step++
				if step == 1 {
					return &core.ChatResponse{
						ID: "resp_1",
						Message: core.ChatMessage{
							Role: "assistant",
							ToolCalls: []core.ToolCall{
								{ID: "c_1", Name: "enterprise_tool"},
							},
						},
					}, nil
				}
				lastMsg := req.Messages[len(req.Messages)-1]
				return &core.ChatResponse{
					ID: "resp_2",
					Message: core.ChatMessage{
						Role:    "assistant",
						Content: fmt.Sprintf("Tool rejected with: %s", lastMsg.Content),
					},
				}, nil
			},
		}

		state := &core.ReqState{
			RequestID: "req_ent_deny",
			ProjectID: "proj_1",
			AIAppID:   "app_1",
			Snapshot:  snap,
			AIApp:     snap.Apps["app_1"],
			Identity: &core.Identity{
				Subject: "alice",
				Claims: map[string]any{
					"plan": "starter",
				},
			},
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{{Role: "user", Content: "Run enterprise tool"}},
			},
		}

		err := loop.Run(context.Background(), state, provider)
		require.NoError(t, err)
		assert.Contains(t, state.ChatResponse.Message.Content, "authorization denied")
	})

	t.Run("allows tool execution when claim matches", func(t *testing.T) {
		provider := &mockProvider{
			chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
				return &core.ChatResponse{
					ID: "resp_1",
					Message: core.ChatMessage{
						Role: "assistant",
						ToolCalls: []core.ToolCall{
							{ID: "c_1", Name: "enterprise_tool"},
						},
					},
				}, nil
			},
		}

		state := &core.ReqState{
			RequestID: "req_ent_allow",
			ProjectID: "proj_1",
			AIAppID:   "app_1",
			Snapshot:  snap,
			AIApp:     snap.Apps["app_1"],
			Identity: &core.Identity{
				Subject: "bob",
				Claims: map[string]any{
					"plan": "enterprise",
				},
			},
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{{Role: "user", Content: "Run enterprise tool"}},
			},
		}

		err := loop.Run(context.Background(), state, provider)
		require.NoError(t, err)
		assert.Equal(t, "client_tool", state.ChatResponse.FinishReason)
		require.Len(t, state.ChatResponse.Message.ToolCalls, 1)
	})
}

func TestLoop_ToolResultCaching(t *testing.T) {
	ctx := context.Background()
	cacheStore := cache.NewMemoryCacheStore()

	execCount := 0
	executor := &mockExecutor{
		execFunc: func(_ context.Context, call core.ToolCall, _ core.Identity) (core.ToolResult, error) {
			execCount++
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  "weather_tool",
				Content: fmt.Sprintf("Live weather for args %s (exec #%d)", call.Arguments, execCount),
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "prod", 1)
	snap.Tools["weather_tool"] = &core.ToolDefinition{
		ID:         "weather_tool",
		Name:       "weather_tool",
		SideEffect: "read",
		Cache: core.CacheConfig{
			Enabled: true,
			TTL:     300,
		},
	}
	snap.Apps["app_1"] = &core.AIApp{
		ID:      "app_1",
		ToolIDs: []string{"weather_tool"},
	}

	makeProvider := func() *mockProvider {
		turn := 0
		return &mockProvider{
			chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
				turn++
				if turn == 1 {
					return &core.ChatResponse{
						ID: "turn1",
						Message: core.ChatMessage{
							Role: "assistant",
							ToolCalls: []core.ToolCall{
								{ID: "call_1", Name: "weather_tool", Arguments: `{"city":"Tokyo"}`},
							},
						},
					}, nil
				}
				lastMsg := req.Messages[len(req.Messages)-1]
				return &core.ChatResponse{
					ID: "turn2",
					Message: core.ChatMessage{
						Role:    "assistant",
						Content: fmt.Sprintf("Result: %s", lastMsg.Content),
					},
				}, nil
			},
		}
	}

	loop := NewLoop(LoopConfig{
		ExecutorRegistry: &mockRegistry{executor: executor},
		CacheStore:       cacheStore,
	})

	// Run 1: Alice requests Tokyo weather -> Cache Miss -> Executed
	stateAlice1 := &core.ReqState{
		RequestID:   "req_1",
		ProjectID:   "proj_1",
		Environment: "prod",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		Identity:    &core.Identity{Subject: "alice"},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Weather in Tokyo?"}},
		},
	}

	err := loop.Run(ctx, stateAlice1, makeProvider())
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if execCount != 1 {
		t.Fatalf("expected execCount 1, got %d", execCount)
	}
	if !strings.Contains(stateAlice1.ChatResponse.Message.Content, "exec #1") {
		t.Fatalf("unexpected content: %s", stateAlice1.ChatResponse.Message.Content)
	}

	// Run 2: Alice requests same Tokyo weather -> Cache Hit -> ExecCount remains 1!
	stateAlice2 := &core.ReqState{
		RequestID:   "req_2",
		ProjectID:   "proj_1",
		Environment: "prod",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		Identity:    &core.Identity{Subject: "alice"},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Weather in Tokyo again?"}},
		},
	}

	err = loop.Run(ctx, stateAlice2, makeProvider())
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	if execCount != 1 {
		t.Fatalf("expected execCount to stay 1 on cache hit, got %d", execCount)
	}
	if !strings.Contains(stateAlice2.ChatResponse.Message.Content, "exec #1") {
		t.Fatalf("unexpected cached content: %s", stateAlice2.ChatResponse.Message.Content)
	}

	// Run 3: Bob requests Tokyo weather -> Different user -> User isolation -> Cache Miss -> ExecCount becomes 2!
	stateBob := &core.ReqState{
		RequestID:   "req_3",
		ProjectID:   "proj_1",
		Environment: "prod",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		Identity:    &core.Identity{Subject: "bob"},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{{Role: "user", Content: "Bob Tokyo weather"}},
		},
	}

	err = loop.Run(ctx, stateBob, makeProvider())
	if err != nil {
		t.Fatalf("run 3 failed: %v", err)
	}
	if execCount != 2 {
		t.Fatalf("expected execCount 2 due to user isolation, got %d", execCount)
	}
	if !strings.Contains(stateBob.ChatResponse.Message.Content, "exec #2") {
		t.Fatalf("unexpected content for bob: %s", stateBob.ChatResponse.Message.Content)
	}
}

func TestDefaultExecutorRegistry_MCPAndSQL(t *testing.T) {
	registry := NewDefaultExecutorRegistry(nil, false)

	// Test MCP tool resolution
	mcpDef := core.ToolDefinition{
		ID:   "mcp_search",
		Name: "mcp_search",
		Executor: core.ExecutorConfig{
			Type: "mcp",
			URL:  "http://example.com/mcp",
		},
	}
	mcpExec, err := registry.GetExecutor(mcpDef)
	if err != nil {
		t.Fatalf("failed to resolve mcp executor: %v", err)
	}
	if _, ok := mcpExec.(*tools.MCPExecutor); !ok {
		t.Fatalf("expected *tools.MCPExecutor, got %T", mcpExec)
	}

	// Test SQL template tool resolution
	sqlDef := core.ToolDefinition{
		ID:   "sql_query",
		Name: "sql_query",
		Executor: core.ExecutorConfig{
			Type:     "sql_template",
			Template: "SELECT 1",
			URL:      "mock://db",
		},
	}
	sqlExec, err := registry.GetExecutor(sqlDef)
	if err != nil {
		t.Fatalf("failed to resolve sql_template executor: %v", err)
	}
	if _, ok := sqlExec.(*tools.SQLTemplateExecutor); !ok {
		t.Fatalf("expected *tools.SQLTemplateExecutor, got %T", sqlExec)
	}

	// Test SearXNG tool resolution
	searxngDef := core.ToolDefinition{
		ID:   "web_search",
		Name: "web_search",
		Executor: core.ExecutorConfig{
			Type: "searxng",
		},
	}
	searxngExec, err := registry.GetExecutor(searxngDef)
	if err != nil {
		t.Fatalf("failed to resolve searxng executor: %v", err)
	}
	if _, ok := searxngExec.(*tools.SearXNGExecutor); !ok {
		t.Fatalf("expected *tools.SearXNGExecutor, got %T", searxngExec)
	}
}

func TestLoop_Unary_WebSearchAutoTool(t *testing.T) {
	ctx := context.Background()

	// Mock SearXNG server
	searxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":             "latest golang",
			"number_of_results": 1,
			"results": []map[string]any{
				{
					"title":   "Go 1.24 is released",
					"url":     "https://go.dev/blog/go1.24",
					"content": "Announcement of Go 1.24.",
				},
			},
		})
	}))
	defer searxServer.Close()

	searxClient := tools.NewSearXNGClient(searxServer.URL, true, 2*time.Second, searxServer.Client())
	registry := NewDefaultExecutorRegistry(nil, false)
	registry.SetSearXNGClient(searxClient)

	snap := core.NewSnapshot("proj_1", "dev", 1)
	snap.Apps["search_assistant"] = &core.AIApp{
		ID:   "search_assistant",
		Name: "Search Assistant",
		Search: core.SearchConfig{
			Enabled:    true,
			MaxResults: 3,
		},
	}

	var step int
	provider := &mockProvider{
		chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			step++
			if step == 1 {
				// Verify web_search is in req.Tools
				found := false
				for _, t := range req.Tools {
					if t.Name == "web_search" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected web_search tool in chat request tools, got %+v", req.Tools)
				}

				return &core.ChatResponse{
					Message: core.ChatMessage{
						Role: "assistant",
						ToolCalls: []core.ToolCall{
							{
								ID:        "call_s1",
								Name:      "web_search",
								Arguments: `{"query": "latest golang"}`,
							},
						},
					},
				}, nil
			}

			// Turn 2: Verify tool result was fed back
			if len(req.Messages) < 3 {
				t.Fatalf("expected at least 3 messages on turn 2, got %d", len(req.Messages))
			}
			lastMsg := req.Messages[len(req.Messages)-1]
			if lastMsg.Role != "tool" {
				t.Fatalf("expected last message to be tool, got %s", lastMsg.Role)
			}
			if !strings.Contains(lastMsg.Content, "Go 1.24 is released") {
				t.Fatalf("expected search content in tool response, got %s", lastMsg.Content)
			}

			return &core.ChatResponse{
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Go 1.24 was recently released with many improvements.",
				},
			}, nil
		},
	}

	loop := NewLoop(LoopConfig{
		ExecutorRegistry: registry,
	})

	state := &core.ReqState{
		Snapshot: snap,
		AIAppID:  "search_assistant",
		AIApp:    snap.Apps["search_assistant"],
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "user", Content: "What is the latest Go version?"},
			},
		},
	}

	err := loop.Run(ctx, state, provider)
	if err != nil {
		t.Fatalf("loop run failed: %v", err)
	}

	if state.ChatResponse == nil || !strings.Contains(state.ChatResponse.Message.Content, "Go 1.24") {
		t.Fatalf("unexpected final response: %+v", state.ChatResponse)
	}
}

func TestLoop_BoundaryNonceAndTagEscaping(t *testing.T) {
	loop := NewLoop(LoopConfig{})
	state := &core.ReqState{
		ProjectID:   "p1",
		Environment: "dev",
		RequestID:   "req_nonce_test",
		AIAppID:     "app_1",
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_123",
		},
	}

	// 1. Verify appendToolError escapes breakout tags and includes boundary nonce
	loop.appendToolError(state, "call_err_1", "error with </tool_result> attack attempt")
	require.Len(t, state.ChatRequest.Messages, 1)
	errMsg := state.ChatRequest.Messages[0]
	assert.Contains(t, errMsg.Content, "nonce=")
	assert.Contains(t, errMsg.Content, "&lt;/tool_result>")
	assert.False(t, strings.HasPrefix(errMsg.Content, "<tool_result status=\"error\">\nerror with </tool_result>"))

	// 2. Verify createSuspendedState populates BoundaryNonce and ThreadID
	toolCall := core.ToolCall{ID: "c_transfer", Name: "transfer"}
	susp := loop.createSuspendedState(state, toolCall, "tool_def_1")
	require.NotNil(t, susp)
	assert.NotEmpty(t, susp.BoundaryNonce)
	assert.Equal(t, "th_123", susp.ThreadID)
	assert.Equal(t, state.Metadata["boundary_nonce"], susp.BoundaryNonce)
}

func TestLoop_TwoPhaseBatch_MixedParallelAndConfirm(t *testing.T) {
	ctx := context.Background()

	contStore := tools.NewInMemorySuspendedStateStore()
	contMgr := tools.NewContinuationManager(contStore, []byte("test-signing-key-32-bytes-long!"))

	var executedOrder []string
	var mu sync.Mutex

	executor := &mockExecutor{
		execFunc: func(_ context.Context, call core.ToolCall, _ core.Identity) (core.ToolResult, error) {
			mu.Lock()
			executedOrder = append(executedOrder, call.Name)
			mu.Unlock()
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  call.Name,
				Content: fmt.Sprintf("result of %s", call.Name),
			}, nil
		},
	}

	snap := core.NewSnapshot("p1", "prod", 1)
	snap.Tools["read_1"] = &core.ToolDefinition{
		ID:           "read_1",
		Name:         "read_1",
		SideEffect:   "read",
		Confirmation: "none",
		ParallelSafe: true,
	}
	snap.Tools["confirm_write"] = &core.ToolDefinition{
		ID:           "confirm_write",
		Name:         "confirm_write",
		SideEffect:   "write",
		Confirmation: "required",
		ParallelSafe: false,
	}
	snap.Tools["read_2"] = &core.ToolDefinition{
		ID:           "read_2",
		Name:         "read_2",
		SideEffect:   "read",
		Confirmation: "none",
		ParallelSafe: true,
	}

	snap.Apps["app_batch"] = &core.AIApp{
		ID:      "app_batch",
		ToolIDs: []string{"read_1", "confirm_write", "read_2"},
	}

	loop := NewLoop(LoopConfig{
		ContinuationMgr:  contMgr,
		ExecutorRegistry: &mockRegistry{executor: executor},
	})

	state := &core.ReqState{
		RequestID:   "req_batch_1",
		ProjectID:   "p1",
		Environment: "prod",
		AIAppID:     "app_batch",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_batch"],
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_batch",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Run batch"},
			},
		},
	}

	provider := &mockProvider{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				ID: "resp_batch",
				Message: core.ChatMessage{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "c1", Name: "read_1", Arguments: "{}"},
						{ID: "c2", Name: "confirm_write", Arguments: "{}"},
						{ID: "c3", Name: "read_2", Arguments: "{}"},
					},
				},
				Usage: core.UsageStats{TotalTokens: 100},
			}, nil
		},
	}

	err := loop.Run(ctx, state, provider)
	require.NoError(t, err)

	// 1. Verify loop suspended due to confirm_write
	require.NotNil(t, state.ChatResponse)
	assert.Equal(t, "confirmation_required", state.ChatResponse.FinishReason)
	assert.NotEmpty(t, state.ChatRequest.Metadata["confirmation_token"])

	// 2. Verify non-suspending tools read_1 and read_2 executed first
	mu.Lock()
	assert.Contains(t, executedOrder, "read_1")
	assert.Contains(t, executedOrder, "read_2")
	assert.NotContains(t, executedOrder, "confirm_write") // Not executed yet, suspended!
	mu.Unlock()

	// 3. Verify messages slice contains results for read_1 and read_2 in deterministic order
	msgs := state.ChatRequest.Messages
	// Expected: [user, assistant (with 3 tool calls), tool:read_1, tool:read_2]
	require.Len(t, msgs, 4)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "tool", msgs[2].Role)
	assert.Equal(t, "c1", msgs[2].ToolCallID)
	assert.Equal(t, "tool", msgs[3].Role)
	assert.Equal(t, "c3", msgs[3].ToolCallID)

	// 4. Verify suspended state has confirm_write stored in PendingToolCalls
	susp, err := contStore.Get(ctx, "p1", "req_batch_1")
	require.NoError(t, err)
	require.NotNil(t, susp)
	assert.Equal(t, "confirm_write", susp.PendingToolCall.Name)
	require.Len(t, susp.PendingToolCalls, 1)
	assert.Equal(t, "confirm_write", susp.PendingToolCalls[0].Name)
}

func TestLoop_BuiltinAskUser_Suspension(t *testing.T) {
	ctx := context.Background()

	contStore := tools.NewInMemorySuspendedStateStore()
	contMgr := tools.NewContinuationManager(contStore, []byte("test-signing-key-32-bytes-long!"))

	snap := core.NewSnapshot("p1", "dev", 1)
	snap.Apps["app_ask"] = &core.AIApp{
		ID:      "app_ask",
		ToolIDs: []string{"ask_user"},
	}

	loop := NewLoop(LoopConfig{
		ContinuationMgr: contMgr,
	})

	// 1. Unary turn suspension
	state := &core.ReqState{
		RequestID:   "req_ask_1",
		ProjectID:   "p1",
		Environment: "dev",
		AIAppID:     "app_ask",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_ask"],
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_ask",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "I want to book a ticket"},
			},
		},
	}

	provider := &mockProvider{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				ID: "resp_ask",
				Message: core.ChatMessage{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{
							ID:        "c_ask",
							Name:      "ask_user",
							Arguments: `{"question":"Window or aisle?","options":["window","aisle"]}`,
						},
					},
				},
				Usage: core.UsageStats{TotalTokens: 50},
			}, nil
		},
	}

	err := loop.Run(ctx, state, provider)
	require.NoError(t, err)

	require.NotNil(t, state.ChatResponse)
	assert.Equal(t, "question_required", state.ChatResponse.FinishReason)
	assert.NotEmpty(t, state.ChatRequest.Metadata["confirmation_token"])

	susp, err := contStore.Get(ctx, "p1", "req_ask_1")
	require.NoError(t, err)
	assert.Equal(t, "ask_user", susp.PendingToolCall.Name)

	// 2. Streaming turn question event
	streamState := &core.ReqState{
		RequestID:   "req_ask_stream",
		ProjectID:   "p1",
		Environment: "dev",
		AIAppID:     "app_ask",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_ask"],
		IsStreaming: true,
		StreamChan:  make(chan core.StreamEvent, 16),
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_ask",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Stream ask"},
			},
		},
	}

	streamProvider := &mockProvider{
		streamFunc: func(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
			ch := make(chan core.StreamEvent, 4)
			go func() {
				defer close(ch)
				ch <- core.StreamEvent{
					Type: core.StreamEventToolCall,
					ToolCall: &core.ToolCall{
						ID:        "c_stream_ask",
						Name:      "ask_user",
						Arguments: `{"question":"Confirm proceed?","options":["yes","no"]}`,
					},
				}
				ch <- core.StreamEvent{Type: core.StreamEventDone}
			}()
			return ch, nil
		},
	}

	err = loop.Run(ctx, streamState, streamProvider)
	require.NoError(t, err)
	close(streamState.StreamChan)

	var emittedEvents []core.StreamEvent
	for ev := range streamState.StreamChan {
		emittedEvents = append(emittedEvents, ev)
	}

	require.NotEmpty(t, emittedEvents)
	var foundQuestion bool
	for _, ev := range emittedEvents {
		if ev.Type == core.StreamEventQuestion {
			foundQuestion = true
			assert.NotEmpty(t, ev.ConfirmationToken)
			qMap, ok := ev.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "Confirm proceed?", qMap["question"])
		}
	}
	assert.True(t, foundQuestion, "stream should emit StreamEventQuestion")
}

func TestLoop_BuiltinSubAgent(t *testing.T) {
	ctx := context.Background()

	var childToolsReceived []core.ToolDefinition

	childProvider := &mockProvider{
		chatFunc: func(_ context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			childToolsReceived = req.Tools
			return &core.ChatResponse{
				ID: "resp_child",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Subtask completed successfully with calculation 42.",
				},
				Usage: core.UsageStats{
					TotalTokens: 30,
				},
			}, nil
		},
	}

	snap := core.NewSnapshot("p1", "dev", 1)
	snap.Tools["safe_read"] = &core.ToolDefinition{
		ID:           "safe_read",
		Name:         "safe_read",
		SideEffect:   "read",
		Confirmation: "none",
	}
	snap.Tools["write_danger"] = &core.ToolDefinition{
		ID:           "write_danger",
		Name:         "write_danger",
		SideEffect:   "write",
		Confirmation: "required",
	}
	snap.Tools["client_tool"] = &core.ToolDefinition{
		ID:   "client_tool",
		Name: "client_tool",
		Executor: core.ExecutorConfig{
			Type: "client_side",
		},
	}

	snap.Apps["app_sub"] = &core.AIApp{
		ID:      "app_sub",
		ToolIDs: []string{"safe_read", "write_danger", "client_tool", "ask_user", "sub_agent"},
	}

	loop := NewLoop(LoopConfig{})

	// 1. Normal execution with depth 0
	tokenBudget := core.NewTokenBudgetTracker(100)

	state := &core.ReqState{
		RequestID:       "req_sub_parent",
		ProjectID:       "p1",
		Environment:     "dev",
		AIAppID:         "app_sub",
		Snapshot:        snap,
		AIApp:           snap.Apps["app_sub"],
		TokenBudget:     tokenBudget,
		ResolvedAdapter: childProvider,
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Calculate subtask"},
			},
		},
	}

	subToolDef := tools.BuiltinSubAgentToolDefinition()
	subToolCall := core.ToolCall{
		ID:        "c_sub_1",
		Name:      "sub_agent",
		Arguments: `{"task":"compute 6*7","context":"mathematics"}`,
	}

	toolMsg, err := loop.executeSubAgent(ctx, state, &subToolDef, subToolCall)
	require.NoError(t, err)
	assert.Contains(t, toolMsg.Content, "Subtask completed successfully with calculation 42.")
	assert.Equal(t, int64(30), tokenBudget.UsedTokens.Load())

	// Verify suspending tools were stripped from child tools
	var strippedToolNames []string
	for _, ct := range childToolsReceived {
		strippedToolNames = append(strippedToolNames, ct.Name)
	}
	assert.Contains(t, strippedToolNames, "safe_read")
	assert.NotContains(t, strippedToolNames, "write_danger") // confirmation: required stripped!
	assert.NotContains(t, strippedToolNames, "client_tool")  // client_side stripped!
	assert.NotContains(t, strippedToolNames, "ask_user")     // ask_user stripped!
	assert.NotContains(t, strippedToolNames, "sub_agent")    // recursion prevention stripped!

	// 2. Bounded recursion depth: if current depth >= 1, reject
	stateDepth1 := &core.ReqState{
		RequestID: "req_sub_nested",
		Metadata: map[string]any{
			"sub_agent_depth": 1,
		},
	}
	nestedMsg, err := loop.executeSubAgent(ctx, stateDepth1, &subToolDef, subToolCall)
	require.NoError(t, err)
	assert.Contains(t, nestedMsg.Content, "sub_agent recursion depth limit exceeded (max depth: 1)")

	// 3. Token runaway prevention: when token budget is exceeded
	budgetExhausted := core.NewTokenBudgetTracker(10)
	budgetExhausted.UsedTokens.Store(10) // 0 remaining

	stateExhausted := &core.ReqState{
		RequestID:   "req_sub_exhausted",
		TokenBudget: budgetExhausted,
	}
	budgetMsg, err := loop.executeSubAgent(ctx, stateExhausted, &subToolDef, subToolCall)
	require.NoError(t, err)
	assert.Contains(t, budgetMsg.Content, "token budget exceeded")
}

func TestLoop_SubAgentChildToolExecution(t *testing.T) {
	ctx := context.Background()

	turn := 0
	childProvider := &mockProvider{
		chatFunc: func(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
			turn++
			if turn == 1 {
				// Turn 1: model returns tool call for safe_read
				return &core.ChatResponse{
					ID: "resp_child_turn1",
					Message: core.ChatMessage{
						Role: "assistant",
						ToolCalls: []core.ToolCall{
							{
								ID:        "c_child_read",
								Name:      "safe_read",
								Arguments: `{"path":"/secret.txt"}`,
							},
						},
					},
					Usage: core.UsageStats{TotalTokens: 25},
				}, nil
			}

			// Turn 2: verify child tool result is in messages, return final text
			require.GreaterOrEqual(t, len(req.Messages), 3)
			lastMsg := req.Messages[len(req.Messages)-1]
			assert.Equal(t, "tool", lastMsg.Role)
			assert.Equal(t, "c_child_read", lastMsg.ToolCallID)
			assert.Contains(t, lastMsg.Content, "file content: classified data")

			return &core.ChatResponse{
				ID: "resp_child_turn2",
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Analysis based on file: classified data.",
				},
				Usage: core.UsageStats{TotalTokens: 35},
			}, nil
		},
	}

	exec := &mockExecutor{
		execFunc: func(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
			assert.Equal(t, "c_child_read", call.ID)
			return core.ToolResult{
				CallID:  call.ID,
				Content: "file content: classified data",
			}, nil
		},
	}

	snap := core.NewSnapshot("p1", "dev", 1)
	snap.Tools["safe_read"] = &core.ToolDefinition{
		ID:           "safe_read",
		Name:         "safe_read",
		SideEffect:   "read",
		Confirmation: "none",
	}
	snap.Apps["app_sub"] = &core.AIApp{
		ID:      "app_sub",
		ToolIDs: []string{"safe_read", "sub_agent"},
	}

	loop := NewLoop(LoopConfig{
		ExecutorRegistry: &mockRegistry{executor: exec},
	})

	tokenBudget := core.NewTokenBudgetTracker(200)
	state := &core.ReqState{
		RequestID:       "req_sub_tool_exec",
		ProjectID:       "p1",
		Environment:     "dev",
		AIAppID:         "app_sub",
		Snapshot:        snap,
		AIApp:           snap.Apps["app_sub"],
		TokenBudget:     tokenBudget,
		ResolvedAdapter: childProvider,
	}

	subToolDef := tools.BuiltinSubAgentToolDefinition()
	subToolCall := core.ToolCall{
		ID:        "c_sub_parent",
		Name:      "sub_agent",
		Arguments: `{"task":"read file and summarize"}`,
	}

	toolMsg, err := loop.executeSubAgent(ctx, state, &subToolDef, subToolCall)
	require.NoError(t, err)
	assert.Contains(t, toolMsg.Content, "Analysis based on file: classified data.")
	// Both turns accounted for: 25 + 35 = 60 tokens
	assert.Equal(t, int64(60), tokenBudget.UsedTokens.Load())
	assert.Equal(t, 60, state.Usage.TotalTokens)
}

func TestLoop_ResolveBuiltinDocumentAndKnowledgeTools(t *testing.T) {
	ctx := context.Background()

	snap := core.NewSnapshot("proj_test", "prod", 42)
	snap.Apps["app_kb"] = &core.AIApp{
		ID:      "app_kb",
		ToolIDs: []string{"propose_memory"},
	}
	snap.KnowledgeCatalog["app_kb"] = []core.KnowledgeDocMeta{
		{ID: "doc_1", Title: "Runbook"},
	}

	loop := NewLoop(LoopConfig{})

	state := &core.ReqState{
		ProjectID:   "proj_test",
		Environment: "prod",
		AIAppID:     "app_kb",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_kb"],
	}

	// 1. findToolDef resolves both builtins
	propDef, ok := loop.findToolDef(state, "propose_memory")
	require.True(t, ok)
	assert.Equal(t, "propose_memory", propDef.Name)

	kbDef, ok := loop.findToolDef(state, "knowledge_search")
	require.True(t, ok)
	assert.Equal(t, "knowledge_search", kbDef.Name)

	// 2. resolveTools resolves propose_memory from ToolIDs AND auto-injects knowledge_search from KnowledgeCatalog
	resolved := loop.resolveTools(state)
	require.Len(t, resolved, 2)
	toolNames := []string{resolved[0].Name, resolved[1].Name}
	assert.Contains(t, toolNames, "propose_memory")
	assert.Contains(t, toolNames, "knowledge_search")

	// 3. executeServerTool propagates snapshot and appID to executor context
	var receivedSnap *core.Snapshot
	var receivedAppID string
	testExec := &mockExecutor{
		execFunc: func(execCtx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
			s, _ := core.SnapshotFromContext(execCtx)
			receivedSnap = s
			a, _ := core.AppIDFromContext(execCtx)
			receivedAppID = a
			return core.ToolResult{CallID: call.ID, Content: "ok"}, nil
		},
	}
	execLoop := NewLoop(LoopConfig{
		ExecutorRegistry: &mockRegistry{executor: testExec},
	})
	_, err := execLoop.executeServerTool(ctx, state, propDef, core.ToolCall{ID: "tc_1", Name: "propose_memory"})
	require.NoError(t, err)
	require.NotNil(t, receivedSnap)
	assert.Equal(t, "proj_test", receivedSnap.ProjectID)
	assert.Equal(t, int64(42), receivedSnap.Revision)
	assert.Equal(t, "app_kb", receivedAppID)
}




