package providers

import (
	"context"
	"net/http"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

func TestWireConversion(t *testing.T) {
	temp := float32(0.7)
	maxTokens := 256
	req := core.ChatRequest{
		Model: "fast",
		Messages: []core.ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello world"},
		},
		Tools: []core.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get current weather",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
		},
		Temperature:    &temp,
		MaxTokens:      &maxTokens,
		Stream:         true,
		ResponseSchema: `{"type": "object", "properties": {"answer": {"type": "string"}}}`,
	}

	wireReq := FromCoreChatRequest(req, "gpt-4o")

	if wireReq.Model != "gpt-4o" {
		t.Fatalf("expected deployment model gpt-4o, got %s", wireReq.Model)
	}
	if len(wireReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(wireReq.Messages))
	}
	if len(wireReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(wireReq.Tools))
	}
	if wireReq.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("expected tool name get_weather, got %s", wireReq.Tools[0].Function.Name)
	}
	if wireReq.ResponseFormat == nil || wireReq.ResponseFormat.Type != "json_schema" {
		t.Fatalf("expected json_schema response format, got %+v", wireReq.ResponseFormat)
	}

	// Test Response Mapping
	finishReason := "stop"
	cost := 0.0015
	wireResp := &OpenAIChatResponse{
		ID:      "chatcmpl-123",
		Created: 1677652288,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you?",
				},
				FinishReason: &finishReason,
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
			Cost:             &cost,
		},
	}

	coreResp := ToCoreChatResponse(wireResp, "req_123", "app_demo", nil)
	if coreResp.ID != "chatcmpl-123" {
		t.Fatalf("expected ID chatcmpl-123, got %s", coreResp.ID)
	}
	if coreResp.RequestID != "req_123" {
		t.Fatalf("expected RequestID req_123, got %s", coreResp.RequestID)
	}
	if coreResp.FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %s", coreResp.FinishReason)
	}
	if coreResp.Message.Content != "Hello! How can I help you?" {
		t.Fatalf("expected content mismatch: %s", coreResp.Message.Content)
	}
	if coreResp.Usage.TotalTokens != 30 {
		t.Fatalf("expected total tokens 30, got %d", coreResp.Usage.TotalTokens)
	}
	if coreResp.Usage.CostUSD == nil || *coreResp.Usage.CostUSD != 0.0015 {
		t.Fatalf("expected cost 0.0015, got %v", coreResp.Usage.CostUSD)
	}
}

func TestEmbedConversion(t *testing.T) {
	req := core.EmbedRequest{
		Model: "text-embedding-3-small",
		Input: []string{"Hello", "World"},
	}

	wireReq := FromCoreEmbedRequest(req, "text-embedding-3-small-deploy")
	if wireReq.Model != "text-embedding-3-small-deploy" {
		t.Fatalf("expected deployment model, got %s", wireReq.Model)
	}
	if len(wireReq.Input) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(wireReq.Input))
	}

	wireResp := &OpenAIEmbedResponse{
		Model: "text-embedding-3-small",
		Data: []OpenAIEmbeddingData{
			{Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
			{Index: 1, Embedding: []float32{0.4, 0.5, 0.6}},
		},
		Usage: &OpenAIUsage{
			PromptTokens: 8,
			TotalTokens:  8,
		},
	}

	coreResp := ToCoreEmbedResponse(wireResp)
	if len(coreResp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(coreResp.Embeddings))
	}
	if len(coreResp.Embeddings[0]) != 3 || coreResp.Embeddings[0][0] != 0.1 {
		t.Fatalf("unexpected embedding[0]: %v", coreResp.Embeddings[0])
	}
	if coreResp.Usage.TotalTokens != 8 {
		t.Fatalf("expected 8 total tokens, got %d", coreResp.Usage.TotalTokens)
	}
}

func TestNormalizeHTTPError(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "120")

	body := []byte(`{"error": {"message": "Rate limit reached", "type": "tokens_exceeded", "code": 429}}`)
	errResp := NormalizeHTTPError(http.StatusTooManyRequests, body, headers, "req_999")

	if errResp.Code != "rate_limited" {
		t.Fatalf("expected rate_limited, got %s", errResp.Code)
	}
	if errResp.Message != "Rate limit reached" {
		t.Fatalf("expected message 'Rate limit reached', got %s", errResp.Message)
	}
	if errResp.RetryAfter != 120 {
		t.Fatalf("expected retry_after 120, got %d", errResp.RetryAfter)
	}
	if errResp.RequestID != "req_999" {
		t.Fatalf("expected req_999, got %s", errResp.RequestID)
	}

	// Test 503 Unavailable
	err503 := NormalizeHTTPError(http.StatusServiceUnavailable, []byte("Service Down"), nil, "req_100")
	if err503.Code != "unavailable" {
		t.Fatalf("expected unavailable, got %s", err503.Code)
	}
}

func TestSSEParser(t *testing.T) {
	sseData := []byte(`: ping

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hel"}}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"lo "}}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"world!"}}]}

data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}

data: [DONE]
`)

	events := ParseSSEBytes(context.Background(), sseData, "req_sse_1")

	var deltas []string
	var hasUsage bool
	var hasDone bool

	for _, e := range events {
		switch e.Type {
		case "delta":
			deltas = append(deltas, e.Delta)
		case "usage":
			hasUsage = true
			if e.Usage.TotalTokens != 8 {
				t.Fatalf("expected 8 tokens, got %d", e.Usage.TotalTokens)
			}
		case "done":
			hasDone = true
		}
	}

	combined := ""
	for _, d := range deltas {
		combined += d
	}
	if combined != "Hello world!" {
		t.Fatalf("expected 'Hello world!', got %q", combined)
	}
	if !hasUsage {
		t.Fatalf("expected usage event")
	}
	if !hasDone {
		t.Fatalf("expected done event")
	}
}

