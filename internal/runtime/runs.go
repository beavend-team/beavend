package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/runtime/stream"
)

var (
	// ErrQueueFull indicates the concurrent async run capacity is exhausted.
	ErrQueueFull = errors.New("queue full")
	// ErrWorkerTerminated indicates the worker lease expired before completion.
	ErrWorkerTerminated = errors.New("worker terminated")
)

const (
	// DefaultMaxConcurrentAsyncRuns is the default maximum concurrent async runs permitted per replica.
	DefaultMaxConcurrentAsyncRuns = 100
	// DefaultHeartbeatLeaseDuration is the 15-second worker lease expiration.
	DefaultHeartbeatLeaseDuration = 15 * time.Second
	// DefaultHeartbeatRenewInterval is the 5-second interval at which worker leases are refreshed.
	DefaultHeartbeatRenewInterval = 5 * time.Second
)

// QuotaRefunder provides a hook to refund reserved tokens/quota when an async run worker terminates prematurely.
type QuotaRefunder interface {
	RefundQuota(ctx context.Context, projectID, env, reqID string, reservedTokens int) error
}

// AsyncRunManagerConfig provides initialization options for AsyncRunManager.
type AsyncRunManagerConfig struct {
	RunStore               core.RunStore
	EventBuffer            stream.TurnEventBuffer
	RedisClient            redis.UniversalClient
	Pipeline               *Pipeline
	AuthZStage             core.Stage
	LimitsStage            core.Stage
	UsageStore             core.UsageStore
	MaxConcurrentAsyncRuns int
	HeartbeatLeaseDuration time.Duration
	HeartbeatRenewInterval time.Duration
	Logger                 *slog.Logger
	QuotaRefunder          QuotaRefunder
}

// AsyncRunManager orchestrates asynchronous run scheduling, worker leases, crash recovery, and cancellation.
type AsyncRunManager struct {
	runStore      core.RunStore
	eventBuffer   stream.TurnEventBuffer
	redisClient   redis.UniversalClient
	pipeline      *Pipeline
	authZStage    core.Stage
	limitsStage   core.Stage
	usageStore    core.UsageStore
	maxRuns       int
	leaseDuration time.Duration
	renewInterval time.Duration
	logger        *slog.Logger
	refunder      QuotaRefunder

	activeRuns    atomic.Int64
	cancellations sync.Map // runID -> context.CancelFunc

	// In-memory heartbeat tracker when Redis is not configured
	memHeartbeats sync.Map // leaseKey -> time.Time
}

