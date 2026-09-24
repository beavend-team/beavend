package providers

import (
	"encoding/json"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// OpenAIChatRequest represents the standard OpenAI wire format for chat completions.
type OpenAIChatRequest struct {
	Model          string                `json:"model"`
	Messages       []OpenAIMessage       `json:"messages"`
	Tools          []OpenAITool          `json:"tools,omitempty"`
	ToolChoice     any                   `json:"tool_choice,omitempty"`
	Temperature    *float32              `json:"temperature,omitempty"`
	MaxTokens      *int                  `json:"max_tokens,omitempty"`
	Stream         bool                  `json:"stream,omitempty"`
	StreamOptions  *OpenAIStreamOptions  `json:"stream_options,omitempty"`
	ResponseFormat *OpenAIResponseFormat `json:"response_format,omitempty"`
}

// OpenAIStreamOptions specifies streaming options.
type OpenAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// OpenAIResponseFormat specifies structured response format.
type OpenAIResponseFormat struct {
	Type       string         `json:"type"` // "json_object", "json_schema"
	JSONSchema map[string]any `json:"json_schema,omitempty"`
}

// OpenAIMessage represents a single message in OpenAI format.
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OpenAITool defines a tool exposed to the model.
type OpenAITool struct {
	Type     string                   `json:"type"` // always "function"
	Function OpenAIFunctionDefinition `json:"function"`
}

// OpenAIFunctionDefinition defines function metadata for a tool.
type OpenAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// OpenAIToolCall represents a tool invocation from the model.
type OpenAIToolCall struct {
	Index    *int               `json:"index,omitempty"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall contains the name and arguments of a tool invocation.
type OpenAIFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// OpenAIChatResponse represents the non-streaming chat completion response.
type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

// OpenAIChoice represents a single completion choice.
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason *string       `json:"finish_reason"`
}

// OpenAIStreamChunk represents a streaming chunk event payload.
type OpenAIStreamChunk struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OpenAIChunkChoice `json:"choices"`
	Usage   *OpenAIUsage        `json:"usage,omitempty"`
}

// OpenAIChunkChoice represents a choice within a stream chunk.
type OpenAIChunkChoice struct {
	Index        int           `json:"index"`
	Delta        OpenAIMessage `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

// OpenAIUsage represents token count statistics.
type OpenAIUsage struct {
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Cost             *float64 `json:"cost,omitempty"` // LiteLLM cost extension
}

// OpenAIEmbedRequest represents embedding generation input.
type OpenAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// OpenAIEmbedResponse represents embedding generation output.
type OpenAIEmbedResponse struct {
	Object string                `json:"object"`
	Data   []OpenAIEmbeddingData `json:"data"`
	Model  string                `json:"model"`
	Usage  *OpenAIUsage          `json:"usage,omitempty"`
}

// OpenAIEmbeddingData represents a single vector embedding.
type OpenAIEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// OpenAIErrorEnvelope represents the standard OpenAI error response envelope.
type OpenAIErrorEnvelope struct {
	Error OpenAIErrorDetail `json:"error"`
}

// OpenAIErrorDetail contains detailed error attributes.
type OpenAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    any    `json:"code,omitempty"`
}

// FromCoreChatRequest converts a core.ChatRequest into OpenAI wire format.
func FromCoreChatRequest(req core.ChatRequest, deployment string) OpenAIChatRequest {
	wireReq := OpenAIChatRequest{
		Model:       deployment,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
	}

	if req.Stream {
		wireReq.StreamOptions = &OpenAIStreamOptions{
			IncludeUsage: true,
		}
	}

	// Map Messages
	wireReq.Messages = make([]OpenAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := OpenAIMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}

		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]OpenAIToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = OpenAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		wireReq.Messages[i] = msg
	}

	// Map Tools
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]OpenAITool, len(req.Tools))
		for i, t := range req.Tools {
			name := t.Name
			if name == "" {
				name = t.ID
			}
			wireReq.Tools[i] = OpenAITool{
				Type: "function",
				Function: OpenAIFunctionDefinition{
					Name:        name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			}
		}
	}

	// Response Schema
	if req.ResponseSchema != "" {
		var schemaMap map[string]any
		if err := json.Unmarshal([]byte(req.ResponseSchema), &schemaMap); err == nil {
			wireReq.ResponseFormat = &OpenAIResponseFormat{
				Type:       "json_schema",
				JSONSchema: schemaMap,
			}
		} else {
			wireReq.ResponseFormat = &OpenAIResponseFormat{
				Type: "json_object",
			}
		}
	}

	return wireReq
}

// ToCoreChatResponse maps an OpenAI wire response into core.ChatResponse.
func ToCoreChatResponse(resp *OpenAIChatResponse, requestID, aiAppID string, extraCost *float64) *core.ChatResponse {
	if resp == nil {
		return nil
	}

	res := &core.ChatResponse{
		ID:        resp.ID,
		RequestID: requestID,
		AIAppID:   aiAppID,
		CreatedAt: resp.Created,
	}

	if res.CreatedAt == 0 {
		res.CreatedAt = time.Now().Unix()
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.FinishReason != nil {
			res.FinishReason = *choice.FinishReason
		}

		var toolCalls []core.ToolCall
		if len(choice.Message.ToolCalls) > 0 {
			toolCalls = make([]core.ToolCall, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				toolCalls[i] = core.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				}
			}
		}

		res.Message = core.ChatMessage{
			Role:       choice.Message.Role,
			Content:    choice.Message.Content,
			ToolCalls:  toolCalls,
			ToolCallID: choice.Message.ToolCallID,
		}
	}

	if resp.Usage != nil {
		cost := resp.Usage.Cost
		if cost == nil {
			cost = extraCost
		}
		res.Usage = core.UsageStats{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
			CostUSD:      cost,
		}
	} else if extraCost != nil {
		res.Usage.CostUSD = extraCost
	}

	return res
}

// FromCoreEmbedRequest converts core.EmbedRequest to OpenAIEmbedRequest.
func FromCoreEmbedRequest(req core.EmbedRequest, deployment string) OpenAIEmbedRequest {
	return OpenAIEmbedRequest{
		Model: deployment,
		Input: req.Input,
	}
}

// ToCoreEmbedResponse converts OpenAIEmbedResponse to core.EmbedResponse.
func ToCoreEmbedResponse(resp *OpenAIEmbedResponse) *core.EmbedResponse {
	if resp == nil {
		return nil
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, item := range resp.Data {
		targetIdx := item.Index
		if targetIdx < 0 || targetIdx >= len(embeddings) || (targetIdx == 0 && i > 0 && embeddings[0] != nil) {
			targetIdx = i
		}
		embeddings[targetIdx] = item.Embedding
	}

	var usage core.UsageStats
	if resp.Usage != nil {
		usage = core.UsageStats{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
			CostUSD:      resp.Usage.Cost,
		}
	}

	return &core.EmbedResponse{
		Model:      resp.Model,
		Embeddings: embeddings,
		Usage:      usage,
	}
}
