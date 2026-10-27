package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/runtime/stream"
)

type mockQuotaRefunder struct {
	refundedTokens int
	callCount      int
}

func (m *mockQuotaRefunder) RefundQuota(_ context.Context, _, _, _ string, tokens int) error {
	m.callCount++
	m.refundedTokens += tokens
	return nil
}

type asyncMockStage struct {
	handled bool
}

func (d *asyncMockStage) Handle(_ context.Context, _ *core.ReqState) error {
	d.handled = true
	return nil
}

type blockingMockStage struct{}

func (b *blockingMockStage) Handle(ctx context.Context, _ *core.ReqState) error {
	<-ctx.Done()
	return ctx.Err()
}

func setupAsyncRunServer(t *testing.T, maxRuns int) (*AsyncRunManager, *Handler, *chi.Mux, *mockQuotaRefunder) {
	t.Helper()

	runStore := core.NewMemoryRunStore()
	eventBuffer := stream.NewMemoryTurnEventBuffer()
	refunder := &mockQuotaRefunder{}

	pipeline := NewPipeline(&asyncMockStage{})

	runMgr := NewAsyncRunManager(AsyncRunManagerConfig{
		RunStore:               runStore,
		EventBuffer:            eventBuffer,
		Pipeline:               pipeline,
		AuthZStage:             &asyncMockStage{},
		LimitsStage:            &asyncMockStage{},
		MaxConcurrentAsyncRuns: maxRuns,
		HeartbeatLeaseDuration: 15 * time.Second,
		HeartbeatRenewInterval: 5 * time.Second,
		QuotaRefunder:          refunder,
	})

	snap := &core.Snapshot{
		ProjectID:   "proj_test",
		Environment: "production",
		Revision:    1,
		Apps: map[string]*core.AIApp{
			"app_async": {
				ID:        "app_async",
				ProjectID: "proj_test",
			},
		},
	}

	resolver := &mockSnapshotResolver{
		snapshots: map[string]*core.Snapshot{
			"proj_test:production": snap,
		},
	}

	handler := NewHandler(resolver, pipeline, HandlerOptions{
		DefaultEnvironment: "production",
		RunManager:         runMgr,
		RunStore:           runStore,
		EventBuffer:        eventBuffer,
	})

	r := chi.NewRouter()
	r.Post("/v1/apps/{ai_app_id}/invoke", handler.HandleInvoke)
	r.Get("/v1/runs/{run_id}", handler.HandleGetRun)
	r.Get("/v1/runs/{run_id}/stream", handler.HandleStreamRun)
	r.Post("/v1/runs/{run_id}/cancel", handler.HandleCancelRun)

	return runMgr, handler, r, refunder
}

func TestAsyncRunManager_Lifecycle(t *testing.T) {
	_, _, r, _ := setupAsyncRunServer(t, 10)

	body := `{"messages":[{"role":"user","content":"Run long analysis"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_async/invoke", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Project-ID", "proj_test")
	req.Header.Set("X-Environment", "production")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var invokeResp struct {
		RunID  string         `json:"run_id"`
		Status core.RunStatus `json:"status"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&invokeResp))
	assert.NotEmpty(t, invokeResp.RunID)
	assert.Equal(t, core.RunStatusQueued, invokeResp.Status)

	// Poll GET /v1/runs/{run_id} until completed or timeout
	require.Eventually(t, func() bool {
		getReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+invokeResp.RunID, nil)
		getReq.Header.Set("X-Project-ID", "proj_test")
		getReq.Header.Set("X-Environment", "production")
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)

		if getRec.Code != http.StatusOK {
			return false
		}
		var run core.Run
		if err := json.NewDecoder(getRec.Body).Decode(&run); err != nil {
			return false
		}
		return run.Status == core.RunStatusCompleted
	}, 3*time.Second, 50*time.Millisecond, "run should complete successfully")
}

