package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/tools"
)

var (
	// ErrMaxDepthExceeded indicates the agent loop reached the maximum allowed iterations.
	ErrMaxDepthExceeded = errors.New("agent loop max depth exceeded")
	// ErrToolNotFound indicates a requested tool is not defined in the active snapshot.
	ErrToolNotFound = errors.New("tool definition not found in snapshot")

	agentMeter              = otel.Meter("beavend/agent")
	cacheRequestsCounter, _ = agentMeter.Int64Counter("beavend_cache_requests_total", metric.WithDescription("Total count of cache lookups partitioned by type and hit status"))
)

const (
	// DefaultMaxDepth is the default upper bound on agent loop iterations.
	DefaultMaxDepth = 5
)

// ExecutorRegistry resolves core.Executor instances for tool definitions.
type ExecutorRegistry interface {
	GetExecutor(def core.ToolDefinition) (core.Executor, error)
}

// DefaultExecutorRegistry creates standard tool executors (HTTP, Webhook, etc.).
type DefaultExecutorRegistry struct {
	authenticator core.Authenticator
	allowPrivate  bool
}

// NewDefaultExecutorRegistry creates a new DefaultExecutorRegistry.
func NewDefaultExecutorRegistry(authenticator core.Authenticator, allowPrivate bool) *DefaultExecutorRegistry {
	return &DefaultExecutorRegistry{
		authenticator: authenticator,
		allowPrivate:  allowPrivate,
	}
}

// GetExecutor instantiates or returns an executor for the specified tool definition.
func (r *DefaultExecutorRegistry) GetExecutor(def core.ToolDefinition) (core.Executor, error) {
	switch def.Executor.Type {
	case "http", "webhook", "":
		client := tools.NewSafeHTTPClient(tools.SafeDialerConfig{AllowPrivate: r.allowPrivate}, 15*time.Second)
		return tools.NewHTTPExecutor(def, client, r.authenticator), nil
	default:
		return nil, fmt.Errorf("unsupported executor type: %s", def.Executor.Type)
	}
}

// LoopConfig configures the bounded agent loop.
type LoopConfig struct {
	MaxDepth         int
	PolicyEvaluator  core.PolicyEvaluator
	ContinuationMgr  *tools.ContinuationManager
	ExecutorRegistry ExecutorRegistry
	Sanitizer        *tools.Sanitizer
	CacheStore       core.CacheStore
	Logger           *slog.Logger
}

// Loop executes the multi-turn agent decision cycle.
type Loop struct {
	cfg LoopConfig
}

// NewLoop creates a new agent Loop.
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = DefaultMaxDepth
	}
	if cfg.ContinuationMgr == nil {
		cfg.ContinuationMgr = tools.NewContinuationManager(nil, nil)
	}
	if cfg.ExecutorRegistry == nil {
		cfg.ExecutorRegistry = NewDefaultExecutorRegistry(nil, false)
	}
	if cfg.Sanitizer == nil {
		cfg.Sanitizer = tools.NewSanitizer()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Loop{cfg: cfg}
}

// ContinuationManager returns the configured continuation manager.
func (l *Loop) ContinuationManager() *tools.ContinuationManager {
	return l.cfg.ContinuationMgr
}

// Run executes the bounded agent loop for unary or streaming requests.
func (l *Loop) Run(ctx context.Context, state *core.ReqState, adapter core.ProviderAdapter) error {
	if state == nil {
		return errors.New("nil req state")
	}
	if state.ChatRequest == nil {
		return errors.New("nil chat request in state")
	}

	// 1. Resolve available tools from Snapshot for this AI App
	availableTools := l.resolveTools(state)
	state.ChatRequest.Tools = availableTools

	for depth := 0; depth < l.cfg.MaxDepth; depth++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if state.IsStreaming {
			suspended, hasToolCalls, err := l.runStreamingTurn(ctx, depth, state, adapter)
			if err != nil {
				return err
			}
			if suspended || !hasToolCalls {
				return nil // Loop suspended or final text completion delivered
			}
		} else {
			suspended, hasToolCalls, err := l.runUnaryTurn(ctx, depth, state, adapter)
			if err != nil {
				return err
			}
			if suspended || !hasToolCalls {
				return nil // Suspended waiting for user confirmation or final completion returned
			}
		}
	}

	return ErrMaxDepthExceeded
}

