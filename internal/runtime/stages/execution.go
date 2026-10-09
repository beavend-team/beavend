package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/pricing"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime/agent"
)

var (
	// ErrMissingResolvedModel indicates model routing stage did not resolve a model.
	ErrMissingResolvedModel = errors.New("missing resolved model in request state")
	// ErrNilChatRequest indicates ChatRequest was not provided or initialized.
	ErrNilChatRequest = errors.New("missing chat request in request state")

	execMeter             = otel.Meter("beavend/stages/execution")
	invocationsCounter, _ = execMeter.Int64Counter("beavend_provider_invocations_total", metric.WithDescription("Total count of model provider invocations"))
	invocationErrors, _   = execMeter.Int64Counter("beavend_provider_errors_total", metric.WithDescription("Total count of model provider errors"))
)

// ExecutionStage invokes the resolved provider adapter to execute unary or streaming chat completions.
type ExecutionStage struct {
	resolver *routing.Resolver
	loop     *agent.Loop
	tracer   trace.Tracer
}

// NewExecutionStage creates a new ExecutionStage using the provided model resolver.
func NewExecutionStage(resolver *routing.Resolver, loop ...*agent.Loop) (*ExecutionStage, error) {
	if resolver == nil {
		return nil, errors.New("nil model resolver")
	}

	var l *agent.Loop
	if len(loop) > 0 && loop[0] != nil {
		l = loop[0]
	} else {
		l = agent.NewLoop(agent.LoopConfig{})
	}

	return &ExecutionStage{
		resolver: resolver,
		loop:     l,
		tracer:   otel.Tracer("beavend/stages/execution"),
	}, nil
}

// Loop returns the agent loop used by this execution stage.
func (s *ExecutionStage) Loop() *agent.Loop {
	return s.loop
}

// Handle executes the provider invocation for unary or streaming requests.
func (s *ExecutionStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	if state.ChatResponse != nil {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stage.execution",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
			attribute.Bool("is_streaming", state.IsStreaming),
		),
	)
	defer span.End()

	if state.ResolvedModel == nil {
		state.Error = &core.ErrorResponse{
			Code:      "internal_error",
			Message:   "model deployment not resolved before execution",
			RequestID: state.RequestID,
		}
		return ErrMissingResolvedModel
	}

	if state.ChatRequest == nil {
		state.Error = &core.ErrorResponse{
			Code:      "invalid_argument",
			Message:   "missing chat request payload",
			RequestID: state.RequestID,
		}
		return ErrNilChatRequest
	}

	adapterName := state.ResolvedModel.Adapter
	if adapterName == "" {
		adapterName = "litellm"
	}

	var adapter core.ProviderAdapter
	if state.ResolvedAdapter != nil {
		adapter = state.ResolvedAdapter
	} else {
		var ok bool
		adapter, ok = s.resolver.GetAdapterWithSnapshot(adapterName, state.Snapshot)
		if !ok {
			state.Error = &core.ErrorResponse{
				Code:      "unavailable",
				Message:   fmt.Sprintf("provider adapter %q not available", adapterName),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("%w: %s", routing.ErrAdapterNotFound, adapterName)
		}
	}

	span.SetAttributes(
		attribute.String("adapter", adapterName),
		attribute.String("logical_model", state.ResolvedModel.LogicalModel),
		attribute.String("deployment", state.ChatRequest.Model),
	)

	if invocationsCounter != nil {
		invocationsCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("adapter", adapterName),
			attribute.String("logical_model", state.ResolvedModel.LogicalModel),
			attribute.Bool("streaming", state.IsStreaming),
		))
	}

	if state.IsStreaming {
		return s.handleStreaming(ctx, adapter, state, span)
	}

	return s.handleUnary(ctx, adapter, state, span)
}

