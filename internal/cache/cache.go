package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrKeyNotFound indicates a cache key does not exist or has expired.
	ErrKeyNotFound = errors.New("cache key not found")
)

// PurgeableCacheStore extends core.CacheStore with bulk and subject-level deletion.
type PurgeableCacheStore interface {
	core.CacheStore
	DeletePattern(ctx context.Context, pattern string) error
	DeleteSubjectKeys(ctx context.Context, projectID, env, subject string) error
}

// ResponseCacheKey builds a standardized response cache key.
// Scope can be "shared" or "user" (default).
// Keys follow Architecture §7.3: rc:{project}:{env}:{scope}:{hash}
func ResponseCacheKey(projectID, env, scope, subject, hash string) string {
	if strings.EqualFold(scope, "shared") {
		return fmt.Sprintf("rc:%s:%s:shared:%s", projectID, env, hash)
	}
	if subject == "" {
		subject = "anonymous"
	}
	return fmt.Sprintf("rc:%s:%s:user:%s:%s", projectID, env, subject, hash)
}

// ToolCacheKey builds a standardized tool result cache key.
// Keys follow Architecture §7.3: tc:{project}:{env}:{user}:{tool}:{argshash}
func ToolCacheKey(projectID, env, subject, toolID, argsHash string) string {
	if subject == "" {
		subject = "anonymous"
	}
	return fmt.Sprintf("tc:%s:%s:%s:%s:%s", projectID, env, subject, toolID, argsHash)
}

// HashBytes computes the hex-encoded SHA-256 checksum of raw bytes.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashString computes the hex-encoded SHA-256 checksum of a string.
func HashString(s string) string {
	return HashBytes([]byte(s))
}

// ComputeRequestHash generates a deterministic SHA-256 hash for a ChatRequest.
// It canonicalizes messages (role, content, tool calls), model, temperature, max_tokens, and response schema.
func ComputeRequestHash(req *core.ChatRequest, app *core.AIApp) (string, error) {
	if req == nil {
		return "", errors.New("nil chat request")
	}

	type canonicalToolCall struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}

	type canonicalMessage struct {
		Role       string              `json:"role"`
		Content    string              `json:"content"`
		ToolCalls  []canonicalToolCall `json:"tool_calls,omitempty"`
		ToolCallID string              `json:"tool_call_id,omitempty"`
	}

	messages := make([]canonicalMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		var toolCalls []canonicalToolCall
		if len(m.ToolCalls) > 0 {
			toolCalls = make([]canonicalToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				toolCalls = append(toolCalls, canonicalToolCall{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				})
			}
		}
		messages = append(messages, canonicalMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  toolCalls,
			ToolCallID: m.ToolCallID,
		})
	}

	canonicalPayload := struct {
		Messages       []canonicalMessage `json:"messages"`
		Model          string             `json:"model,omitempty"`
		Temperature    *float32           `json:"temperature,omitempty"`
		MaxTokens      *int               `json:"max_tokens,omitempty"`
		ResponseSchema string             `json:"response_schema,omitempty"`
		PromptVersion  int                `json:"prompt_version,omitempty"`
	}{
		Messages:       messages,
		Model:          req.Model,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		ResponseSchema: req.ResponseSchema,
	}

	if app != nil {
		canonicalPayload.PromptVersion = app.PromptVersion
		if canonicalPayload.ResponseSchema == "" {
			canonicalPayload.ResponseSchema = app.ResponseSchema
		}
	}

	b, err := json.Marshal(canonicalPayload)
	if err != nil {
		return "", fmt.Errorf("marshal canonical request payload: %w", err)
	}

	return HashBytes(b), nil
}

// ComputeToolArgsHash generates a deterministic SHA-256 hash for tool arguments.
func ComputeToolArgsHash(args string) string {
	var normalized any
	if err := json.Unmarshal([]byte(args), &normalized); err != nil {
		return HashString(args)
	}
	canonicalBytes, err := json.Marshal(normalized)
	if err != nil {
		return HashString(args)
	}
	return HashBytes(canonicalBytes)
}

// --- Memory Cache Store ---

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// MemoryCacheStore provides a concurrency-safe in-memory cache with TTL expiration.
type MemoryCacheStore struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
}

