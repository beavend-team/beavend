package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrBufferClosed indicates that the turn event buffer has already been closed.
	ErrBufferClosed = errors.New("turn event buffer is closed")
)

// TurnEventBuffer buffers and streams SSE StreamEvents for a single turn or request.
// It supports live subscriptions, disconnections, and SSE resumption via ReplayFrom.
type TurnEventBuffer interface {
	Append(ctx context.Context, projectID, env, reqID string, event core.StreamEvent) error
	ReplayFrom(ctx context.Context, projectID, env, reqID, lastEventID string) ([]core.StreamEvent, error)
	Subscribe(ctx context.Context, projectID, env, reqID, lastEventID string) (<-chan core.StreamEvent, error)
	CloseBuffer(ctx context.Context, projectID, env, reqID string) error
}

func bufferKey(projectID, env, reqID string) string {
	return fmt.Sprintf("turn_stream:%s:%s:%s", projectID, env, reqID)
}

// MemoryTurnEventBuffer provides a thread-safe in-memory implementation of TurnEventBuffer.
type MemoryTurnEventBuffer struct {
	mu      sync.RWMutex
	buffers map[string]*memBufferEntry
}

type memBufferEntry struct {
	mu          sync.RWMutex
	events      []core.StreamEvent
	closed      bool
	seq         atomic.Int64
	subscribers map[chan core.StreamEvent]struct{}
}

// NewMemoryTurnEventBuffer creates an in-memory turn event buffer.
func NewMemoryTurnEventBuffer() *MemoryTurnEventBuffer {
	return &MemoryTurnEventBuffer{
		buffers: make(map[string]*memBufferEntry),
	}
}

func (m *MemoryTurnEventBuffer) getOrCreateEntry(key string) *memBufferEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.buffers[key]
	if !exists {
		entry = &memBufferEntry{
			events:      make([]core.StreamEvent, 0, 16),
			subscribers: make(map[chan core.StreamEvent]struct{}),
		}
		m.buffers[key] = entry
	}
	return entry
}

func (m *MemoryTurnEventBuffer) Append(_ context.Context, projectID, env, reqID string, event core.StreamEvent) error {
	key := bufferKey(projectID, env, reqID)
	entry := m.getOrCreateEntry(key)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.closed {
		return ErrBufferClosed
	}

	if event.ID == "" {
		seq := entry.seq.Add(1)
		event.ID = fmt.Sprintf("%d", seq)
	}

	entry.events = append(entry.events, event)

	for ch := range entry.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer channel full; drop or slow consumer
		}
	}

	if event.Type == "done" || event.Type == "error" {
		entry.closed = true
		for ch := range entry.subscribers {
			close(ch)
		}
		entry.subscribers = make(map[chan core.StreamEvent]struct{})
	}

	return nil
}

func (m *MemoryTurnEventBuffer) ReplayFrom(_ context.Context, projectID, env, reqID, lastEventID string) ([]core.StreamEvent, error) {
	key := bufferKey(projectID, env, reqID)
	m.mu.RLock()
	entry, exists := m.buffers[key]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	if lastEventID == "" || lastEventID == "0" {
		out := make([]core.StreamEvent, len(entry.events))
		copy(out, entry.events)
		return out, nil
	}

	foundIdx := -1
	for i, ev := range entry.events {
		if ev.ID == lastEventID {
			foundIdx = i
			break
		}
	}

	if foundIdx >= 0 {
		if foundIdx+1 >= len(entry.events) {
			return nil, nil
		}
		out := make([]core.StreamEvent, len(entry.events)-(foundIdx+1))
		copy(out, entry.events[foundIdx+1:])
		return out, nil
	}

	// If exact match not found, attempt numeric comparison
	lastSeq, err := strconv.ParseInt(lastEventID, 10, 64)
	if err != nil {
		out := make([]core.StreamEvent, len(entry.events))
		copy(out, entry.events)
		return out, nil
	}

	var out []core.StreamEvent
	for _, ev := range entry.events {
		evSeq, err := strconv.ParseInt(ev.ID, 10, 64)
		if err == nil && evSeq > lastSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *MemoryTurnEventBuffer) Subscribe(ctx context.Context, projectID, env, reqID, lastEventID string) (<-chan core.StreamEvent, error) {
	key := bufferKey(projectID, env, reqID)
	entry := m.getOrCreateEntry(key)

	replayed, err := m.ReplayFrom(ctx, projectID, env, reqID, lastEventID)
	if err != nil {
		return nil, err
	}

	ch := make(chan core.StreamEvent, 128)
	for _, ev := range replayed {
		ch <- ev
	}

	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		close(ch)
		return ch, nil
	}
	entry.subscribers[ch] = struct{}{}
	entry.mu.Unlock()

	go func() {
		<-ctx.Done()
		entry.mu.Lock()
		delete(entry.subscribers, ch)
		entry.mu.Unlock()
	}()

	return ch, nil
}

