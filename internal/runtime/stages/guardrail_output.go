package stages

import (
	"context"
	"errors"
	"log/slog"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
)

// OutputGuardrailStage evaluates output guardrails (payload size, output PII, structured schema) on responses.
type OutputGuardrailStage struct {
	tracer trace.Tracer
}

// NewOutputGuardrailStage creates a new OutputGuardrailStage.
func NewOutputGuardrailStage() *OutputGuardrailStage {
	return &OutputGuardrailStage{
		tracer: otel.Tracer("beavend/stages/guardrail_output"),
	}
}

// Handle executes output guardrail checks on the chat response.
func (s *OutputGuardrailStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	// Output guardrail on synchronous responses only; streaming responses handle guardrails via stream delivery
	if state.ChatResponse == nil {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stage.guardrail_output",
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
		MaxOutputBytes: cfg.MaxOutputBytes,
		PIIRedaction:   cfg.PIIRedaction,
	})

	content := state.ChatResponse.Message.Content

	// 1. Enforce output byte size limit
	if cfg.MaxOutputBytes > 0 && len(content) > cfg.MaxOutputBytes {
		v := &guardrails.Violation{
			Type:    "size",
			Message: "model response size exceeds maximum allowed bytes",
			Details: map[string]any{
				"actual_bytes": len(content),
				"max_bytes":    cfg.MaxOutputBytes,
			},
		}
		return s.fail(ctx, state, v)
	}

	// 2. Enforce structured JSON Schema if defined on AI App
	if app.ResponseSchema != "" {
		var schema *jsonschema.Schema

		// Check snapshot pre-compiled schema
		if state.Snapshot != nil && state.Snapshot.CompiledSchemas != nil {
			if raw, ok := state.Snapshot.CompiledSchemas["app_response:"+app.ID]; ok {
				if s, ok2 := raw.(*jsonschema.Schema); ok2 && s != nil {
					schema = s
				}
			}
		}

		// Fallback compile if not in snapshot
		if schema == nil {
			var err error
			schema, err = guardrails.CompileSchema(app.ID, app.ResponseSchema)
			if err != nil {
				slog.Error("failed to compile response schema on the fly", slog.String("app_id", app.ID), slog.String("error", err.Error()))
			}
		}

		if schema != nil {
			sg := guardrails.NewSchemaGuardrail(schema)
			if err := sg.CheckOutput(ctx, content); err != nil {
				var v *guardrails.Violation
				if errors.As(err, &v) {
					return s.fail(ctx, state, v)
				}
				return err
			}

			// Clean and standardize JSON formatting in response
			if cleanJSON, err := guardrails.ExtractCleanJSON(content); err == nil {
				state.ChatResponse.Message.Content = cleanJSON
				content = cleanJSON
			}
		}
	}

	// 3. Redact PII in output if enabled
	if cfg.PIIRedaction {
		redacted, _ := engine.RedactOutput(content)
		state.ChatResponse.Message.Content = redacted
	}

	return nil
}

func (s *OutputGuardrailStage) fail(ctx context.Context, state *core.ReqState, v *guardrails.Violation) error {
	guardrailViolationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("violation_type", v.Type),
		),
	)

	slog.Warn("output guardrail blocked response",
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
