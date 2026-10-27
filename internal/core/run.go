package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RunStatus defines the lifecycle state of an asynchronous run.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

var (
	// ErrRunNotFound indicates that the requested run does not exist.
	ErrRunNotFound = errors.New("run not found")
	// ErrRunAlreadyExists indicates that a run with the given ID already exists.
	ErrRunAlreadyExists = errors.New("run already exists")
)

// Run represents an asynchronous invocation execution lifecycle.
type Run struct {
	ID          string         `json:"id" yaml:"id"`
	ProjectID   string         `json:"project_id" yaml:"project_id"`
	Environment string         `json:"environment" yaml:"environment"`
	AIAppID     string         `json:"ai_app_id" yaml:"ai_app_id"`
	RequestID   string         `json:"request_id" yaml:"request_id"`
	Status      RunStatus      `json:"status" yaml:"status"`
	CreatedAt   time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" yaml:"updated_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
	Error       string         `json:"error,omitempty" yaml:"error,omitempty"`
	Result      *ChatResponse  `json:"result,omitempty" yaml:"result,omitempty"`
	Usage       *UsageStats    `json:"usage,omitempty" yaml:"usage,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// RunStore abstracts persistence for asynchronous runs.
type RunStore interface {
	CreateRun(ctx context.Context, run *Run) error
	GetRun(ctx context.Context, projectID, env, runID string) (*Run, error)
	UpdateRunStatus(ctx context.Context, projectID, env, runID string, status RunStatus, result *ChatResponse, runErr string, usage *UsageStats) error
	CancelRun(ctx context.Context, projectID, env, runID string) error
}

func runStorageKey(projectID, env, runID string) string {
	return fmt.Sprintf("run:%s:%s:%s", projectID, env, runID)
}

// MemoryRunStore is an in-memory thread-safe implementation of RunStore.
type MemoryRunStore struct {
	mu     sync.RWMutex
	runs   map[string]*Run
	lookup map[string]string // run_id -> runStorageKey
}

// NewMemoryRunStore creates a new in-memory run store.
func NewMemoryRunStore() *MemoryRunStore {
	return &MemoryRunStore{
		runs:   make(map[string]*Run),
		lookup: make(map[string]string),
	}
}

// CreateRun persists a new run in memory.
func (m *MemoryRunStore) CreateRun(_ context.Context, run *Run) error {
	if run == nil || run.ID == "" {
		return errors.New("nil run or empty run id")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := runStorageKey(run.ProjectID, run.Environment, run.ID)
	if _, exists := m.runs[key]; exists {
		return ErrRunAlreadyExists
	}

	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}

	m.runs[key] = cloneRun(run)
	m.lookup[run.ID] = key
	return nil
}

// GetRun retrieves a run by project, environment, and runID.
// If projectID or env is empty, it attempts to resolve via secondary runID index.
func (m *MemoryRunStore) GetRun(_ context.Context, projectID, env, runID string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var key string
	if projectID != "" && env != "" {
		key = runStorageKey(projectID, env, runID)
	} else {
		resolvedKey, ok := m.lookup[runID]
		if !ok {
			return nil, ErrRunNotFound
		}
		key = resolvedKey
	}

	run, ok := m.runs[key]
	if !ok || run == nil {
		return nil, ErrRunNotFound
	}

	return cloneRun(run), nil
}

// UpdateRunStatus updates the state, result, error, and usage of a run.
func (m *MemoryRunStore) UpdateRunStatus(_ context.Context, projectID, env, runID string, status RunStatus, result *ChatResponse, runErr string, usage *UsageStats) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var key string
	if projectID != "" && env != "" {
		key = runStorageKey(projectID, env, runID)
	} else {
		resolvedKey, ok := m.lookup[runID]
		if !ok {
			return ErrRunNotFound
		}
		key = resolvedKey
	}

	run, ok := m.runs[key]
	if !ok || run == nil {
		return ErrRunNotFound
	}

	now := time.Now().UTC()
	run.Status = status
	run.UpdatedAt = now
	if runErr != "" {
		run.Error = runErr
	}
	if result != nil {
		run.Result = result
	}
	if usage != nil {
		run.Usage = usage
	}
	if status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled {
		if run.CompletedAt == nil {
			run.CompletedAt = &now
		}
	}

	return nil
}

// CancelRun transitions an active run to cancelled.
func (m *MemoryRunStore) CancelRun(_ context.Context, projectID, env, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var key string
	if projectID != "" && env != "" {
		key = runStorageKey(projectID, env, runID)
	} else {
		resolvedKey, ok := m.lookup[runID]
		if !ok {
			return ErrRunNotFound
		}
		key = resolvedKey
	}

	run, ok := m.runs[key]
	if !ok || run == nil {
		return ErrRunNotFound
	}

	if run.Status == RunStatusCompleted || run.Status == RunStatusFailed || run.Status == RunStatusCancelled {
		return nil // already terminal
	}

	now := time.Now().UTC()
	run.Status = RunStatusCancelled
	run.UpdatedAt = now
	run.CompletedAt = &now
	return nil
}

func cloneRun(src *Run) *Run {
	if src == nil {
		return nil
	}
	dst := *src
	if src.CompletedAt != nil {
		t := *src.CompletedAt
		dst.CompletedAt = &t
	}
	if src.Result != nil {
		resCopy := *src.Result
		dst.Result = &resCopy
	}
	if src.Usage != nil {
		uCopy := *src.Usage
		dst.Usage = &uCopy
	}
	if src.Metadata != nil {
		dst.Metadata = make(map[string]any, len(src.Metadata))
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	return &dst
}

// RedisRunStore is a Redis-backed implementation of RunStore.
type RedisRunStore struct {
	client redis.UniversalClient
	ttl    time.Duration
}

const (
	// DefaultRunTTL is the default retention time for runs in Redis (7 days).
	DefaultRunTTL = 7 * 24 * time.Hour
)

// NewRedisRunStore creates a new RedisRunStore.
func NewRedisRunStore(client redis.UniversalClient, ttl time.Duration) *RedisRunStore {
	if ttl <= 0 {
		ttl = DefaultRunTTL
	}
	return &RedisRunStore{
		client: client,
		ttl:    ttl,
	}
}

func runLookupKey(runID string) string {
	return fmt.Sprintf("run:lookup:%s", runID)
}

// CreateRun stores the run in Redis with the configured TTL.
func (r *RedisRunStore) CreateRun(ctx context.Context, run *Run) error {
	if run == nil || run.ID == "" {
		return errors.New("nil run or empty run id")
	}

	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}

	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}

	key := runStorageKey(run.ProjectID, run.Environment, run.ID)
	// SET key data NX EX ttl
	ok, err := r.client.SetNX(ctx, key, data, r.ttl).Result()
	if err != nil {
		return fmt.Errorf("redis setnx run: %w", err)
	}
	if !ok {
		return ErrRunAlreadyExists
	}

	// Secondary lookup key
	lookupVal := fmt.Sprintf("%s:%s", run.ProjectID, run.Environment)
	_ = r.client.Set(ctx, runLookupKey(run.ID), lookupVal, r.ttl).Err()

	return nil
}

// GetRun retrieves a run from Redis.
func (r *RedisRunStore) GetRun(ctx context.Context, projectID, env, runID string) (*Run, error) {
	key := runStorageKey(projectID, env, runID)
	if projectID == "" || env == "" {
		lookupVal, err := r.client.Get(ctx, runLookupKey(runID)).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return nil, ErrRunNotFound
			}
			return nil, fmt.Errorf("redis get lookup: %w", err)
		}
		var p, e string
		if _, scanErr := fmt.Sscanf(lookupVal, "%s:%s", &p, &e); scanErr == nil && p != "" && e != "" {
			key = runStorageKey(p, e, runID)
		} else {
			return nil, ErrRunNotFound
		}
	}

	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("redis get run: %w", err)
	}

	var run Run
	if err := json.Unmarshal([]byte(val), &run); err != nil {
		return nil, fmt.Errorf("unmarshal run: %w", err)
	}

	return &run, nil
}

const updateRunScript = `
local key = KEYS[1]
local raw = redis.call('GET', key)
if not raw then
	return redis.error_reply('ERR_NOT_FOUND')
end
local run = cjson.decode(raw)
run['status'] = ARGV[1]
run['updated_at'] = ARGV[2]
if ARGV[3] ~= '' then
	run['completed_at'] = ARGV[3]
end
if ARGV[4] ~= '' then
	run['error'] = ARGV[4]
end
if ARGV[5] ~= '' then
	run['result'] = cjson.decode(ARGV[5])
end
if ARGV[6] ~= '' then
	run['usage'] = cjson.decode(ARGV[6])
end
local updated = cjson.encode(run)
redis.call('SET', key, updated, 'KEEPTTL')
return 'OK'
`

// UpdateRunStatus updates the run in Redis using Lua for atomicity.
func (r *RedisRunStore) UpdateRunStatus(ctx context.Context, projectID, env, runID string, status RunStatus, result *ChatResponse, runErr string, usage *UsageStats) error {
	key := runStorageKey(projectID, env, runID)
	if projectID == "" || env == "" {
		resolved, err := r.GetRun(ctx, "", "", runID)
		if err != nil {
			return err
		}
		key = runStorageKey(resolved.ProjectID, resolved.Environment, runID)
	}

	now := time.Now().UTC()
	nowStr, _ := now.MarshalText()
	completedAtStr := ""
	if status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled {
		completedAtStr = string(nowStr)
	}

	resultJSON := ""
	if result != nil {
		b, _ := json.Marshal(result)
		resultJSON = string(b)
	}

	usageJSON := ""
	if usage != nil {
		b, _ := json.Marshal(usage)
		usageJSON = string(b)
	}

	res, err := r.client.Eval(ctx, updateRunScript, []string{key},
		string(status),
		string(nowStr),
		completedAtStr,
		runErr,
		resultJSON,
		usageJSON,
	).Result()

	if err != nil {
		if errors.Is(err, redis.Nil) || (err != nil && err.Error() == "ERR_NOT_FOUND") {
			return ErrRunNotFound
		}
		return fmt.Errorf("eval update run status: %w", err)
	}

	if str, ok := res.(string); ok && str == "OK" {
		return nil
	}
	return nil
}

const cancelRunScript = `
local key = KEYS[1]
local raw = redis.call('GET', key)
if not raw then
	return redis.error_reply('ERR_NOT_FOUND')
end
local run = cjson.decode(raw)
if run['status'] == 'completed' or run['status'] == 'failed' or run['status'] == 'cancelled' then
	return 'ALREADY_TERMINAL'
end
run['status'] = 'cancelled'
run['updated_at'] = ARGV[1]
run['completed_at'] = ARGV[1]
redis.call('SET', key, cjson.encode(run), 'KEEPTTL')
return 'OK'
`

// CancelRun atomically marks a run as cancelled in Redis.
func (r *RedisRunStore) CancelRun(ctx context.Context, projectID, env, runID string) error {
	key := runStorageKey(projectID, env, runID)
	if projectID == "" || env == "" {
		resolved, err := r.GetRun(ctx, "", "", runID)
		if err != nil {
			return err
		}
		key = runStorageKey(resolved.ProjectID, resolved.Environment, runID)
	}

	now := time.Now().UTC()
	nowStr, _ := now.MarshalText()

	res, err := r.client.Eval(ctx, cancelRunScript, []string{key}, string(nowStr)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || (err != nil && err.Error() == "ERR_NOT_FOUND") {
			return ErrRunNotFound
		}
		return fmt.Errorf("eval cancel run: %w", err)
	}

	_ = res
	return nil
}
