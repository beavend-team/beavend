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
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/media"
	"github.com/beavend/beavend/internal/memory"
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
	cacheStore      core.CacheStore
	threadStore     core.ThreadStore
	memoryEngine    *memory.Engine
	jobStore        media.JobStore
	mediaStore      core.MediaStore
	defaultEnv      string
}

// HandlerOptions configures runtime Handler.
type HandlerOptions struct {
	DefaultEnvironment string
	HeartbeatInterval  time.Duration
	ContinuationMgr    *tools.ContinuationManager
	ExecutorRegistry   agent.ExecutorRegistry
	UsageStore         core.UsageStore
	CacheStore         core.CacheStore
	ThreadStore        core.ThreadStore
	MemoryEngine       *memory.Engine
	JobStore           media.JobStore
	MediaStore         core.MediaStore
}

// NewHandler creates a new runtime Handler.
func NewHandler(resolver SnapshotResolver, pipeline *Pipeline, opts ...HandlerOptions) *Handler {
	defaultEnv := "development"
	heartbeat := 15 * time.Second
	var continuationMgr *tools.ContinuationManager
	var execRegistry agent.ExecutorRegistry
	var usageStore core.UsageStore
	var cacheStore core.CacheStore
	var threadStore core.ThreadStore
	var memoryEngine *memory.Engine
	var jobStore media.JobStore
	var mediaStore core.MediaStore

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
		cacheStore = opts[0].CacheStore
		threadStore = opts[0].ThreadStore
		memoryEngine = opts[0].MemoryEngine
		jobStore = opts[0].JobStore
		mediaStore = opts[0].MediaStore
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
		cacheStore:      cacheStore,
		threadStore:     threadStore,
		memoryEngine:    memoryEngine,
		jobStore:        jobStore,
		mediaStore:      mediaStore,
		defaultEnv:      defaultEnv,
	}
}

// ContinuationManager returns the handler's continuation manager.
func (h *Handler) ContinuationManager() *tools.ContinuationManager {
	return h.continuationMgr
}

