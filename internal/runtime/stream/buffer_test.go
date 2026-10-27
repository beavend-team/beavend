package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestMemoryTurnEventBuffer_AppendAndReplay(t *testing.T) {
	buf := NewMemoryTurnEventBuffer()
	ctx := context.Background()

	ev1 := core.StreamEvent{Type: core.StreamEventDelta, Delta: "Hello"}
	ev2 := core.StreamEvent{Type: core.StreamEventDelta, Delta: " world"}
	ev3 := core.StreamEvent{
		Type: core.StreamEventToolEnd,
		ToolEnd: &core.ToolEndData{
			ID:        "call_123",
			Name:      "web_fetch",
			Status:    "success",
			LatencyMS: 45,
			ResultRef: "ref_abc",
		},
	}
	evDone := core.StreamEvent{Type: core.StreamEventDone, RequestID: "req_99"}

	require.NoError(t, buf.Append(ctx, "p1", "dev", "req1", ev1))
	require.NoError(t, buf.Append(ctx, "p1", "dev", "req1", ev2))
	require.NoError(t, buf.Append(ctx, "p1", "dev", "req1", ev3))

	// Replay all events
	all, err := buf.ReplayFrom(ctx, "p1", "dev", "req1", "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "1", all[0].ID)
	assert.Equal(t, "2", all[1].ID)
	assert.Equal(t, "3", all[2].ID)
	assert.Equal(t, "Hello", all[0].Delta)

	// Replay strictly after ID "1"
	after1, err := buf.ReplayFrom(ctx, "p1", "dev", "req1", "1")
	require.NoError(t, err)
	require.Len(t, after1, 2)
	assert.Equal(t, "2", after1[0].ID)
	assert.Equal(t, "3", after1[1].ID)

	// Replay strictly after ID "3"
	after3, err := buf.ReplayFrom(ctx, "p1", "dev", "req1", "3")
	require.NoError(t, err)
	assert.Empty(t, after3)

	// Appending done should close buffer
	require.NoError(t, buf.Append(ctx, "p1", "dev", "req1", evDone))
	err = buf.Append(ctx, "p1", "dev", "req1", core.StreamEvent{Type: core.StreamEventDelta, Delta: "after done"})
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestMemoryTurnEventBuffer_Subscribe(t *testing.T) {
	buf := NewMemoryTurnEventBuffer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Append initial event
	require.NoError(t, buf.Append(ctx, "p1", "dev", "req2", core.StreamEvent{ID: "10", Type: core.StreamEventDelta, Delta: "First"}))

	// Subscribe from beginning
	ch, err := buf.Subscribe(ctx, "p1", "dev", "req2", "")
	require.NoError(t, err)

	first := <-ch
	assert.Equal(t, "First", first.Delta)

	// Append second event while subscribed
	require.NoError(t, buf.Append(ctx, "p1", "dev", "req2", core.StreamEvent{ID: "11", Type: core.StreamEventDelta, Delta: "Second"}))

	select {
	case second := <-ch:
		assert.Equal(t, "Second", second.Delta)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live stream event")
	}

	// Close buffer should close subscription channel
	require.NoError(t, buf.CloseBuffer(ctx, "p1", "dev", "req2"))

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "expected channel to be closed")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscription channel close")
	}
}

func TestWriter_WriteEventWithMonotonicIDAndAdditiveTypes(t *testing.T) {
	wr := NewWriter(15 * time.Second)

	tests := []struct {
		name      string
		event     core.StreamEvent
		wantID    string
		wantEvent string
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name: "event with monotonic ID and delta",
			event: core.StreamEvent{
				ID:    "1609459200000-1",
				Type:  core.StreamEventDelta,
				Delta: "chunk text",
			},
			wantID:    "id: 1609459200000-1\n",
			wantEvent: "event: delta\n",
			checkJSON: func(t *testing.T, data map[string]any) {
				assert.Equal(t, "chunk text", data["text"])
			},
		},
		{
			name: "reasoning event",
			event: core.StreamEvent{
				ID:        "seq-2",
				Type:      core.StreamEventReasoning,
				Reasoning: "thinking step by step",
			},
			wantID:    "id: seq-2\n",
			wantEvent: "event: reasoning\n",
			checkJSON: func(t *testing.T, data map[string]any) {
				assert.Equal(t, "thinking step by step", data["text"])
			},
		},
		{
			name: "tool_end event carries metadata without raw output",
			event: core.StreamEvent{
				ID:   "seq-3",
				Type: core.StreamEventToolEnd,
				ToolEnd: &core.ToolEndData{
					ID:        "call_456",
					Name:      "web_fetch",
					Status:    "success",
					LatencyMS: 120,
					ResultRef: "res_cache_key_999",
				},
			},
			wantID:    "id: seq-3\n",
			wantEvent: "event: tool_end\n",
			checkJSON: func(t *testing.T, data map[string]any) {
				assert.Equal(t, "call_456", data["id"])
				assert.Equal(t, "web_fetch", data["name"])
				assert.Equal(t, "success", data["status"])
				assert.Equal(t, float64(120), data["latency_ms"])
				assert.Equal(t, "res_cache_key_999", data["result_ref"])
				assert.Nil(t, data["output"], "raw tool output must never be present in tool_end payload")
			},
		},
		{
			name: "event without ID omits id field",
			event: core.StreamEvent{
				Type:      core.StreamEventDone,
				RequestID: "req_final",
			},
			wantID:    "",
			wantEvent: "event: done\n",
			checkJSON: func(t *testing.T, data map[string]any) {
				assert.Equal(t, "req_final", data["request_id"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := wr.WriteEvent(&buf, tt.event)
			require.NoError(t, err)

			output := buf.String()

			if tt.wantID != "" {
				assert.True(t, strings.HasPrefix(output, tt.wantID), "expected %q prefix, got %q", tt.wantID, output)
			} else {
				assert.False(t, strings.HasPrefix(output, "id: "), "unexpected id line in %q", output)
			}

			assert.Contains(t, output, tt.wantEvent)
			assert.True(t, strings.HasSuffix(output, "\n\n"))

			// Extract data line JSON
			lines := strings.Split(output, "\n")
			var dataLine string
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					dataLine = strings.TrimPrefix(line, "data: ")
					break
				}
			}
			require.NotEmpty(t, dataLine)

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(dataLine), &parsed))
			tt.checkJSON(t, parsed)
		})
	}
}
