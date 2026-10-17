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
	"github.com/beavend/beavend/internal/media"
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

// DefaultExecutorRegistry creates standard tool executors (HTTP, Webhook, MCP, SQL template, media generation, etc.).
type DefaultExecutorRegistry struct {
	authenticator core.Authenticator
	allowPrivate  bool
	sqlPool       *tools.SQLConnectionPool
	searxngClient *tools.SearXNGClient
	mediaStore    core.MediaStore
	jobStore      media.JobStore
	resolver      tools.ModelAdapterResolver
}

// NewDefaultExecutorRegistry creates a new DefaultExecutorRegistry.
func NewDefaultExecutorRegistry(authenticator core.Authenticator, allowPrivate bool) *DefaultExecutorRegistry {
	return &DefaultExecutorRegistry{
		authenticator: authenticator,
		allowPrivate:  allowPrivate,
		sqlPool:       tools.DefaultSQLConnectionPool(),
	}
}

// NewDefaultExecutorRegistryWithPool creates a DefaultExecutorRegistry with a custom SQLConnectionPool.
func NewDefaultExecutorRegistryWithPool(authenticator core.Authenticator, allowPrivate bool, pool *tools.SQLConnectionPool) *DefaultExecutorRegistry {
	if pool == nil {
		pool = tools.DefaultSQLConnectionPool()
	}
	return &DefaultExecutorRegistry{
		authenticator: authenticator,
		allowPrivate:  allowPrivate,
		sqlPool:       pool,
	}
}

// SetSearXNGClient configures the SearXNG search client for web search tools.
func (r *DefaultExecutorRegistry) SetSearXNGClient(client *tools.SearXNGClient) {
	r.searxngClient = client
}

// SetMediaServices configures media storage, job storage, and resolver for media generation tools.
func (r *DefaultExecutorRegistry) SetMediaServices(mediaStore core.MediaStore, jobStore media.JobStore, resolver tools.ModelAdapterResolver) {
	r.mediaStore = mediaStore
	r.jobStore = jobStore
	r.resolver = resolver
}

