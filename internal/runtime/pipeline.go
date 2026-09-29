package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/runtime/stages"
)

var (
	// ErrPipelineAborted indicates pipeline execution stopped due to a stage error.
	ErrPipelineAborted = errors.New("pipeline aborted")
)

// Pipeline manages sequential execution of runtime stages.
type Pipeline struct {
	stages []core.Stage
	tracer trace.Tracer
}

// NewPipeline creates a new Pipeline with the specified stages.
func NewPipeline(stages ...core.Stage) *Pipeline {
	return &Pipeline{
		stages: stages,
		tracer: otel.Tracer("beavend/runtime/pipeline"),
	}
}

// Stages returns the list of stages configured in this pipeline.
func (p *Pipeline) Stages() []core.Stage {
	return p.stages
}

// AddStage appends a stage to the end of the pipeline.
func (p *Pipeline) AddStage(stage core.Stage) {
	if stage != nil {
		p.stages = append(p.stages, stage)
	}
}

// Execute runs the request state sequentially through all configured stages.
// Halts immediately if context is cancelled or if any stage returns an error.
func (p *Pipeline) Execute(ctx context.Context, state *core.ReqState) error {
	if state == nil {
		return errors.New("nil req state")
	}

	if state.StartTime.IsZero() {
		state.StartTime = time.Now().UTC()
	}

	ctx, span := p.tracer.Start(ctx, "pipeline.execute",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
			attribute.Bool("is_streaming", state.IsStreaming),
		),
	)
	defer span.End()

	for idx, stage := range p.stages {
		if err := ctx.Err(); err != nil {
			span.RecordError(err)
			state.Error = &core.ErrorResponse{
				Code:      "cancelled",
				Message:   "request cancelled by client",
				RequestID: state.RequestID,
			}
			return err
		}

		stageName := fmt.Sprintf("%T", stage)
		stageStart := time.Now()

		err := stage.Handle(ctx, state)
		stageLatency := time.Since(stageStart)

		if err != nil {
			span.RecordError(err)
			span.SetAttributes(
				attribute.Int("failed_stage_index", idx),
				attribute.String("failed_stage_name", stageName),
			)

			// Ensure a structured RFC 7807 ErrorResponse is set on state
			if state.Error == nil {
				state.Error = &core.ErrorResponse{
					Code:      "internal_error",
					Message:   fmt.Sprintf("pipeline stage %s error: %v", stageName, err),
					RequestID: state.RequestID,
				}
			}

			slog.WarnContext(ctx, "pipeline stage failed",
				"stage", stageName,
				"index", idx,
				"latency", stageLatency,
				"request_id", state.RequestID,
				"project_id", state.ProjectID,
				"env", state.Environment,
				"error", err,
			)

			return fmt.Errorf("%w at stage %s: %v", ErrPipelineAborted, stageName, err)
		}
	}

	span.SetAttributes(attribute.Bool("pipeline.success", true))
	return nil
}

// NewStandardPipeline builds the standard Beavend AI runtime pipeline:
// Ingress AuthZ -> Rate Limits -> Prompt Templating -> Model Routing -> Execution
func NewStandardPipeline(
	authz *stages.AuthZStage,
	limits *stages.LimitsStage,
	prompt *stages.PromptStage,
	routing *stages.ModelRoutingStage,
	execution *stages.ExecutionStage,
) *Pipeline {
	return NewFullStandardPipeline(authz, limits, nil, prompt, routing, execution, nil)
}

// NewFullStandardPipeline builds the complete Beavend AI runtime pipeline with guardrails:
// Ingress AuthZ -> Rate Limits -> Input Guardrails -> Cache -> Prompt Templating -> Model Routing -> Execution -> Output Guardrails
func NewFullStandardPipeline(
	authz *stages.AuthZStage,
	limits *stages.LimitsStage,
	inputGuard *stages.InputGuardrailStage,
	prompt *stages.PromptStage,
	routing *stages.ModelRoutingStage,
	execution *stages.ExecutionStage,
	outputGuard *stages.OutputGuardrailStage,
	cacheStage ...*stages.CacheStage,
) *Pipeline {
	p := NewPipeline()
	if authz != nil {
		p.AddStage(authz)
	}
	if limits != nil {
		p.AddStage(limits)
	}
	if inputGuard != nil {
		p.AddStage(inputGuard)
	}
	if len(cacheStage) > 0 && cacheStage[0] != nil {
		p.AddStage(cacheStage[0])
	}
	if prompt != nil {
		p.AddStage(prompt)
	}
	if routing != nil {
		p.AddStage(routing)
	}
	if execution != nil {
		p.AddStage(execution)
	}
	if outputGuard != nil {
		p.AddStage(outputGuard)
	}
	return p
}

// NewAcceleratedPipeline builds the full accelerated Beavend AI runtime pipeline:
// Ingress AuthZ -> Rate Limits -> Input Guardrails -> Cache -> Prompt Templating -> Model Routing -> Execution -> Output Guardrails
func NewAcceleratedPipeline(
	authz *stages.AuthZStage,
	limits *stages.LimitsStage,
	inputGuard *stages.InputGuardrailStage,
	cacheStage *stages.CacheStage,
	prompt *stages.PromptStage,
	routing *stages.ModelRoutingStage,
	execution *stages.ExecutionStage,
	outputGuard *stages.OutputGuardrailStage,
) *Pipeline {
	return NewFullStandardPipeline(authz, limits, inputGuard, prompt, routing, execution, outputGuard, cacheStage)
}

// NewContextAwarePipeline builds the complete Beavend AI runtime pipeline with Context, Threads, Memory, and Cache:
// Ingress AuthZ -> Rate Limits -> Input Guardrails -> Cache -> Context (Threads & Memory) -> Prompt -> Model Routing -> Execution -> Output Guardrails
func NewContextAwarePipeline(
	authz *stages.AuthZStage,
	limits *stages.LimitsStage,
	inputGuard *stages.InputGuardrailStage,
	cacheStage *stages.CacheStage,
	contextStage *stages.ContextStage,
	prompt *stages.PromptStage,
	routing *stages.ModelRoutingStage,
	execution *stages.ExecutionStage,
	outputGuard *stages.OutputGuardrailStage,
) *Pipeline {
	p := NewPipeline()
	if authz != nil {
		p.AddStage(authz)
	}
	if limits != nil {
		p.AddStage(limits)
	}
	if inputGuard != nil {
		p.AddStage(inputGuard)
	}
	if cacheStage != nil {
		p.AddStage(cacheStage)
	}
	if contextStage != nil {
		p.AddStage(contextStage)
	}
	if prompt != nil {
		p.AddStage(prompt)
	}
	if routing != nil {
		p.AddStage(routing)
	}
	if execution != nil {
		p.AddStage(execution)
	}
	if outputGuard != nil {
		p.AddStage(outputGuard)
	}
	return p
}

