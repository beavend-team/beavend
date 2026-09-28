package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
)

var (
	// ErrPromptNotFound indicates the configured prompt was not found in the snapshot.
	ErrPromptNotFound = errors.New("prompt not found in snapshot")

	promptMeter             = otel.Meter("beavend/stages/prompt")
	promptRendersCounter, _ = promptMeter.Int64Counter("beavend_prompt_renders_total", metric.WithDescription("Total count of prompt template renders"))
)

// PromptStage renders versioned prompt templates and injects system instructions.
type PromptStage struct {
	engine *prompts.Engine
	tracer trace.Tracer
}

// NewPromptStage creates a new PromptStage.
func NewPromptStage(engine *prompts.Engine) (*PromptStage, error) {
	if engine == nil {
		engine = prompts.NewEngine()
	}

	return &PromptStage{
		engine: engine,
		tracer: otel.Tracer("beavend/stages/prompt"),
	}, nil
}

// Handle resolves the prompt from the snapshot, interpolates variables, and sets the system message.
func (s *PromptStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	ctx, span := s.tracer.Start(ctx, "stage.prompt",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
		),
	)
	defer span.End()

	if state.Snapshot == nil {
		state.Error = &core.ErrorResponse{
			Code:      "unavailable",
			Message:   "missing configuration snapshot in request state",
			RequestID: state.RequestID,
		}
		return ErrMissingSnapshot
	}

	if state.AIApp == nil {
		if state.AIAppID == "" {
			state.Error = &core.ErrorResponse{
				Code:      "invalid_argument",
				Message:   "missing ai app id in request state",
				RequestID: state.RequestID,
			}
			return ErrMissingAIApp
		}
		app, exists := state.Snapshot.App(state.AIAppID)
		if !exists {
			state.Error = &core.ErrorResponse{
				Code:      "not_found",
				Message:   fmt.Sprintf("ai app %q not found", state.AIAppID),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("%w: %s", ErrMissingAIApp, state.AIAppID)
		}
		state.AIApp = app
	}

	// If no prompt is configured on this AI App, skip rendering
	if state.AIApp.PromptID == "" {
		span.SetAttributes(attribute.Bool("prompt.configured", false))
		return nil
	}

	span.SetAttributes(
		attribute.Bool("prompt.configured", true),
		attribute.String("prompt.id", state.AIApp.PromptID),
	)

	promptVer, exists := state.Snapshot.Prompt(state.AIApp.PromptID)
	if !exists {
		state.Error = &core.ErrorResponse{
			Code:      "not_found",
			Message:   fmt.Sprintf("prompt %q not found in snapshot", state.AIApp.PromptID),
			RequestID: state.RequestID,
		}
		return fmt.Errorf("%w: %s", ErrPromptNotFound, state.AIApp.PromptID)
	}

	span.SetAttributes(attribute.Int("prompt.version", promptVer.Version))

	// Merge request-level metadata if present
	mergedMeta := make(map[string]any)
	for k, v := range state.Metadata {
		mergedMeta[k] = v
	}
	if state.ChatRequest != nil && state.ChatRequest.Metadata != nil {
		for k, v := range state.ChatRequest.Metadata {
			mergedMeta[k] = v
		}
	}

	varCtx := prompts.BuildVariableContext(state.Snapshot, state.AIApp, state.Identity, mergedMeta)

	rendered, err := s.engine.Render(promptVer.Template, varCtx)
	if err != nil {
		span.RecordError(err)
		state.Error = &core.ErrorResponse{
			Code:      "invalid_argument",
			Message:   fmt.Sprintf("prompt render failed: %v", err),
			RequestID: state.RequestID,
		}
		return fmt.Errorf("render prompt template: %w", err)
	}

	span.SetAttributes(attribute.Int("prompt.char_count", len(rendered)))

	if promptRendersCounter != nil {
		promptRendersCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("prompt_id", state.AIApp.PromptID),
			attribute.Int("version", promptVer.Version),
		))
	}

	// Inject rendered prompt into ChatRequest if not already present
	if state.ChatRequest == nil {
		state.ChatRequest = &core.ChatRequest{}
	}

	if len(state.ChatRequest.Messages) == 0 {
		state.ChatRequest.Messages = []core.ChatMessage{{Role: "system", Content: rendered}}
	} else if state.ChatRequest.Messages[0].Role == "system" {
		state.ChatRequest.Messages[0].Content = rendered
	} else {
		systemMsg := core.ChatMessage{
			Role:    "system",
			Content: rendered,
		}
		state.ChatRequest.Messages = append([]core.ChatMessage{systemMsg}, state.ChatRequest.Messages...)
	}

	slog.InfoContext(ctx, "prompt rendered and injected",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"ai_app_id", state.AIAppID,
		"prompt_id", state.AIApp.PromptID,
		"version", promptVer.Version,
		"length", len(rendered),
	)

	return nil
}
