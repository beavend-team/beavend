package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