// NewAsyncRunManager creates a configured AsyncRunManager.
func NewAsyncRunManager(cfg AsyncRunManagerConfig) *AsyncRunManager {
	if cfg.RunStore == nil {
		cfg.RunStore = core.NewMemoryRunStore()
	}
	if cfg.EventBuffer == nil {
		cfg.EventBuffer = stream.NewMemoryTurnEventBuffer()
	}
	if cfg.MaxConcurrentAsyncRuns <= 0 {
		cfg.MaxConcurrentAsyncRuns = DefaultMaxConcurrentAsyncRuns
	}
	if cfg.HeartbeatLeaseDuration <= 0 {
		cfg.HeartbeatLeaseDuration = DefaultHeartbeatLeaseDuration
	}
	if cfg.HeartbeatRenewInterval <= 0 {
		cfg.HeartbeatRenewInterval = DefaultHeartbeatRenewInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &AsyncRunManager{
		runStore:      cfg.RunStore,
		eventBuffer:   cfg.EventBuffer,
		redisClient:   cfg.RedisClient,
		pipeline:      cfg.Pipeline,
		authZStage:    cfg.AuthZStage,
		limitsStage:   cfg.LimitsStage,
		usageStore:    cfg.UsageStore,
		maxRuns:       cfg.MaxConcurrentAsyncRuns,
		leaseDuration: cfg.HeartbeatLeaseDuration,
		renewInterval: cfg.HeartbeatRenewInterval,
		logger:        cfg.Logger,
		refunder:      cfg.QuotaRefunder,
	}
}

// RunStore returns the underlying RunStore.
func (m *AsyncRunManager) RunStore() core.RunStore {
	return m.runStore
}

// EventBuffer returns the underlying TurnEventBuffer.
func (m *AsyncRunManager) EventBuffer() stream.TurnEventBuffer {
	return m.eventBuffer
}

// AuthZStage returns the configured authorization stage.
func (m *AsyncRunManager) AuthZStage() core.Stage {
	return m.authZStage
}

// LimitsStage returns the configured limits stage.
func (m *AsyncRunManager) LimitsStage() core.Stage {
	return m.limitsStage
}

// IsSaturated returns true if active async runs reached or exceeded the concurrency limit.
func (m *AsyncRunManager) IsSaturated() bool {
	return m.activeRuns.Load() >= int64(m.maxRuns)
}

func leaseKey(projectID, env, runID string) string {
	return fmt.Sprintf("run:lease:%s:%s:%s", projectID, env, runID)
}

func cancelChannel(projectID, env, runID string) string {
	return fmt.Sprintf("cancel:%s:%s:%s", projectID, env, runID)
}

// Dispatch starts an asynchronous worker for the queued run.
func (m *AsyncRunManager) Dispatch(run *core.Run, state *core.ReqState) error {
	if m.IsSaturated() {
		return ErrQueueFull
	}

	m.activeRuns.Add(1)

	workerCtx, cancel := context.WithCancel(context.Background())
	m.cancellations.Store(run.ID, cancel)

	go m.runWorker(workerCtx, cancel, run, state)
	return nil
}

func (m *AsyncRunManager) runWorker(workerCtx context.Context, cancel context.CancelFunc, run *core.Run, state *core.ReqState) {
	defer func() {
		cancel()
		m.cancellations.Delete(run.ID)
		m.activeRuns.Add(-1)
	}()

	projectID := run.ProjectID
	env := run.Environment
	runID := run.ID
	lKey := leaseKey(projectID, env, runID)

	// Transition status to running
	if err := m.runStore.UpdateRunStatus(workerCtx, projectID, env, runID, core.RunStatusRunning, nil, "", nil); err != nil {
		m.logger.Error("failed to mark run running", "run_id", runID, "error", err)
	}

	// 1. Establish Heartbeat Lease
	stopLease := make(chan struct{})
	defer close(stopLease)

	if m.redisClient != nil {
		_ = m.redisClient.Set(workerCtx, lKey, "active", m.leaseDuration).Err()
	} else {
		m.memHeartbeats.Store(lKey, time.Now().UTC())
	}

	go func() {
		ticker := time.NewTicker(m.renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopLease:
				if m.redisClient != nil {
					_ = m.redisClient.Del(context.Background(), lKey).Err()
				} else {
					m.memHeartbeats.Delete(lKey)
				}
				return
			case <-workerCtx.Done():
				if m.redisClient != nil {
					_ = m.redisClient.Del(context.Background(), lKey).Err()
				} else {
					m.memHeartbeats.Delete(lKey)
				}
				return
			case <-ticker.C:
				if m.redisClient != nil {
					_ = m.redisClient.Expire(context.Background(), lKey, m.leaseDuration).Err()
				} else {
					m.memHeartbeats.Store(lKey, time.Now().UTC())
				}
			}
		}
	}()

	// 2. Cancellation Listener via Redis Pub/Sub
	if m.redisClient != nil {
		pubsub := m.redisClient.Subscribe(workerCtx, cancelChannel(projectID, env, runID))
		go func() {
			defer pubsub.Close()
			ch := pubsub.Channel()
			select {
			case <-workerCtx.Done():
			case msg, ok := <-ch:
				if ok && msg != nil {
					cancel()
				}
			}
		}()
	}

	// 3. Connect state.StreamChan to TurnEventBuffer
	state.IsStreaming = true
	state.StreamChan = make(chan core.StreamEvent, 64)

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		for ev := range state.StreamChan {
			if ev.RequestID == "" {
				ev.RequestID = state.RequestID
			}
			_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, ev)
		}
	}()

	// 4. Execute pipeline
	var pipelineErr error
	if m.pipeline != nil {
		pipelineErr = m.pipeline.Execute(workerCtx, state)
	}

	// Wait for any remaining streaming chunks to drain
	close(state.StreamChan)
	<-streamDone

	// 5. Finalize status and buffer
	now := time.Now().UTC()
	if workerCtx.Err() != nil {
		if state.Error == nil {
			state.Error = &core.ErrorResponse{
				Code:      "cancelled",
				Message:   "run was cancelled",
				RequestID: state.RequestID,
			}
		}
		_ = m.runStore.UpdateRunStatus(context.Background(), projectID, env, runID, core.RunStatusCancelled, nil, "run cancelled", &state.Usage)
		_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, core.StreamEvent{
			Type:      core.StreamEventError,
			Error:     state.Error,
			RequestID: state.RequestID,
		})
		_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, core.StreamEvent{
			Type:      core.StreamEventDone,
			RequestID: state.RequestID,
		})
	} else if pipelineErr != nil || state.Error != nil {
		errMsg := "execution failed"
		if state.Error != nil && state.Error.Message != "" {
			errMsg = state.Error.Message
		} else if pipelineErr != nil {
			errMsg = pipelineErr.Error()
			state.Error = &core.ErrorResponse{
				Code:      "execution_failed",
				Message:   errMsg,
				RequestID: state.RequestID,
			}
		}
		_ = m.runStore.UpdateRunStatus(context.Background(), projectID, env, runID, core.RunStatusFailed, nil, errMsg, &state.Usage)
		_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, core.StreamEvent{
			Type:      core.StreamEventError,
			Error:     state.Error,
			RequestID: state.RequestID,
		})
		_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, core.StreamEvent{
			Type:      core.StreamEventDone,
			RequestID: state.RequestID,
		})
	} else {
		_ = m.runStore.UpdateRunStatus(context.Background(), projectID, env, runID, core.RunStatusCompleted, state.ChatResponse, "", &state.Usage)
		_ = m.eventBuffer.Append(context.Background(), projectID, env, state.RequestID, core.StreamEvent{
			Type:      core.StreamEventDone,
			RequestID: state.RequestID,
		})
	}

	if m.usageStore != nil {
		stateCopy := *state
		if state.Metadata != nil {
			metaCopy := make(map[string]any, len(state.Metadata))
			for k, v := range state.Metadata {
				metaCopy[k] = v
			}
			stateCopy.Metadata = metaCopy
		}
		// If model tokens are 0, fallback to estimated_tokens from metadata if available
		if stateCopy.Usage.TotalTokens == 0 && stateCopy.Usage.InputTokens == 0 && stateCopy.Usage.OutputTokens == 0 {
			if rawEst, ok := stateCopy.Metadata["estimated_tokens"]; ok {
				var estTokens int64
				switch v := rawEst.(type) {
				case int:
					estTokens = int64(v)
				case int64:
					estTokens = v
				case float64:
					estTokens = int64(v)
				case json.Number:
					if n, err := v.Int64(); err == nil {
						estTokens = n
					}
				}
				if estTokens > 0 {
					stateCopy.Usage.TotalTokens = int(estTokens)
				}
			}
		}
		_ = m.usageStore.RecordUsage(context.Background(), &stateCopy)
	}

	_ = m.eventBuffer.CloseBuffer(context.Background(), projectID, env, state.RequestID)
	_ = now
}