func writeRateLimitHeaders(w http.ResponseWriter, meta map[string]any) {
	if meta == nil {
		return
	}
	if lim, ok := meta["ratelimit_limit"].(int64); ok && lim > 0 {
		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(lim, 10))
	}
	if reset, ok := meta["ratelimit_reset"].(int64); ok && reset > 0 {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
	}
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
	writeRateLimitHeaders(w, state.Metadata)
	if state.SelectedModel != "" {
		w.Header().Set("X-Beavend-Model", state.SelectedModel)
	}
	if state.Metadata != nil {
		if hit, ok := state.Metadata["cache_hit"].(bool); ok && hit {
			w.Header().Set("X-Cache", "HIT")
		} else if state.AIApp != nil && state.AIApp.Cache.Enabled {
			w.Header().Set("X-Cache", "MISS")
		}
	}
	w.WriteHeader(http.StatusOK)
	if state.ChatResponse != nil {
		if state.SelectedModel != "" && state.ChatResponse.Model == "" {
			state.ChatResponse.Model = state.SelectedModel
		}
		if state.ChatRequest != nil && state.ChatRequest.ThreadID != "" {
			state.ChatResponse.ThreadID = state.ChatRequest.ThreadID
			if h.threadStore != nil && state.Error == nil {
				ttl := 24 * time.Hour
				if state.AIApp != nil && state.AIApp.Memory.ThreadTTLSec > 0 {
					ttl = time.Duration(state.AIApp.Memory.ThreadTTLSec) * time.Second
				}

				historyCount := 0
				if state.Metadata != nil {
					if hc, ok := state.Metadata["thread_history_count"].(int); ok {
						historyCount = hc
					}
				}

				var newTurns []core.ChatMessage
				if historyCount < len(state.ChatRequest.Messages) {
					for _, m := range state.ChatRequest.Messages[historyCount:] {
						if m.Role == "user" || m.Role == "tool" {
							newTurns = append(newTurns, m)
						}
					}
				}
				newTurns = append(newTurns, state.ChatResponse.Message)

				_ = h.threadStore.AppendMessages(r.Context(), state.ProjectID, state.Environment, state.ChatRequest.ThreadID, newTurns, ttl)
			}
		}
		_ = json.NewEncoder(w).Encode(state.ChatResponse)
		if h.cacheStore != nil && state.AIApp != nil && state.AIApp.Cache.Enabled && state.Error == nil {
			if hit, _ := state.Metadata["cache_hit"].(bool); !hit {
				h.cacheChatResponse(r.Context(), state, state.ChatResponse)
			}
		}
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

	writeRateLimitHeaders(w, state.Metadata)
	if state.SelectedModel != "" {
		w.Header().Set("X-Beavend-Model", state.SelectedModel)
	}
	if state.Metadata != nil {
		if hit, ok := state.Metadata["cache_hit"].(bool); ok && hit {
			w.Header().Set("X-Cache", "HIT")
		} else if state.AIApp != nil && state.AIApp.Cache.Enabled {
			w.Header().Set("X-Cache", "MISS")
		}
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
		usageMu         sync.Mutex
		capturedUsage   core.UsageStats
		capturedErr     *core.ErrorResponse
		fullTextBuilder strings.Builder
	)

	bufferedMode := state.AIApp != nil && state.AIApp.Guardrails.StreamMode == "buffered"
	postCheckMode := state.AIApp != nil && state.AIApp.Guardrails.StreamMode == "passthrough_with_post_check"

	go func() {
		defer close(done)
		defer close(monitoredChan)

		var (
			bufferedEvents []core.StreamEvent
			pendingDone    *core.StreamEvent
		)

		for ev := range origChan {
			if ev.Model == "" {
				ev.Model = state.SelectedModel
			}
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
			if ev.Type == "delta" {
				fullTextBuilder.WriteString(ev.Delta)
			}

			if bufferedMode {
				bufferedEvents = append(bufferedEvents, ev)
				if err := r.Context().Err(); err != nil {
					// Client disconnected; drain origChan to capture final usage without blocking
					for ev := range origChan {
						if ev.Type == "usage" && ev.Usage != nil {
							usageMu.Lock()
							capturedUsage = *ev.Usage
							usageMu.Unlock()
						}
					}
					return
				}
				continue
			}

			if postCheckMode && ev.Type == "done" {
				evCopy := ev
				pendingDone = &evCopy
				continue
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

		if bufferedMode {
			shouldEvaluate := fullTextBuilder.Len() > 0 || (state.AIApp != nil && state.AIApp.ResponseSchema != "")
			if capturedErr == nil && shouldEvaluate {
				sanitized, err := h.evaluateOutputGuardrails(r.Context(), state, fullTextBuilder.String())
				if err != nil {
					usageMu.Lock()
					capturedErr = &core.ErrorResponse{
						Code:      "guardrail_blocked",
						Message:   err.Error(),
						RequestID: state.RequestID,
					}
					usageMu.Unlock()

					if state.Metadata == nil {
						state.Metadata = make(map[string]any)
					}
					state.Metadata["guardrail_blocked"] = true

					select {
					case monitoredChan <- core.StreamEvent{Type: "error", Error: capturedErr, RequestID: state.RequestID}:
					case <-r.Context().Done():
					}
					return
				}

				if sanitized != fullTextBuilder.String() {
					var nonDeltas []core.StreamEvent
					for _, ev := range bufferedEvents {
						if ev.Type != "delta" {
							nonDeltas = append(nonDeltas, ev)
						}
					}
					bufferedEvents = append([]core.StreamEvent{{Type: "delta", Delta: sanitized}}, nonDeltas...)
				}
			}

			for _, ev := range bufferedEvents {
				select {
				case monitoredChan <- ev:
				case <-r.Context().Done():
					return
				}
			}
		} else if postCheckMode {
			shouldEvaluate := fullTextBuilder.Len() > 0 || (state.AIApp != nil && state.AIApp.ResponseSchema != "")
			if capturedErr == nil && shouldEvaluate {
				_, err := h.evaluateOutputGuardrails(r.Context(), state, fullTextBuilder.String())
				if err != nil {
					usageMu.Lock()
					capturedErr = &core.ErrorResponse{
						Code:      "guardrail_blocked",
						Message:   err.Error(),
						RequestID: state.RequestID,
					}
					usageMu.Unlock()

					if state.Metadata == nil {
						state.Metadata = make(map[string]any)
					}
					state.Metadata["guardrail_blocked"] = true

					select {
					case monitoredChan <- core.StreamEvent{Type: "error", Error: capturedErr, RequestID: state.RequestID}:
					case <-r.Context().Done():
					}
					return
				}
			}

			if pendingDone != nil {
				select {
				case monitoredChan <- *pendingDone:
				case <-r.Context().Done():
				}
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
	case <-time.After(2 * time.Second):
	}

	usageMu.Lock()
	if capturedUsage.TotalTokens > 0 || capturedUsage.InputTokens > 0 || capturedUsage.OutputTokens > 0 {
		state.Usage = capturedUsage
	}
	if capturedErr != nil {
		state.Error = capturedErr
	}
	usageMu.Unlock()

	// Cache response on miss if stream completed successfully without error
	if h.cacheStore != nil && state.AIApp != nil && state.AIApp.Cache.Enabled && state.Error == nil {
		if hit, _ := state.Metadata["cache_hit"].(bool); !hit {
			text := fullTextBuilder.String()
			if text != "" {
				h.cacheChatResponse(r.Context(), state, &core.ChatResponse{
					Message: core.ChatMessage{
						Role:    "assistant",
						Content: text,
					},
					Usage: state.Usage,
				})
			}
		}
	}

	// Append assistant message to thread if thread is active
	if h.threadStore != nil && state.ChatRequest != nil && state.ChatRequest.ThreadID != "" && state.Error == nil {
		text := fullTextBuilder.String()
		if text != "" {
			ttl := 24 * time.Hour
			if state.AIApp != nil && state.AIApp.Memory.ThreadTTLSec > 0 {
				ttl = time.Duration(state.AIApp.Memory.ThreadTTLSec) * time.Second
			}

			historyCount := 0
			if state.Metadata != nil {
				if hc, ok := state.Metadata["thread_history_count"].(int); ok {
					historyCount = hc
				}
			}

			var newTurns []core.ChatMessage
			if historyCount < len(state.ChatRequest.Messages) {
				for _, m := range state.ChatRequest.Messages[historyCount:] {
					if m.Role == "user" || m.Role == "tool" {
						newTurns = append(newTurns, m)
					}
				}
			}
			newTurns = append(newTurns, core.ChatMessage{
				Role:    "assistant",
				Content: text,
			})

			_ = h.threadStore.AppendMessages(r.Context(), state.ProjectID, state.Environment, state.ChatRequest.ThreadID, newTurns, ttl)
		}
	}

	h.recordUsage(state)
}

func (h *Handler) cacheChatResponse(ctx context.Context, state *core.ReqState, resp *core.ChatResponse) {
	if h.cacheStore == nil || state.AIApp == nil || resp == nil {
		return
	}
	if !cache.ShouldCacheResponse(state.ChatRequest, resp, state.AIApp) {
		return
	}
	var key string
	if state.Metadata != nil {
		if k, ok := state.Metadata["cache_key"].(string); ok && k != "" {
			key = k
		}
	}
	if key == "" {
		hash, err := cache.ComputeRequestHash(state.ChatRequest, state.AIApp, state.SelectedModel)
		if err != nil {
			return
		}
		subject := ""
		if state.Identity != nil {
			subject = state.Identity.Subject
		}
		key = cache.ResponseCacheKey(state.ProjectID, state.Environment, state.AIApp.Cache.Scope, subject, hash)
	}

	ttl := time.Duration(state.AIApp.Cache.TTL) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	if b, err := json.Marshal(resp); err == nil {
		_ = h.cacheStore.Set(ctx, key, b, ttl)
	}
}

func (h *Handler) evaluateOutputGuardrails(ctx context.Context, state *core.ReqState, content string) (string, error) {
	if state == nil || state.AIApp == nil {
		return content, nil
	}

	app := state.AIApp
	cfg := app.Guardrails
	engine := guardrails.NewEngine(guardrails.Config{
		MaxOutputBytes: cfg.MaxOutputBytes,
		PIIRedaction:   cfg.PIIRedaction,
	})

	if app.ResponseSchema != "" {
		var schema *jsonschema.Schema
		if state.Snapshot != nil && state.Snapshot.CompiledSchemas != nil {
			if raw, ok := state.Snapshot.CompiledSchemas["app_response:"+app.ID]; ok {
				if s, ok2 := raw.(*jsonschema.Schema); ok2 && s != nil {
					schema = s
				}
			}
		}
		if schema == nil {
			var err error
			schema, err = guardrails.CompileSchema(app.ID, app.ResponseSchema)
			if err != nil {
				slog.Error("failed to compile response schema on the fly", slog.String("app_id", app.ID), slog.String("error", err.Error()))
				return "", fmt.Errorf("invalid application response schema configuration: %w", err)
			}
		}
		if schema != nil {
			engine.SetSchemaGuardrail(guardrails.NewSchemaGuardrail(schema))
		}
	}

	return engine.EvaluateOutput(ctx, content)
}

// CancelRequestBody represents the payload to cancel an active or suspended request.
type CancelRequestBody struct {
	RequestID         string `json:"request_id,omitempty"`
	ContinuationToken string `json:"continuation_token,omitempty"`
}

// HandleCancel handles cancelling an active or suspended request (POST /v1/apps/{ai_app_id}/cancel).
func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req CancelRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeRawError(w, "invalid_argument", "invalid request body", reqID)
		return
	}

	targetReqID := strings.TrimSpace(req.RequestID)
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	id, _ := identity.FromContext(r.Context())
	if id != nil && id.Project != "" && projectID == "" {
		projectID = id.Project
	}
	if snap, ok := core.SnapshotFromContext(r.Context()); ok && snap != nil && projectID == "" {
		projectID = snap.ProjectID
	}

	if req.ContinuationToken != "" {
		claims, err := h.continuationMgr.ValidateConfirmationToken(req.ContinuationToken)
		if err != nil {
			h.writeRawError(w, continuationErrorCode(err), fmt.Sprintf("continuation validation failed: %v", err), reqID)
			return
		}
		if claims.Subject != "" && id != nil && id.Subject != "" && claims.Subject != id.Subject {
			h.writeRawError(w, "forbidden", fmt.Sprintf("token subject %q != caller subject %q", claims.Subject, id.Subject), reqID)
			return
		}
		if claims.ProjectID != "" {
			projectID = claims.ProjectID
		}
		if targetReqID == "" {
			targetReqID = claims.RequestID
		}
	}

	if targetReqID == "" {
		h.writeRawError(w, "invalid_argument", "request_id or continuation_token is required", reqID)
		return
	}

	if h.continuationMgr != nil && h.continuationMgr.Store() != nil && projectID != "" {
		_ = h.continuationMgr.Store().Delete(r.Context(), projectID, targetReqID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "cancelled",
		"request_id": targetReqID,
	})
}

// HandleGetApp returns the AI App metadata from snapshot (GET /v1/apps/{ai_app_id}).
func (h *Handler) HandleGetApp(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)
	appID := chi.URLParam(r, "ai_app_id")
	if appID == "" {
		h.writeRawError(w, "invalid_argument", "missing ai_app_id parameter", reqID)
		return
	}

	var snap *core.Snapshot
	if s, ok := core.SnapshotFromContext(r.Context()); ok && s != nil {
		snap = s
	} else {
		projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
		env := strings.TrimSpace(r.Header.Get("X-Environment"))
		if env == "" {
			env = h.defaultEnv
		}
		if h.resolver != nil && projectID != "" {
			snap, _ = h.resolver.Get(projectID, env)
		}
	}

	if snap == nil {
		h.writeRawError(w, "unavailable", "configuration snapshot not found", reqID)
		return
	}

	app, ok := snap.App(appID)
	if !ok || app == nil {
		h.writeRawError(w, "not_found", fmt.Sprintf("ai app %q not found", appID), reqID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":               app.ID,
		"name":             app.Name,
		"model_routing_id": app.ModelRoutingID,
		"status":           "active",
		"created_at":       app.CreatedAt,
		"updated_at":       app.UpdatedAt,
	})
}

// HandleGetJob returns the status and result of an async media job (GET /v1/apps/{ai_app_id}/jobs/{job_id}).
func (h *Handler) HandleGetJob(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)
	if h.jobStore == nil {
		h.writeRawError(w, "unavailable", "job store not configured", reqID)
		return
	}

	jobID := chi.URLParam(r, "job_id")
	if jobID == "" {
		h.writeRawError(w, "invalid_argument", "missing job_id parameter", reqID)
		return
	}

	var projectID string
	if snap, ok := core.SnapshotFromContext(r.Context()); ok && snap != nil {
		projectID = snap.ProjectID
	} else {
		projectID = strings.TrimSpace(r.Header.Get("X-Project-ID"))
	}

	job, err := h.jobStore.GetJob(r.Context(), projectID, jobID)
	if err != nil {
		if errors.Is(err, media.ErrJobNotFound) {
			h.writeRawError(w, "not_found", fmt.Sprintf("job %q not found", jobID), reqID)
			return
		}
		h.writeRawError(w, "internal", fmt.Sprintf("failed to get job: %v", err), reqID)
		return
	}

	// Verify that the job belongs to the requested app if ai_app_id is present
	appID := chi.URLParam(r, "ai_app_id")
	if appID != "" && job.AIAppID != "" && job.AIAppID != appID {
		h.writeRawError(w, "not_found", fmt.Sprintf("job %q not found for app %q", jobID, appID), reqID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(job)
}

// HandleGetMedia serves a media file with signature verification (GET /v1/media/{project_id}/{media_id}).
func (h *Handler) HandleGetMedia(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)
	if h.mediaStore == nil {
		h.writeRawError(w, "not_implemented", "media storage not configured", reqID)
		return
	}
	projectID := chi.URLParam(r, "project_id")
	mediaID := chi.URLParam(r, "media_id")
	if projectID == "" || mediaID == "" {
		h.writeRawError(w, "invalid_argument", "project_id and media_id are required", reqID)
		return
	}

	expStr := r.URL.Query().Get("exp")
	sig := r.URL.Query().Get("sig")
	if expStr == "" || sig == "" {
		h.writeRawError(w, "unauthenticated", "signature and expiration are required", reqID)
		return
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		h.writeRawError(w, "invalid_argument", "invalid expiration timestamp", reqID)
		return
	}

	if localStore, ok := h.mediaStore.(*media.LocalMediaStore); ok {
		if err := localStore.VerifySignature(projectID, mediaID, exp, sig); err != nil {
			h.writeRawError(w, "unauthenticated", "invalid or expired media signature", reqID)
			return
		}
	}

	rc, ref, err := h.mediaStore.Get(r.Context(), projectID, mediaID)
	if err != nil {
		if errors.Is(err, media.ErrMediaNotFound) {
			h.writeRawError(w, "not_found", fmt.Sprintf("media %q not found", mediaID), reqID)
			return
		}
		h.writeRawError(w, "internal", fmt.Sprintf("failed to get media: %v", err), reqID)
		return
	}
	defer rc.Close()

	if ref != nil && ref.MimeType != "" {
		w.Header().Set("Content-Type", ref.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if ref != nil && ref.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(ref.Size, 10))
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, rc)
}

// ConfirmRequestBody represents the payload to confirm or deny tool execution.
type ConfirmRequestBody struct {
	ConfirmationToken string `json:"confirmation_token"`
	Approved          bool   `json:"approved"`
}

// HandleConfirm handles resuming a request suspended for tool confirmation (POST /v1/apps/{ai_app_id}/confirm).
func (h *Handler) HandleConfirm(w http.ResponseWriter, r *http.Request) {
	reqID := getOrGenerateRequestID(r)

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
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
		case <-time.After(2 * time.Second):
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

	var snap *core.Snapshot
	var projectID string
	var env string

	if s, ok := core.SnapshotFromContext(r.Context()); ok && s != nil {
		snap = s
		projectID = s.ProjectID
		env = s.Environment
	}

	if projectID == "" {
		projectID = strings.TrimSpace(r.Header.Get("X-Project-ID"))
	}
	if env == "" {
		env = strings.TrimSpace(r.Header.Get("X-Environment"))
		if env == "" {
			env = h.defaultEnv
		}
	}

	if snap == nil {
		if projectID == "" {
			h.writeRawError(w, "invalid_argument", "missing required X-Project-ID header", reqID)
			return nil, false
		}
		if h.resolver != nil {
			var ok bool
			snap, ok = h.resolver.Get(projectID, env)
			if !ok || snap == nil {
				h.writeRawError(w, "unavailable", fmt.Sprintf("configuration snapshot for project %q (%s) not found", projectID, env), reqID)
				return nil, false
			}
		} else {
			h.writeRawError(w, "unavailable", "snapshot resolver not configured", reqID)
			return nil, false
		}
	}

	app, ok := snap.App(appID)
	if !ok || app == nil {
		h.writeRawError(w, "not_found", fmt.Sprintf("ai app %q not found", appID), reqID)
		return nil, false
	}

	var chatReq core.ChatRequest
	if r.Body != nil {
		const maxBodySize = 10 << 20 // 10MB limit
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				h.writeRawError(w, "payload_too_large", "request payload exceeds 10MB limit", reqID)
				return nil, false
			}
			h.writeRawError(w, "invalid_argument", fmt.Sprintf("failed to read request body: %v", err), reqID)
			return nil, false
		}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &chatReq); err != nil {
				h.writeRawError(w, "invalid_argument", fmt.Sprintf("invalid json payload: %v", err), reqID)
				return nil, false
			}
		}
	}
	chatReq.Stream = isStreaming

	id, _ := identity.FromContext(r.Context())
	rawToken, _ := identity.RawTokenFromContext(r.Context())
	if rawToken == "" {
		if authHdr := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(authHdr), "bearer ") {
			rawToken = strings.TrimSpace(authHdr[7:])
		}
	}

	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP != "" {
		if idx := strings.Index(clientIP, ","); idx != -1 {
			clientIP = strings.TrimSpace(clientIP[:idx])
		}
	}
	if clientIP == "" {
		clientIP = r.Header.Get("X-Real-IP")
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}
	}

	metadata := map[string]any{
		"client_ip": clientIP,
	}

	return &core.ReqState{
		RequestID:   reqID,
		ProjectID:   projectID,
		Environment: env,
		AIAppID:     appID,
		Snapshot:    snap,
		AIApp:       app,
		Identity:    id,
		RawToken:    rawToken,
		IsStreaming: isStreaming,
		ChatRequest: &chatReq,
		Metadata:    metadata,
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
	case "payload_too_large":
		return http.StatusRequestEntityTooLarge
	case "guardrail_blocked":
		return http.StatusUnprocessableEntity
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
		_ = h.usageStore.RecordUsage(context.Background(), &stateCopy)
	}
}

