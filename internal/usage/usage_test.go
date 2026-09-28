package usage

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuffer_EnqueueAndConsume(t *testing.T) {
	cfg := BufferConfig{
		Capacity: 10,
		Policy:   DropOnFull,
	}
	buf := NewBuffer(cfg)
	defer buf.Close()

	rec := Record{
		Log: &LogEvent{ID: "log_1", RequestID: "req_1"},
	}

	if !buf.Enqueue(rec) {
		t.Fatalf("expected enqueue to succeed")
	}

	enqueued, dropped, inFlight := buf.Stats()
	if enqueued != 1 || dropped != 0 || inFlight != 1 {
		t.Fatalf("unexpected stats: enqueued=%d, dropped=%d, inFlight=%d", enqueued, dropped, inFlight)
	}

	consumed := <-buf.Events()
	if consumed.Log == nil || consumed.Log.ID != "log_1" {
		t.Fatalf("unexpected consumed record: %+v", consumed)
	}
}

func TestBuffer_BackpressureDrop(t *testing.T) {
	cfg := BufferConfig{
		Capacity: 2,
		Policy:   DropOnFull,
	}
	buf := NewBuffer(cfg)
	defer buf.Close()

	// Enqueue 2 items (fills buffer)
	buf.Enqueue(Record{Log: &LogEvent{ID: "1"}})
	buf.Enqueue(Record{Log: &LogEvent{ID: "2"}})

	// 3rd item should drop immediately
	ok := buf.Enqueue(Record{Log: &LogEvent{ID: "3"}})
	if ok {
		t.Fatalf("expected 3rd enqueue to drop")
	}

	enqueued, dropped, inFlight := buf.Stats()
	if enqueued != 2 || dropped != 1 || inFlight != 2 {
		t.Fatalf("expected enqueued=2, dropped=1, inFlight=2, got: enqueued=%d, dropped=%d, inFlight=%d",
			enqueued, dropped, inFlight)
	}
}

func TestBuffer_BackpressureBlockWithTimeout(t *testing.T) {
	cfg := BufferConfig{
		Capacity:     1,
		Policy:       BlockWithTimeout,
		BlockTimeout: 20 * time.Millisecond,
	}
	buf := NewBuffer(cfg)
	defer buf.Close()

	buf.Enqueue(Record{Log: &LogEvent{ID: "1"}})

	start := time.Now()
	ok := buf.Enqueue(Record{Log: &LogEvent{ID: "2"}})
	elapsed := time.Since(start)

	if ok {
		t.Fatalf("expected enqueue to fail after timeout")
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("expected to block for at least ~20ms, got %v", elapsed)
	}

	_, dropped, _ := buf.Stats()
	if dropped != 1 {
		t.Fatalf("expected 1 dropped, got %d", dropped)
	}
}

func TestBatchWriter_FlushOnBatchSize(t *testing.T) {
	sink := NewInMemorySink()
	buf := NewBuffer(BufferConfig{Capacity: 100})
	writer := NewBatchWriter(sink, buf, BatchWriterConfig{
		BatchSize:     5,
		FlushInterval: 10 * time.Second, // Long interval so flush is driven by size
	})
	writer.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = writer.Close(ctx)
	}()

	for i := 0; i < 5; i++ {
		buf.Enqueue(Record{
			Log: &LogEvent{ID: "log", RequestID: "req"},
		})
	}

	// Should flush within a few hundred milliseconds due to size threshold
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.Logs()) == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(sink.Logs()) != 5 {
		t.Fatalf("expected 5 logs flushed by size threshold, got %d", len(sink.Logs()))
	}
}

func TestBatchWriter_FlushOnInterval(t *testing.T) {
	sink := NewInMemorySink()
	buf := NewBuffer(BufferConfig{Capacity: 100})
	writer := NewBatchWriter(sink, buf, BatchWriterConfig{
		BatchSize:     50, // High batch size
		FlushInterval: 50 * time.Millisecond,
	})
	writer.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = writer.Close(ctx)
	}()

	buf.Enqueue(Record{
		Log:   &LogEvent{ID: "log_1"},
		Usage: &Event{ID: "usage_1"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.Logs()) == 1 && len(sink.Usage()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(sink.Logs()) != 1 || len(sink.Usage()) != 1 {
		t.Fatalf("expected 1 log and 1 usage record flushed on ticker, got logs=%d, usage=%d",
			len(sink.Logs()), len(sink.Usage()))
	}
}

type flakySink struct {
	mu        sync.Mutex
	failTimes int
	callCount int
	logs      []LogEvent
	usage     []Event
}

func (s *flakySink) WriteBatch(_ context.Context, logs []LogEvent, usage []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	if s.callCount <= s.failTimes {
		return errors.New("transient database connection error")
	}
	s.logs = append(s.logs, logs...)
	s.usage = append(s.usage, usage...)
	return nil
}

func TestBatchWriter_RetryOnTransientFailure(t *testing.T) {
	sink := &flakySink{failTimes: 2}
	buf := NewBuffer(BufferConfig{Capacity: 100})
	writer := NewBatchWriter(sink, buf, BatchWriterConfig{
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		MaxRetries:    3,
		RetryBackoff:  10 * time.Millisecond,
	})
	writer.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = writer.Close(ctx)
	}()

	buf.Enqueue(Record{
		Log: &LogEvent{ID: "retry_log"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		count := len(sink.logs)
		sink.mu.Unlock()
		if count == 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.logs) != 1 {
		t.Fatalf("expected 1 log persisted after retries, got %d (callCount=%d)", len(sink.logs), sink.callCount)
	}
	if sink.callCount != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", sink.callCount)
	}
}

func TestBatchWriter_GracefulShutdownDrains(t *testing.T) {
	sink := NewInMemorySink()
	buf := NewBuffer(BufferConfig{Capacity: 1000})
	writer := NewBatchWriter(sink, buf, BatchWriterConfig{
		BatchSize:     500, // Very large batch size so flush doesn't happen automatically
		FlushInterval: 1 * time.Minute,
	})
	writer.Start()

	const count = 150
	for i := 0; i < count; i++ {
		buf.Enqueue(Record{
			Log:   &LogEvent{ID: "log"},
			Usage: &Event{ID: "usage"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := writer.Close(ctx); err != nil {
		t.Fatalf("expected clean shutdown, got error: %v", err)
	}

	if len(sink.Logs()) != count || len(sink.Usage()) != count {
		t.Fatalf("expected all %d records drained on shutdown, got logs=%d, usage=%d",
			count, len(sink.Logs()), len(sink.Usage()))
	}
}

func TestBatchWriter_ConcurrentEnqueues(t *testing.T) {
	sink := NewInMemorySink()
	buf := NewBuffer(BufferConfig{Capacity: 5000})
	writer := NewBatchWriter(sink, buf, BatchWriterConfig{
		BatchSize:     50,
		FlushInterval: 20 * time.Millisecond,
	})
	writer.Start()

	var wg sync.WaitGroup
	const goroutines = 10
	const perGoroutine = 50

	var totalEnqueued atomic.Int64

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if buf.Enqueue(Record{
					Log:   &LogEvent{ID: "concurrent_log"},
					Usage: &Event{ID: "concurrent_usage"},
				}) {
					totalEnqueued.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := writer.Close(ctx); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	expected := int(totalEnqueued.Load())
	if len(sink.Logs()) != expected || len(sink.Usage()) != expected {
		t.Fatalf("expected %d logs and usage records, got logs=%d, usage=%d",
			expected, len(sink.Logs()), len(sink.Usage()))
	}
}
