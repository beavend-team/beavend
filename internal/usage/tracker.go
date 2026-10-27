package usage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/pricing"
)

// Tracker extracts operational metrics and usage records from core.ReqState
// and enqueues them into the asynchronous buffer while reconciling token limits.
type Tracker struct {
	buffer     *Buffer
	limitStore core.LimitStore
}

// NewTracker creates a new Tracker.
func NewTracker(buffer *Buffer, limitStore core.LimitStore) *Tracker {
	return &Tracker{
		buffer:     buffer,
		limitStore: limitStore,
	}
}

// RecordUsage extracts logs and usage from the finished ReqState and enqueues them.
// It also reconciles actual token consumption with pre-request estimates on the LimitStore.
func (t *Tracker) RecordUsage(ctx context.Context, state *core.ReqState) error {
	if state == nil {
		return nil
	}

	// 1. Asynchronous token reconciliation
	t.reconcileLimits(ctx, state)

	// 2. Build LogEvent and UsageEvent
	logEv, usageEv := t.extractEvents(state)

	// 3. Enqueue into bounded in-process buffer
	if t.buffer != nil {
		rec := Record{
			Log:   logEv,
			Usage: usageEv,
		}
		if !t.buffer.Enqueue(rec) {
			slog.WarnContext(ctx, "usage buffer full; event dropped under backpressure",
				"request_id", state.RequestID,
				"project_id", state.ProjectID,
				"ai_app_id", state.AIAppID,
			)
		}
	}

	return nil
}

func (t *Tracker) reconcileLimits(ctx context.Context, state *core.ReqState) {
	if t.limitStore == nil || state.Metadata == nil {
		return
	}

	rawKey, exists := state.Metadata["limit_user_key"]
	if !exists {
		return
	}
	userKey, ok := rawKey.(string)
	if !ok || userKey == "" {
		return
	}

	var estimatedTokens int64
	if rawEst, ok := state.Metadata["estimated_tokens"]; ok {
		if est, ok := rawEst.(int64); ok {
			estimatedTokens = est
		}
	}

	actualTokens := int64(state.Usage.TotalTokens)
	if actualTokens == 0 && state.Usage.InputTokens+state.Usage.OutputTokens > 0 {
		actualTokens = int64(state.Usage.InputTokens + state.Usage.OutputTokens)
	}

	if err := t.limitStore.ReconcileTokens(ctx, userKey, estimatedTokens, actualTokens); err != nil {
		slog.WarnContext(ctx, "failed to reconcile tokens with limit store",
			"request_id", state.RequestID,
			"user_key", userKey,
			"estimated", estimatedTokens,
			"actual", actualTokens,
			"error", err,
		)
	}
}

