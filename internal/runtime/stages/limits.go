package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/limits"
)

var (
	// ErrRateLimited indicates that caller exceeded configured inference or token quotas.
	ErrRateLimited = errors.New("rate_limited: limit exceeded")

	limitsMeter               = otel.Meter("beavend/stages/limits")
	limitChecksCounter, _     = limitsMeter.Int64Counter("beavend_limit_checks_total", metric.WithDescription("Total count of rate limit evaluations"))
	limitRejectionsCounter, _ = limitsMeter.Int64Counter("beavend_limit_rejections_total", metric.WithDescription("Total count of rate limit rejections"))
)

// LimitsStage evaluates user inference and token rate limits in the runtime pipeline.
type LimitsStage struct {
	store        core.LimitStore
	tierResolver *limits.TierResolver
	failClosed   bool
	tracer       trace.Tracer
}

// LimitsStageOptions provides optional configuration for LimitsStage.
type LimitsStageOptions struct {
	FailClosed bool
}

// NewLimitsStage creates a new LimitsStage.
func NewLimitsStage(store core.LimitStore, tierResolver *limits.TierResolver, opts *LimitsStageOptions) (*LimitsStage, error) {
	if store == nil {
		return nil, errors.New("nil limit store")
	}

	if tierResolver == nil {
		var err error
		tierResolver, err = limits.NewTierResolver(nil, nil)
		if err != nil {
			return nil, fmt.Errorf("create default tier resolver: %w", err)
		}
	}

	failClosed := true
	if opts != nil {
		failClosed = opts.FailClosed
	}

	return &LimitsStage{
		store:        store,
		tierResolver: tierResolver,
		failClosed:   failClosed,
		tracer:       otel.Tracer("beavend/stages/limits"),
	}, nil
}

// Handle executes rate limit checks in the request pipeline.
func (s *LimitsStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ctx, span := s.tracer.Start(ctx, "stage.limits")
	defer span.End()

	if state == nil {
		return errors.New("nil req state")
	}

	if state.Snapshot == nil {
		return ErrMissingSnapshot
	}

	// Resolve AIApp if not already populated
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

	span.SetAttributes(
		attribute.String("app.id", state.AIAppID),
		attribute.String("project.id", state.ProjectID),
		attribute.String("environment", state.Environment),
	)

	// If no limits defined for this AI App, allow request
	if len(state.AIApp.Limits) == 0 {
		span.SetAttributes(attribute.Bool("limits.allowed", true))
		return nil
	}

	// Resolve active rules via TierResolver
	activeRules, err := s.tierResolver.ResolveRules(ctx, state.AIApp.Limits, state.Identity, state.Metadata)
	if err != nil {
		span.RecordError(err)
		if s.failClosed {
			state.Error = &core.ErrorResponse{
				Code:      "unavailable",
				Message:   "limit evaluation failed",
				RequestID: state.RequestID,
			}
			return fmt.Errorf("resolve limit tiers: %w", err)
		}
		slog.WarnContext(ctx, "resolve limit tiers error, failing open", "error", err)
		return nil
	}

	if len(activeRules) == 0 {
		span.SetAttributes(attribute.Bool("limits.allowed", true))
		return nil
	}

	// Estimate token consumption for token limits
	hasTokenRule := false
	for _, r := range activeRules {
		if r.Resource == "tokens" {
			hasTokenRule = true
			break
		}
	}

	var estimatedTokens int64
	if hasTokenRule {
		estimatedTokens = limits.EstimateRequestTokens(state.ChatRequest)
		if state.Metadata == nil {
			state.Metadata = make(map[string]any)
		}
		state.Metadata["estimated_tokens"] = estimatedTokens
	}

	userKey := limits.UserKey(state.ProjectID, state.Environment, state.Identity)
	if state.Metadata != nil {
		state.Metadata["limit_user_key"] = userKey
	}

	allowed, retryAfter, err := s.store.CheckAndDebit(ctx, userKey, activeRules, estimatedTokens)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("limits.allowed", false))
		if s.failClosed {
			state.Error = &core.ErrorResponse{
				Code:      "unavailable",
				Message:   "rate limit store unavailable",
				RequestID: state.RequestID,
			}
			return fmt.Errorf("limit store check: %w", err)
		}
		slog.WarnContext(ctx, "limit store check error, failing open", "error", err)
		return nil
	}

	if limitChecksCounter != nil {
		limitChecksCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("app_id", state.AIAppID),
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
		))
	}

	if !allowed {
		span.SetAttributes(
			attribute.Bool("limits.allowed", false),
			attribute.Float64("limits.retry_after_seconds", retryAfter.Seconds()),
		)

		if limitRejectionsCounter != nil {
			limitRejectionsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("app_id", state.AIAppID),
				attribute.String("project_id", state.ProjectID),
				attribute.String("environment", state.Environment),
			))
		}

		retrySec := int(math.Ceil(retryAfter.Seconds()))
		if retrySec <= 0 {
			retrySec = 1
		}

		state.Error = &core.ErrorResponse{
			Code:       "rate_limited",
			Message:    "rate limit exceeded",
			RequestID:  state.RequestID,
			RetryAfter: retrySec,
		}

		slog.InfoContext(ctx, "rate limit exceeded",
			slog.String("request_id", state.RequestID),
			slog.String("app_id", state.AIAppID),
			slog.String("user_key", userKey),
			slog.Int("retry_after", retrySec),
		)

		return ErrRateLimited
	}

	span.SetAttributes(attribute.Bool("limits.allowed", true))
	return nil
}