func TestAsyncRunManager_Cancellation(t *testing.T) {
	runStore := core.NewMemoryRunStore()
	eventBuffer := stream.NewMemoryTurnEventBuffer()

	// Pipeline that blocks until context cancelled
	blockingPipeline := NewPipeline(&blockingMockStage{})

	runMgr := NewAsyncRunManager(AsyncRunManagerConfig{
		RunStore:               runStore,
		EventBuffer:            eventBuffer,
		Pipeline:               blockingPipeline,
		MaxConcurrentAsyncRuns: 10,
	})

	snap := &core.Snapshot{
		ProjectID:   "proj_test",
		Environment: "production",
		Apps: map[string]*core.AIApp{
			"app_block": {ID: "app_block", ProjectID: "proj_test"},
		},
	}
	resolver := &mockSnapshotResolver{
		snapshots: map[string]*core.Snapshot{"proj_test:production": snap},
	}

	handler := NewHandler(resolver, blockingPipeline, HandlerOptions{
		DefaultEnvironment: "production",
		RunManager:         runMgr,
		RunStore:           runStore,
		EventBuffer:        eventBuffer,
	})

	r := chi.NewRouter()
	r.Post("/v1/apps/{ai_app_id}/invoke", handler.HandleInvoke)
	r.Get("/v1/runs/{run_id}", handler.HandleGetRun)
	r.Post("/v1/runs/{run_id}/cancel", handler.HandleCancelRun)

	// 1. Invoke run
	body := `{"messages":[{"role":"user","content":"Block indefinitely"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_block/invoke", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Project-ID", "proj_test")
	req.Header.Set("X-Environment", "production")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var invokeResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&invokeResp))

	// Wait until status transitions to running
	require.Eventually(t, func() bool {
		run, err := runStore.GetRun(context.Background(), "proj_test", "production", invokeResp.RunID)
		return err == nil && run.Status == core.RunStatusRunning
	}, 2*time.Second, 20*time.Millisecond)

	// 2. Cancel run
	cancelReq := httptest.NewRequest(http.MethodPost, "/v1/runs/"+invokeResp.RunID+"/cancel", nil)
	cancelReq.Header.Set("X-Project-ID", "proj_test")
	cancelReq.Header.Set("X-Environment", "production")
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code)

	var cancelResp struct {
		RunID  string         `json:"run_id"`
		Status core.RunStatus `json:"status"`
	}
	require.NoError(t, json.NewDecoder(cancelRec.Body).Decode(&cancelResp))
	assert.Equal(t, invokeResp.RunID, cancelResp.RunID)
	assert.Equal(t, core.RunStatusCancelled, cancelResp.Status)

	// Verify status is cancelled
	run, err := runStore.GetRun(context.Background(), "proj_test", "production", invokeResp.RunID)
	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCancelled, run.Status)
}

func TestAsyncRunManager_Backpressure(t *testing.T) {
	// Saturated manager with capacity = 1
	runMgr, _, r, _ := setupAsyncRunServer(t, 1)

	// Artificially saturate
	runMgr.activeRuns.Store(1)

	body := `{"messages":[{"role":"user","content":"Queue full test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_async/invoke", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Project-ID", "proj_test")
	req.Header.Set("X-Environment", "production")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))

	var errResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "queue_full", errResp["code"])
}

func TestAsyncRunManager_CrashTimeoutRecovery(t *testing.T) {
	ctx := context.Background()
	runStore := core.NewMemoryRunStore()
	eventBuffer := stream.NewMemoryTurnEventBuffer()
	refunder := &mockQuotaRefunder{}

	runMgr := NewAsyncRunManager(AsyncRunManagerConfig{
		RunStore:               runStore,
		EventBuffer:            eventBuffer,
		HeartbeatLeaseDuration: 100 * time.Millisecond,
		QuotaRefunder:          refunder,
	})

	// Create a run that was left in "running" state by a crashed pod
	run := &core.Run{
		ID:          "run_crashed_worker",
		ProjectID:   "proj_test",
		Environment: "production",
		AIAppID:     "app_async",
		RequestID:   "req_crashed",
		Status:      core.RunStatusRunning,
		Metadata:    map[string]any{"estimated_tokens": int64(1500)},
		CreatedAt:   time.Now().UTC().Add(-10 * time.Minute),
		UpdatedAt:   time.Now().UTC().Add(-10 * time.Minute),
	}
	require.NoError(t, runStore.CreateRun(ctx, run))

	// No heartbeat lease active (simulating crashed pod where lease expired)
	// Calling GetOrRecoverRun should detect lease expiration, transition to failed, and refund quota
	recovered, err := runMgr.GetOrRecoverRun(ctx, "proj_test", "production", "run_crashed_worker")
	require.NoError(t, err)
	assert.Equal(t, core.RunStatusFailed, recovered.Status)
	assert.Equal(t, "worker_terminated", recovered.Error)
	assert.NotNil(t, recovered.CompletedAt)
	assert.Equal(t, 1, refunder.callCount)
	assert.Equal(t, 1500, refunder.refundedTokens, "should refund estimated_tokens from run metadata")

	// Verify TurnEventBuffer has terminal error event
	events, err := eventBuffer.ReplayFrom(ctx, "proj_test", "production", "req_crashed", "")
	require.NoError(t, err)
	require.NotEmpty(t, events)

	var hasTerminatedError bool
	for _, ev := range events {
		if ev.Type == core.StreamEventError && ev.Error != nil && ev.Error.Code == "worker_terminated" {
			hasTerminatedError = true
		}
	}
	assert.True(t, hasTerminatedError, "terminal error event should be recorded in event buffer")
}