func (t *Tracker) extractEvents(state *core.ReqState) (*LogEvent, *Event) {
	now := time.Now()
	createdAt := state.StartTime
	if createdAt.IsZero() {
		createdAt = now
	}
	latencyMS := now.Sub(createdAt).Milliseconds()
	if latencyMS < 0 {
		latencyMS = 0
	}

	var (
		userID      string
		userSubject string
		userIssuer  string
	)
	if state.Identity != nil {
		userSubject = state.Identity.Subject
		userIssuer = state.Identity.Issuer
		userID = state.Identity.Subject
		if state.Identity.Issuer != "" {
			userID = fmt.Sprintf("%s:%s", state.Identity.Issuer, state.Identity.Subject)
		}
	}

	var (
		logicalModel    string
		provider        string
		modelDeployment string
	)
	if state.ResolvedModel != nil {
		logicalModel = state.ResolvedModel.LogicalModel
		provider = state.ResolvedModel.Adapter
		modelDeployment = state.ResolvedModel.Deployment
	} else if state.ChatRequest != nil && state.ChatRequest.Model != "" {
		logicalModel = state.ChatRequest.Model
	}

	var (
		status       = "success"
		statusCode   = 200
		errorCode    string
		errorMessage string
	)

	if state.Error != nil {
		errorCode = state.Error.Code
		errorMessage = state.Error.Message
		status = "error"
		switch errorCode {
		case "rate_limited":
			status = "rate_limited"
			statusCode = 429
		case "unauthenticated":
			statusCode = 401
		case "forbidden":
			statusCode = 403
		case "not_found":
			statusCode = 404
		case "invalid_argument", "invalid_request":
			statusCode = 400
		case "provider_error":
			statusCode = 502
		case "provider_timeout":
			statusCode = 504
		default:
			statusCode = 500
		}
	}

	// Calculate tokens
	inputTokens := state.Usage.InputTokens
	outputTokens := state.Usage.OutputTokens
	totalTokens := state.Usage.TotalTokens
	if totalTokens == 0 && (inputTokens > 0 || outputTokens > 0) {
		totalTokens = inputTokens + outputTokens
	}

	var costUSD float64
	if state.Usage.CostUSD != nil && *state.Usage.CostUSD > 0 {
		costUSD = *state.Usage.CostUSD
	} else if hit, _ := state.Metadata["cache_hit"].(bool); hit {
		costUSD = 0.0
	} else if inputTokens > 0 || outputTokens > 0 {
		// Calculate accurate cost based on model used
		var override *core.ModelPrice
		if state.ResolvedModel != nil && (state.ResolvedModel.InputCostPer1M != nil || state.ResolvedModel.OutputCostPer1M != nil || state.ResolvedModel.CachedInputCostPer1M != nil) {
			override = &core.ModelPrice{}
			if state.ResolvedModel.InputCostPer1M != nil {
				override.InputCostPer1M = *state.ResolvedModel.InputCostPer1M
			}
			if state.ResolvedModel.OutputCostPer1M != nil {
				override.OutputCostPer1M = *state.ResolvedModel.OutputCostPer1M
			}
			if state.ResolvedModel.CachedInputCostPer1M != nil {
				override.CachedInputCostPer1M = state.ResolvedModel.CachedInputCostPer1M
			}
		}

		targetModel := modelDeployment
		if targetModel == "" && state.ChatRequest != nil {
			targetModel = state.ChatRequest.Model
		}
		if targetModel == "" {
			targetModel = logicalModel
		}

		calculatedCost, costSource := pricing.CalculateCostWithSource(inputTokens, outputTokens, state.Usage.CachedInputTokens, targetModel, override)
		costUSD = calculatedCost
		if state.Usage.CostUSD == nil {
			state.Usage.CostUSD = &costUSD
		}
		if state.Usage.CostSource == "" {
			state.Usage.CostSource = costSource
		}
	}

	// Extract tool call summaries from chat request messages and response
	toolCalls := make([]ToolCallSummary, 0)
	seenToolIDs := make(map[string]struct{})
	addToolCall := func(id, name, status string) {
		if id == "" {
			return
		}
		if _, exists := seenToolIDs[id]; exists {
			return
		}
		seenToolIDs[id] = struct{}{}
		toolCalls = append(toolCalls, ToolCallSummary{
			ID:     id,
			Name:   name,
			Status: status,
		})
	}

	if state.ChatRequest != nil {
		for _, msg := range state.ChatRequest.Messages {
			for _, tc := range msg.ToolCalls {
				addToolCall(tc.ID, tc.Name, "success")
			}
		}
	}
	if state.ChatResponse != nil && len(state.ChatResponse.Message.ToolCalls) > 0 {
		for _, tc := range state.ChatResponse.Message.ToolCalls {
			addToolCall(tc.ID, tc.Name, "success")
		}
	}

	// Clone metadata so we don't hold references
	meta := make(map[string]any)
	for k, v := range state.Metadata {
		meta[k] = v
	}

	logID := generateRandomID("log_")
	logEv := &LogEvent{
		ID:              logID,
		RequestID:       truncateString(state.RequestID, 64),
		ProjectID:       truncateString(state.ProjectID, 64),
		Environment:     truncateString(state.Environment, 64),
		AIAppID:         truncateString(state.AIAppID, 64),
		UserID:          truncateString(userID, 255),
		UserSubject:     truncateString(userSubject, 255),
		UserIssuer:      truncateString(userIssuer, 255),
		LogicalModel:    truncateString(logicalModel, 128),
		Provider:        truncateString(provider, 64),
		ModelDeployment: truncateString(modelDeployment, 255),
		Status:          truncateString(status, 32),
		StatusCode:      statusCode,
		LatencyMS:       latencyMS,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		TotalTokens:     totalTokens,
		Images:          state.Usage.Images,
		AudioSeconds:    state.Usage.AudioSeconds,
		VideoSeconds:    state.Usage.VideoSeconds,
		CostUSD:         costUSD,
		IsStreaming:     state.IsStreaming,
		ToolCalls:       toolCalls,
		ErrorCode:       truncateString(errorCode, 64),
		ErrorMessage:    errorMessage,
		Metadata:        meta,
		CreatedAt:       createdAt,
	}

	usageID := generateRandomID("usg_")
	usageEv := &Event{
		ID:           usageID,
		RequestID:    truncateString(state.RequestID, 64),
		ProjectID:    truncateString(state.ProjectID, 64),
		Environment:  truncateString(state.Environment, 64),
		AIAppID:      truncateString(state.AIAppID, 64),
		UserID:       truncateString(userID, 255),
		LogicalModel: truncateString(logicalModel, 128),
		Provider:     truncateString(provider, 64),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
		Images:       state.Usage.Images,
		AudioSeconds: state.Usage.AudioSeconds,
		VideoSeconds: state.Usage.VideoSeconds,
		CostUSD:      costUSD,
		CreatedAt:    createdAt,
	}

	return logEv, usageEv
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func generateRandomID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}
