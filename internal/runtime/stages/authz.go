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
	"github.com/beavend/beavend/internal/policy"
)

var (
	// ErrForbidden indicates that the caller is not authorized by policy.
	ErrForbidden = errors.New("forbidden: access denied by policy")
	// ErrMissingSnapshot indicates the request state is missing an active configuration snapshot.
	ErrMissingSnapshot = errors.New("missing snapshot in request state")
	// ErrMissingAIApp indicates the request state is missing an active AIApp definition.
	ErrMissingAIApp = errors.New("missing ai app in request state")

	meter                 = otel.Meter("beavend/stages/authz")
	evaluationsCounter, _ = meter.Int64Counter("beavend_policy_evaluations_total", metric.WithDescription("Total count of policy evaluations"))
	denialsCounter, _     = meter.Int64Counter("beavend_policy_denials_total", metric.WithDescription("Total count of policy denials"))
)

// AuthZStage evaluates authorization policies for AI Apps in the runtime pipeline.
type AuthZStage struct {
	evaluator *policy.Evaluator
	tracer    trace.Tracer
}

// NewAuthZStage creates a new AuthZStage with the provided policy evaluator.
func NewAuthZStage(evaluator *policy.Evaluator) (*AuthZStage, error) {
	if evaluator == nil {
		var err error
		evaluator, err = policy.NewEvaluator(nil)
		if err != nil {
			return nil, fmt.Errorf("create default policy evaluator: %w", err)
		}
	}

	return &AuthZStage{
		evaluator: evaluator,
		tracer:    otel.Tracer("beavend/stages/authz"),
	}, nil
}

// Evaluator returns the policy.Evaluator used by this stage.
func (s *AuthZStage) Evaluator() *policy.Evaluator {
	return s.evaluator
}

// Handle executes policy evaluation for the AI App in the request pipeline.
func (s *AuthZStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ctx, span := s.tracer.Start(ctx, "stage.authz")
	defer span.End()

	if state == nil {
		return errors.New("nil req state")
	}

	if state.Snapshot == nil {
		return ErrMissingSnapshot
	}

	// Resolve AIApp if not already set
	if state.AIApp == nil {
		if state.AIAppID == "" {
			return ErrMissingAIApp
		}
		app, exists := state.Snapshot.App(state.AIAppID)
		if !exists {
			return fmt.Errorf("%w: %s", ErrMissingAIApp, state.AIAppID)
		}
		state.AIApp = app
	} else if state.AIAppID == "" {
		state.AIAppID = state.AIApp.ID
	}

	policyID := state.AIApp.AccessPolicy
	span.SetAttributes(
		attribute.String("app.id", state.AIAppID),
		attribute.String("policy.id", policyID),
	)

	// If no policy is defined, allow access by default
	if policyID == "" {
		span.SetAttributes(attribute.Bool("policy.allowed", true))
		return nil
	}

	if evaluationsCounter != nil {
		evaluationsCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("app_id", state.AIAppID),
			attribute.String("policy_id", policyID),
		))
	}

	allowed, err := s.evaluator.EvaluatePolicy(ctx, state.Snapshot, policyID, state.Identity, state.Metadata)
	if err != nil {
		span.SetAttributes(
			attribute.Bool("policy.allowed", false),
			attribute.String("policy.error", err.Error()),
		)
		slog.WarnContext(ctx, "policy evaluation error",
			slog.String("request_id", state.RequestID),
			slog.String("app_id", state.AIAppID),
			slog.String("policy_id", policyID),
			slog.String("error", err.Error()),
		)

		if denialsCounter != nil {
			denialsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("app_id", state.AIAppID),
				attribute.String("policy_id", policyID),
				attribute.String("reason", "error"),
			))
		}

		state.Error = &core.ErrorResponse{
			Code:      "forbidden",
			Message:   "access denied by policy",
			RequestID: state.RequestID,
		}
		return fmt.Errorf("%w: %v", ErrForbidden, err)
	}

	if !allowed {
		span.SetAttributes(attribute.Bool("policy.allowed", false))
		slog.InfoContext(ctx, "access denied by policy",
			slog.String("request_id", state.RequestID),
			slog.String("app_id", state.AIAppID),
			slog.String("policy_id", policyID),
		)

		if denialsCounter != nil {
			denialsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("app_id", state.AIAppID),
				attribute.String("policy_id", policyID),
				attribute.String("reason", "denied"),
			))
		}

		state.Error = &core.ErrorResponse{
			Code:      "forbidden",
			Message:   "access denied by policy",
			RequestID: state.RequestID,
		}
		return ErrForbidden
	}

	span.SetAttributes(attribute.Bool("policy.allowed", true))
	return nil
}

// AuthorizeTool checks if caller is authorized to execute a specific tool (per PRD §18.1).
func (s *AuthZStage) AuthorizeTool(ctx context.Context, snap *core.Snapshot, tool *core.ToolDefinition, id *core.Identity, meta map[string]any) (bool, error) {
	return AuthorizeTool(ctx, s.evaluator, snap, tool, id, meta)
}

// AuthorizeTool evaluates tool-level authorization policies with dedicated telemetry.
func AuthorizeTool(ctx context.Context, evaluator *policy.Evaluator, snap *core.Snapshot, tool *core.ToolDefinition, id *core.Identity, meta map[string]any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if tool == nil {
		return false, errors.New("nil tool definition")
	}

	if tool.Authorization == "" {
		return true, nil
	}

	if snap == nil {
		return false, ErrMissingSnapshot
	}

	ctx, span := otel.Tracer("beavend/stages/authz").Start(ctx, "stage.authz.tool")
	defer span.End()

	span.SetAttributes(
		attribute.String("tool.id", tool.ID),
		attribute.String("policy.id", tool.Authorization),
	)

	if evaluator == nil {
		var err error
		evaluator, err = policy.NewEvaluator(nil)
		if err != nil {
			span.RecordError(err)
			return false, fmt.Errorf("create evaluator: %w", err)
		}
	}

	allowed, err := evaluator.EvaluatePolicy(ctx, snap, tool.Authorization, id, meta)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("policy.allowed", false))
		return false, err
	}

	span.SetAttributes(attribute.Bool("policy.allowed", allowed))
	return allowed, nil
}