func TestAsyncRunManager_SSEStreamEndpoint(t *testing.T) {
	_, _, r, _ := setupAsyncRunServer(t, 10)

	body := `{"messages":[{"role":"user","content":"Stream analysis"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/app_async/invoke", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Project-ID", "proj_test")
	req.Header.Set("X-Environment", "production")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var invokeResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&invokeResp))

	// Connect to SSE stream
	streamReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+invokeResp.RunID+"/stream", nil)
	streamReq.Header.Set("X-Project-ID", "proj_test")
	streamReq.Header.Set("X-Environment", "production")
	streamRec := httptest.NewRecorder()

	r.ServeHTTP(streamRec, streamReq)

	assert.Equal(t, http.StatusOK, streamRec.Code)
	assert.Contains(t, streamRec.Header().Get("Content-Type"), "text/event-stream")
}

type mockRunsUsageStore struct {
	mu       sync.Mutex
	recorded []*core.ReqState
}

func (m *mockRunsUsageStore) RecordUsage(_ context.Context, state *core.ReqState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recorded = append(m.recorded, state)
	return nil
}

func (m *mockRunsUsageStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.recorded)
}

func (m *mockRunsUsageStore) last() *core.ReqState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.recorded) == 0 {
		return nil
	}
	return m.recorded[len(m.recorded)-1]
}

func TestAsyncRunManager_UsageRecordingOnTerminalStatuses(t *testing.T) {
	ctx := context.Background()
	usageStore := &mockRunsUsageStore{}
	runStore := core.NewMemoryRunStore()
	eventBuffer := stream.NewMemoryTurnEventBuffer()

	pipeline := NewPipeline(&asyncMockStage{})
	runMgr := NewAsyncRunManager(AsyncRunManagerConfig{
		RunStore:    runStore,
		EventBuffer: eventBuffer,
		Pipeline:    pipeline,
		UsageStore:  usageStore,
	})

	// 1. Completed run with fallback to estimated_tokens
	run1 := &core.Run{
		ID:          "run_term_completed",
		ProjectID:   "proj_test",
		Environment: "production",
		AIAppID:     "app_async",
		RequestID:   "req_term_completed",
		Status:      core.RunStatusQueued,
		Metadata:    map[string]any{"estimated_tokens": int64(450), "limit_user_key": "user_1"},
	}
	require.NoError(t, runStore.CreateRun(ctx, run1))

	state1 := &core.ReqState{
		RequestID:   "req_term_completed",
		ProjectID:   "proj_test",
		Environment: "production",
		AIAppID:     "app_async",
		Metadata:    map[string]any{"estimated_tokens": int64(450), "limit_user_key": "user_1"},
	}

	require.NoError(t, runMgr.Dispatch(run1, state1))

	// Wait for worker to finish
	require.Eventually(t, func() bool {
		r, err := runStore.GetRun(ctx, "proj_test", "production", "run_term_completed")
		return err == nil && r.Status == core.RunStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		return usageStore.count() == 1
	}, 2*time.Second, 20*time.Millisecond)

	lastRecorded := usageStore.last()
	require.NotNil(t, lastRecorded)
	assert.Equal(t, 450, lastRecorded.Usage.TotalTokens, "should fallback to estimated_tokens when model tokens are 0")
}