// GetOrRecoverRun retrieves a run, performing crash recovery if its heartbeat lease expired while status was running.
func (m *AsyncRunManager) GetOrRecoverRun(ctx context.Context, projectID, env, runID string) (*core.Run, error) {
	run, err := m.runStore.GetRun(ctx, projectID, env, runID)
	if err != nil {
		return nil, err
	}

	if run.Status != core.RunStatusRunning {
		return run, nil
	}

	// Status is running: check if lease is still alive
	lKey := leaseKey(run.ProjectID, run.Environment, run.ID)
	leaseAlive := false

	if m.redisClient != nil {
		exists, err := m.redisClient.Exists(ctx, lKey).Result()
		if err == nil && exists > 0 {
			leaseAlive = true
		}
	} else {
		if val, ok := m.memHeartbeats.Load(lKey); ok {
			if t, ok := val.(time.Time); ok && time.Since(t) <= m.leaseDuration {
				leaseAlive = true
			}
		}
	}

	if !leaseAlive {
		// Lease expired! Worker terminated / pod crashed!
		m.logger.WarnContext(ctx, "worker lease expired, transitioning run to failed",
			"run_id", run.ID,
			"project_id", run.ProjectID,
			"env", run.Environment,
		)

		now := time.Now().UTC()
		run.Status = core.RunStatusFailed
		run.Error = "worker_terminated"
		run.CompletedAt = &now
		run.UpdatedAt = now

		_ = m.runStore.UpdateRunStatus(ctx, run.ProjectID, run.Environment, run.ID, core.RunStatusFailed, nil, "worker_terminated", run.Usage)

		// Append terminal error event to TurnEventBuffer
		_ = m.eventBuffer.Append(ctx, run.ProjectID, run.Environment, run.RequestID, core.StreamEvent{
			Type: core.StreamEventError,
			Error: &core.ErrorResponse{
				Code:      "worker_terminated",
				Message:   "worker lease expired, run terminated",
				RequestID: run.RequestID,
			},
			RequestID: run.RequestID,
		})
		_ = m.eventBuffer.Append(ctx, run.ProjectID, run.Environment, run.RequestID, core.StreamEvent{
			Type:      core.StreamEventDone,
			RequestID: run.RequestID,
		})
		_ = m.eventBuffer.CloseBuffer(ctx, run.ProjectID, run.Environment, run.RequestID)

		// Refund reserved quota
		if m.refunder != nil {
			var estimatedTokens int
			if run.Metadata != nil {
				if rawEst, ok := run.Metadata["estimated_tokens"]; ok {
					switch v := rawEst.(type) {
					case int:
						estimatedTokens = v
					case int64:
						estimatedTokens = int(v)
					case float64:
						estimatedTokens = int(v)
					case json.Number:
						if n, err := v.Int64(); err == nil {
							estimatedTokens = int(n)
						}
					}
				}
			}
			_ = m.refunder.RefundQuota(ctx, run.ProjectID, run.Environment, run.RequestID, estimatedTokens)
		}
	}

	return run, nil
}

