package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/routing"
)

var (
	// ErrRouteSelectFailed indicates logical model selection failed.
	ErrRouteSelectFailed = errors.New("route select failed")
)

// RouteSelectStage evaluates routing rules, request model overrides, and defaults to select the logical model.
// Placed before CacheStage so the selected logical model can be included in the cache key.
type RouteSelectStage struct {
	resolver *routing.Resolver
	tracer   trace.Tracer
}

// NewRouteSelectStage creates a new RouteSelectStage with the provided resolver.
func NewRouteSelectStage(resolver *routing.Resolver) (*RouteSelectStage, error) {
	if resolver == nil {
		return nil, errors.New("nil model resolver")
	}

	return &RouteSelectStage{
		resolver: resolver,
		tracer:   otel.Tracer("beavend/stages/route_select"),
	}, nil
}

// Resolver returns the routing.Resolver used by this stage.
func (s *RouteSelectStage) Resolver() *routing.Resolver {
	return s.resolver
}

// Handle executes the route selection stage, setting state.SelectedModel.
func (s *RouteSelectStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil request state")
	}

	if state.ChatResponse != nil {
		return nil
	}

	if state.SelectedModel != "" {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stages.route_select",
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
		return ErrMissingSnapshot
	}

	if state.AIApp == nil && state.Snapshot != nil && state.AIAppID != "" {
		if app, exists := state.Snapshot.App(state.AIAppID); exists {
			state.AIApp = app
		}
	}

	if state.AIApp == nil {
		state.Error = &core.ErrorResponse{
			Code:      "invalid_argument",
			Message:   "missing ai app definition in request state",
			RequestID: state.RequestID,
		}
		return ErrMissingAIApp
	}

	selectedModel, err := s.resolver.SelectLogicalModel(ctx, state.Snapshot, state.AIApp, state.ChatRequest, state.Identity, state.Metadata)
	if err != nil {
		span.RecordError(err)

		var code string
		if errors.Is(err, routing.ErrModelNotFound) {
			code = "not_found"
		} else {
			code = "provider_error"
		}

		state.Error = &core.ErrorResponse{
			Code:      code,
			Message:   fmt.Sprintf("select logical model: %v", err),
			RequestID: state.RequestID,
		}

		slog.WarnContext(ctx, "route select stage failed",
			"request_id", state.RequestID,
			"project_id", state.ProjectID,
			"env", state.Environment,
			"ai_app_id", state.AIAppID,
			"error", err,
		)
		return fmt.Errorf("%w: %v", ErrRouteSelectFailed, err)
	}

	state.SelectedModel = selectedModel
	span.SetAttributes(attribute.String("selected_model", selectedModel))

	slog.InfoContext(ctx, "route select resolved model",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"ai_app_id", state.AIAppID,
		"selected_model", selectedModel,
	)

	return nil
}
