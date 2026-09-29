package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/runtime/agent"
	"github.com/beavend/beavend/internal/runtime/stream"
	"github.com/beavend/beavend/internal/telemetry"
	"github.com/beavend/beavend/internal/tools"
)

// SnapshotResolver retrieves a Snapshot for a given project and environment.
type SnapshotResolver interface {
	Get(projectID, env string) (*core.Snapshot, bool)
}

// Handler handles runtime AI App requests for chat completion, streaming, and tool continuation.
type Handler struct {
	resolver        SnapshotResolver
	pipeline        *Pipeline
	streamWriter    *stream.Writer
	continuationMgr *tools.ContinuationManager
	execRegistry    agent.ExecutorRegistry
	sanitizer       *tools.Sanitizer
	usageStore      core.UsageStore
	defaultEnv      string
}

// HandlerOptions configures runtime Handler.
type HandlerOptions struct {
	DefaultEnvironment string
	HeartbeatInterval  time.Duration
	ContinuationMgr    *tools.ContinuationManager
	ExecutorRegistry   agent.ExecutorRegistry
	UsageStore         core.UsageStore
}

// NewHandler creates a new runtime Handler.
func NewHandler(resolver SnapshotResolver, pipeline *Pipeline, opts ...HandlerOptions) *Handler {
	defaultEnv := "development"
	heartbeat := 15 * time.Second
	var continuationMgr *tools.ContinuationManager
	var execRegistry agent.ExecutorRegistry
	var usageStore core.UsageStore

	if len(opts) > 0 {
		if opts[0].DefaultEnvironment != "" {
			defaultEnv = opts[0].DefaultEnvironment
		}
		if opts[0].HeartbeatInterval > 0 {
			heartbeat = opts[0].HeartbeatInterval
		}
		continuationMgr = opts[0].ContinuationMgr
		execRegistry = opts[0].ExecutorRegistry
		usageStore = opts[0].UsageStore
	}

	if continuationMgr == nil {
		continuationMgr = tools.NewContinuationManager(nil, nil)
	}
	if execRegistry == nil {
		execRegistry = agent.NewDefaultExecutorRegistry(nil, false)
	}

	return &Handler{
		resolver:        resolver,
		pipeline:        pipeline,
		streamWriter:    stream.NewWriter(heartbeat),
		continuationMgr: continuationMgr,
		execRegistry:    execRegistry,
		sanitizer:       tools.NewSanitizer(),
		usageStore:      usageStore,
		defaultEnv:      defaultEnv,
	}
}

// ContinuationManager returns the handler's continuation manager.
func (h *Handler) ContinuationManager() *tools.ContinuationManager {
	return h.continuationMgr
}

// HandleChat handles synchronous unary chat requests (POST /v1/apps/{ai_app_id}/chat).
func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	state, ok := h.buildRequestState(w, r, false)
	if !ok {
		return
	}

	if err := h.pipeline.Execute(r.Context(), state); err != nil {
		h.writeError(w, state.Error)
		h.recordUsage(state)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if state.ChatResponse != nil {
		_ = json.NewEncoder(w).Encode(state.ChatResponse)
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}
	h.recordUsage(state)
}

// HandleStream handles Server-Sent Events streaming chat requests (POST /v1/apps/{ai_app_id}/stream).
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	state, ok := h.buildRequestState(w, r, true)
	if !ok {
		return
	}

	// Bypass default write timeout for long-lived SSE streaming
	rc := http.NewResponseController(w) //nolint:bodyclose // False positive: ResponseController is not an http.Response.
	_ = rc.SetWriteDeadline(time.Time{})

	if err := h.pipeline.Execute(r.Context(), state); err != nil {
		h.writeError(w, state.Error)
		h.recordUsage(state)
		return
	}

	origChan := state.StreamChan
	if origChan == nil {
		h.writeRawError(w, "internal_error", "stream channel not initialized", state.RequestID)
		h.recordUsage(state)
		return
	}

	monitoredChan := make(chan core.StreamEvent, 16)
	done := make(chan struct{})

	var (
		usageMu       sync.Mutex
		capturedUsage core.UsageStats
		capturedErr   *core.ErrorResponse
	)

	go func() {
		defer close(done)
		defer close(monitoredChan)
		for ev := range origChan {
			if ev.Type == "usage" && ev.Usage != nil {
				usageMu.Lock()
				capturedUsage = *ev.Usage
				usageMu.Unlock()
			}
			if ev.Type == "error" && ev.Error != nil {
				usageMu.Lock()
				capturedErr = ev.Error
				usageMu.Unlock()
			}
			select {
			case monitoredChan <- ev:
			case <-r.Context().Done():
				// Client disconnected; drain origChan to capture final usage without blocking on monitoredChan
				for ev := range origChan {
					if ev.Type == "usage" && ev.Usage != nil {
						usageMu.Lock()
						capturedUsage = *ev.Usage
						usageMu.Unlock()
					}
				}
				return
			}
		}
	}()

	if err := h.streamWriter.Stream(r.Context(), w, monitoredChan); err != nil {
		slog.WarnContext(r.Context(), "sse streaming ended with error",
			"request_id", state.RequestID,
			"project_id", state.ProjectID,
			"env", state.Environment,
			"ai_app_id", state.AIAppID,
			"error", err,
		)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}

	usageMu.Lock()
	if capturedUsage.TotalTokens > 0 || capturedUsage.InputTokens > 0 || capturedUsage.OutputTokens > 0 {
		state.Usage = capturedUsage
	}
	if capturedErr != nil {
		state.Error = capturedErr
	}
	usageMu.Unlock()

	h.recordUsage(state)
}

