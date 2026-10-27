package providers

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
)

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

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
	Role             string           `json:"role"`
	Content          any              `json:"content"` // string or []map[string]any for multimodal parts
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Thinking         string           `json:"thinking,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
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

// OpenAIDelta represents delta updates within a stream chunk.
type OpenAIDelta struct {
	Role             string           `json:"role"`
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Thinking         string           `json:"thinking,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

// OpenAIChunkChoice represents a choice within a stream chunk.
type OpenAIChunkChoice struct {
	Index        int         `json:"index"`
	Delta        OpenAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
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
		content := formatWireMessageContent(m)
		// If last message in request and request has attachments, append them to content
		if i == len(req.Messages)-1 && len(req.Attachments) > 0 {
			content = appendAttachmentsToContent(content, req.Attachments)
		}

		msg := OpenAIMessage{
			Role:       m.Role,
			Content:    content,
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

func formatWireMessageContent(m core.ChatMessage) any {
	if len(m.Parts) == 0 {
		return m.Content
	}

	hasMedia := false
	for _, p := range m.Parts {
		if p.Type == core.PartTypeMedia && p.Media != nil {
			hasMedia = true
			break
		}
	}

	// If no media parts, simple string representation suffices
	if !hasMedia {
		return m.TextContent()
	}

	// Multi-part block content for OpenAI multimodal standard
	var blocks []map[string]any
	for _, p := range m.Parts {
		switch p.Type {
		case core.PartTypeText:
			if p.Text != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": p.Text,
				})
			}
		case core.PartTypeMedia:
			if p.Media != nil {
				urlStr := p.Media.URL
				if urlStr == "" && p.Media.Digest != "" {
					urlStr = p.Media.Digest
				}
				if urlStr != "" {
					blocks = append(blocks, buildMediaContentBlock(
						"",
						p.Media.MimeType,
						urlStr,
						p.Media.Filename,
					))
				}
			}
		}
	}

	if len(blocks) == 0 {
		return m.TextContent()
	}
	return blocks
}

func appendAttachmentsToContent(existing any, atts []core.Attachment) any {
	var blocks []map[string]any
	switch v := existing.(type) {
	case string:
		if v != "" {
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": v,
			})
		}
	case []map[string]any:
		blocks = append(blocks, v...)
	}

	for _, att := range atts {
		urlStr := att.URL
		if urlStr == "" && len(att.Data) > 0 {
			mime := att.MimeType
			if mime == "" {
				if strings.EqualFold(att.Type, "document") {
					mime = "application/pdf"
				} else {
					mime = "application/octet-stream"
				}
			}
			urlStr = "data:" + mime + ";base64," + base64Encode(att.Data)
		}
		if urlStr != "" {
			blocks = append(blocks, buildMediaContentBlock(
				att.Type,
				att.MimeType,
				urlStr,
				att.Filename,
			))
		}
	}

	if len(blocks) == 0 {
		return existing
	}
	return blocks
}

func buildMediaContentBlock(attType, mimeType, urlStr, filename string) map[string]any {
	normalizedType := strings.ToLower(strings.TrimSpace(attType))
	normalizedMime := strings.ToLower(strings.TrimSpace(mimeType))

	// 1. Check if it's an image
	isImage := normalizedType == "image" ||
		strings.HasPrefix(normalizedMime, "image/") ||
		strings.HasPrefix(urlStr, "data:image/")

	if isImage {
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": urlStr,
			},
		}
	}

	// 2. Non-image media: documents (PDF, text, docx), audio, video, etc.
	// In OpenAI and LiteLLM standard chat completions, documents/files are passed via the "file" content block.
	name := strings.TrimSpace(filename)
	if name == "" {
		switch {
		case normalizedMime == "application/pdf" || strings.Contains(urlStr, "data:application/pdf"):
			name = "document.pdf"
		case strings.HasPrefix(normalizedMime, "audio/"):
			name = "audio"
		case strings.HasPrefix(normalizedMime, "video/"):
			name = "video"
		default:
			name = "document"
		}
	}

	return map[string]any{
		"type": "file",
		"file": map[string]any{
			"filename":  name,
			"file_data": urlStr,
		},
	}
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

		contentStr := ""
		if s, ok := choice.Message.Content.(string); ok {
			contentStr = s
		}

		res.Message = core.ChatMessage{
			Role:       choice.Message.Role,
			Content:    contentStr,
			ToolCalls:  toolCalls,
			ToolCallID: choice.Message.ToolCallID,
		}
		res.Message.EnsureParts()
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

// OpenAIImageRequest represents input for OpenAI /v1/images/generations.
type OpenAIImageRequest struct {
	Model          string `json:"model,omitempty"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"` // "url" or "b64_json"
}

// OpenAIImageResponse represents output from OpenAI /v1/images/generations.
type OpenAIImageResponse struct {
	Created int64                 `json:"created"`
	Data    []OpenAIImageDataItem `json:"data"`
}

// OpenAIImageDataItem represents an image entry in OpenAIImageResponse.
type OpenAIImageDataItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// OpenAISpeechRequest represents input for OpenAI /v1/audio/speech.
type OpenAISpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"` // "mp3", "opus", "aac", "flac", "wav", "pcm"
	Speed          float64 `json:"speed,omitempty"`
}
