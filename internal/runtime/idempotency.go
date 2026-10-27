package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrIdempotencyConflict indicates a duplicate request is currently executing in-progress.
	ErrIdempotencyConflict = errors.New("idempotent request in progress")
	// ErrPayloadMismatch indicates a request with the same idempotency key was sent with a different payload hash.
	ErrPayloadMismatch = errors.New("idempotency key payload mismatch")
)

const (
	// DefaultIdempotencyTTL is the default retention period for idempotency keys (24 hours).
	DefaultIdempotencyTTL = 24 * time.Hour

	// StatusInProgress indicates the request is currently being handled.
	StatusInProgress = "in_progress"
	// StatusCompleted indicates the request completed and returned a response.
	StatusCompleted = "completed"
)

// IdempotencyRecord represents the stored state machine state for a unique Idempotency-Key.
type IdempotencyRecord struct {
	Status      string    `json:"status"` // "in_progress", "completed"
	RequestID   string    `json:"req_id"`
	PayloadHash string    `json:"payload_hash,omitempty"`
	Payload     string    `json:"payload,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// FormatIdempotencyKey formats the canonical Redis key: idem:{proj}:{env}:{app}:{subj}:{key}.
func FormatIdempotencyKey(proj, env, app, subj, key string) string {
	if subj == "" {
		subj = "anonymous"
	}
	return fmt.Sprintf("idem:%s:%s:%s:%s:%s", proj, env, app, subj, key)
}

// IdempotencyStore defines the atomic state machine transitions for request idempotency.
type IdempotencyStore interface {
	Acquire(ctx context.Context, proj, env, app, subj, key, reqID, payloadHash string, ttl time.Duration) (*IdempotencyRecord, bool, error)
	Complete(ctx context.Context, proj, env, app, subj, key, reqID, payload string, ttl time.Duration) error
	Release(ctx context.Context, proj, env, app, subj, key string) error
}

// Redis Lua script to atomically check and set in_progress state with payload hash verification
var acquireScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
    local rec = cjson.encode({
        status = ARGV[1],
        req_id = ARGV[2],
        payload_hash = ARGV[3],
        created_at = ARGV[4]
    })
    redis.call("SET", KEYS[1], rec, "EX", tonumber(ARGV[5]))
    return { "acquired", rec }
end

local existing = cjson.decode(current)
if existing.payload_hash and ARGV[3] ~= "" and existing.payload_hash ~= ARGV[3] then
    return { "mismatch", current }
end

return { "exists", current }
`)

// Redis Lua script to atomically transition to completed state, validating matching req_id
var completeScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
    return redis.error_reply("idempotency record not found")
end

local existing = cjson.decode(current)
if existing.req_id ~= ARGV[2] then
    return redis.error_reply("idempotency request_id mismatch")
end

