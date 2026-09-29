package stages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/core"
)

var (
	cacheMeter              = otel.Meter("beavend/stages/cache")
	cacheRequestsCounter, _ = cacheMeter.Int64Counter("beavend_cache_requests_total", metric.WithDescription("Total count of cache lookups partitioned by type and hit status"))
)

// CacheStage performs response cache lookups before expensive model routing and agent loop invocation.
type CacheStage struct {
	store  core.CacheStore
	tracer trace.Tracer
}

// NewCacheStage creates a new CacheStage.
func NewCacheStage(store core.CacheStore) *CacheStage {
	return &CacheStage{
		store:  store,
		tracer: otel.Tracer("beavend/stages/cache"),
	}
}

// Store returns the configured cache store.
func (s *CacheStage) Store() core.CacheStore {
	return s.store
}

// Handle checks if a cached response exists for the current request.
// If found, it populates state.ChatResponse (and replays stream events if streaming) and marks the request as a cache hit.
func (s *CacheStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	// If response already produced by an earlier stage, skip
	if state.ChatResponse != nil {
		return nil
	}

	if s.store == nil {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stage.cache",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
		),
	)
	defer span.End()

	// Ensure AIApp is resolved
	if state.AIApp == nil && state.Snapshot != nil && state.AIAppID != "" {
		if app, exists := state.Snapshot.App(state.AIAppID); exists {
			state.AIApp = app
		}
	}

	if state.AIApp == nil || !state.AIApp.Cache.Enabled {
		span.SetAttributes(attribute.Bool("cache.enabled", false))
		if state.Metadata != nil {
			state.Metadata["cache_hit"] = false
		}
		return nil
	}

	span.SetAttributes(
		attribute.Bool("cache.enabled", true),
		attribute.String("cache.scope", state.AIApp.Cache.Scope),
		attribute.Int("cache.ttl", state.AIApp.Cache.TTL),
	)

	// Compute hash of request
	hash, err := cache.ComputeRequestHash(state.ChatRequest, state.AIApp)
	if err != nil {
		slog.WarnContext(ctx, "failed to compute request cache hash",
			"request_id", state.RequestID,
			"error", err,
		)
		return nil // Fail open
	}

	subject := ""
	if state.Identity != nil {
		subject = state.Identity.Subject
	}

	key := cache.ResponseCacheKey(state.ProjectID, state.Environment, state.AIApp.Cache.Scope, subject, hash)
	span.SetAttributes(attribute.String("cache.key", key))

	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}
	state.Metadata["cache_key"] = key

	// Lookup in cache store
	cachedBytes, err := s.store.Get(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "cache store lookup error; failing open",
			"request_id", state.RequestID,
			"cache_key", key,
			"error", err,
		)
		state.Metadata["cache_hit"] = false
		return nil // Fail open
	}

	if cachedBytes == nil {
		// Cache miss
		span.SetAttributes(attribute.Bool("cache.hit", false))
		state.Metadata["cache_hit"] = false

		if cacheRequestsCounter != nil {
			cacheRequestsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("cache_type", "response"),
				attribute.String("status", "miss"),
				attribute.String("ai_app_id", state.AIAppID),
			))
		}
		return nil
	}

	// Cache hit!
	var cachedResp core.ChatResponse
	if err := json.Unmarshal(cachedBytes, &cachedResp); err != nil {
		slog.WarnContext(ctx, "failed to unmarshal cached chat response",
			"request_id", state.RequestID,
			"cache_key", key,
			"error", err,
		)
		state.Metadata["cache_hit"] = false
		return nil
	}

	span.SetAttributes(attribute.Bool("cache.hit", true))
	state.Metadata["cache_hit"] = true

	if cacheRequestsCounter != nil {
		cacheRequestsCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("cache_type", "response"),
			attribute.String("status", "hit"),
			attribute.String("ai_app_id", state.AIAppID),
		))
	}

	// Adapt cached response to current request
	cachedResp.ID = fmt.Sprintf("chatcmpl_cached_%s", state.RequestID)
	cachedResp.RequestID = state.RequestID
	cachedResp.AIAppID = state.AIAppID
	cachedResp.CreatedAt = time.Now().UTC().Unix()

	state.ChatResponse = &cachedResp
	state.Usage = cachedResp.Usage
	zeroCost := 0.0
	state.Usage.CostUSD = &zeroCost

	slog.InfoContext(ctx, "response cache hit",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"ai_app_id", state.AIAppID,
		"cache_key", key,
		"is_streaming", state.IsStreaming,
	)

	// If streaming, replay cached response through state.StreamChan
	if state.IsStreaming {
		if state.StreamChan == nil {
			state.StreamChan = make(chan core.StreamEvent, 32)
		}

		go func() {
			defer close(state.StreamChan)

			// Emit text content delta
			if cachedResp.Message.Content != "" {
				select {
				case <-ctx.Done():
					return
				case state.StreamChan <- core.StreamEvent{
					Type:      "delta",
					Delta:     cachedResp.Message.Content,
					RequestID: state.RequestID,
				}:
				}
			}

			// Emit tool calls if any
			for i := range cachedResp.Message.ToolCalls {
				tc := cachedResp.Message.ToolCalls[i]
				select {
				case <-ctx.Done():
					return
				case state.StreamChan <- core.StreamEvent{
					Type:      "tool_call",
					ToolCall:  &tc,
					RequestID: state.RequestID,
				}:
				}
			}

			// Emit usage
			select {
			case <-ctx.Done():
				return
			case state.StreamChan <- core.StreamEvent{
				Type:      "usage",
				Usage:     &state.Usage,
				RequestID: state.RequestID,
			}:
			}

			// Emit done
			select {
			case <-ctx.Done():
				return
			case state.StreamChan <- core.StreamEvent{
				Type:      "done",
				RequestID: state.RequestID,
			}:
			}
		}()
	}

	return nil
}