func (s *ExecutionStage) handleUnary(ctx context.Context, adapter core.ProviderAdapter, state *core.ReqState, span trace.Span) error {
	var initialMessages []core.ChatMessage
	if state.ChatRequest != nil {
		initialMessages = make([]core.ChatMessage, len(state.ChatRequest.Messages))
		copy(initialMessages, state.ChatRequest.Messages)
	}

	adapterName := state.ResolvedModel.Adapter
	if adapterName == "" {
		adapterName = "litellm"
	}
	currentRoute := &routing.ResolvedRoute{
		ModelRouting: state.ResolvedModel,
		Adapter:      adapter,
		Deployment:   state.ChatRequest.Model,
		AdapterName:  adapterName,
		Snapshot:     state.Snapshot,
	}

	attempted := map[string]bool{
		currentRoute.Deployment: true,
	}
	if currentRoute.AdapterName != "" {
		attempted[currentRoute.AdapterName+"/"+currentRoute.Deployment] = true
	}

	for {
		err := s.loop.Run(ctx, state, adapter)
		if err == nil {
			break
		}

		if ctx.Err() != nil {
			return err
		}

		// Attempt fallback resolution across multiple providers
		fbRoute, fbErr := s.resolver.ResolveFallback(currentRoute, attempted, state.Snapshot)
		if fbErr != nil {
			span.RecordError(err)
			if invocationErrors != nil {
				invocationErrors.Add(ctx, 1)
			}

			code := "provider_error"
			if errors.Is(err, context.DeadlineExceeded) {
				code = "provider_timeout"
			}

			var appErr *core.ErrorResponse
			if errors.As(err, &appErr) {
				state.Error = appErr
			} else {
				state.Error = &core.ErrorResponse{
					Code:      code,
					Message:   fmt.Sprintf("upstream model error: %v", err),
					RequestID: state.RequestID,
				}
			}

			slog.ErrorContext(ctx, "provider chat invocation failed (all fallbacks exhausted)",
				"request_id", state.RequestID,
				"project_id", state.ProjectID,
				"env", state.Environment,
				"ai_app_id", state.AIAppID,
				"adapter", currentRoute.AdapterName,
				"deployment", currentRoute.Deployment,
				"error", err,
			)
			return err
		}

		slog.WarnContext(ctx, "model invocation failed, failing over to fallback deployment",
			"request_id", state.RequestID,
			"failed_adapter", currentRoute.AdapterName,
			"failed_deployment", currentRoute.Deployment,
			"fallback_adapter", fbRoute.AdapterName,
			"fallback_deployment", fbRoute.Deployment,
			"error", err,
		)

		attempted[fbRoute.Deployment] = true
		if fbRoute.AdapterName != "" {
			attempted[fbRoute.AdapterName+"/"+fbRoute.Deployment] = true
		}

		// Reset messages to pre-failure state
		if state.ChatRequest != nil && initialMessages != nil {
			state.ChatRequest.Messages = make([]core.ChatMessage, len(initialMessages))
			copy(state.ChatRequest.Messages, initialMessages)
		}

		adapter = fbRoute.Adapter
		state.ResolvedAdapter = fbRoute.Adapter
		if state.ChatRequest != nil {
			state.ChatRequest.Model = fbRoute.Deployment
		}
		currentRoute = fbRoute
	}

	resp := state.ChatResponse
	if resp != nil {
		if resp.ID == "" {
			resp.ID = fmt.Sprintf("chat_%d", time.Now().UnixNano())
		}
		resp.RequestID = state.RequestID
		resp.AIAppID = state.AIAppID
		if resp.CreatedAt == 0 {
			resp.CreatedAt = time.Now().Unix()
		}

		state.Usage = resp.Usage
		if (state.Usage.CostUSD == nil || *state.Usage.CostUSD == 0) && (state.Usage.InputTokens > 0 || state.Usage.OutputTokens > 0) {
			var override *core.ModelPrice
			if state.ResolvedModel != nil && (state.ResolvedModel.InputCostPer1M != nil || state.ResolvedModel.OutputCostPer1M != nil) {
				override = &core.ModelPrice{}
				if state.ResolvedModel.InputCostPer1M != nil {
					override.InputCostPer1M = *state.ResolvedModel.InputCostPer1M
				}
				if state.ResolvedModel.OutputCostPer1M != nil {
					override.OutputCostPer1M = *state.ResolvedModel.OutputCostPer1M
				}
			}
			targetModel := currentRoute.Deployment
			if targetModel == "" && state.ChatRequest != nil {
				targetModel = state.ChatRequest.Model
			}
			calculated := pricing.CalculateCost(state.Usage.InputTokens, state.Usage.OutputTokens, targetModel, override)
			state.Usage.CostUSD = &calculated
			resp.Usage.CostUSD = &calculated
		}

		span.SetAttributes(
			attribute.Int("usage.input_tokens", resp.Usage.InputTokens),
			attribute.Int("usage.output_tokens", resp.Usage.OutputTokens),
			attribute.Int("usage.total_tokens", resp.Usage.TotalTokens),
			attribute.String("finish_reason", resp.FinishReason),
		)
	}

	slog.InfoContext(ctx, "provider chat completed successfully",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"ai_app_id", state.AIAppID,
	)

	return nil
}