// ConfirmRequestBody represents the payload to confirm or deny tool execution.
type ConfirmRequestBody struct {
	ConfirmationToken string `json:"confirmation_token"`
	Approved          bool   `json:"approved"`
}

// HandleConfirm handles resuming a request suspended for tool confirmation (POST /v1/apps/{ai_app_id}/confirm).
func (h *Handler) HandleConfirm(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)

	var req ConfirmRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeRawError(w, "invalid_argument", "invalid request body", reqID)
		return
	}

	id, _ := identity.FromContext(r.Context())

	// Validate confirmation token and retrieve state
	suspState, claims, err := h.continuationMgr.ValidateResumption(r.Context(), req.ConfirmationToken, id)
	if err != nil {
		h.writeRawError(w, continuationErrorCode(err), fmt.Sprintf("continuation validation failed: %v", err), reqID)
		return
	}

	// Delete state from store once consumed
	_ = h.continuationMgr.Store().Delete(r.Context(), claims.ProjectID, claims.RequestID)

	snap, ok := h.resolver.Get(suspState.ProjectID, suspState.Environment)
	if !ok || snap == nil {
		h.writeRawError(w, "unavailable", "configuration snapshot not found", reqID)
		return
	}

	app, ok := snap.App(suspState.AIAppID)
	if !ok || app == nil {
		h.writeRawError(w, "not_found", "ai app not found", reqID)
		return
	}

	if !req.Approved {
		// User declined execution: inject cancellation tool message
		suspState.Messages = append(suspState.Messages, core.ChatMessage{
			Role:       "tool",
			ToolCallID: suspState.PendingToolCall.ID,
			Content:    "<tool_result status=\"cancelled\">\nUser declined to confirm tool execution.\n</tool_result>",
		})
	} else {
		// User approved: execute pending tool
		toolDef, ok := snap.Tool(suspState.PendingToolCall.Name)
		if !ok {
			toolDef, ok = snap.Tool(suspState.PendingToolCall.ID)
		}

		if !ok || toolDef == nil {
			h.writeRawError(w, "not_found", fmt.Sprintf("tool %q not found", suspState.PendingToolCall.Name), reqID)
			return
		}

		executor, err := h.execRegistry.GetExecutor(*toolDef)
		if err != nil {
			h.writeRawError(w, "internal_error", fmt.Sprintf("resolve executor: %v", err), reqID)
			return
		}

		var callerID core.Identity
		if id != nil {
			callerID = *id
		}

		res, err := executor.Execute(r.Context(), suspState.PendingToolCall, callerID)
		if err != nil {
			res = core.ToolResult{
				CallID:  suspState.PendingToolCall.ID,
				ToolID:  toolDef.ID,
				Content: fmt.Sprintf("execution error: %v", err),
				IsError: true,
			}
		}

		sanitized := h.sanitizer.Sanitize(res, toolDef.Result)

		suspState.Messages = append(suspState.Messages, core.ChatMessage{
			Role:       "tool",
			ToolCallID: suspState.PendingToolCall.ID,
			Content:    sanitized.Content,
		})
	}

	state := &core.ReqState{
		RequestID:   suspState.RequestID,
		ProjectID:   suspState.ProjectID,
		Environment: suspState.Environment,
		AIAppID:     suspState.AIAppID,
		Snapshot:    snap,
		AIApp:       app,
		Identity:    id,
		IsStreaming: suspState.IsStreaming,
		ChatRequest: &core.ChatRequest{
			Messages: suspState.Messages,
		},
		StartTime: time.Now(),
	}

	h.executeResumption(w, r, state)
}

