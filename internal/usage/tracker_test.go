package usage

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
)

type mockLimitStore struct {
	mu           sync.Mutex
	reconcileKey string
	estimated    int64
	actual       int64
	calls        int
}

func (m *mockLimitStore) CheckAndDebit(_ context.Context, _ string, _ []core.LimitRule, _ int64) (bool, time.Duration, error) {
	return true, 0, nil
}

func (m *mockLimitStore) ReconcileTokens(_ context.Context, key string, estimatedTokens, actualTokens int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.reconcileKey = key
	m.estimated = estimatedTokens
	m.actual = actualTokens
	return nil
}

func TestTracker_RecordUsage_ExtractsMetadata(t *testing.T) {
	buf := NewBuffer(BufferConfig{Capacity: 10})
	defer buf.Close()

	tracker := NewTracker(buf, nil)

	cost := 0.0042
	state := &core.ReqState{
		RequestID:   "req_123",
		ProjectID:   "proj_abc",
		Environment: "production",
		AIAppID:     "app_support",
		Identity: &core.Identity{
			Subject: "user_456",
			Issuer:  "https://auth.example.com",
		},
		ResolvedModel: &core.ModelRouting{
			LogicalModel: "fast",
			Deployment:   "gpt-4o-mini",
			Adapter:      "litellm",
		},
		Usage: core.UsageStats{
			InputTokens:  120,
			OutputTokens: 80,
			TotalTokens:  200,
			CostUSD:      &cost,
		},
		StartTime: time.Now().Add(-150 * time.Millisecond),
	}

	err := tracker.RecordUsage(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := <-buf.Events()
	if rec.Log == nil || rec.Usage == nil {
		t.Fatalf("expected both log and usage events, got: %+v", rec)
	}

	logEv := rec.Log
	if logEv.RequestID != "req_123" {
		t.Errorf("expected RequestID req_123, got %s", logEv.RequestID)
	}
	if logEv.ProjectID != "proj_abc" {
		t.Errorf("expected ProjectID proj_abc, got %s", logEv.ProjectID)
	}
	if logEv.Environment != "production" {
		t.Errorf("expected Environment production, got %s", logEv.Environment)
	}
	if logEv.AIAppID != "app_support" {
		t.Errorf("expected AIAppID app_support, got %s", logEv.AIAppID)
	}
	if logEv.UserSubject != "user_456" {
		t.Errorf("expected UserSubject user_456, got %s", logEv.UserSubject)
	}
	if logEv.UserIssuer != "https://auth.example.com" {
		t.Errorf("expected UserIssuer https://auth.example.com, got %s", logEv.UserIssuer)
	}
	if logEv.LogicalModel != "fast" {
		t.Errorf("expected LogicalModel fast, got %s", logEv.LogicalModel)
	}
	if logEv.Provider != "litellm" {
		t.Errorf("expected Provider litellm, got %s", logEv.Provider)
	}
	if logEv.ModelDeployment != "gpt-4o-mini" {
		t.Errorf("expected ModelDeployment gpt-4o-mini, got %s", logEv.ModelDeployment)
	}
	if logEv.Status != "success" || logEv.StatusCode != 200 {
		t.Errorf("expected status=success code=200, got status=%s code=%d", logEv.Status, logEv.StatusCode)
	}
	if logEv.InputTokens != 120 || logEv.OutputTokens != 80 || logEv.TotalTokens != 200 {
		t.Errorf("unexpected tokens: %d/%d/%d", logEv.InputTokens, logEv.OutputTokens, logEv.TotalTokens)
	}
	if logEv.CostUSD != 0.0042 {
		t.Errorf("expected cost 0.0042, got %f", logEv.CostUSD)
	}
	if logEv.LatencyMS < 100 {
		t.Errorf("expected latency >= 100ms, got %d", logEv.LatencyMS)
	}

	usgEv := rec.Usage
	if usgEv.TotalTokens != 200 || usgEv.InputTokens != 120 || usgEv.OutputTokens != 80 {
		t.Errorf("unexpected usage event tokens: %d/%d/%d", usgEv.InputTokens, usgEv.OutputTokens, usgEv.TotalTokens)
	}
}

func TestTracker_RecordUsage_ReconcilesTokens(t *testing.T) {
	buf := NewBuffer(BufferConfig{Capacity: 10})
	defer buf.Close()

	limitsMock := &mockLimitStore{}
	tracker := NewTracker(buf, limitsMock)

	state := &core.ReqState{
		RequestID: "req_recon",
		Metadata: map[string]any{
			"limit_user_key":   "lim:proj_1:prod:user_123",
			"estimated_tokens": int64(1000),
		},
		Usage: core.UsageStats{
			InputTokens:  150,
			OutputTokens: 250,
			TotalTokens:  400,
		},
	}

	err := tracker.RecordUsage(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	limitsMock.mu.Lock()
	defer limitsMock.mu.Unlock()

	if limitsMock.calls != 1 {
		t.Fatalf("expected 1 call to ReconcileTokens, got %d", limitsMock.calls)
	}
	if limitsMock.reconcileKey != "lim:proj_1:prod:user_123" {
		t.Errorf("expected key lim:proj_1:prod:user_123, got %s", limitsMock.reconcileKey)
	}
	if limitsMock.estimated != 1000 {
		t.Errorf("expected estimated 1000, got %d", limitsMock.estimated)
	}
	if limitsMock.actual != 400 {
		t.Errorf("expected actual 400, got %d", limitsMock.actual)
	}
}

func TestTracker_RecordUsage_ErrorMapping(t *testing.T) {
	buf := NewBuffer(BufferConfig{Capacity: 10})
	defer buf.Close()

	tracker := NewTracker(buf, nil)

	state := &core.ReqState{
		RequestID: "req_err_test",
		Error: &core.ErrorResponse{
			Code:    "rate_limited",
			Message: "rate limit exceeded",
		},
	}

	err := tracker.RecordUsage(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := <-buf.Events()
	if rec.Log == nil {
		t.Fatalf("expected log record")
	}
	if rec.Log.Status != "rate_limited" {
		t.Errorf("expected status rate_limited, got %s", rec.Log.Status)
	}
	if rec.Log.StatusCode != 429 {
		t.Errorf("expected status code 429, got %d", rec.Log.StatusCode)
	}
	if rec.Log.ErrorCode != "rate_limited" {
		t.Errorf("expected error code rate_limited, got %s", rec.Log.ErrorCode)
	}
}

func TestTracker_RecordUsage_ToolCallsDeduplication(t *testing.T) {
	buf := NewBuffer(BufferConfig{Capacity: 10})
	defer buf.Close()

	tracker := NewTracker(buf, nil)

	state := &core.ReqState{
		RequestID: "req_tools",
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "call_1", Name: "get_weather"},
						{ID: "call_2", Name: "search_db"},
					},
				},
				{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "call_1", Name: "get_weather"}, // Duplicate
					},
				},
			},
		},
		ChatResponse: &core.ChatResponse{
			Message: core.ChatMessage{
				ToolCalls: []core.ToolCall{
					{ID: "call_2", Name: "search_db"}, // Duplicate
					{ID: "call_3", Name: "send_email"},
				},
			},
		},
	}

	err := tracker.RecordUsage(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := <-buf.Events()
	if rec.Log == nil {
		t.Fatalf("expected log record")
	}

	if len(rec.Log.ToolCalls) != 3 {
		t.Fatalf("expected 3 deduplicated tool calls, got %d", len(rec.Log.ToolCalls))
	}
	expectedIDs := []string{"call_1", "call_2", "call_3"}
	for i, expected := range expectedIDs {
		if rec.Log.ToolCalls[i].ID != expected {
			t.Errorf("expected tool call index %d ID=%s, got %s", i, expected, rec.Log.ToolCalls[i].ID)
		}
	}
}

