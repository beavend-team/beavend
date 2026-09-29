package stages

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
)

var (
	// ErrGuardrailBlocked indicates that a request or response violated configured guardrails.
	ErrGuardrailBlocked = errors.New("guardrail blocked request")

	guardrailMeter              = otel.Meter("beavend/stages/guardrails")
	guardrailViolationsTotal, _ = guardrailMeter.Int64Counter("beavend_guardrail_violations_total", metric.WithDescription("Total count of guardrail violations"))
)

// InputGuardrailStage evaluates input guardrails (payload size, prompt injection, input PII) in the runtime pipeline.
type InputGuardrailStage struct {
	tracer trace.Tracer
}

// NewInputGuardrailStage creates a new InputGuardrailStage.
func NewInputGuardrailStage() *InputGuardrailStage {
	return &InputGuardrailStage{
		tracer: otel.Tracer("beavend/stages/guardrail_input"),
	}
}

// Handle executes input guardrail checks on the request.
func (s *InputGuardrailStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	ctx, span := s.tracer.Start(ctx, "stage.guardrail_input",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
		),
	)
	defer span.End()

	app := state.AIApp
	if app == nil && state.Snapshot != nil {
		app = state.Snapshot.Apps[state.AIAppID]
		state.AIApp = app
	}
	if app == nil {
		return nil
	}

	cfg := app.Guardrails
	engine := guardrails.NewEngine(guardrails.Config{
		MaxInputBytes:    cfg.MaxInputBytes,
		PIIRedaction:     cfg.PIIRedaction,
		InjectionDefense: cfg.InjectionDefense,
	})

	if state.ChatRequest == nil {
		return nil
	}

	// Calculate cumulative input bytes and check messages
	totalInputBytes := 0
	for _, att := range state.ChatRequest.Attachments {
		totalInputBytes += len(att.Data) + len(att.URL)
	}

	for i := range state.ChatRequest.Messages {
		msg := &state.ChatRequest.Messages[i]
		totalInputBytes += len(msg.Content)

		// Check prompt injection only on user / untrusted turns
		if cfg.InjectionDefense && (msg.Role == "user" || msg.Role == "system") {
			if err := engine.CheckInput(ctx, msg.Content); err != nil {
				var v *guardrails.Violation
				if errors.As(err, &v) {
					return s.fail(ctx, state, v)
				}
				return err
			}
		}

		// Redact PII in user/system input messages if enabled
		if cfg.PIIRedaction && (msg.Role == "user" || msg.Role == "system") {
			redacted, _ := engine.RedactInput(msg.Content)
			msg.Content = redacted
		}
	}

	// Verify maximum input bytes limit
	if cfg.MaxInputBytes > 0 && totalInputBytes > cfg.MaxInputBytes {
		v := &guardrails.Violation{
			Type:    "size",
			Message: "input payload size exceeds maximum allowed bytes",
			Details: map[string]any{
				"actual_bytes": totalInputBytes,
				"max_bytes":    cfg.MaxInputBytes,
			},
		}
		return s.fail(ctx, state, v)
	}

	return nil
}

func (s *InputGuardrailStage) fail(ctx context.Context, state *core.ReqState, v *guardrails.Violation) error {
	guardrailViolationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("violation_type", v.Type),
		),
	)

	slog.Warn("input guardrail blocked request",
		slog.String("request_id", state.RequestID),
		slog.String("project_id", state.ProjectID),
		slog.String("ai_app_id", state.AIAppID),
		slog.String("violation_type", v.Type),
		slog.String("violation_message", v.Message),
	)

	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}
	state.Metadata["guardrail_blocked"] = true
	state.Metadata["guardrail_violation_type"] = v.Type

	state.Error = &core.ErrorResponse{
		Code:      "guardrail_blocked",
		Message:   v.Message,
		RequestID: state.RequestID,
		Details:   v.Details,
	}

	return ErrGuardrailBlocked
}