func (s *ExecutionStage) handleStreaming(ctx context.Context, adapter core.ProviderAdapter, state *core.ReqState, span trace.Span) error {
	if state.StreamChan == nil {
		state.StreamChan = make(chan core.StreamEvent, 32)
	}

	// Forward stream events through the agent loop in background goroutine to state.StreamChan
	go func() {
		defer close(state.StreamChan)

		var initialMessages []core.ChatMessage
		if state.ChatRequest != nil {
			initialMessages = make([]core.ChatMessage, len(state.ChatRequest.Messages))
			copy(initialMessages, state.ChatRequest.Messages)
		}

		adapterName := state.ResolvedModel.Adapter
		if adapterName == "" {
			adapterName = "litellm"
		}
		currentRoute := &routing.ResolvedRoute{
			ModelRouting: state.ResolvedModel,
			Adapter:      adapter,
			Deployment:   state.ChatRequest.Model,
			AdapterName:  adapterName,
			Snapshot:     state.Snapshot,
		}

		attempted := map[string]bool{
			currentRoute.Deployment: true,
		}
		if currentRoute.AdapterName != "" {
			attempted[currentRoute.AdapterName+"/"+currentRoute.Deployment] = true
		}

		for {
			err := s.loop.Run(ctx, state, adapter)
			if err == nil {
				return
			}

			if ctx.Err() != nil {
				return
			}

			fbRoute, fbErr := s.resolver.ResolveFallback(currentRoute, attempted, state.Snapshot)
			if fbErr != nil {
				span.RecordError(err)
				if invocationErrors != nil {
					invocationErrors.Add(ctx, 1)
				}

				code := "provider_error"
				if errors.Is(err, context.DeadlineExceeded) {
					code = "provider_timeout"
				}

				var appErr *core.ErrorResponse
				if errors.As(err, &appErr) {
					state.Error = appErr
				} else {
					state.Error = &core.ErrorResponse{
						Code:      code,
						Message:   fmt.Sprintf("upstream stream error: %v", err),
						RequestID: state.RequestID,
					}
				}

				select {
				case <-ctx.Done():
				case state.StreamChan <- core.StreamEvent{
					Type:      "error",
					Error:     state.Error,
					RequestID: state.RequestID,
				}:
				}
				return
			}

			slog.WarnContext(ctx, "stream invocation failed, failing over to fallback deployment",
				"request_id", state.RequestID,
				"failed_adapter", currentRoute.AdapterName,
				"failed_deployment", currentRoute.Deployment,
				"fallback_adapter", fbRoute.AdapterName,
				"fallback_deployment", fbRoute.Deployment,
				"error", err,
			)

			attempted[fbRoute.Deployment] = true
			if fbRoute.AdapterName != "" {
				attempted[fbRoute.AdapterName+"/"+fbRoute.Deployment] = true
			}

			if state.ChatRequest != nil && initialMessages != nil {
				state.ChatRequest.Messages = make([]core.ChatMessage, len(initialMessages))
				copy(state.ChatRequest.Messages, initialMessages)
			}

			adapter = fbRoute.Adapter
			state.ResolvedAdapter = fbRoute.Adapter
			if state.ChatRequest != nil {
				state.ChatRequest.Model = fbRoute.Deployment
			}
			currentRoute = fbRoute
		}
	}()

	return nil
}