func (l *Loop) resolveTools(state *core.ReqState) []core.ToolDefinition {
	if state.Snapshot == nil || state.AIApp == nil || len(state.AIApp.ToolIDs) == 0 {
		return nil
	}

	var res []core.ToolDefinition
	for _, toolID := range state.AIApp.ToolIDs {
		if def, ok := state.Snapshot.Tool(toolID); ok && def != nil {
			res = append(res, *def)
		}
	}
	return res
}

func (l *Loop) runUnaryTurn(ctx context.Context, _ int, state *core.ReqState, adapter core.ProviderAdapter) (bool, bool, error) {
	resp, err := adapter.Chat(ctx, *state.ChatRequest)
	if err != nil {
		return false, false, err
	}

	// If no tool calls requested, model returned final response
	if len(resp.Message.ToolCalls) == 0 {
		state.ChatResponse = resp
		return false, false, nil
	}

	// Append assistant message turn (with requested tool calls)
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, resp.Message)

	// Execute requested tools
	for _, toolCall := range resp.Message.ToolCalls {
		toolDef, ok := state.Snapshot.Tool(toolCall.Name)
		if !ok {
			// Also check by ID
			toolDef, ok = state.Snapshot.Tool(toolCall.ID)
		}

		if !ok || toolDef == nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q not found in snapshot", toolCall.Name))
			continue
		}

		// Tool Authorization check
		if toolDef.Authorization != "" && l.cfg.PolicyEvaluator != nil {
			allowed, err := l.cfg.PolicyEvaluator.Evaluate(ctx, toolDef.Authorization, state.Identity, map[string]any{
				"tool": toolDef.Name,
				"app":  state.AIAppID,
			})
			if err != nil || !allowed {
				l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q authorization denied by policy %q", toolCall.Name, toolDef.Authorization))
				continue
			}
		}

		// Inject identity bindings (confused-deputy defense)
		var args map[string]any
		if toolCall.Arguments != "" {
			_ = json.Unmarshal([]byte(toolCall.Arguments), &args)
		}
		if args == nil {
			args = make(map[string]any)
		}
		args = tools.InjectIdentityBindings(args, toolDef.IdentityBindings, state.Identity)
		boundArgsBytes, _ := json.Marshal(args)
		toolCall.Arguments = string(boundArgsBytes)

		// Validate arguments against JSON Schema
		if err := tools.ValidateArguments(args, state.Snapshot, toolDef); err != nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("arguments validation failed: %v", err))
			continue
		}

		// Check for confirmation requirement
		if requiresConfirmation(toolDef) {
			susp := l.createSuspendedState(state, toolCall, toolDef.ID)
			if err := l.cfg.ContinuationMgr.Store().Save(ctx, susp, tools.DefaultSuspensionTTL); err != nil {
				return false, false, fmt.Errorf("save suspended state: %w", err)
			}
			token, err := l.cfg.ContinuationMgr.IssueConfirmationToken(susp, toolCall, tools.DefaultSuspensionTTL)
			if err != nil {
				return false, false, fmt.Errorf("issue confirmation token: %w", err)
			}

			// Return response indicating confirmation required
			state.ChatResponse = &core.ChatResponse{
				ID:        fmt.Sprintf("chatcmpl_%s", state.RequestID),
				RequestID: state.RequestID,
				AIAppID:   state.AIAppID,
				CreatedAt: time.Now().UTC().Unix(),
				Message: core.ChatMessage{
					Role:      "assistant",
					ToolCalls: []core.ToolCall{toolCall},
				},
				FinishReason: "confirmation_required",
				Usage: core.UsageStats{
					TotalTokens: resp.Usage.TotalTokens,
				},
			}
			if state.ChatRequest.Metadata == nil {
				state.ChatRequest.Metadata = make(map[string]string)
			}
			state.ChatRequest.Metadata["confirmation_token"] = token
			return true, true, nil // Suspended!
		}

		// Check client-side tool
		if toolDef.Executor.Type == "client_side" {
			susp := l.createSuspendedState(state, toolCall, toolDef.ID)
			if err := l.cfg.ContinuationMgr.Store().Save(ctx, susp, tools.DefaultSuspensionTTL); err != nil {
				return false, false, fmt.Errorf("save suspended state: %w", err)
			}
			token, err := l.cfg.ContinuationMgr.IssueConfirmationToken(susp, toolCall, tools.DefaultSuspensionTTL)
			if err != nil {
				return false, false, fmt.Errorf("issue continuation token: %w", err)
			}

			state.ChatResponse = &core.ChatResponse{
				ID:        fmt.Sprintf("chatcmpl_%s", state.RequestID),
				RequestID: state.RequestID,
				AIAppID:   state.AIAppID,
				CreatedAt: time.Now().UTC().Unix(),
				Message: core.ChatMessage{
					Role:      "assistant",
					ToolCalls: []core.ToolCall{toolCall},
				},
				FinishReason: "client_tool",
			}
			if state.ChatRequest.Metadata == nil {
				state.ChatRequest.Metadata = make(map[string]string)
			}
			state.ChatRequest.Metadata["continuation_token"] = token
			return true, true, nil
		}

		// Server-side tool execution
		toolMsg, err := l.executeServerTool(ctx, state, toolDef, toolCall)
		if err != nil {
			l.appendToolError(state, toolCall.ID, err.Error())
			continue
		}
		state.ChatRequest.Messages = append(state.ChatRequest.Messages, toolMsg)
	}

	return false, true, nil
}