// SetThreadStore updates the handler's thread store.
func (h *Handler) SetThreadStore(store core.ThreadStore) {
	h.threadStore = store
}

// SetMemoryEngine updates the handler's memory engine.
func (h *Handler) SetMemoryEngine(engine *memory.Engine) {
	h.memoryEngine = engine
}

// HandleGetThread retrieves a conversation thread by ID.
func (h *Handler) HandleGetThread(w http.ResponseWriter, r *http.Request) {
	if h.threadStore == nil {
		h.writeRawError(w, "not_implemented", "Thread store not configured", "")
		return
	}

	threadID := chi.URLParam(r, "thread_id")
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	env := strings.TrimSpace(r.Header.Get("X-Environment"))
	if env == "" {
		env = h.defaultEnv
	}

	th, err := h.threadStore.GetThread(r.Context(), projectID, env, threadID)
	if err != nil {
		if errors.Is(err, memory.ErrThreadNotFound) {
			h.writeRawError(w, "not_found", fmt.Sprintf("thread %q not found or expired", threadID), "")
			return
		}
		h.writeRawError(w, "internal_error", err.Error(), "")
		return
	}

	id, _ := core.IdentityFromContext(r.Context())
	if th.Subject != "" && id != nil && th.Subject != id.Subject {
		h.writeRawError(w, "forbidden", "thread belongs to another user", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(th)
}

// HandleDeleteThread removes a conversation thread.
func (h *Handler) HandleDeleteThread(w http.ResponseWriter, r *http.Request) {
	if h.threadStore == nil {
		h.writeRawError(w, "not_implemented", "Thread store not configured", "")
		return
	}

	threadID := chi.URLParam(r, "thread_id")
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	env := strings.TrimSpace(r.Header.Get("X-Environment"))
	if env == "" {
		env = h.defaultEnv
	}

	th, err := h.threadStore.GetThread(r.Context(), projectID, env, threadID)
	if err == nil && th != nil {
		id, _ := core.IdentityFromContext(r.Context())
		if th.Subject != "" && id != nil && th.Subject != id.Subject {
			h.writeRawError(w, "forbidden", "thread belongs to another user", "")
			return
		}
	}

	if err := h.threadStore.DeleteThread(r.Context(), projectID, env, threadID); err != nil {
		h.writeRawError(w, "internal_error", err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "deleted",
		"thread_id": threadID,
	})
}

// HandleCreateThread creates a new conversation thread.
func (h *Handler) HandleCreateThread(w http.ResponseWriter, r *http.Request) {
	if h.threadStore == nil {
		h.writeRawError(w, "not_implemented", "Thread store not configured", "")
		return
	}

	appID := chi.URLParam(r, "ai_app_id")
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	env := strings.TrimSpace(r.Header.Get("X-Environment"))
	if env == "" {
		env = h.defaultEnv
	}

	var req struct {
		ThreadID   string             `json:"thread_id,omitempty"`
		Messages   []core.ChatMessage `json:"messages,omitempty"`
		Metadata   map[string]any     `json:"metadata,omitempty"`
		TTLSeconds int                `json:"ttl_seconds,omitempty"`
	}

	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	id, _ := core.IdentityFromContext(r.Context())
	th := &core.Thread{
		ID:          req.ThreadID,
		ProjectID:   projectID,
		Environment: env,
		AIAppID:     appID,
		Messages:    req.Messages,
		Metadata:    req.Metadata,
		TTLSeconds:  req.TTLSeconds,
	}
	if id != nil {
		th.Issuer = id.Issuer
		th.Subject = id.Subject
	}

	if err := h.threadStore.CreateThread(r.Context(), th); err != nil {
		h.writeRawError(w, "internal_error", err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(th)
}

