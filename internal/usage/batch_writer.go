package usage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
)

var (
	flushedLogsCounter, _  = meter.Int64Counter("beavend_usage_logs_flushed_total", metric.WithDescription("Total number of request log records written to sink"))
	flushedUsageCounter, _ = meter.Int64Counter("beavend_usage_records_flushed_total", metric.WithDescription("Total number of usage records written to sink"))
	flushErrorCounter, _   = meter.Int64Counter("beavend_usage_flush_errors_total", metric.WithDescription("Total number of batch flush errors"))
)

// BatchWriterConfig configures batching behavior.
type BatchWriterConfig struct {
	BatchSize     int
	FlushInterval time.Duration
	MaxRetries    int
	RetryBackoff  time.Duration
}

// DefaultBatchWriterConfig returns recommended batch writer settings.
func DefaultBatchWriterConfig() BatchWriterConfig {
	return BatchWriterConfig{
		BatchSize:     100,
		FlushInterval: 500 * time.Millisecond,
		MaxRetries:    3,
		RetryBackoff:  100 * time.Millisecond,
	}
}

// BatchWriter buffers events and flushes them in batches to a Sink.
type BatchWriter struct {
	sink         Sink
	buffer       *Buffer
	cfg          BatchWriterConfig
	stopCh       chan struct{}
	doneCh       chan struct{}
	started      atomic.Bool
	closed       atomic.Bool
	mu           sync.Mutex
	pendingLogs  []LogEvent
	pendingUsage []Event
	flushedCount atomic.Int64
}

// NewBatchWriter constructs a new BatchWriter.
func NewBatchWriter(sink Sink, buffer *Buffer, cfg BatchWriterConfig) *BatchWriter {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 500 * time.Millisecond
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 100 * time.Millisecond
	}

	return &BatchWriter{
		sink:         sink,
		buffer:       buffer,
		cfg:          cfg,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		pendingLogs:  make([]LogEvent, 0, cfg.BatchSize),
		pendingUsage: make([]Event, 0, cfg.BatchSize),
	}
}

// Start begins the background worker goroutine.
func (w *BatchWriter) Start() {
	if w.started.CompareAndSwap(false, true) {
		go w.run()
	}
}

// run is the background batch collection loop.
func (w *BatchWriter) run() {
	defer close(w.doneCh)

	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			// Drain remaining events from buffer
			w.drainRemaining()
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			w.flushCurrentBatch(flushCtx)
			cancel()
			return

		case rec, ok := <-w.buffer.Events():
			if !ok {
				// Buffer channel closed
				flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				w.flushCurrentBatch(flushCtx)
				cancel()
				return
			}

			w.appendRecord(rec)
			if w.shouldFlush() {
				flushCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				w.flushCurrentBatch(flushCtx)
				cancel()
			}

		case <-ticker.C:
			if w.hasPending() {
				flushCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				w.flushCurrentBatch(flushCtx)
				cancel()
			}
		}
	}
}

func (w *BatchWriter) appendRecord(rec Record) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if rec.Log != nil {
		w.pendingLogs = append(w.pendingLogs, *rec.Log)
	}
	if rec.Usage != nil {
		w.pendingUsage = append(w.pendingUsage, *rec.Usage)
	}
}

func (w *BatchWriter) shouldFlush() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pendingLogs) >= w.cfg.BatchSize || len(w.pendingUsage) >= w.cfg.BatchSize
}

func (w *BatchWriter) hasPending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pendingLogs) > 0 || len(w.pendingUsage) > 0
}

func (w *BatchWriter) drainRemaining() {
	for {
		select {
		case rec, ok := <-w.buffer.Events():
			if !ok {
				return
			}
			w.appendRecord(rec)
		default:
			return
		}
	}
}

func (w *BatchWriter) flushCurrentBatch(ctx context.Context) {
	w.mu.Lock()
	if len(w.pendingLogs) == 0 && len(w.pendingUsage) == 0 {
		w.mu.Unlock()
		return
	}

	logs := w.pendingLogs
	usage := w.pendingUsage
	w.pendingLogs = make([]LogEvent, 0, w.cfg.BatchSize)
	w.pendingUsage = make([]Event, 0, w.cfg.BatchSize)
	w.mu.Unlock()

	batchSize := w.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	for len(logs) > 0 || len(usage) > 0 {
		var chunkLogs []LogEvent
		if len(logs) > batchSize {
			chunkLogs = logs[:batchSize]
			logs = logs[batchSize:]
		} else {
			chunkLogs = logs
			logs = nil
		}

		var chunkUsage []Event
		if len(usage) > batchSize {
			chunkUsage = usage[:batchSize]
			usage = usage[batchSize:]
		} else {
			chunkUsage = usage
			usage = nil
		}

		w.writeChunkWithRetry(ctx, chunkLogs, chunkUsage)
	}
}

func (w *BatchWriter) writeChunkWithRetry(ctx context.Context, logs []LogEvent, usage []Event) {
	var err error
	for attempt := 0; attempt < w.cfg.MaxRetries; attempt++ {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = w.sink.WriteBatch(writeCtx, logs, usage)
		cancel()

		if err == nil {
			break
		}

		if flushErrorCounter != nil {
			flushErrorCounter.Add(ctx, 1)
		}

		slog.Warn("failed to write usage batch, retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("logs_count", len(logs)),
			slog.Int("usage_count", len(usage)),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			slog.Warn("batch writer retry interrupted by context cancellation", "error", ctx.Err())
			return
		case <-time.After(w.cfg.RetryBackoff * time.Duration(1<<attempt)):
		}
	}

	if err != nil {
		slog.Error("failed to write usage batch after retries; records dropped",
			slog.Int("logs_count", len(logs)),
			slog.Int("usage_count", len(usage)),
			slog.String("error", err.Error()),
		)
		return
	}

	count := int64(len(logs) + len(usage))
	w.flushedCount.Add(count)

	if flushedLogsCounter != nil && len(logs) > 0 {
		flushedLogsCounter.Add(ctx, int64(len(logs)))
	}
	if flushedUsageCounter != nil && len(usage) > 0 {
		flushedUsageCounter.Add(ctx, int64(len(usage)))
	}
}

// Close gracefully closes the buffer, stops the worker, and waits for pending events to flush.
func (w *BatchWriter) Close(ctx context.Context) error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil
	}

	w.buffer.Close()

	if !w.started.Load() {
		// If worker never started, just flush whatever was buffered
		w.drainRemaining()
		w.flushCurrentBatch(ctx)
		return nil
	}

	close(w.stopCh)

	select {
	case <-w.doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("batch writer shutdown timed out: %w", ctx.Err())
	}
}

// FlushedCount returns the total number of items successfully written to the sink.
func (w *BatchWriter) FlushedCount() int64 {
	return w.flushedCount.Load()
}
