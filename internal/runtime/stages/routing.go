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
	"github.com/beavend/beavend/internal/routing"
)

var (
	// ErrModelRoutingFailed indicates model resolution failed.
	ErrModelRoutingFailed = errors.New("model routing failed")

	routingMeter          = otel.Meter("beavend/stages/routing")
	routingResolutions, _ = routingMeter.Int64Counter("beavend_routing_resolutions_total", metric.WithDescription("Total count of model routing resolutions"))
	routingFailures, _    = routingMeter.Int64Counter("beavend_routing_failures_total", metric.WithDescription("Total count of model routing failures"))
)

// ModelRoutingStage resolves the logical model, concrete deployment, and provider adapter for the request.
type ModelRoutingStage struct {
	resolver *routing.Resolver
	tracer   trace.Tracer
}

// NewModelRoutingStage creates a new ModelRoutingStage with the provided resolver.
func NewModelRoutingStage(resolver *routing.Resolver) (*ModelRoutingStage, error) {
	if resolver == nil {
		return nil, errors.New("nil model resolver")
	}

	return &ModelRoutingStage{
		resolver: resolver,
		tracer:   otel.Tracer("beavend/stages/routing"),
	}, nil
}

// Resolver returns the routing.Resolver used by this stage.
func (s *ModelRoutingStage) Resolver() *routing.Resolver {
	return s.resolver
}

// Handle executes the model routing resolution stage.
func (s *ModelRoutingStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil request state")
	}

	if state.ChatResponse != nil {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stages.model_routing",
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
			Code:      "invalid_argument",
			Message:   "missing configuration snapshot in request state",
			RequestID: state.RequestID,
		}
		routingFailures.Add(ctx, 1)
		return ErrMissingSnapshot
	}

	if state.AIApp == nil {
		state.Error = &core.ErrorResponse{
			Code:      "invalid_argument",
			Message:   "missing ai app definition in request state",
			RequestID: state.RequestID,
		}
		routingFailures.Add(ctx, 1)
		return ErrMissingAIApp
	}

	route, err := s.resolver.Resolve(state.Snapshot, state.AIApp, state.ChatRequest)
	if err != nil {
		routingFailures.Add(ctx, 1)
		span.RecordError(err)

		var code string
		if errors.Is(err, routing.ErrModelNotFound) {
			code = "not_found"
		} else {
			code = "provider_error"
		}

		state.Error = &core.ErrorResponse{
			Code:      code,
			Message:   fmt.Sprintf("resolve model deployment: %v", err),
			RequestID: state.RequestID,
		}

		slog.WarnContext(ctx, "model routing resolution failed",
			"request_id", state.RequestID,
			"project_id", state.ProjectID,
			"env", state.Environment,
			"ai_app_id", state.AIAppID,
			"error", err,
		)
		return fmt.Errorf("%w: %v", ErrModelRoutingFailed, err)
	}

	state.ResolvedModel = route.ModelRouting
	if state.ChatRequest != nil {
		state.ChatRequest.Model = route.Deployment
	}

	span.SetAttributes(
		attribute.String("model.logical", route.ModelRouting.LogicalModel),
		attribute.String("model.deployment", route.Deployment),
		attribute.String("model.adapter", route.ModelRouting.Adapter),
	)

	routingResolutions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("logical_model", route.ModelRouting.LogicalModel),
		attribute.String("adapter", route.ModelRouting.Adapter),
	))

	slog.InfoContext(ctx, "model routing resolved",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"ai_app_id", state.AIAppID,
		"logical_model", route.ModelRouting.LogicalModel,
		"deployment", route.Deployment,
		"adapter", route.ModelRouting.Adapter,
	)

	return nil
}
