package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type contextKey string

const (
	ctxKeyRequestID   contextKey = "request_id"
	ctxKeyProjectID   contextKey = "project_id"
	ctxKeyEnvironment contextKey = "environment"
)

// Sensitive keys to be redacted in logs.
var sensitiveKeys = map[string]bool{
	"password":      true,
	"passwd":        true,
	"token":         true,
	"secret":        true,
	"authorization": true,
	"api_key":       true,
	"apikey":        true,
	"master_key":    true,
	"private_key":   true,
	"credential":    true,
	"bearer":        true,
}

// RedactValue determines if a key or attribute is sensitive and redacts its value.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if sensitiveKeys[lower] {
		return true
	}
	for sensitive := range sensitiveKeys {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

// RedactingHandler wraps an slog.Handler to redact sensitive keys and inject context attributes.
type RedactingHandler struct {
	next slog.Handler
}

// NewRedactingHandler creates a new RedactingHandler wrapping next.
func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract context attributes if present
	var contextAttrs []slog.Attr
	if reqID := RequestIDFromContext(ctx); reqID != "" {
		contextAttrs = append(contextAttrs, slog.String("request_id", reqID))
	}
	if projID := ProjectIDFromContext(ctx); projID != "" {
		contextAttrs = append(contextAttrs, slog.String("project_id", projID))
	}
	if env := EnvironmentFromContext(ctx); env != "" {
		contextAttrs = append(contextAttrs, slog.String("environment", env))
	}

	if len(contextAttrs) > 0 {
		r.AddAttrs(contextAttrs...)
	}

	return h.next.Handle(ctx, r)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redactedAttrs := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redactedAttrs[i] = sanitizeAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(redactedAttrs)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		sanitized := make([]slog.Attr, len(groupAttrs))
		for i, child := range groupAttrs {
			sanitized[i] = sanitizeAttr(child)
		}
		return slog.Attr{
			Key:   a.Key,
			Value: slog.GroupValue(sanitized...),
		}
	}
	return a
}

// SetupLogger initializes the global slog logger.
func SetupLogger(levelStr string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}

	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			return sanitizeAttr(a)
		},
	}

	baseHandler := slog.NewJSONHandler(w, opts)
	handler := NewRedactingHandler(baseHandler)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// Context helpers for attaching and retrieving logging identifiers.

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func WithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, ctxKeyProjectID, projectID)
}

func ProjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyProjectID).(string); ok {
		return v
	}
	return ""
}

func WithEnvironment(ctx context.Context, environment string) context.Context {
	return context.WithValue(ctx, ctxKeyEnvironment, environment)
}

func EnvironmentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyEnvironment).(string); ok {
		return v
	}
	return ""
}
