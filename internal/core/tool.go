package core

// ToolDefinition defines a callable capability for models.
type ToolDefinition struct {
	ID               string               `json:"id" yaml:"id"`
	Name             string               `json:"name" yaml:"name"`
	Description      string               `json:"description" yaml:"description"`
	Version          int                  `json:"version" yaml:"version"`
	SideEffect       string               `json:"side_effect" yaml:"side_effect"` // "read", "write"
	InputSchema      map[string]any       `json:"input_schema" yaml:"input_schema"`
	IdentityBindings map[string]string    `json:"identity_bindings,omitempty" yaml:"identity_bindings,omitempty"`
	Executor         ExecutorConfig       `json:"executor" yaml:"executor"`
	DownstreamAuth   DownstreamAuthConfig `json:"downstream_auth,omitempty" yaml:"downstream_auth,omitempty"`
	Confirmation     string               `json:"confirmation" yaml:"confirmation"` // "none", "required"
	Authorization    string               `json:"authorization,omitempty" yaml:"authorization,omitempty"`
	ParallelSafe     bool                 `json:"parallel_safe,omitempty" yaml:"parallel_safe,omitempty"`
	Result           ResultConfig         `json:"result,omitempty" yaml:"result,omitempty"`
	Cache            CacheConfig          `json:"cache,omitempty" yaml:"cache,omitempty"`
}

// ExecutorConfig describes the tool executor type and parameters.
type ExecutorConfig struct {
	Type          string            `json:"type" yaml:"type"` // "http", "webhook", "mcp", "sql_template", "client_side", "wasm"
	Method        string            `json:"method,omitempty" yaml:"method,omitempty"`
	URL           string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	TimeoutMS     int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Retries       int               `json:"retries,omitempty" yaml:"retries,omitempty"`
	Template      string            `json:"template,omitempty" yaml:"template,omitempty"`
	Transport     string            `json:"transport,omitempty" yaml:"transport,omitempty"` // "http", "sse", "stdio" for mcp
	Command       string            `json:"command,omitempty" yaml:"command,omitempty"`     // stdio command
	Args          []string          `json:"args,omitempty" yaml:"args,omitempty"`           // stdio command args
	DiscoveryMode string            `json:"discovery_mode,omitempty" yaml:"discovery_mode,omitempty"` // "full", "on_demand"
	WASM          WASMConfig        `json:"wasm,omitempty" yaml:"wasm,omitempty"`
}

// WASMConfig describes sandboxed WebAssembly execution settings.
type WASMConfig struct {
	BinaryBase64 string `json:"binary_base64,omitempty" yaml:"binary_base64,omitempty"`
	BinaryURL    string `json:"binary_url,omitempty" yaml:"binary_url,omitempty"`
	Entrypoint   string `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"` // default "_start" or "run"
	MaxMemoryMB  int    `json:"max_memory_mb,omitempty" yaml:"max_memory_mb,omitempty"` // default 16MB
}

// DownstreamAuthConfig specifies how caller credentials are propagated.
type DownstreamAuthConfig struct {
	Mode         string `json:"mode" yaml:"mode"` // "forward_user_token", "token_exchange", "service_token", "signed_headers"
	SecretRef    string `json:"secret_ref,omitempty" yaml:"secret_ref,omitempty"`
	HeaderName   string `json:"header_name,omitempty" yaml:"header_name,omitempty"`
	TokenService string `json:"token_service,omitempty" yaml:"token_service,omitempty"`
}

// ResultConfig defines post-processing on tool execution output.
type ResultConfig struct {
	MaxBytes  int      `json:"max_bytes,omitempty" yaml:"max_bytes,omitempty"`
	Redact    []string `json:"redact,omitempty" yaml:"redact,omitempty"`
	Transform string   `json:"transform,omitempty" yaml:"transform,omitempty"` // JSONPath / JMESPath
}

// ToolCall represents a specific tool invocation requested by an LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

// ToolResult represents the output of a tool invocation.
type ToolResult struct {
	CallID   string         `json:"call_id"`
	ToolID   string         `json:"tool_id"`
	Content  string         `json:"content"`
	IsError  bool           `json:"is_error"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