// GetExecutor instantiates or returns an executor for the specified tool definition.
func (r *DefaultExecutorRegistry) GetExecutor(def core.ToolDefinition) (core.Executor, error) {
	switch def.Executor.Type {
	case "http", "webhook", "":
		client := tools.NewSafeHTTPClient(tools.SafeDialerConfig{AllowPrivate: r.allowPrivate}, 15*time.Second)
		return tools.NewHTTPExecutor(def, client, r.authenticator), nil
	case "mcp":
		client := tools.NewSafeHTTPClient(tools.SafeDialerConfig{AllowPrivate: r.allowPrivate}, 15*time.Second)
		return tools.NewMCPExecutor(def, client, r.authenticator), nil
	case "sql_template":
		pool := r.sqlPool
		if pool == nil {
			pool = tools.DefaultSQLConnectionPool()
		}
		return tools.NewSQLTemplateExecutor(def, pool), nil
	case "searxng":
		var cfg core.SearchConfig
		if def.Executor.Template != "" {
			_ = json.Unmarshal([]byte(def.Executor.Template), &cfg)
		}
		return tools.NewSearXNGExecutor(r.searxngClient, cfg), nil
	case "media_generate":
		client := tools.NewSafeHTTPClient(tools.SafeDialerConfig{AllowPrivate: r.allowPrivate}, 30*time.Second)
		return tools.NewMediaToolExecutor(tools.MediaToolExecutorConfig{
			MediaStore:   r.mediaStore,
			JobStore:     r.jobStore,
			Resolver:     r.resolver,
			SSRFClient:   client,
			LogicalModel: def.Executor.Template,
		}), nil
	case "skill_loader":
		return tools.NewSkillLoaderExecutor(def), nil
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

	if state.Snapshot != nil {
		if _, ok := core.SnapshotFromContext(ctx); !ok {
			ctx = core.ContextWithSnapshot(ctx, state.Snapshot)
		}
	}

	// 1. Resolve available tools from Snapshot for this AI App
	availableTools := l.resolveTools(state)
	state.ChatRequest.Tools = availableTools

	maxDepth := l.cfg.MaxDepth
	if state.AIApp != nil && state.AIApp.Guardrails.MaxDepth > 0 {
		maxDepth = state.AIApp.Guardrails.MaxDepth
	}

	for depth := 0; depth < maxDepth; depth++ {
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

	// Depth bound reached: rather than failing, force one last turn with tools
	// disabled so the model summarizes what it gathered so far.
	l.cfg.Logger.WarnContext(ctx, "agent loop max depth reached; requesting final summary",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"max_depth", maxDepth,
	)
	state.ChatRequest.Tools = nil
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, core.ChatMessage{
		Role: "user",
		Content: "[System notice] The tool-call limit for this request has been reached. " +
			"Do not call any more tools. Using the information gathered so far, give the best possible " +
			"final answer, summarize your findings, and clearly mention anything you could not complete.",
	})

	if state.IsStreaming {
		_, hasToolCalls, err := l.runStreamingTurn(ctx, maxDepth, state, adapter)
		if err != nil {
			return err
		}
		if hasToolCalls {
			return ErrMaxDepthExceeded
		}
		return nil
	}
	_, hasToolCalls, err := l.runUnaryTurn(ctx, maxDepth, state, adapter)
	if err != nil {
		return err
	}
	if hasToolCalls {
		return ErrMaxDepthExceeded
	}
	return nil
}

func (l *Loop) resolveOnDemandSkills(state *core.ReqState) []*core.SkillVersion {
	if state == nil || state.Snapshot == nil || state.AIApp == nil || len(state.AIApp.SkillIDs) == 0 {
		return nil
	}
	var onDemand []*core.SkillVersion
	for _, skillID := range state.AIApp.SkillIDs {
		if sk, ok := state.Snapshot.Skill(skillID); ok && sk != nil && sk.InjectionMode == core.SkillInjectionOnDemand {
			onDemand = append(onDemand, sk)
		}
	}
	return onDemand
}

func (l *Loop) requestStringMetadata(state *core.ReqState) map[string]string {
	if state == nil {
		return nil
	}
	meta := make(map[string]string)
	for k, v := range state.Metadata {
		if str, ok := v.(string); ok {
			meta[k] = str
		}
	}
	if state.ChatRequest != nil {
		for k, v := range state.ChatRequest.Metadata {
			meta[k] = v
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func (l *Loop) findToolDef(state *core.ReqState, toolName string) (*core.ToolDefinition, bool) {
	if state.Snapshot != nil {
		if def, ok := state.Snapshot.Tool(toolName); ok && def != nil {
			return def, true
		}
	}
	// Check if this is the built-in load_skill tool for on-demand skills
	if toolName == "load_skill" {
		if onDemand := l.resolveOnDemandSkills(state); len(onDemand) > 0 {
			def := tools.BuiltinLoadSkillToolDefinition(state.AIApp.ID, onDemand, l.requestStringMetadata(state))
			return &def, true
		}
	}
	// Check if this is the built-in web search tool enabled on the app
	if state.AIApp != nil && state.AIApp.Search.Enabled {
		if toolName == "web_search" {
			searchDef := tools.BuiltinSearchToolDefinition(state.AIApp.Search)
			return &searchDef, true
		}
	}
	// Check if this is one of the built-in media generation tools
	switch toolName {
	case "generate_image":
		t := tools.BuiltinImageToolDefinition("image")
		return &t, true
	case "generate_speech":
		t := tools.BuiltinSpeechToolDefinition("speech")
		return &t, true
	case "generate_video":
		t := tools.BuiltinVideoToolDefinition("video")
		return &t, true
	}
	return nil, false
}

func (l *Loop) resolveTools(state *core.ReqState) []core.ToolDefinition {
	if state.Snapshot == nil || state.AIApp == nil {
		return nil
	}

	var res []core.ToolDefinition
	for _, toolID := range state.AIApp.ToolIDs {
		if def, ok := state.Snapshot.Tool(toolID); ok && def != nil {
			res = append(res, *def)
		} else {
			// Check built-in media tools referenced in ToolIDs
			switch toolID {
			case "generate_image":
				res = append(res, tools.BuiltinImageToolDefinition("image"))
			case "generate_speech":
				res = append(res, tools.BuiltinSpeechToolDefinition("speech"))
			case "generate_video":
				res = append(res, tools.BuiltinVideoToolDefinition("video"))
			}
		}
	}

	if state.AIApp.Search.Enabled {
		hasSearch := false
		for _, t := range res {
			if t.ID == "web_search" || t.Name == "web_search" {
				hasSearch = true
				break
			}
		}
		if !hasSearch {
			res = append(res, tools.BuiltinSearchToolDefinition(state.AIApp.Search))
		}
	}

	if onDemand := l.resolveOnDemandSkills(state); len(onDemand) > 0 {
		hasLoadSkill := false
		for _, t := range res {
			if t.ID == "load_skill" || t.Name == "load_skill" {
				hasLoadSkill = true
				break
			}
		}
		if !hasLoadSkill {
			res = append(res, tools.BuiltinLoadSkillToolDefinition(state.AIApp.ID, onDemand, l.requestStringMetadata(state)))
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
		toolDef, ok := l.findToolDef(state, toolCall.Name)
		if !ok {
			// Also check by ID
			toolDef, ok = l.findToolDef(state, toolCall.ID)
		}

		if !ok || toolDef == nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q not found in snapshot", toolCall.Name))
			continue
		}

		// Tool Authorization check
		if toolDef.Authorization != "" && l.cfg.PolicyEvaluator != nil {
			evalMeta := map[string]any{
				"tool": toolDef.Name,
				"app":  state.AIAppID,
			}
			allowed, err := l.cfg.PolicyEvaluator.Evaluate(ctx, toolDef.Authorization, state.Identity, evalMeta)
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

		if event.Type == "done" {
			if len(accumulatedToolCalls) == 0 {
				// Model finished completion without requesting tools
				select {
				case <-ctx.Done():
					return false, false, ctx.Err()
				case state.StreamChan <- event:
				}
				return false, false, nil
			}
			// When tool calls are accumulated, do not forward "done" to the client yet,
			// as the agent loop will execute the tools and stream the model's follow-up turn.
			continue
		}

		// Forward any other events (such as "usage")
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case state.StreamChan <- event:
		}
	}

	if len(accumulatedToolCalls) == 0 {
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case state.StreamChan <- core.StreamEvent{Type: "done", RequestID: state.RequestID}:
		}
		return false, false, nil
	}

	// Model requested tool calls during streaming
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, core.ChatMessage{
		Role:      "assistant",
		Content:   currentContent,
		ToolCalls: accumulatedToolCalls,
	})

	for _, toolCall := range accumulatedToolCalls {
		toolDef, ok := l.findToolDef(state, toolCall.Name)
		if !ok {
			toolDef, ok = l.findToolDef(state, toolCall.ID)
		}

		if !ok || toolDef == nil {
			l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q not found in snapshot", toolCall.Name))
			continue
		}

		// Tool Authorization check
		if toolDef.Authorization != "" && l.cfg.PolicyEvaluator != nil {
			evalMeta := map[string]any{
				"tool": toolDef.Name,
				"app":  state.AIAppID,
			}
			allowed, err := l.cfg.PolicyEvaluator.Evaluate(ctx, toolDef.Authorization, state.Identity, evalMeta)
			if err != nil || !allowed {
				l.appendToolError(state, toolCall.ID, fmt.Sprintf("tool %q authorization denied by policy %q", toolCall.Name, toolDef.Authorization))
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

	// Built-in skill_loader reads trusted control-plane configuration from Snapshot;
	// bypass untrusted external tool-injection regexes so instructional markdown is not false-positive blocked.
	if toolDef.Executor.Type == "skill_loader" {
		content := result.Content
		if result.IsError {
			content = fmt.Sprintf("<tool_result tool_id=%q call_id=%q status=\"error\">\n%s\n</tool_result>",
				toolDef.ID, toolCall.ID, result.Content)
		}
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    content,
		}, nil
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