// CancelRun aborts a running or queued async run.
func (m *AsyncRunManager) CancelRun(ctx context.Context, projectID, env, runID string) error {
	run, err := m.GetOrRecoverRun(ctx, projectID, env, runID)
	if err != nil {
		return err
	}

	if run.Status == core.RunStatusCompleted || run.Status == core.RunStatusFailed || run.Status == core.RunStatusCancelled {
		return nil // already terminal
	}

	// 1. Mark in RunStore
	if err := m.runStore.CancelRun(ctx, run.ProjectID, run.Environment, run.ID); err != nil {
		return err
	}

	// 2. Abort local worker if running on this instance
	if cancelVal, ok := m.cancellations.Load(run.ID); ok {
		if cancel, ok := cancelVal.(context.CancelFunc); ok {
			cancel()
		}
	}

	// 3. Publish cancel signal to Redis Pub/Sub for cross-instance workers
	if m.redisClient != nil {
		_ = m.redisClient.Publish(ctx, cancelChannel(run.ProjectID, run.Environment, run.ID), "cancel").Err()
	}

	// 4. Emit terminal cancel events to event buffer
	_ = m.eventBuffer.Append(ctx, run.ProjectID, run.Environment, run.RequestID, core.StreamEvent{
		Type: core.StreamEventError,
		Error: &core.ErrorResponse{
			Code:      "cancelled",
			Message:   "run was cancelled by caller",
			RequestID: run.RequestID,
		},
		RequestID: run.RequestID,
	})
	_ = m.eventBuffer.Append(ctx, run.ProjectID, run.Environment, run.RequestID, core.StreamEvent{
		Type:      core.StreamEventDone,
		RequestID: run.RequestID,
	})
	_ = m.eventBuffer.CloseBuffer(ctx, run.ProjectID, run.Environment, run.RequestID)

	return nil
}