// NewMemoryCacheStore creates a new MemoryCacheStore.
func NewMemoryCacheStore() *MemoryCacheStore {
	return &MemoryCacheStore{
		entries: make(map[string]memoryEntry),
	}
}

// Get retrieves cached bytes for a key. Returns nil, nil if expired or not found.
func (m *MemoryCacheStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	entry, exists := m.entries[key]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		m.mu.Lock()
		delete(m.entries, key)
		m.mu.Unlock()
		return nil, nil
	}

	res := make([]byte, len(entry.value))
	copy(res, entry.value)
	return res, nil
}

// Set stores bytes for a key with the given TTL.
func (m *MemoryCacheStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}

	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	m.entries[key] = memoryEntry{
		value:     valCopy,
		expiresAt: exp,
	}
	return nil
}

// Delete removes a key from the cache.
func (m *MemoryCacheStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

// DeletePattern removes all keys matching the glob pattern.
func (m *MemoryCacheStore) DeletePattern(_ context.Context, pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k := range m.entries {
		matched, err := filepath.Match(pattern, k)
		if err == nil && matched {
			delete(m.entries, k)
		}
	}
	return nil
}

// DeleteSubjectKeys removes all response and tool cache keys for a given subject.
func (m *MemoryCacheStore) DeleteSubjectKeys(ctx context.Context, projectID, env, subject string) error {
	if subject == "" {
		return nil
	}
	// Pattern 1: Response cache rc:{project}:{env}:user:{subject}:*
	p1 := fmt.Sprintf("rc:%s:%s:user:%s:*", projectID, env, subject)
	if err := m.DeletePattern(ctx, p1); err != nil {
		return err
	}
	// Pattern 2: Tool cache tc:{project}:{env}:{subject}:*
	p2 := fmt.Sprintf("tc:%s:%s:%s:*", projectID, env, subject)
	return m.DeletePattern(ctx, p2)
}

// --- Redis Cache Store ---

// RedisCacheStore implements PurgeableCacheStore on top of Redis / Valkey.
type RedisCacheStore struct {
	client *redis.Client
}

// NewRedisCacheStore creates a new RedisCacheStore.
func NewRedisCacheStore(client *redis.Client) *RedisCacheStore {
	return &RedisCacheStore{client: client}
}

// Get retrieves cached bytes for a key from Redis.
func (r *RedisCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	if r.client == nil {
		return nil, errors.New("redis client not configured")
	}

	val, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Cache miss
		}
		return nil, fmt.Errorf("redis get %q: %w", key, err)
	}

	return val, nil
}

// Set stores a key-value pair in Redis with the specified TTL.
func (r *RedisCacheStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if r.client == nil {
		return errors.New("redis client not configured")
	}

	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %q: %w", key, err)
	}
	return nil
}

// Delete removes a key from Redis.
func (r *RedisCacheStore) Delete(ctx context.Context, key string) error {
	if r.client == nil {
		return errors.New("redis client not configured")
	}

	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis del %q: %w", key, err)
	}
	return nil
}

// DeletePattern scans and removes all keys matching the pattern in Redis using SCAN.
func (r *RedisCacheStore) DeletePattern(ctx context.Context, pattern string) error {
	if r.client == nil {
		return errors.New("redis client not configured")
	}

	var (
		cursor uint64
		err    error
	)

	for {
		var keys []string
		keys, cursor, err = r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis scan pattern %q: %w", pattern, err)
		}

		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("redis del keys for pattern %q: %w", pattern, err)
			}
		}

		if cursor == 0 {
			break
		}
	}

	return nil
}

// DeleteSubjectKeys purges all user-scoped response and tool caches for a subject from Redis.
func (r *RedisCacheStore) DeleteSubjectKeys(ctx context.Context, projectID, env, subject string) error {
	if subject == "" {
		return nil
	}

	p1 := fmt.Sprintf("rc:%s:%s:user:%s:*", projectID, env, subject)
	if err := r.DeletePattern(ctx, p1); err != nil {
		return err
	}

	p2 := fmt.Sprintf("tc:%s:%s:%s:*", projectID, env, subject)
	return r.DeletePattern(ctx, p2)
}

// Ensure interface implementations
var (
	_ PurgeableCacheStore = (*MemoryCacheStore)(nil)
	_ PurgeableCacheStore = (*RedisCacheStore)(nil)
)
