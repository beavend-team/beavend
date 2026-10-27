package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/media"
	"github.com/beavend/beavend/internal/memory"
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
	authenticator    core.Authenticator
	allowPrivate     bool
	sqlPool          *tools.SQLConnectionPool
	searxngClient    *tools.SearXNGClient
	mediaStore       core.MediaStore
	jobStore         media.JobStore
	resolver         tools.ModelAdapterResolver
	docMemoryStore   memory.DocumentMemoryStore
	knowledgeBackend tools.KnowledgeSearchBackend
	cacheStore       core.CacheStore
	wasmRuntime      *tools.WASMRuntime
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

// SetDocumentMemoryStore configures the document memory store for propose_memory tools.
func (r *DefaultExecutorRegistry) SetDocumentMemoryStore(s memory.DocumentMemoryStore) {
	r.docMemoryStore = s
}

// SetKnowledgeSearch configures the knowledge search backend and cache store for knowledge_search tools.
func (r *DefaultExecutorRegistry) SetKnowledgeSearch(b tools.KnowledgeSearchBackend, cache core.CacheStore) {
	r.knowledgeBackend = b
	r.cacheStore = cache
}

// GetExecutor instantiates or returns an executor for the specified tool definition.
func (r *DefaultExecutorRegistry) GetExecutor(def core.ToolDefinition) (core.Executor, error) {
	if def.Name == "propose_memory" || def.Executor.Type == "propose_memory" {
		return tools.NewProposeMemoryExecutor(r.docMemoryStore), nil
	}
	if def.Name == "knowledge_search" || def.Executor.Type == "knowledge_search" {
		return tools.NewKnowledgeSearchExecutor(r.knowledgeBackend, r.cacheStore, 1), nil
	}

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
	case "wasm":
		rt := r.wasmRuntime
		if rt == nil {
			var err error
			rt, err = tools.NewWASMRuntime(context.Background(), nil, nil)
			if err != nil {
				return nil, fmt.Errorf("init wasm runtime: %w", err)
			}
			r.wasmRuntime = rt
		}
		return tools.NewWASMExecutor(def, rt), nil
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
	// Check if this is one of the built-in tools
	switch toolName {
	case "propose_memory":
		t := tools.BuiltinProposeMemoryToolDefinition()
		return &t, true
	case "knowledge_search":
		t := tools.BuiltinKnowledgeSearchToolDefinition()
		return &t, true
	case "ask_user":
		t := tools.BuiltinAskUserToolDefinition()
		return &t, true
	case "sub_agent":
		t := tools.BuiltinSubAgentToolDefinition()
		return &t, true
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
			// Check built-in tools referenced in ToolIDs
			switch toolID {
			case "propose_memory":
				res = append(res, tools.BuiltinProposeMemoryToolDefinition())
			case "knowledge_search":
				res = append(res, tools.BuiltinKnowledgeSearchToolDefinition())
			case "ask_user":
				res = append(res, tools.BuiltinAskUserToolDefinition())
			case "sub_agent":
				res = append(res, tools.BuiltinSubAgentToolDefinition())
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

	if len(state.Snapshot.KnowledgeDocs(state.AIAppID)) > 0 {
		hasKnowledgeSearch := false
		for _, t := range res {
			if t.ID == "knowledge_search" || t.Name == "knowledge_search" {
				hasKnowledgeSearch = true
				break
			}
		}
		if !hasKnowledgeSearch {
			res = append(res, tools.BuiltinKnowledgeSearchToolDefinition())
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

	return l.executeBatchToolCalls(ctx, state, resp.Message.ToolCalls, false, resp.Usage.TotalTokens)
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

	return l.executeBatchToolCalls(ctx, state, accumulatedToolCalls, true, state.Usage.TotalTokens)
}

type validatedToolCall struct {
	tc       core.ToolCall
	def      *core.ToolDefinition
	err      string
	suspends bool
}

// executeBatchToolCalls implements Two-Phase Batch tool validation, partitioning, concurrent/sequential execution,
// and state suspension for continuation/confirmation.
func (l *Loop) executeBatchToolCalls(ctx context.Context, state *core.ReqState, toolCalls []core.ToolCall, isStreaming bool, totalTokens int) (bool, bool, error) {
	// Pre-generate boundary nonce on state before spawning concurrent workers
	_ = getOrGenerateBoundaryNonce(state)

	// ==========================================
	// Phase A: Validate all ToolCalls in the turn
	// ==========================================
	validated := make([]validatedToolCall, len(toolCalls))

	for i, tc := range toolCalls {
		toolDef, ok := l.findToolDef(state, tc.Name)
		if !ok {
			toolDef, ok = l.findToolDef(state, tc.ID)
		}
		if !ok || toolDef == nil {
			validated[i] = validatedToolCall{
				tc:  tc,
				err: fmt.Sprintf("tool %q not found in snapshot", tc.Name),
			}
			continue
		}

		if toolDef.Authorization != "" && l.cfg.PolicyEvaluator != nil {
			evalMeta := map[string]any{
				"tool": toolDef.Name,
				"app":  state.AIAppID,
			}
			allowed, err := l.cfg.PolicyEvaluator.Evaluate(ctx, toolDef.Authorization, state.Identity, evalMeta)
			if err != nil || !allowed {
				validated[i] = validatedToolCall{
					tc:  tc,
					def: toolDef,
					err: fmt.Sprintf("tool %q authorization denied by policy %q", toolDef.Name, toolDef.Authorization),
				}
				continue
			}
		}

		var args map[string]any
		if tc.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Arguments), &args)
		}
		if args == nil {
			args = make(map[string]any)
		}
		args = tools.InjectIdentityBindings(args, toolDef.IdentityBindings, state.Identity)
		boundBytes, _ := json.Marshal(args)
		tc.Arguments = string(boundBytes)

		if err := tools.ValidateArguments(args, state.Snapshot, toolDef); err != nil {
			validated[i] = validatedToolCall{
				tc:  tc,
				def: toolDef,
				err: fmt.Sprintf("arguments validation failed: %v", err),
			}
			continue
		}

		suspends := false
		if toolDef.Name == "ask_user" || toolDef.ID == "ask_user" {
			suspends = true
		} else if requiresConfirmation(toolDef) {
			suspends = true
		} else if toolDef.Executor.Type == "client_side" {
			suspends = true
		}

		validated[i] = validatedToolCall{
			tc:       tc,
			def:      toolDef,
			suspends: suspends,
		}
	}

	// ====================================================================
	// Phase B: Partition tool calls into non-suspending and suspending
	// ====================================================================
	var nonSuspending []validatedToolCall
	var suspending []validatedToolCall

	for _, v := range validated {
		if v.suspends {
			suspending = append(suspending, v)
		} else {
			nonSuspending = append(nonSuspending, v)
		}
	}

	// Execute non-suspending tool calls first.
	// For calls with toolDef.ParallelSafe == true, execute concurrently using errgroup (max concurrency 5).
	// For non-parallel tools, execute sequentially.
	// Preserves deterministic index order in result slice.
	nonSuspResults := make([]core.ChatMessage, len(nonSuspending))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for i, call := range nonSuspending {
		idx := i
		c := call
		if c.err != "" {
			if err := g.Wait(); err != nil {
				return false, false, err
			}
			nonSuspResults[idx] = l.createToolErrorMessage(state, c.tc.ID, c.err)
			g, gctx = errgroup.WithContext(ctx)
			g.SetLimit(5)
			continue
		}

		if c.def != nil && c.def.ParallelSafe {
			g.Go(func() error {
				msg, err := l.executeServerTool(gctx, state, c.def, c.tc)
				if err != nil {
					msg = l.createToolErrorMessage(state, c.tc.ID, err.Error())
				}
				nonSuspResults[idx] = msg
				return nil
			})
		} else {
			if err := g.Wait(); err != nil {
				return false, false, err
			}
			msg, err := l.executeServerTool(ctx, state, c.def, c.tc)
			if err != nil {
				msg = l.createToolErrorMessage(state, c.tc.ID, err.Error())
			}
			nonSuspResults[idx] = msg
			g, gctx = errgroup.WithContext(ctx)
			g.SetLimit(5)
		}
	}

	if err := g.Wait(); err != nil {
		return false, false, err
	}

	for _, resMsg := range nonSuspResults {
		state.ChatRequest.Messages = append(state.ChatRequest.Messages, resMsg)
	}

	// If no suspending tools exist, turn continues to next model turn
	if len(suspending) == 0 {
		return false, true, nil
	}

	// Suspending tools exist: store all unresolved tool calls in SuspendedState.PendingToolCalls
	firstSusp := suspending[0]
	pendingCalls := make([]core.ToolCall, 0, len(suspending))
	for _, s := range suspending {
		pendingCalls = append(pendingCalls, s.tc)
	}

	susp := l.createSuspendedState(state, firstSusp.tc, firstSusp.def.ID)
	susp.PendingToolCalls = pendingCalls

	if err := l.cfg.ContinuationMgr.Store().Save(ctx, susp, tools.DefaultSuspensionTTL); err != nil {
		return false, false, fmt.Errorf("save suspended state: %w", err)
	}

	token, err := l.cfg.ContinuationMgr.IssueConfirmationToken(susp, firstSusp.tc, tools.DefaultSuspensionTTL)
	if err != nil {
		return false, false, fmt.Errorf("issue continuation token: %w", err)
	}

	// Case 1: ask_user
	if firstSusp.def.Name == "ask_user" || firstSusp.def.ID == "ask_user" {
		var qData map[string]any
		if firstSusp.tc.Arguments != "" {
			_ = json.Unmarshal([]byte(firstSusp.tc.Arguments), &qData)
		}
		if qData == nil {
			qData = map[string]any{"question": firstSusp.tc.Arguments}
		}

		if isStreaming {
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- core.StreamEvent{
				Type:              core.StreamEventQuestion,
				ConfirmationToken: token,
				Data:              qData,
				RequestID:         state.RequestID,
			}:
			}
			select {
			case <-ctx.Done():
			case state.StreamChan <- core.StreamEvent{Type: core.StreamEventDone}:
			}
			return true, true, nil
		}

		state.ChatResponse = &core.ChatResponse{
			ID:        fmt.Sprintf("chatcmpl_%s", state.RequestID),
			RequestID: state.RequestID,
			AIAppID:   state.AIAppID,
			CreatedAt: time.Now().UTC().Unix(),
			Message: core.ChatMessage{
				Role:      "assistant",
				ToolCalls: []core.ToolCall{firstSusp.tc},
			},
			FinishReason: "question_required",
			Usage: core.UsageStats{
				TotalTokens: totalTokens,
			},
		}
		if state.ChatRequest.Metadata == nil {
			state.ChatRequest.Metadata = make(map[string]string)
		}
		state.ChatRequest.Metadata["confirmation_token"] = token
		return true, true, nil
	}

	// Case 2: client_side
	if firstSusp.def.Executor.Type == "client_side" {
		if isStreaming {
			tc := firstSusp.tc
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case state.StreamChan <- core.StreamEvent{
				Type:              core.StreamEventToolCall,
				ConfirmationToken: token,
				ToolCall:          &tc,
				RequestID:         state.RequestID,
			}:
			}
			select {
			case <-ctx.Done():
			case state.StreamChan <- core.StreamEvent{Type: core.StreamEventDone}:
			}
			return true, true, nil
		}

		state.ChatResponse = &core.ChatResponse{
			ID:        fmt.Sprintf("chatcmpl_%s", state.RequestID),
			RequestID: state.RequestID,
			AIAppID:   state.AIAppID,
			CreatedAt: time.Now().UTC().Unix(),
			Message: core.ChatMessage{
				Role:      "assistant",
				ToolCalls: []core.ToolCall{firstSusp.tc},
			},
			FinishReason: "client_tool",
			Usage: core.UsageStats{
				TotalTokens: totalTokens,
			},
		}
		if state.ChatRequest.Metadata == nil {
			state.ChatRequest.Metadata = make(map[string]string)
		}
		state.ChatRequest.Metadata["continuation_token"] = token
		return true, true, nil
	}

	// Case 3: confirmation: required
	if isStreaming {
		tc := firstSusp.tc
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case state.StreamChan <- core.StreamEvent{
			Type:              core.StreamEventConfirmation,
			ConfirmationToken: token,
			ToolCall:          &tc,
			RequestID:         state.RequestID,
		}:
		}
		select {
		case <-ctx.Done():
		case state.StreamChan <- core.StreamEvent{Type: core.StreamEventDone}:
		}
		return true, true, nil
	}

	state.ChatResponse = &core.ChatResponse{
		ID:        fmt.Sprintf("chatcmpl_%s", state.RequestID),
		RequestID: state.RequestID,
		AIAppID:   state.AIAppID,
		CreatedAt: time.Now().UTC().Unix(),
		Message: core.ChatMessage{
			Role:      "assistant",
			ToolCalls: []core.ToolCall{firstSusp.tc},
		},
		FinishReason: "confirmation_required",
		Usage: core.UsageStats{
			TotalTokens: totalTokens,
		},
	}
	if state.ChatRequest.Metadata == nil {
		state.ChatRequest.Metadata = make(map[string]string)
	}
	state.ChatRequest.Metadata["confirmation_token"] = token
	return true, true, nil
}

