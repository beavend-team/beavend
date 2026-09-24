package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/beavend/beavend/internal/core"
)

// StreamParser reads Server-Sent Events from an upstream model provider and dispatches core.StreamEvent objects.
type StreamParser struct {
	requestID string
}

// NewStreamParser creates a new StreamParser for a given request ID.
func NewStreamParser(requestID string) *StreamParser {
	return &StreamParser{
		requestID: requestID,
	}
}

type toolCallBuilder struct {
	id        string
	name      string
	arguments strings.Builder
	emitted   bool
}

// ParseStream reads lines from reader until EOF or ctx cancel, sending events to out.
// It closes the out channel when finished.
func (p *StreamParser) ParseStream(ctx context.Context, reader io.Reader, out chan<- core.StreamEvent) {
	defer close(out)

	scanner := bufio.NewReader(reader)
	toolCalls := make(map[int]*toolCallBuilder)
	var hasError bool

	sendEvent := func(evt core.StreamEvent) bool {
		if evt.RequestID == "" {
			evt.RequestID = p.requestID
		}
		select {
		case <-ctx.Done():
			return false
		case out <- evt:
			return true
		}
	}

	flushToolCalls := func() bool {
		if len(toolCalls) == 0 {
			return true
		}
		indices := make([]int, 0, len(toolCalls))
		for idx := range toolCalls {
			indices = append(indices, idx)
		}
		sort.Ints(indices)

		for _, i := range indices {
			tc := toolCalls[i]
			if !tc.emitted {
				tc.emitted = true
				call := &core.ToolCall{
					ID:        tc.id,
					Name:      tc.name,
					Arguments: tc.arguments.String(),
				}
				if !sendEvent(core.StreamEvent{
					Type:     "tool_call",
					ToolCall: call,
				}) {
					return false
				}
			}
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		lineBytes, err := scanner.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			if err != io.EOF {
				hasError = true
				sendEvent(core.StreamEvent{
					Type: "error",
					Error: &core.ErrorResponse{
						Code:      "provider_error",
						Message:   fmt.Sprintf("read sse stream: %v", err),
						RequestID: p.requestID,
					},
				})
			}
			break
		}

		line := strings.TrimSpace(string(lineBytes))
		if line == "" {
			continue
		}

		// Comment/Heartbeat (e.g. ": ping", ": keep-alive")
		if strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}

		if payload == "[DONE]" {
			if !flushToolCalls() {
				return
			}
			sendEvent(core.StreamEvent{
				Type: "done",
			})
			return
		}

		// Check if payload is an error object
		var errEnv OpenAIErrorEnvelope
		if json.Unmarshal([]byte(payload), &errEnv) == nil && errEnv.Error.Message != "" {
			sendEvent(core.StreamEvent{
				Type: "error",
				Error: &core.ErrorResponse{
					Code:      "provider_error",
					Message:   errEnv.Error.Message,
					RequestID: p.requestID,
				},
			})
			return
		}

		var chunk OpenAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			sendEvent(core.StreamEvent{
				Type: "error",
				Error: &core.ErrorResponse{
					Code:      "provider_error",
					Message:   fmt.Sprintf("malformed sse chunk: %v", err),
					RequestID: p.requestID,
				},
			})
			return
		}

		// Process usage if present
		if chunk.Usage != nil {
			stats := &core.UsageStats{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
				CostUSD:      chunk.Usage.Cost,
			}
			if !sendEvent(core.StreamEvent{
				Type:  "usage",
				Usage: stats,
			}) {
				return
			}
		}

		for _, choice := range chunk.Choices {
			// Delta content
			if choice.Delta.Content != "" {
				if !sendEvent(core.StreamEvent{
					Type:  "delta",
					Delta: choice.Delta.Content,
				}) {
					return
				}
			}

			// Tool call delta accumulation
			if len(choice.Delta.ToolCalls) > 0 {
				for _, tcDelta := range choice.Delta.ToolCalls {
					idx := 0
					if tcDelta.Index != nil {
						idx = *tcDelta.Index
					}

					builder, exists := toolCalls[idx]
					if !exists {
						builder = &toolCallBuilder{}
						toolCalls[idx] = builder
					}

					if tcDelta.ID != "" {
						if builder.id == "" {
							builder.id = tcDelta.ID
						} else {
							builder.id += tcDelta.ID
						}
					}
					if tcDelta.Function.Name != "" {
						builder.name += tcDelta.Function.Name
					}
					if tcDelta.Function.Arguments != "" {
						builder.arguments.WriteString(tcDelta.Function.Arguments)
					}
				}
			}

			// If finish_reason indicates tool_calls or stop, check tool call flush
			if choice.FinishReason != nil {
				if *choice.FinishReason == "tool_calls" || *choice.FinishReason == "stop" {
					if !flushToolCalls() {
						return
					}
				}
			}
		}
	}

	if hasError {
		return
	}

	// Flush any remaining tool calls before exit
	if !flushToolCalls() {
		return
	}

	sendEvent(core.StreamEvent{
		Type: "done",
	})
}

// ParseSSEBytes is a helper to parse an entire byte slice into stream events (used in tests/mocks).
func ParseSSEBytes(ctx context.Context, data []byte, requestID string) []core.StreamEvent {
	out := make(chan core.StreamEvent, 50)
	parser := NewStreamParser(requestID)
	go parser.ParseStream(ctx, bytes.NewReader(data), out)

	var events []core.StreamEvent
	for evt := range out {
		events = append(events, evt)
	}
	return events
}