local rec = cjson.encode({
    status = ARGV[1],
    req_id = ARGV[2],
    payload_hash = existing.payload_hash,
    payload = ARGV[3],
    created_at = existing.created_at,
    completed_at = ARGV[4]
})
redis.call("SET", KEYS[1], rec, "EX", tonumber(ARGV[5]))
return "ok"
`)

// RedisIdempotencyStore implements IdempotencyStore backed by Redis Lua scripts.
type RedisIdempotencyStore struct {
	client redis.UniversalClient
}

// NewRedisIdempotencyStore creates a new RedisIdempotencyStore.
func NewRedisIdempotencyStore(client redis.UniversalClient) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{client: client}
}

func (s *RedisIdempotencyStore) Acquire(ctx context.Context, proj, env, app, subj, key, reqID, payloadHash string, ttl time.Duration) (*IdempotencyRecord, bool, error) {
	if ttl <= 0 {
		ttl = DefaultIdempotencyTTL
	}
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ttlSec := int(ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 86400
	}

	res, err := acquireScript.Run(ctx, s.client, []string{redisKey}, StatusInProgress, reqID, payloadHash, now, ttlSec).Result()
	if err != nil {
		return nil, false, fmt.Errorf("redis acquire idempotency key: %w", err)
	}

	arr, ok := res.([]any)
	if !ok || len(arr) < 2 {
		return nil, false, errors.New("invalid lua acquire response format")
	}

	action, _ := arr[0].(string)
	jsonStr, _ := arr[1].(string)

	var rec IdempotencyRecord
	if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
		return nil, false, fmt.Errorf("unmarshal idempotency record: %w", err)
	}

	if action == "mismatch" {
		return &rec, false, ErrPayloadMismatch
	}

	return &rec, action == "acquired", nil
}

func (s *RedisIdempotencyStore) Complete(ctx context.Context, proj, env, app, subj, key, reqID, payload string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultIdempotencyTTL
	}
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ttlSec := int(ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 86400
	}

	_, err := completeScript.Run(ctx, s.client, []string{redisKey}, StatusCompleted, reqID, payload, now, ttlSec).Result()
	if err != nil {
		return fmt.Errorf("redis complete idempotency key: %w", err)
	}
	return nil
}

func (s *RedisIdempotencyStore) Release(ctx context.Context, proj, env, app, subj, key string) error {
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)
	return s.client.Del(ctx, redisKey).Err()
}

// MemoryIdempotencyStore provides an in-memory thread-safe implementation of IdempotencyStore.
type MemoryIdempotencyStore struct {
	mu      sync.RWMutex
	records map[string]memoryIdemItem
}

type memoryIdemItem struct {
	record    IdempotencyRecord
	expiresAt time.Time
}

// NewMemoryIdempotencyStore creates an in-memory idempotency store.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{
		records: make(map[string]memoryIdemItem),
	}
}

func (m *MemoryIdempotencyStore) Acquire(_ context.Context, proj, env, app, subj, key, reqID, payloadHash string, ttl time.Duration) (*IdempotencyRecord, bool, error) {
	if ttl <= 0 {
		ttl = DefaultIdempotencyTTL
	}
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)

	m.mu.Lock()
	defer m.mu.Unlock()

	item, exists := m.records[redisKey]
	if exists && time.Now().Before(item.expiresAt) {
		cp := item.record
		if cp.PayloadHash != "" && payloadHash != "" && cp.PayloadHash != payloadHash {
			return &cp, false, ErrPayloadMismatch
		}
		return &cp, false, nil
	}

	rec := IdempotencyRecord{
		Status:      StatusInProgress,
		RequestID:   reqID,
		PayloadHash: payloadHash,
		CreatedAt:   time.Now().UTC(),
	}

	m.records[redisKey] = memoryIdemItem{
		record:    rec,
		expiresAt: time.Now().Add(ttl),
	}

	return &rec, true, nil
}

func (m *MemoryIdempotencyStore) Complete(_ context.Context, proj, env, app, subj, key, reqID, payload string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultIdempotencyTTL
	}
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)

	m.mu.Lock()
	defer m.mu.Unlock()

	item, exists := m.records[redisKey]
	if !exists || time.Now().After(item.expiresAt) {
		return errors.New("idempotency record not found")
	}

	if item.record.RequestID != reqID {
		return errors.New("idempotency request_id mismatch")
	}

	rec := IdempotencyRecord{
		Status:      StatusCompleted,
		RequestID:   reqID,
		PayloadHash: item.record.PayloadHash,
		Payload:     payload,
		CreatedAt:   item.record.CreatedAt,
		CompletedAt: time.Now().UTC(),
	}

	m.records[redisKey] = memoryIdemItem{
		record:    rec,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

func (m *MemoryIdempotencyStore) Release(_ context.Context, proj, env, app, subj, key string) error {
	redisKey := FormatIdempotencyKey(proj, env, app, subj, key)

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.records, redisKey)
	return nil
}