var nonceMu sync.Mutex

func getOrGenerateBoundaryNonce(state *core.ReqState) string {
	if state == nil {
		return ""
	}
	nonceMu.Lock()
	defer nonceMu.Unlock()
	if state.Metadata != nil {
		if n, ok := state.Metadata["boundary_nonce"].(string); ok && n != "" {
			return n
		}
	}
	nonceBytes := make([]byte, 8)
	var nonce string
	if _, err := rand.Read(nonceBytes); err == nil {
		nonce = hex.EncodeToString(nonceBytes)
	} else {
		nonce = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}
	state.Metadata["boundary_nonce"] = nonce
	return nonce
}

func (l *Loop) createToolErrorMessage(state *core.ReqState, toolCallID, message string) core.ChatMessage {
	nonce := getOrGenerateBoundaryNonce(state)
	escapedMsg := tools.EscapeClosingToolResultTags(message)
	content := tools.WrapToolResult(escapedMsg, nonce, "", toolCallID, "error")

	return core.ChatMessage{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    content,
	}
}

func (l *Loop) appendToolError(state *core.ReqState, toolCallID, message string) {
	state.ChatRequest.Messages = append(state.ChatRequest.Messages, l.createToolErrorMessage(state, toolCallID, message))
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
	threadID := ""
	if state.ChatRequest != nil {
		threadID = state.ChatRequest.ThreadID
		if len(state.ChatRequest.Messages) > 0 {
			msgs = make([]core.ChatMessage, len(state.ChatRequest.Messages))
			copy(msgs, state.ChatRequest.Messages)
		}
	}

	nonce := getOrGenerateBoundaryNonce(state)

	return &tools.SuspendedState{
		ProjectID:        state.ProjectID,
		Environment:      state.Environment,
		RequestID:        state.RequestID,
		AIAppID:          state.AIAppID,
		ThreadID:         threadID,
		Subject:          subject,
		Issuer:           issuer,
		Messages:         msgs,
		PendingToolCall:  toolCall,
		PendingToolCalls: []core.ToolCall{toolCall},
		ToolDefinitionID: toolDefID,
		SnapshotRevision: rev,
		BoundaryNonce:    nonce,
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
	if toolDef.Name == "sub_agent" || toolDef.Executor.Type == "sub_agent" {
		return l.executeSubAgent(ctx, state, toolDef, toolCall)
	}

	executor, err := l.cfg.ExecutorRegistry.GetExecutor(*toolDef)
	if err != nil {
		return core.ChatMessage{}, fmt.Errorf("resolve executor: %w", err)
	}

	var identity core.Identity
	if state.Identity != nil {
		identity = *state.Identity
	}

	execCtx := ctx
	if state.Snapshot != nil {
		execCtx = core.ContextWithSnapshot(execCtx, state.Snapshot)
	}
	if state.AIAppID != "" {
		execCtx = core.ContextWithAppID(execCtx, state.AIAppID)
	}

	// 3. Execute
	result, err := executor.Execute(execCtx, toolCall, identity)
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
		nonce := getOrGenerateBoundaryNonce(state)
		content := tools.EscapeClosingToolResultTags(result.Content)
		status := "success"
		if result.IsError {
			status = "error"
		}
		wrapped := tools.WrapToolResult(content, nonce, toolDef.ID, toolCall.ID, status)
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	// 4. Sanitize untrusted data with boundary nonce
	nonce := getOrGenerateBoundaryNonce(state)
	sanitized := l.cfg.Sanitizer.Sanitize(result, toolDef.Result, tools.WithNonce(nonce))

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

func (l *Loop) executeSubAgent(ctx context.Context, state *core.ReqState, toolDef *core.ToolDefinition, toolCall core.ToolCall) (core.ChatMessage, error) {
	nonce := getOrGenerateBoundaryNonce(state)

	var subArgs struct {
		Task    string `json:"task"`
		Context string `json:"context"`
	}
	if toolCall.Arguments != "" {
		_ = json.Unmarshal([]byte(toolCall.Arguments), &subArgs)
	}
	if subArgs.Task == "" {
		wrapped := tools.WrapToolResult("missing task in sub_agent arguments", nonce, toolDef.ID, toolCall.ID, "error")
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	currentDepth := 0
	if state.Metadata != nil {
		if d, ok := state.Metadata["sub_agent_depth"].(int); ok {
			currentDepth = d
		}
	}
	if currentDepth >= 1 {
		wrapped := tools.WrapToolResult("sub_agent recursion depth limit exceeded (max depth: 1)", nonce, toolDef.ID, toolCall.ID, "error")
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	// Token budget check
	if state.TokenBudget != nil && state.TokenBudget.MaxTokens > 0 && state.TokenBudget.Remaining() <= 0 {
		wrapped := tools.WrapToolResult("token budget exceeded", nonce, toolDef.ID, toolCall.ID, "error")
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	// Emit activity event if streaming
	if state.StreamChan != nil {
		select {
		case state.StreamChan <- core.StreamEvent{
			Type:      core.StreamEventSubAgentActivity,
			RequestID: state.RequestID,
			Data: map[string]any{
				"task":    subArgs.Task,
				"depth":   currentDepth + 1,
				"call_id": toolCall.ID,
			},
		}:
		default:
		}
	}

	// Tool registry stripping: remove ask_user, sub_agent, client_side, confirmation: required
	allTools := l.resolveTools(state)
	var strippedTools []core.ToolDefinition
	for _, t := range allTools {
		if t.Name == "ask_user" || t.ID == "ask_user" {
			continue
		}
		if t.Name == "sub_agent" || t.ID == "sub_agent" {
			continue
		}
		if t.Executor.Type == "client_side" {
			continue
		}
		if requiresConfirmation(&t) {
			continue
		}
		strippedTools = append(strippedTools, t)
	}

	childMessages := []core.ChatMessage{
		{
			Role:    "system",
			Content: "You are a sub-agent solving a delegated subtask. Output the concise answer.",
		},
	}
	if subArgs.Context != "" {
		childMessages = append(childMessages, core.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("Context: %s\n\nTask: %s", subArgs.Context, subArgs.Task),
		})
	} else {
		childMessages = append(childMessages, core.ChatMessage{
			Role:    "user",
			Content: subArgs.Task,
		})
	}

	childMeta := make(map[string]any)
	for k, v := range state.Metadata {
		childMeta[k] = v
	}
	childMeta["sub_agent_depth"] = currentDepth + 1

	model := ""
	if state.ChatRequest != nil {
		model = state.ChatRequest.Model
	}

	childState := &core.ReqState{
		RequestID:       fmt.Sprintf("%s_sub_%s", state.RequestID, toolCall.ID),
		ProjectID:       state.ProjectID,
		Environment:     state.Environment,
		AIAppID:         state.AIAppID,
		Identity:        state.Identity,
		Snapshot:        state.Snapshot,
		AIApp:           state.AIApp,
		SelectedModel:   state.SelectedModel,
		ResolvedModel:   state.ResolvedModel,
		ResolvedAdapter: state.ResolvedAdapter,
		ChatRequest: &core.ChatRequest{
			Model:    model,
			Messages: childMessages,
			Tools:    strippedTools,
		},
		TokenBudget: state.TokenBudget,
		Metadata:    childMeta,
		StartTime:   time.Now(),
	}

	adapter := state.ResolvedAdapter
	if adapter == nil {
		wrapped := tools.WrapToolResult("resolved adapter not found for sub_agent", nonce, toolDef.ID, toolCall.ID, "error")
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	childResp, err := adapter.Chat(ctx, *childState.ChatRequest)
	if err != nil {
		wrapped := tools.WrapToolResult(tools.EscapeClosingToolResultTags(err.Error()), nonce, toolDef.ID, toolCall.ID, "error")
		return core.ChatMessage{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    wrapped,
		}, nil
	}

	if childResp != nil {
		state.Usage.TotalTokens += childResp.Usage.TotalTokens
		state.Usage.InputTokens += childResp.Usage.InputTokens
		state.Usage.OutputTokens += childResp.Usage.OutputTokens
		if state.TokenBudget != nil {
			state.TokenBudget.Add(int64(childResp.Usage.TotalTokens))
		}
	}

	if childResp != nil && len(childResp.Message.ToolCalls) > 0 {
		// Child model emitted tool calls: execute child tools and run turn 2
		childState.ChatRequest.Messages = append(childState.ChatRequest.Messages, childResp.Message)

		toolDefMap := make(map[string]*core.ToolDefinition, len(strippedTools)*2)
		for i := range strippedTools {
			toolDefMap[strippedTools[i].Name] = &strippedTools[i]
			toolDefMap[strippedTools[i].ID] = &strippedTools[i]
		}

		for _, childTC := range childResp.Message.ToolCalls {
			var toolMsg core.ChatMessage
			def, ok := toolDefMap[childTC.Name]
			if !ok {
				def, ok = toolDefMap[childTC.ID]
			}
			if ok && def != nil {
				var execErr error
				toolMsg, execErr = l.executeServerTool(ctx, childState, def, childTC)
				if execErr != nil {
					toolMsg = l.createToolErrorMessage(childState, childTC.ID, execErr.Error())
				}
			} else {
				toolMsg = l.createToolErrorMessage(childState, childTC.ID, fmt.Sprintf("tool %s not found in sub-agent scope", childTC.Name))
			}
			childState.ChatRequest.Messages = append(childState.ChatRequest.Messages, toolMsg)
		}

		// Check token budget before executing turn 2
		if state.TokenBudget != nil && state.TokenBudget.MaxTokens > 0 && state.TokenBudget.Remaining() <= 0 {
			wrapped := tools.WrapToolResult("token budget exceeded", nonce, toolDef.ID, toolCall.ID, "error")
			return core.ChatMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Content:    wrapped,
			}, nil
		}

		childResp2, err2 := adapter.Chat(ctx, *childState.ChatRequest)
		if err2 != nil {
			wrapped := tools.WrapToolResult(tools.EscapeClosingToolResultTags(err2.Error()), nonce, toolDef.ID, toolCall.ID, "error")
			return core.ChatMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Content:    wrapped,
			}, nil
		}

		if childResp2 != nil {
			state.Usage.TotalTokens += childResp2.Usage.TotalTokens
			state.Usage.InputTokens += childResp2.Usage.InputTokens
			state.Usage.OutputTokens += childResp2.Usage.OutputTokens
			if state.TokenBudget != nil {
				state.TokenBudget.Add(int64(childResp2.Usage.TotalTokens))
			}
			childResp = childResp2
		}
	}

	childContent := ""
	if childResp != nil {
		childContent = childResp.Message.Content
	}
	escaped := tools.EscapeClosingToolResultTags(childContent)
	wrapped := tools.WrapToolResult(escaped, nonce, toolDef.ID, toolCall.ID, "success")

	return core.ChatMessage{
		Role:       "tool",
		ToolCallID: toolCall.ID,
		Content:    wrapped,
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