func TestSSEParserStreamingToolCalls(t *testing.T) {
	sseData := []byte(`
data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_weather_1","function":{"name":"get_weather","arguments":""}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ci"}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Paris\"}"}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`)

	events := ParseSSEBytes(context.Background(), sseData, "req_tc_1")

	var toolCalls []*core.ToolCall
	for _, e := range events {
		if e.Type == "tool_call" {
			toolCalls = append(toolCalls, e.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 assembled tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_weather_1" {
		t.Fatalf("expected ID call_weather_1, got %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Fatalf("expected name get_weather, got %s", tc.Name)
	}
	if tc.Arguments != `{"city":"Paris"}` {
		t.Fatalf("expected arguments {\"city\":\"Paris\"}, got %q", tc.Arguments)
	}
}

func TestSSEParserContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sseData := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"test\"}}]}\n\ndata: [DONE]\n\n")
	events := ParseSSEBytes(ctx, sseData, "req_cancel")
	if len(events) > 10 {
		t.Fatalf("expected few or zero events on canceled context, got %d", len(events))
	}
}

func TestCoreToolCallIDHandling(t *testing.T) {
	req := core.ChatRequest{
		Messages: []core.ChatMessage{
			{
				Role:       "tool",
				Content:    `{"temperature": 22}`,
				ToolCallID: "call_abc123",
			},
		},
	}
	wire := FromCoreChatRequest(req, "model-x")
	if len(wire.Messages) != 1 || wire.Messages[0].ToolCallID != "call_abc123" {
		t.Fatalf("expected ToolCallID call_abc123, got %+v", wire.Messages[0])
	}
}

func TestToolNameFallback(t *testing.T) {
	req := core.ChatRequest{
		Tools: []core.ToolDefinition{
			{
				ID:          "tool_fallback_id",
				Name:        "", // empty name
				Description: "some desc",
			},
		},
	}
	wire := FromCoreChatRequest(req, "model-x")
	if len(wire.Tools) != 1 || wire.Tools[0].Function.Name != "tool_fallback_id" {
		t.Fatalf("expected fallback to tool ID, got %s", wire.Tools[0].Function.Name)
	}
}

func TestSSEParserSparseToolCallIndices(t *testing.T) {
	sseData := []byte(`
data: {"id":"chatcmpl-sparse","choices":[{"delta":{"tool_calls":[{"index":3,"id":"call_3","function":{"name":"tool3","arguments":"{}"}}]}}]}

data: {"id":"chatcmpl-sparse","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"tool1","arguments":"{}"}}]}}]}

data: [DONE]
`)

	events := ParseSSEBytes(context.Background(), sseData, "req_sparse")
	var toolCalls []*core.ToolCall
	for _, e := range events {
		if e.Type == "tool_call" {
			toolCalls = append(toolCalls, e.ToolCall)
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	// Verify sorted order (index 1 first, then index 3)
	if toolCalls[0].ID != "call_1" || toolCalls[1].ID != "call_3" {
		t.Fatalf("expected sorted order call_1 then call_3, got %s, %s", toolCalls[0].ID, toolCalls[1].ID)
	}
}

func TestSSEParserErrorPreventsDone(t *testing.T) {
	sseData := []byte(`
data: {"id":"c-1","choices":[{"delta":{"content":"starting"}}]}` + "\n\n" +
		`data: {"error":{"message":"Mid-stream failure","type":"upstream_error"}}` + "\n\n" +
		`data: [DONE]` + "\n\n")

	events := ParseSSEBytes(context.Background(), sseData, "req_err")
	var hasError bool
	var hasDone bool

	for _, e := range events {
		if e.Type == "error" {
			hasError = true
		}
		if e.Type == "done" {
			hasDone = true
		}
	}

	if !hasError {
		t.Fatalf("expected error event")
	}
	if hasDone {
		t.Fatalf("done event must not be emitted after fatal stream error")
	}
}

func TestToCoreEmbedResponseUnindexedData(t *testing.T) {
	// Simulate provider returning items where Index is 0 for all items
	wireResp := &OpenAIEmbedResponse{
		Model: "text-embed",
		Data: []OpenAIEmbeddingData{
			{Index: 0, Embedding: []float32{0.1, 0.2}},
			{Index: 0, Embedding: []float32{0.3, 0.4}},
		},
	}
	res := ToCoreEmbedResponse(wireResp)
	if len(res.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(res.Embeddings))
	}
	if res.Embeddings[0][0] != 0.1 || res.Embeddings[1][0] != 0.3 {
		t.Fatalf("unexpected embeddings content: %+v", res.Embeddings)
	}
}

func TestSSEParserToolCallRepeatedNameAndID(t *testing.T) {
	// Simulates providers that repeatedly send full ID and Name across delta chunks
	sseData := []byte(`
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc123","function":{"name":"search","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc123","function":{"name":"search","arguments":"\"golang\"}"}}]}}]}

data: [DONE]
`)

	events := ParseSSEBytes(context.Background(), sseData, "req_repeat")
	var toolCalls []*core.ToolCall
	for _, e := range events {
		if e.Type == "tool_call" {
			toolCalls = append(toolCalls, e.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].ID != "call_abc123" {
		t.Fatalf("expected tool call ID call_abc123, got %s", toolCalls[0].ID)
	}
	if toolCalls[0].Name != "search" {
		t.Fatalf("expected tool call Name search, got %s", toolCalls[0].Name)
	}
	if toolCalls[0].Arguments != `{"q":"golang"}` {
		t.Fatalf(`expected arguments {"q":"golang"}, got %s`, toolCalls[0].Arguments)
	}
}
