package core

import "time"

// ReqState holds the mutable request context as it flows through the pipeline stages.
type ReqState struct {
	RequestID     string           `json:"request_id" yaml:"request_id"`
	ProjectID     string           `json:"project_id" yaml:"project_id"`
	Environment   string           `json:"environment" yaml:"environment"`
	AIAppID       string           `json:"ai_app_id" yaml:"ai_app_id"`
	RawToken      string           `json:"-" yaml:"-"`
	Identity      *Identity        `json:"identity,omitempty" yaml:"identity,omitempty"`
	Snapshot      *Snapshot        `json:"-" yaml:"-"`
	AIApp         *AIApp           `json:"-" yaml:"-"`
	SelectedModel string           `json:"selected_model,omitempty" yaml:"selected_model,omitempty"`
	ResolvedModel *ModelRouting    `json:"-" yaml:"-"`
	ResolvedAdapter ProviderAdapter `json:"-" yaml:"-"`
	ChatRequest   *ChatRequest     `json:"chat_request,omitempty" yaml:"chat_request,omitempty"`
	ChatResponse  *ChatResponse    `json:"chat_response,omitempty" yaml:"chat_response,omitempty"`
	StreamChan    chan StreamEvent `json:"-" yaml:"-"`
	IsStreaming   bool             `json:"is_streaming" yaml:"is_streaming"`
	Usage         UsageStats       `json:"usage" yaml:"usage"`
	Error         *ErrorResponse   `json:"error,omitempty" yaml:"error,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	StartTime     time.Time        `json:"start_time" yaml:"start_time"`
}

// ChatMessage represents a single message turn in a chat conversation.
type ChatMessage struct {
	Role       string     `json:"role" yaml:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content" yaml:"content"`
	Parts      []Part     `json:"parts,omitempty" yaml:"parts,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty" yaml:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty" yaml:"tool_call_id,omitempty"`
}

// Attachment represents an uploaded file or media attachment.
type Attachment struct {
	Type     string `json:"type" yaml:"type"`           // "image", "audio", "video", "document"
	MimeType string `json:"mime_type" yaml:"mime_type"` // e.g. "image/png"
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Data     []byte `json:"data,omitempty" yaml:"data,omitempty"`
	Digest   string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

// ChatRequest represents the input to a chat completion or stream.
type ChatRequest struct {
	ThreadID         string            `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
	Model            string            `json:"model,omitempty" yaml:"model,omitempty"`
	Messages         []ChatMessage     `json:"messages" yaml:"messages"`
	Attachments      []Attachment      `json:"attachments,omitempty" yaml:"attachments,omitempty"`
	Tools            []ToolDefinition  `json:"tools,omitempty" yaml:"tools,omitempty"`
	Temperature      *float32          `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Stream           bool              `json:"stream,omitempty" yaml:"stream,omitempty"`
	ResponseSchema   string            `json:"response_schema,omitempty" yaml:"response_schema,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	OutputModalities []string          `json:"output_modalities,omitempty" yaml:"output_modalities,omitempty"`
}

// ChatResponse represents the output of a synchronous chat request.
type ChatResponse struct {
	ID           string      `json:"id" yaml:"id"`
	RequestID    string      `json:"request_id" yaml:"request_id"`
	AIAppID      string      `json:"ai_app_id" yaml:"ai_app_id"`
	ThreadID     string      `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
	Model        string      `json:"model,omitempty" yaml:"model,omitempty"`
	CreatedAt    int64       `json:"created_at" yaml:"created_at"`
	Message      ChatMessage `json:"message" yaml:"message"`
	FinishReason string      `json:"finish_reason" yaml:"finish_reason"` // "stop", "tool_calls", "length"
	Usage        UsageStats  `json:"usage" yaml:"usage"`
}

// StreamEvent represents a single event chunk pushed over Server-Sent Events.
type StreamEvent struct {
	Type              string         `json:"event" yaml:"event"` // "delta", "tool_call", "confirmation", "usage", "error", "done"
	Delta             string         `json:"delta,omitempty" yaml:"delta,omitempty"`
	ToolCall          *ToolCall      `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`
	ConfirmationToken string         `json:"confirmation_token,omitempty" yaml:"confirmation_token,omitempty"`
	Usage             *UsageStats    `json:"usage,omitempty" yaml:"usage,omitempty"`
	Error             *ErrorResponse `json:"error,omitempty" yaml:"error,omitempty"`
	RequestID         string         `json:"request_id,omitempty" yaml:"request_id,omitempty"`
	ThreadID          string         `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
	Model             string         `json:"model,omitempty" yaml:"model,omitempty"`
}

// UsageStats tracks token consumption, media generation units, and estimated costs.
type UsageStats struct {
	InputTokens  int      `json:"input_tokens" yaml:"input_tokens"`
	OutputTokens int      `json:"output_tokens" yaml:"output_tokens"`
	TotalTokens  int      `json:"total_tokens" yaml:"total_tokens"`
	Images       int      `json:"images,omitempty" yaml:"images,omitempty"`
	AudioSeconds float64  `json:"audio_seconds,omitempty" yaml:"audio_seconds,omitempty"`
	VideoSeconds float64  `json:"video_seconds,omitempty" yaml:"video_seconds,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty" yaml:"cost_usd,omitempty"`
}

// ErrorResponse represents the RFC 7807 compliant standardized error taxonomy.
type ErrorResponse struct {
	Code       string         `json:"code" yaml:"code"`
	Message    string         `json:"message" yaml:"message"`
	RequestID  string         `json:"request_id,omitempty" yaml:"request_id,omitempty"`
	RetryAfter int            `json:"retry_after,omitempty" yaml:"retry_after,omitempty"`
	Details    map[string]any `json:"details,omitempty" yaml:"details,omitempty"`
}

func (e *ErrorResponse) Error() string {
	return e.Message
}