func (l *Loop) runStreamingTurn(ctx context.Context, _ int, state *core.ReqState, adapter core.ProviderAdapter) (bool, bool, error) {
	streamEvents, err := adapter.Stream(ctx, *state.ChatRequest)
	if err != nil {
		return false, false, err
	}

	var (
		accumulatedToolCalls []core.ToolCall
		currentContent       string
	)

	for event := range streamEvents {
		if event.RequestID == "" {
			event.RequestID = state.RequestID
		}
		if event.Usage != nil {
			state.Usage = *event.Usage
		}

		if event.Type == "tool_call" && event.ToolCall != nil {
			accumulatedToolCalls = append(accumulatedToolCalls, *event.ToolCall)
			continue
		}

		if event.Type == "delta" {
			currentContent += event.Delta
			// Forward delta chunk to client SSE stream channel
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- event:
			}
			continue
		}

		if event.Type == "done" && len(accumulatedToolCalls) == 0 {
			// Model finished completion without requesting tools
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- event:
			}
			return false, false, nil
		}

		// Forward any other events (such as "usage")
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case state.StreamChan <- event:
		}
	}

	if len(accumulatedToolCalls) == 0 {
		return false, false, nil
	}

	// Model requested tool calls during streaming
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, core.ChatMessage{
		Role:      "assistant",
		Content:   currentContent,
		ToolCalls: accumulatedToolCalls,
	})

	for _, toolCall := range accumulatedToolCalls {
		toolDef, ok := state.Snapshot.Tool(toolCall.Name)
		if !ok {
			toolDef, ok = state.Snapshot.Tool(toolCall.ID)
		}

		if !ok || toolDef == nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q not found in snapshot", toolCall.Name))
			continue
		}

		// Tool Authorization check
		if toolDef.Authorization != "" && l.cfg.PolicyEvaluator != nil {
			allowed, err := l.cfg.PolicyEvaluator.Evaluate(ctx, toolDef.Authorization, state.Identity, map[string]any{
				"tool": toolDef.Name,
				"app":  state.AIAppID,
			})
			if err != nil || !allowed {
				l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q authorization denied by policy", toolCall.Name))
				continue
			}
		}

		// Inject identity bindings
		var args map[string]any
		if toolCall.Arguments != "" {
			_ = json.Unmarshal([]byte(toolCall.Arguments), &args)
		}
		if args == nil {
			args = make(map[string]any)
		}
		args = tools.InjectIdentityBindings(args, toolDef.IdentityBindings, state.Identity)
		boundArgsBytes, _ := json.Marshal(args)
		toolCall.Arguments = string(boundArgsBytes)

		// Validate schema
		if err := tools.ValidateArguments(args, state.Snapshot, toolDef); err != nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("arguments validation failed: %v", err))
			continue
		}

		// Confirmation check
		if requiresConfirmation(toolDef) {
			susp := l.createSuspendedState(state, toolCall, toolDef.ID)
			if err := l.cfg.ContinuationMgr.Store().Save(ctx, susp, tools.DefaultSuspensionTTL); err != nil {
				return false, false, fmt.Errorf("save suspended state: %w", err)
			}
			token, err := l.cfg.ContinuationMgr.IssueConfirmationToken(susp, toolCall, tools.DefaultSuspensionTTL)
			if err != nil {
				return false, false, fmt.Errorf("issue confirmation token: %w", err)
			}

			tc := toolCall
			// Emit SSE confirmation event and terminate stream
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- core.StreamEvent{
				Type:              "confirmation",
				ConfirmationToken: token,
				ToolCall:          &tc,
				RequestID:         state.RequestID,
			}:
			}

			select {
			case <-ctx.Done():
			case state.StreamChan <- core.StreamEvent{Type: "done"}:
			}

			return true, true, nil // Suspended!
		}

		// Client-side tool
		if toolDef.Executor.Type == "client_side" {
			susp := l.createSuspendedState(state, toolCall, toolDef.ID)
			if err := l.cfg.ContinuationMgr.Store().Save(ctx, susp, tools.DefaultSuspensionTTL); err != nil {
				return false, false, fmt.Errorf("save suspended state: %w", err)
			}
			token, err := l.cfg.ContinuationMgr.IssueConfirmationToken(susp, toolCall, tools.DefaultSuspensionTTL)
			if err != nil {
				return false, false, fmt.Errorf("issue continuation token: %w", err)
			}

			tc := toolCall
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- core.StreamEvent{
				Type:              "tool_call",
				ConfirmationToken: token,
				ToolCall:          &tc,
				RequestID:         state.RequestID,
			}:
			}

			select {
			case <-ctx.Done():
			case state.StreamChan <- core.StreamEvent{Type: "done"}:
			}

			return true, true, nil
		}

		// Server tool execution
		toolMsg, err := l.executeServerTool(ctx, state, toolDef, toolCall)
		if err != nil {
			l.appendToolError(state, toolCall.ID, err.Error())
			continue
		}
		state.ChatRequest.Messages = append(state.ChatRequest.Messages, toolMsg)
	}

	return false, true, nil
}

