package usage

import (
	"time"
)

// ToolCallSummary records metadata about a tool execution without raw untrusted payloads.
type ToolCallSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"` // "success", "error", "cancelled"
	LatencyMS int64  `json:"latency_ms"`
}

// LogEvent represents an operational request metadata log entry.
type LogEvent struct {
	ID              string            `json:"id"`
	RequestID       string            `json:"request_id"`
	ProjectID       string            `json:"project_id"`
	Environment     string            `json:"environment"`
	AIAppID         string            `json:"ai_app_id"`
	UserID          string            `json:"user_id"`
	UserSubject     string            `json:"user_subject"`
	UserIssuer      string            `json:"user_issuer"`
	LogicalModel    string            `json:"logical_model"`
	Provider        string            `json:"provider"`
	ModelDeployment string            `json:"model_deployment"`
	Status          string            `json:"status"` // "success", "error", "cancelled", "rate_limited"
	StatusCode      int               `json:"status_code"`
	LatencyMS       int64             `json:"latency_ms"`
	InputTokens     int               `json:"input_tokens"`
	OutputTokens    int               `json:"output_tokens"`
	TotalTokens     int               `json:"total_tokens"`
	CostUSD         float64           `json:"cost_usd"`
	IsStreaming     bool              `json:"is_streaming"`
	ToolCalls       []ToolCallSummary `json:"tool_calls"`
	ErrorCode       string            `json:"error_code"`
	ErrorMessage    string            `json:"error_message"`
	Metadata        map[string]any    `json:"metadata"`
	CreatedAt       time.Time         `json:"created_at"`
}

// Event represents a fine-grained token usage and billing event.
type Event struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"request_id"`
	ProjectID    string    `json:"project_id"`
	Environment  string    `json:"environment"`
	AIAppID      string    `json:"ai_app_id"`
	UserID       string    `json:"user_id"`
	LogicalModel string    `json:"logical_model"`
	Provider     string    `json:"provider"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	CreatedAt    time.Time `json:"created_at"`
}

// Record bundles a log event and/or a usage event for batch processing.
type Record struct {
	Log   *LogEvent
	Usage *Event
}
