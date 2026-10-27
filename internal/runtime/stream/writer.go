package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrStreamingNotSupported indicates the ResponseWriter does not support http.Flusher.
	ErrStreamingNotSupported = errors.New("streaming unsupported: ResponseWriter does not implement http.Flusher")
)

// Writer formats and flushes Server-Sent Events to HTTP clients.
type Writer struct {
	heartbeatInterval time.Duration
}

// NewWriter creates a new SSE Writer with the given heartbeat interval.
// If heartbeatInterval <= 0, defaults to 15 seconds.
func NewWriter(heartbeatInterval time.Duration) *Writer {
	if heartbeatInterval <= 0 {
		heartbeatInterval = 15 * time.Second
	}
	return &Writer{
		heartbeatInterval: heartbeatInterval,
	}
}

// SetHeaders configures the HTTP response headers required for SSE.
func (wr *Writer) SetHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

// Stream reads StreamEvents from eventChan and pushes them as SSE frames to w,
// emitting heartbeat comments during idle periods until the channel is closed
// or the context is cancelled.
func (wr *Writer) Stream(ctx context.Context, w http.ResponseWriter, eventChan <-chan core.StreamEvent) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return ErrStreamingNotSupported
	}

	wr.SetHeaders(w)
	flusher.Flush()

	ticker := time.NewTicker(wr.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			// Emit SSE heartbeat ping comment
			if err := wr.WritePing(w); err != nil {
				return err
			}
			flusher.Flush()

		case event, open := <-eventChan:
			if !open {
				return nil
			}

			if err := wr.WriteEvent(w, event); err != nil {
				return err
			}
			flusher.Flush()

			// Reset heartbeat ticker after active event
			ticker.Reset(wr.heartbeatInterval)

			if event.Type == "done" {
				return nil
			}
		}
	}
}

// WriteEvent formats and writes a single core.StreamEvent as an SSE event frame.
func (wr *Writer) WriteEvent(w io.Writer, event core.StreamEvent) error {
	var payload any

	switch event.Type {
	case "delta", "thinking", "reasoning":
		text := event.Delta
		if text == "" && event.Reasoning != "" {
			text = event.Reasoning
		}
		payload = map[string]string{
			"text": text,
		}
	case "tool_start":
		if event.ToolCall != nil {
			payload = map[string]any{
				"call_id": event.ToolCall.ID,
				"tool":    event.ToolCall.Name,
			}
		} else if event.Data != nil {
			payload = event.Data
		} else {
			payload = map[string]any{}
		}
	case "tool_delta":
		if event.Delta != "" {
			payload = map[string]any{
				"delta": event.Delta,
			}
		} else if event.Data != nil {
			payload = event.Data
		} else {
			payload = map[string]any{}
		}
	case "tool_end":
		if event.ToolEnd != nil {
			payload = event.ToolEnd
		} else if event.Data != nil {
			payload = event.Data
		} else {
			payload = map[string]any{}
		}
	case "question":
		if event.Data != nil {
			payload = event.Data
		} else {
			payload = map[string]string{"question": event.Delta}
		}
	case "sub_agent_activity":
		if event.Data != nil {
			payload = event.Data
		} else {
			payload = map[string]string{"activity": event.Delta}
		}
	case "tool_call":
		if event.ToolCall != nil {
			var args any = event.ToolCall.Arguments
			if event.ToolCall.Arguments != "" {
				var parsed any
				if err := json.Unmarshal([]byte(event.ToolCall.Arguments), &parsed); err == nil {
					args = parsed
				}
			}
			payload = map[string]any{
				"call_id": event.ToolCall.ID,
				"tool":    event.ToolCall.Name,
				"args":    args,
			}
		} else {
			payload = map[string]any{}
		}
	case "confirmation":
		toolName := ""
		var args any
		if event.ToolCall != nil {
			toolName = event.ToolCall.Name
			args = event.ToolCall.Arguments
			if event.ToolCall.Arguments != "" {
				var parsed any
				if err := json.Unmarshal([]byte(event.ToolCall.Arguments), &parsed); err == nil {
					args = parsed
				}
			}
		}
		payload = map[string]any{
			"tool":  toolName,
			"args":  args,
			"token": event.ConfirmationToken,
		}
	case "usage":
		if event.Usage != nil {
			payload = event.Usage
		} else {
			payload = map[string]int{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
		}
	case "error":
		if event.Error != nil {
			payload = map[string]any{
				"code":    event.Error.Code,
				"message": event.Error.Message,
			}
		} else {
			payload = map[string]string{
				"code":    "internal_error",
				"message": "an unknown streaming error occurred",
			}
		}
	case "done":
		payload = map[string]string{
			"request_id": event.RequestID,
		}
	default:
		payload = event
	}

	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal stream event data: %w", err)
	}

	eventType := event.Type
	if eventType == "" {
		eventType = "message"
	}

	var sb strings.Builder
	if event.ID != "" {
		sb.WriteString("id: ")
		sb.WriteString(event.ID)
		sb.WriteString("\n")
	}
	sb.WriteString("event: ")
	sb.WriteString(eventType)
	sb.WriteString("\ndata: ")
	sb.Write(dataBytes)
	sb.WriteString("\n\n")

	_, err = io.WriteString(w, sb.String())
	return err
}

// WritePing writes a standard SSE keep-alive comment frame.
func (wr *Writer) WritePing(w io.Writer) error {
	_, err := io.WriteString(w, ": ping\n\n")
	return err
}