func (l *Loop) appendToolError(state *core.ReqState, toolCallID, message string) {
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, core.ChatMessage{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    fmt.Sprintf("<tool_result status=\"error\">\n%s\n</tool_result>", message),
	})
}

func (l *Loop) createSuspendedState(state *core.ReqState, toolCall core.ToolCall, toolDefID string) *tools.SuspendedState {
	subject := ""
	issuer := ""
	if state.Identity != nil {
		subject = state.Identity.Subject
		issuer = state.Identity.Issuer
	}

	var rev int64
	if state.Snapshot != nil {
		rev = state.Snapshot.Revision
	}

	var msgs []core.ChatMessage
	if state.ChatRequest != nil && len(state.ChatRequest.Messages) > 0 {
		msgs = make([]core.ChatMessage, len(state.ChatRequest.Messages))
		copy(msgs, state.ChatRequest.Messages)
	}

	return &tools.SuspendedState{
		ProjectID:        state.ProjectID,
		Environment:      state.Environment,
		RequestID:        state.RequestID,
		AIAppID:          state.AIAppID,
		Subject:          subject,
		Issuer:           issuer,
		Messages:         msgs,
		PendingToolCall:  toolCall,
		ToolDefinitionID: toolDefID,
		SnapshotRevision: rev,
		CreatedAt:        time.Now().UTC(),
		IsStreaming:      state.IsStreaming,
	}
}