func TestSink_FloatToNumericEdgeCases(t *testing.T) {
	// NaN should map to 0.0
	nanNum := floatToNumeric(math.NaN())
	if numericToFloat(nanNum) != 0.0 {
		t.Errorf("expected NaN to convert to 0.0, got %f", numericToFloat(nanNum))
	}

	// +Inf should map to 0.0
	infNum := floatToNumeric(math.Inf(1))
	if numericToFloat(infNum) != 0.0 {
		t.Errorf("expected Inf to convert to 0.0, got %f", numericToFloat(infNum))
	}

	// Negative should map to 0.0
	negNum := floatToNumeric(-50.5)
	if numericToFloat(negNum) != 0.0 {
		t.Errorf("expected negative to convert to 0.0, got %f", numericToFloat(negNum))
	}

	// Normal positive should preserve precision
	normNum := floatToNumeric(12.345678)
	if val := numericToFloat(normNum); val < 12.345 || val > 12.346 {
		t.Errorf("expected normal value ~12.345678, got %f", val)
	}

	// Large value should cap at 999999.999999
	hugeNum := floatToNumeric(1e12)
	if val := numericToFloat(hugeNum); val < 999999.99 || val > 1000000.0 {
		t.Errorf("expected capped value ~999999.999999, got %f", val)
	}
}