func (m *MemoryTurnEventBuffer) CloseBuffer(_ context.Context, projectID, env, reqID string) error {
	key := bufferKey(projectID, env, reqID)
	m.mu.RLock()
	entry, exists := m.buffers[key]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if !entry.closed {
		entry.closed = true
		for ch := range entry.subscribers {
			close(ch)
		}
		entry.subscribers = make(map[chan core.StreamEvent]struct{})
	}

	return nil
}

// RedisTurnEventBuffer implements TurnEventBuffer backed by Redis Streams.
type RedisTurnEventBuffer struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisTurnEventBuffer creates a new RedisTurnEventBuffer.
func NewRedisTurnEventBuffer(client redis.UniversalClient) *RedisTurnEventBuffer {
	return &RedisTurnEventBuffer{
		client: client,
		ttl:    5 * time.Minute,
	}
}

func (r *RedisTurnEventBuffer) Append(ctx context.Context, projectID, env, reqID string, event core.StreamEvent) error {
	key := bufferKey(projectID, env, reqID)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal stream event: %w", err)
	}

	id := event.ID
	if id == "" {
		id = "*"
	}

	xaddArgs := &redis.XAddArgs{
		Stream: key,
		MaxLen: 512,
		Approx: true,
		ID:     id,
		Values: map[string]any{"data": string(data), "type": event.Type},
	}

	addedID, err := r.client.XAdd(ctx, xaddArgs).Result()
	if err != nil {
		return fmt.Errorf("redis xadd: %w", err)
	}

	_ = r.client.Expire(ctx, key, r.ttl)

	if event.Type == "done" || event.Type == "error" {
		closedKey := key + ":closed"
		_ = r.client.Set(ctx, closedKey, addedID, r.ttl)
	}

	return nil
}

func (r *RedisTurnEventBuffer) ReplayFrom(ctx context.Context, projectID, env, reqID, lastEventID string) ([]core.StreamEvent, error) {
	key := bufferKey(projectID, env, reqID)

	msgs, err := r.client.XRange(ctx, key, "-", "+").Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redis xrange: %w", err)
	}

	var events []core.StreamEvent
	passedLast := (lastEventID == "" || lastEventID == "0" || lastEventID == "0-0")

	for _, msg := range msgs {
		if !passedLast {
			if msg.ID == lastEventID {
				passedLast = true
			}
			continue
		}

		raw, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		var ev core.StreamEvent
		if err := json.Unmarshal([]byte(raw), &ev); err == nil {
			if ev.ID == "" {
				ev.ID = msg.ID
			}
			events = append(events, ev)
		}
	}

	return events, nil
}

func (r *RedisTurnEventBuffer) Subscribe(ctx context.Context, projectID, env, reqID, lastEventID string) (<-chan core.StreamEvent, error) {
	key := bufferKey(projectID, env, reqID)
	ch := make(chan core.StreamEvent, 128)

	replayed, err := r.ReplayFrom(ctx, projectID, env, reqID, lastEventID)
	if err != nil {
		return nil, err
	}

	currentID := "0-0"
	if lastEventID != "" {
		currentID = lastEventID
	}

	for _, ev := range replayed {
		ch <- ev
		if ev.ID != "" {
			currentID = ev.ID
		}
		if ev.Type == "done" || ev.Type == "error" {
			close(ch)
			return ch, nil
		}
	}

	go func() {
		defer close(ch)
		closedKey := key + ":closed"

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Block up to 2 seconds for new events
			streams, err := r.client.XRead(ctx, &redis.XReadArgs{
				Streams: []string{key, currentID},
				Count:   16,
				Block:   2 * time.Second,
			}).Result()

			if err != nil {
				if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// Check if stream was closed
					if isClosed, _ := r.client.Exists(ctx, closedKey).Result(); isClosed > 0 {
						return
					}
					continue
				}
				return
			}

			for _, xStream := range streams {
				for _, msg := range xStream.Messages {
					currentID = msg.ID
					raw, ok := msg.Values["data"].(string)
					if !ok {
						continue
					}
					var ev core.StreamEvent
					if err := json.Unmarshal([]byte(raw), &ev); err == nil {
						if ev.ID == "" {
							ev.ID = msg.ID
						}
						select {
						case ch <- ev:
						case <-ctx.Done():
							return
						}
						if ev.Type == "done" || ev.Type == "error" {
							return
						}
					}
				}
			}

			if isClosed, _ := r.client.Exists(ctx, closedKey).Result(); isClosed > 0 {
				return
			}
		}
	}()

	return ch, nil
}

func (r *RedisTurnEventBuffer) CloseBuffer(ctx context.Context, projectID, env, reqID string) error {
	key := bufferKey(projectID, env, reqID)
	closedKey := key + ":closed"
	return r.client.Set(ctx, closedKey, "1", r.ttl).Err()
}