// ToolResultsRequestBody represents the payload from client-side tool execution.
type ToolResultsRequestBody struct {
	ContinuationToken string `json:"continuation_token"`
	CallID            string `json:"call_id"`
	Result            any    `json:"result"`
}

// HandleToolResults handles receiving client-side tool results (POST /v1/apps/{ai_app_id}/tool-results).
func (h *Handler) HandleToolResults(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)

	var req ToolResultsRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeRawError(w, "invalid_argument", "invalid request body", reqID)
		return
	}

	id, _ := identity.FromContext(r.Context())

	suspState, claims, err := h.continuationMgr.ValidateResumption(r.Context(), req.ContinuationToken, id)
	if err != nil {
		h.writeRawError(w, continuationErrorCode(err), fmt.Sprintf("continuation validation failed: %v", err), reqID)
		return
	}

	_ = h.continuationMgr.Store().Delete(r.Context(), claims.ProjectID, claims.RequestID)

	snap, ok := h.resolver.Get(suspState.ProjectID, suspState.Environment)
	if !ok || snap == nil {
		h.writeRawError(w, "unavailable", "configuration snapshot not found", reqID)
		return
	}

	app, ok := snap.App(suspState.AIAppID)
	if !ok || app == nil {
		h.writeRawError(w, "not_found", "ai app not found", reqID)
		return
	}

	// Format client result as JSON string and wrap as untrusted data
	resultJSON, _ := json.Marshal(req.Result)
	wrappedResult := fmt.Sprintf("<tool_result tool_id=%q call_id=%q status=\"success\">\n%s\n</tool_result>",
		suspState.PendingToolCall.Name, req.CallID, string(resultJSON))

	suspState.Messages = append(suspState.Messages, core.ChatMessage{
		Role:       "tool",
		ToolCallID: req.CallID,
		Content:    wrappedResult,
	})

	state := &core.ReqState{
		RequestID:   suspState.RequestID,
		ProjectID:   suspState.ProjectID,
		Environment: suspState.Environment,
		AIAppID:     suspState.AIAppID,
		Snapshot:    snap,
		AIApp:       app,
		Identity:    id,
		IsStreaming: suspState.IsStreaming,
		ChatRequest: &core.ChatRequest{
			Messages: suspState.Messages,
		},
		StartTime: time.Now(),
	}

	h.executeResumption(w, r, state)
}

func (h *Handler) executeResumption(w http.ResponseWriter, r *http.Request, state *core.ReqState) {
	if state.IsStreaming {
		rc := http.NewResponseController(w) //nolint:bodyclose // False positive: ResponseController is not an http.Response.
		_ = rc.SetWriteDeadline(time.Time{})

		if err := h.pipeline.Execute(r.Context(), state); err != nil {
			h.writeError(w, state.Error)
			h.recordUsage(state)
			return
		}

		origChan := state.StreamChan
		if origChan == nil {
			h.writeRawError(w, "internal_error", "stream channel not initialized", state.RequestID)
			h.recordUsage(state)
			return
		}

		monitoredChan := make(chan core.StreamEvent, 16)
		done := make(chan struct{})

		var (
			usageMu       sync.Mutex
			capturedUsage core.UsageStats
			capturedErr   *core.ErrorResponse
		)

		go func() {
			defer close(done)
			defer close(monitoredChan)
			for ev := range origChan {
				if ev.Type == "usage" && ev.Usage != nil {
					usageMu.Lock()
					capturedUsage = *ev.Usage
					usageMu.Unlock()
				}
				if ev.Type == "error" && ev.Error != nil {
					usageMu.Lock()
					capturedErr = ev.Error
					usageMu.Unlock()
				}
				select {
				case monitoredChan <- ev:
				case <-r.Context().Done():
					// Client disconnected; drain origChan to capture final usage without blocking on monitoredChan
					for ev := range origChan {
						if ev.Type == "usage" && ev.Usage != nil {
							usageMu.Lock()
							capturedUsage = *ev.Usage
							usageMu.Unlock()
						}
					}
					return
				}
			}
		}()

		if err := h.streamWriter.Stream(r.Context(), w, monitoredChan); err != nil {
			slog.WarnContext(r.Context(), "resumed sse stream ended with error",
				"request_id", state.RequestID,
				"error", err,
			)
		}

		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}

		usageMu.Lock()
		if capturedUsage.TotalTokens > 0 || capturedUsage.InputTokens > 0 || capturedUsage.OutputTokens > 0 {
			state.Usage = capturedUsage
		}
		if capturedErr != nil {
			state.Error = capturedErr
		}
		usageMu.Unlock()

		h.recordUsage(state)
	} else {
		if err := h.pipeline.Execute(r.Context(), state); err != nil {
			h.writeError(w, state.Error)
			h.recordUsage(state)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if state.ChatResponse != nil {
			_ = json.NewEncoder(w).Encode(state.ChatResponse)
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
		h.recordUsage(state)
	}
}

func (h *Handler) buildRequestState(w http.ResponseWriter, r *http.Request, isStreaming bool) (*core.ReqState, bool) {
	appID := chi.URLParam(r, "ai_app_id")
	if appID == "" {
		appID = strings.TrimPrefix(r.URL.Path, "/v1/apps/")
		if idx := strings.Index(appID, "/"); idx != -1 {
			appID = appID[:idx]
		}
	}

	reqID := getOrGenerateRequestID(r)
	if reqID != "" {
		w.Header().Set("X-Request-Id", reqID)
	}

	if appID == "" {
		h.writeRawError(w, "invalid_argument", "missing ai_app_id in request path", reqID)
		return nil, false
	}

	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		h.writeRawError(w, "invalid_argument", "missing required X-Project-ID header", reqID)
		return nil, false
	}

	env := strings.TrimSpace(r.Header.Get("X-Environment"))
	if env == "" {
		env = h.defaultEnv
	}

	snap, ok := h.resolver.Get(projectID, env)
	if !ok || snap == nil {
		h.writeRawError(w, "unavailable", fmt.Sprintf("configuration snapshot for project %q (%s) not found", projectID, env), reqID)
		return nil, false
	}

	app, ok := snap.App(appID)
	if !ok || app == nil {
		h.writeRawError(w, "not_found", fmt.Sprintf("ai app %q not found", appID), reqID)
		return nil, false
	}

	var chatReq core.ChatRequest
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err == nil && len(b) > 0 {
			if err := json.Unmarshal(b, &chatReq); err != nil {
				h.writeRawError(w, "invalid_argument", fmt.Sprintf("invalid json payload: %v", err), reqID)
				return nil, false
			}
		}
	}
	chatReq.Stream = isStreaming

	id, _ := identity.FromContext(r.Context())

	return &core.ReqState{
		RequestID:   reqID,
		ProjectID:   projectID,
		Environment: env,
		AIAppID:     appID,
		Snapshot:    snap,
		AIApp:       app,
		Identity:    id,
		IsStreaming: isStreaming,
		ChatRequest: &chatReq,
		StartTime:   time.Now(),
	}, true
}

