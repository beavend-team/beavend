package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/runtime/stream"
	"github.com/beavend/beavend/internal/telemetry"
)

// SnapshotResolver retrieves a Snapshot for a given project and environment.
type SnapshotResolver interface {
	Get(projectID, env string) (*core.Snapshot, bool)
}

// Handler handles runtime AI App requests for chat completion and streaming.
type Handler struct {
	resolver     SnapshotResolver
	pipeline     *Pipeline
	streamWriter *stream.Writer
	defaultEnv   string
}

// HandlerOptions configures runtime Handler.
type HandlerOptions struct {
	DefaultEnvironment string
	HeartbeatInterval  time.Duration
}

// NewHandler creates a new runtime Handler.
func NewHandler(resolver SnapshotResolver, pipeline *Pipeline, opts ...HandlerOptions) *Handler {
	defaultEnv := "development"
	heartbeat := 15 * time.Second

	if len(opts) > 0 {
		if opts[0].DefaultEnvironment != "" {
			defaultEnv = opts[0].DefaultEnvironment
		}
		if opts[0].HeartbeatInterval > 0 {
			heartbeat = opts[0].HeartbeatInterval
		}
	}

	return &Handler{
		resolver:     resolver,
		pipeline:     pipeline,
		streamWriter: stream.NewWriter(heartbeat),
		defaultEnv:   defaultEnv,
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
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if state.ChatResponse != nil {
		_ = json.NewEncoder(w).Encode(state.ChatResponse)
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}
}

// HandleStream handles Server-Sent Events streaming chat requests (POST /v1/apps/{ai_app_id}/stream).
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	state, ok := h.buildRequestState(w, r, true)
	if !ok {
		return
	}

	// Bypass default write timeout for long-lived SSE streaming
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	if err := h.pipeline.Execute(r.Context(), state); err != nil {
		h.writeError(w, state.Error)
		return
	}

	if err := h.streamWriter.Stream(r.Context(), w, state.StreamChan); err != nil {
		slog.WarnContext(r.Context(), "sse streaming ended with error",
			"request_id", state.RequestID,
			"project_id", state.ProjectID,
			"env", state.Environment,
			"ai_app_id", state.AIAppID,
			"error", err,
		)
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

	// 1. Resolve Snapshot (from context or registry)
	snap, ok := core.SnapshotFromContext(r.Context())
	if !ok || snap == nil {
		if h.resolver != nil {
			snap, ok = h.resolver.Get(projectID, env)
		}
	}
	if !ok || snap == nil {
		h.writeRawError(w, "unavailable", fmt.Sprintf("project %q (%s) configuration snapshot not available", projectID, env), reqID)
		return nil, false
	}

	// 2. Resolve AI App
	app, exists := snap.App(appID)
	if !exists {
		h.writeRawError(w, "not_found", fmt.Sprintf("ai app %q not found", appID), reqID)
		return nil, false
	}

	// 3. Resolve Identity
	id, _ := identity.FromContext(r.Context())
	rawTok, _ := identity.RawTokenFromContext(r.Context())

	// 4. Parse ChatRequest JSON Body (capped at 10MB)
	var chatReq core.ChatRequest
	if r.Body != nil {
		limitedReader := http.MaxBytesReader(w, r.Body, 10<<20)
		bodyBytes, err := io.ReadAll(limitedReader)
		if err != nil {
			h.writeRawError(w, "invalid_argument", "failed to read request body or exceeded 10MB limit", reqID)
			return nil, false
		}
		if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &chatReq); err != nil {
				h.writeRawError(w, "invalid_argument", fmt.Sprintf("invalid json payload: %v", err), reqID)
				return nil, false
			}
		}
	}

	state := &core.ReqState{
		RequestID:   reqID,
		ProjectID:   projectID,
		Environment: env,
		AIAppID:     appID,
		RawToken:    rawTok,
		Identity:    id,
		Snapshot:    snap,
		AIApp:       app,
		ChatRequest: &chatReq,
		IsStreaming: isStreaming,
		Metadata:    make(map[string]any),
		StartTime:   time.Now().UTC(),
	}

	return state, true
}

func (h *Handler) writeError(w http.ResponseWriter, errResp *core.ErrorResponse) {
	if errResp == nil {
		errResp = &core.ErrorResponse{
			Code:    "internal_error",
			Message: "internal server error",
		}
	}

	status := mapErrorCodeToStatus(errResp.Code)

	if errResp.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(errResp.RetryAfter))
	}
	if errResp.RequestID != "" {
		w.Header().Set("X-Request-Id", errResp.RequestID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errResp)
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
