package usage

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter              = otel.Meter("beavend/usage")
	enqueuedCounter, _ = meter.Int64Counter("beavend_usage_events_enqueued_total", metric.WithDescription("Total number of usage events enqueued into buffer"))
	droppedCounter, _  = meter.Int64Counter("beavend_usage_events_dropped_total", metric.WithDescription("Total number of usage events dropped due to backpressure"))
)

// BackpressurePolicy defines behavior when the in-process event channel is full.
type BackpressurePolicy int

const (
	// DropOnFull drops incoming events immediately when buffer is saturated.
	DropOnFull BackpressurePolicy = iota
	// BlockWithTimeout blocks for up to BlockTimeout before dropping.
	BlockWithTimeout
)

// BufferConfig configures the in-process event buffer.
type BufferConfig struct {
	Capacity     int
	Policy       BackpressurePolicy
	BlockTimeout time.Duration
}

// DefaultBufferConfig returns recommended default buffer settings.
func DefaultBufferConfig() BufferConfig {
	return BufferConfig{
		Capacity:     10000,
		Policy:       DropOnFull,
		BlockTimeout: 50 * time.Millisecond,
	}
}

// Buffer provides a thread-safe, bounded channel for asynchronous event ingestion.
type Buffer struct {
	cfg           BufferConfig
	ch            chan Record
	closed        atomic.Bool
	enqueuedCount atomic.Int64
	droppedCount  atomic.Int64
	mu            sync.RWMutex
}

// NewBuffer constructs a new bounded event buffer.
func NewBuffer(cfg BufferConfig) *Buffer {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 10000
	}
	return &Buffer{
		cfg: cfg,
		ch:  make(chan Record, cfg.Capacity),
	}
}

// Enqueue enqueues an event into the buffer according to the configured backpressure policy.
// Returns true if enqueued, false if dropped or buffer is closed.
func (b *Buffer) Enqueue(rec Record) bool {
	if b.closed.Load() {
		b.droppedCount.Add(1)
		return false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed.Load() {
		b.droppedCount.Add(1)
		return false
	}

	select {
	case b.ch <- rec:
		b.enqueuedCount.Add(1)
		if enqueuedCounter != nil {
			enqueuedCounter.Add(context.Background(), 1)
		}
		return true
	default:
		// Channel is full, evaluate backpressure policy
		if b.cfg.Policy == BlockWithTimeout && b.cfg.BlockTimeout > 0 {
			timer := time.NewTimer(b.cfg.BlockTimeout)
			defer timer.Stop()

			select {
			case b.ch <- rec:
				b.enqueuedCount.Add(1)
				if enqueuedCounter != nil {
					enqueuedCounter.Add(context.Background(), 1)
				}
				return true
			case <-timer.C:
				// Timed out waiting for channel capacity
			}
		}

		b.droppedCount.Add(1)
		if droppedCounter != nil {
			droppedCounter.Add(context.Background(), 1)
		}
		return false
	}
}

// Events returns the receive-only channel for consuming buffered records.
func (b *Buffer) Events() <-chan Record {
	return b.ch
}

// Close closes the event buffer channel. No further records can be enqueued.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed.CompareAndSwap(false, true) {
		close(b.ch)
	}
}

// Stats returns the number of enqueued and dropped records, and current in-flight channel depth.
func (b *Buffer) Stats() (enqueued, dropped, inFlight int64) {
	return b.enqueuedCount.Load(), b.droppedCount.Load(), int64(len(b.ch))
}