func (h *Handler) writeError(w http.ResponseWriter, appErr *core.ErrorResponse) {
	if appErr == nil {
		appErr = &core.ErrorResponse{
			Code:    "internal_error",
			Message: "internal server error",
		}
	}

	status := mapErrorCodeToStatus(appErr.Code)
	w.Header().Set("Content-Type", "application/json")
	if appErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(appErr.RetryAfter))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(appErr)
}

func (h *Handler) writeRawError(w http.ResponseWriter, code, message, reqID string) {
	resp := &core.ErrorResponse{
		Code:      code,
		Message:   message,
		RequestID: reqID,
	}
	h.writeError(w, resp)
}

func mapErrorCodeToStatus(code string) int {
	switch code {
	case "invalid_argument", "invalid_request":
		return http.StatusBadRequest
	case "unauthenticated":
		return http.StatusUnauthorized
	case "forbidden":
		return http.StatusForbidden
	case "not_found":
		return http.StatusNotFound
	case "rate_limited":
		return http.StatusTooManyRequests
	case "provider_error":
		return http.StatusBadGateway
	case "provider_timeout":
		return http.StatusGatewayTimeout
	case "unavailable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func getOrGenerateRequestID(r *http.Request) string {
	if reqID := telemetry.RequestIDFromContext(r.Context()); reqID != "" {
		return reqID
	}
	if reqID := r.Header.Get("X-Request-Id"); reqID != "" {
		return reqID
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

func continuationErrorCode(err error) string {
	if errors.Is(err, tools.ErrSubjectMismatch) {
		return "forbidden"
	}
	if errors.Is(err, tools.ErrStateNotFound) {
		return "not_found"
	}
	if errors.Is(err, tools.ErrTokenSignatureInvalid) || errors.Is(err, tools.ErrArgsHashMismatch) {
		return "forbidden"
	}
	return "invalid_argument"
}

func (h *Handler) recordUsage(state *core.ReqState) {
	if h.usageStore != nil && state != nil {
		stateCopy := *state
		if state.Metadata != nil {
			metaCopy := make(map[string]any, len(state.Metadata))
			for k, v := range state.Metadata {
				metaCopy[k] = v
			}
			stateCopy.Metadata = metaCopy
		}
		go func() {
			_ = h.usageStore.RecordUsage(context.Background(), &stateCopy)
		}()
	}
}
