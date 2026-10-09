package core

import (
	"context"
	"io"
	"time"
)

// Stage defines the contract for an individual pipeline stage in the runtime plane.
type Stage interface {
	Handle(ctx context.Context, state *ReqState) error
}

// EmbedRequest represents input for text embedding generation.
type EmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbedResponse represents output containing vector embeddings.
type EmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
	Usage      UsageStats  `json:"usage"`
}

// ProviderAdapter defines the abstraction over AI model providers (LiteLLM, native adapters).
type ProviderAdapter interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
	Health(ctx context.Context) error
}

// Executor defines the contract for executing a tool call (HTTP, MCP, SQL template, etc.).
type Executor interface {
	Execute(ctx context.Context, call ToolCall, id Identity) (ToolResult, error)
}

// AuthProvider validates end-user identity tokens and extracts normalized identities.
type AuthProvider interface {
	ValidateToken(ctx context.Context, projectID, token string) (*Identity, error)
}

// PolicyEvaluator evaluates CEL policies against caller identity and request metadata.
type PolicyEvaluator interface {
	Evaluate(ctx context.Context, policyID string, id *Identity, meta map[string]any) (bool, error)
}

// LimitStore manages rate limiting counters and token quotas.
type LimitStore interface {
	CheckAndDebit(ctx context.Context, key string, rules []LimitRule, requestedTokens int64) (allowed bool, retryAfter time.Duration, err error)
	ReconcileTokens(ctx context.Context, key string, estimatedTokens, actualTokens int64) error
}

// Authenticator attaches credentials to outbound downstream tool requests.
type Authenticator interface {
	Authenticate(ctx context.Context, req any, id *Identity, cfg DownstreamAuthConfig) error
}

// MemoryProvider manages user-scoped semantic memory storage, recall, and vector similarity search.
type MemoryProvider interface {
	Store(ctx context.Context, record MemoryRecord) error
	Recall(ctx context.Context, filter MemoryFilter) ([]MemoryRecord, error)
	Search(ctx context.Context, query MemorySearchQuery) ([]MemorySearchResult, error)
	DeleteBySubject(ctx context.Context, projectID, issuer, subject string) error
}

// ThreadStore manages server-side conversation sessions with TTL, message history, and thread deletion.
type ThreadStore interface {
	GetThread(ctx context.Context, projectID, env, threadID string) (*Thread, error)
	CreateThread(ctx context.Context, thread *Thread) error
	AppendMessages(ctx context.Context, projectID, env, threadID string, messages []ChatMessage, ttl time.Duration) error
	DeleteThread(ctx context.Context, projectID, env, threadID string) error
	DeleteThreadsBySubject(ctx context.Context, projectID, env, issuer, subject string) error
}

// Guardrail checks inputs and outputs for security and safety.
type Guardrail interface {
	CheckInput(ctx context.Context, text string) error
	CheckOutput(ctx context.Context, text string) error
}

// UsageStore persists usage events and request logs.
type UsageStore interface {
	RecordUsage(ctx context.Context, state *ReqState) error
}

// KeyProvider provides the master key for envelope encryption.
type KeyProvider interface {
	GetMasterKey(ctx context.Context) ([]byte, error)
}

// MediaStore manages media assets (images, audio, video) stored for multimodal message turns.
type MediaStore interface {
	Put(ctx context.Context, projectID string, data io.Reader, mimeType string, filename string) (*MediaRef, error)
	Get(ctx context.Context, projectID, mediaID string) (io.ReadCloser, *MediaRef, error)
	SignedURL(ctx context.Context, projectID, mediaID string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, projectID, mediaID string) error
}

// CacheStore handles ephemeral caching for responses and tool results.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
