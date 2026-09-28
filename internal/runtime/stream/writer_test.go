package stream

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() {
	f.flushed = true
}

type nonFlusherWriter struct {
	http.ResponseWriter
}

func TestStreamWriter_StreamEvents(t *testing.T) {
	wr := NewWriter(50 * time.Millisecond)

	t.Run("unsupported flusher returns error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		nonFlush := &nonFlusherWriter{ResponseWriter: rec}
		ch := make(chan core.StreamEvent)
		close(ch)

		err := wr.Stream(context.Background(), nonFlush, ch)
		require.ErrorIs(t, err, ErrStreamingNotSupported)
	})

	t.Run("streams delta, usage, done events cleanly", func(t *testing.T) {
		rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
		ch := make(chan core.StreamEvent, 10)

		ch <- core.StreamEvent{
			Type:  "delta",
			Delta: "Hello ",
		}
		ch <- core.StreamEvent{
			Type:  "delta",
			Delta: "world!",
		}
		ch <- core.StreamEvent{
			Type: "usage",
			Usage: &core.UsageStats{
				InputTokens:  10,
				OutputTokens: 2,
				TotalTokens:  12,
			},
		}
		ch <- core.StreamEvent{
			Type:      "done",
			RequestID: "req_test_123",
		}
		close(ch)

		err := wr.Stream(context.Background(), rec, ch)
		require.NoError(t, err)

		headers := rec.Header()
		assert.Equal(t, "text/event-stream; charset=utf-8", headers.Get("Content-Type"))
		assert.Equal(t, "no-cache, no-transform", headers.Get("Cache-Control"))
		assert.Equal(t, "keep-alive", headers.Get("Connection"))

		body := rec.Body.String()
		assert.Contains(t, body, "event: delta\ndata: {\"text\":\"Hello \"}\n\n")
		assert.Contains(t, body, "event: delta\ndata: {\"text\":\"world!\"}\n\n")
		assert.Contains(t, body, "event: usage\ndata: {\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}\n\n")
		assert.Contains(t, body, "event: done\ndata: {\"request_id\":\"req_test_123\"}\n\n")
	})

	t.Run("streams error event", func(t *testing.T) {
		rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
		ch := make(chan core.StreamEvent, 2)

		ch <- core.StreamEvent{
			Type: "error",
			Error: &core.ErrorResponse{
				Code:    "provider_error",
				Message: "upstream timed out",
			},
		}
		close(ch)

		err := wr.Stream(context.Background(), rec, ch)
		require.NoError(t, err)

		body := rec.Body.String()
		assert.Contains(t, body, "event: error\ndata: {\"code\":\"provider_error\",\"message\":\"upstream timed out\"}\n\n")
	})

	t.Run("emits periodic heartbeat pings on idle", func(t *testing.T) {
		fastWriter := NewWriter(10 * time.Millisecond)
		rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
		ch := make(chan core.StreamEvent)

		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
		defer cancel()

		err := fastWriter.Stream(ctx, rec, ch)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		body := rec.Body.String()
		// At least one ping comment should have been emitted
		assert.True(t, strings.Contains(body, ": ping\n\n"), "expected ping comments in body, got: %s", body)
	})

	t.Run("client context cancellation terminates stream", func(t *testing.T) {
		rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
		ch := make(chan core.StreamEvent)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err := wr.Stream(ctx, rec, ch)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestStreamWriter_WriteEventTypes(t *testing.T) {
	wr := NewWriter(time.Minute)

	t.Run("tool call formatting with json string args", func(t *testing.T) {
		var buf bytes.Buffer
		evt := core.StreamEvent{
			Type: "tool_call",
			ToolCall: &core.ToolCall{
				ID:        "call_1",
				Name:      "get_weather",
				Arguments: `{"city":"Berlin"}`,
			},
		}

		err := wr.WriteEvent(&buf, evt)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), `event: tool_call`)
		assert.Contains(t, buf.String(), `"call_id":"call_1"`)
		assert.Contains(t, buf.String(), `"tool":"get_weather"`)
		assert.Contains(t, buf.String(), `"city":"Berlin"`)
	})

	t.Run("confirmation formatting", func(t *testing.T) {
		var buf bytes.Buffer
		evt := core.StreamEvent{
			Type:              "confirmation",
			ConfirmationToken: "token_xyz",
			ToolCall: &core.ToolCall{
				ID:        "call_confirm",
				Name:      "delete_account",
				Arguments: `{"user_id":"u1"}`,
			},
		}

		err := wr.WriteEvent(&buf, evt)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), `event: confirmation`)
		assert.Contains(t, buf.String(), `"token":"token_xyz"`)
		assert.Contains(t, buf.String(), `"tool":"delete_account"`)
	})
}