func (l *Loop) executeServerTool(ctx context.Context, state *core.ReqState, toolDef *core.ToolDefinition, toolCall core.ToolCall) (core.ChatMessage, error) {
	subject := "anonymous"
	if state.Identity != nil && state.Identity.Subject != "" {
		subject = state.Identity.Subject
	}

	// 1. Tool cache check
	if toolDef.Cache.Enabled && l.cfg.CacheStore != nil {
		argsHash := cache.ComputeToolArgsHash(toolCall.Arguments)
		cacheKey := cache.ToolCacheKey(state.ProjectID, state.Environment, subject, toolDef.ID, argsHash)

		cachedBytes, err := l.cfg.CacheStore.Get(ctx, cacheKey)
		if err == nil && cachedBytes != nil {
			var cachedRes core.ToolResult
			if err := json.Unmarshal(cachedBytes, &cachedRes); err == nil {
				l.cfg.Logger.InfoContext(ctx, "tool result cache hit",
					"tool", toolDef.Name,
					"tool_id", toolDef.ID,
					"cache_key", cacheKey,
					"request_id", state.RequestID,
				)
				if cacheRequestsCounter != nil {
					cacheRequestsCounter.Add(ctx, 1, metric.WithAttributes(
						attribute.String("cache_type", "tool"),
						attribute.String("status", "hit"),
						attribute.String("tool_id", toolDef.ID),
					))
				}
				return core.ChatMessage{
					Role:       "tool",
					ToolCallID: toolCall.ID,
					Content:    cachedRes.Content,
				}, nil
			}
		}

		if cacheRequestsCounter != nil {
			cacheRequestsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("cache_type", "tool"),
				attribute.String("status", "miss"),
				attribute.String("tool_id", toolDef.ID),
			))
		}
	}

	// 2. Resolve executor
	executor, err := l.cfg.ExecutorRegistry.GetExecutor(*toolDef)
	if err != nil {
		return core.ChatMessage{}, fmt.Errorf("resolve executor: %w", err)
	}

	var identity core.Identity
	if state.Identity != nil {
		identity = *state.Identity
	}

	// 3. Execute
	result, err := executor.Execute(ctx, toolCall, identity)
	if err != nil {
		result = core.ToolResult{
			CallID:  toolCall.ID,
			ToolID:  toolDef.ID,
			Content: fmt.Sprintf("execution error: %v", err),
			IsError: true,
		}
	}

	// 4. Sanitize untrusted data
	sanitized := l.cfg.Sanitizer.Sanitize(result, toolDef.Result)

	// 5. Store in Tool Cache if enabled and execution succeeded without error
	if toolDef.Cache.Enabled && l.cfg.CacheStore != nil && !sanitized.IsError {
		argsHash := cache.ComputeToolArgsHash(toolCall.Arguments)
		cacheKey := cache.ToolCacheKey(state.ProjectID, state.Environment, subject, toolDef.ID, argsHash)

		ttl := time.Duration(toolDef.Cache.TTL) * time.Second
		if ttl <= 0 {
			ttl = 300 * time.Second
		}

		if data, err := json.Marshal(sanitized); err == nil {
			if setErr := l.cfg.CacheStore.Set(ctx, cacheKey, data, ttl); setErr != nil {
				l.cfg.Logger.WarnContext(ctx, "failed to cache tool result",
					"cache_key", cacheKey,
					"error", setErr,
				)
			}
		}
	}

	return core.ChatMessage{
		Role:       "tool",
		ToolCallID: toolCall.ID,
		Content:    sanitized.Content,
	}, nil
}

func requiresConfirmation(def *core.ToolDefinition) bool {
	if def == nil {
		return false
	}
	if def.Confirmation == "required" {
		return true
	}
	if def.SideEffect == "write" && def.Confirmation != "none" {
		return true
	}
	return false
}

// Ensure policy evaluator import compiles if needed
var _ core.PolicyEvaluator = (*policy.Evaluator)(nil)
