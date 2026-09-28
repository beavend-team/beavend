package usage

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockRollupStore struct {
	mu             sync.Mutex
	aggregateCalls []windowCall
	upsertCalls    []upsertCall
	mockRows       []AggregatedUsageRow
}

type windowCall struct {
	start time.Time
	end   time.Time
}

type upsertCall struct {
	row         AggregatedUsageRow
	granularity string
	bucketStart time.Time
}

func (m *mockRollupStore) AggregateWindow(_ context.Context, start, end time.Time) ([]AggregatedUsageRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aggregateCalls = append(m.aggregateCalls, windowCall{start: start, end: end})
	return m.mockRows, nil
}

func (m *mockRollupStore) UpsertRollup(_ context.Context, row AggregatedUsageRow, granularity string, bucketStart time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls = append(m.upsertCalls, upsertCall{
		row:         row,
		granularity: granularity,
		bucketStart: bucketStart,
	})
	return nil
}

func TestRollupAggregator_AggregateHourly(t *testing.T) {
	mockStore := &mockRollupStore{
		mockRows: []AggregatedUsageRow{
			{
				ProjectID:    "proj_1",
				Environment:  "production",
				AIAppID:      "app_support",
				UserID:       "usr_123",
				LogicalModel: "fast",
				Provider:     "litellm",
				RequestCount: 15,
				InputTokens:  1500,
				OutputTokens: 750,
				TotalTokens:  2250,
				CostUSD:      0.035,
			},
		},
	}

	agg := NewRollupAggregator(mockStore, RollupAggregatorConfig{Interval: time.Minute})
	targetTime := time.Date(2026, 9, 28, 14, 35, 10, 0, time.UTC)

	err := agg.AggregateHourly(context.Background(), targetTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mockStore.mu.Lock()
	defer mockStore.mu.Unlock()

	if len(mockStore.aggregateCalls) != 1 {
		t.Fatalf("expected 1 AggregateWindow call, got %d", len(mockStore.aggregateCalls))
	}
	expectedStart := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	if !mockStore.aggregateCalls[0].start.Equal(expectedStart) || !mockStore.aggregateCalls[0].end.Equal(expectedEnd) {
		t.Errorf("expected window [%v, %v), got [%v, %v)",
			expectedStart, expectedEnd, mockStore.aggregateCalls[0].start, mockStore.aggregateCalls[0].end)
	}

	if len(mockStore.upsertCalls) != 1 {
		t.Fatalf("expected 1 UpsertRollup call, got %d", len(mockStore.upsertCalls))
	}
	call := mockStore.upsertCalls[0]
	if call.granularity != "hour" {
		t.Errorf("expected granularity hour, got %s", call.granularity)
	}
	if !call.bucketStart.Equal(expectedStart) {
		t.Errorf("expected bucketStart %v, got %v", expectedStart, call.bucketStart)
	}
	if call.row.TotalTokens != 2250 || call.row.RequestCount != 15 {
		t.Errorf("unexpected row stats: count=%d, tokens=%d", call.row.RequestCount, call.row.TotalTokens)
	}
}

func TestRollupAggregator_AggregateDaily(t *testing.T) {
	mockStore := &mockRollupStore{
		mockRows: []AggregatedUsageRow{
			{
				ProjectID:    "proj_1",
				Environment:  "production",
				AIAppID:      "app_support",
				RequestCount: 100,
				TotalTokens:  50000,
			},
		},
	}

	agg := NewRollupAggregator(mockStore, RollupAggregatorConfig{Interval: time.Minute})
	targetTime := time.Date(2026, 9, 28, 14, 35, 10, 0, time.UTC)

	err := agg.AggregateDaily(context.Background(), targetTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mockStore.mu.Lock()
	defer mockStore.mu.Unlock()

	expectedStart := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	if !mockStore.aggregateCalls[0].start.Equal(expectedStart) || !mockStore.aggregateCalls[0].end.Equal(expectedEnd) {
		t.Errorf("expected day window [%v, %v), got [%v, %v)",
			expectedStart, expectedEnd, mockStore.aggregateCalls[0].start, mockStore.aggregateCalls[0].end)
	}

	if len(mockStore.upsertCalls) != 1 {
		t.Fatalf("expected 1 UpsertRollup call, got %d", len(mockStore.upsertCalls))
	}
	if mockStore.upsertCalls[0].granularity != "day" {
		t.Errorf("expected granularity day, got %s", mockStore.upsertCalls[0].granularity)
	}
}

func TestRollupAggregator_EmptyWindow(t *testing.T) {
	mockStore := &mockRollupStore{
		mockRows: []AggregatedUsageRow{},
	}

	agg := NewRollupAggregator(mockStore, RollupAggregatorConfig{Interval: time.Minute})
	err := agg.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("expected no error on empty window, got %v", err)
	}

	mockStore.mu.Lock()
	defer mockStore.mu.Unlock()

	if len(mockStore.upsertCalls) != 0 {
		t.Errorf("expected 0 upsert calls for empty window, got %d", len(mockStore.upsertCalls))
	}
}

func TestRollupAggregator_StartStop(t *testing.T) {
	mockStore := &mockRollupStore{
		mockRows: []AggregatedUsageRow{},
	}

	agg := NewRollupAggregator(mockStore, RollupAggregatorConfig{Interval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agg.Start(ctx)
	}()

	// Wait until the background goroutine has acquired the started flag
	for !agg.started.Load() {
		time.Sleep(time.Millisecond)
	}

	// Duplicate start should safely return immediately
	agg.Start(ctx)

	time.Sleep(30 * time.Millisecond)
	cancel()
	wg.Wait()

	if agg.started.Load() {
		t.Errorf("expected aggregator to be stopped")
	}
}
