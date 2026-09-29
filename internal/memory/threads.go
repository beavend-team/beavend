package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/store/db"
)

var (
	// ErrThreadNotFound indicates the requested conversation thread does not exist or has expired.
	ErrThreadNotFound = errors.New("thread not found or expired")
)

// GenerateThreadID produces a new unique thread identifier with the "th_" prefix.
func GenerateThreadID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "th_" + hex.EncodeToString(b)
}

// ----------------------------------------------------------------------
// 1. MemoryThreadStore (In-Memory)
// ----------------------------------------------------------------------

// MemoryThreadStore stores threads in-memory with TTL expiration.
type MemoryThreadStore struct {
	mu      sync.RWMutex
	threads map[string]*core.Thread
}

// NewMemoryThreadStore creates a new concurrency-safe in-memory thread store.
func NewMemoryThreadStore() *MemoryThreadStore {
	return &MemoryThreadStore{
		threads: make(map[string]*core.Thread),
	}
}

func threadKey(projectID, env, threadID string) string {
	return fmt.Sprintf("%s:%s:%s", projectID, env, threadID)
}

func (s *MemoryThreadStore) GetThread(ctx context.Context, projectID, env, threadID string) (*core.Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := threadKey(projectID, env, threadID)
	th, exists := s.threads[k]
	if !exists {
		return nil, ErrThreadNotFound
	}

	if !th.ExpiresAt.IsZero() && th.ExpiresAt.Before(time.Now().UTC()) {
		return nil, ErrThreadNotFound
	}

	// Deep copy to prevent race conditions
	copied := *th
	copied.Messages = append([]core.ChatMessage(nil), th.Messages...)
	return &copied, nil
}

func (s *MemoryThreadStore) CreateThread(ctx context.Context, thread *core.Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if thread.ID == "" {
		thread.ID = GenerateThreadID()
	}

	now := time.Now().UTC()
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = now

	if thread.TTLSeconds <= 0 {
		thread.TTLSeconds = 86400 // Default: 24h
	}
	if thread.ExpiresAt.IsZero() {
		thread.ExpiresAt = now.Add(time.Duration(thread.TTLSeconds) * time.Second)
	}

	k := threadKey(thread.ProjectID, thread.Environment, thread.ID)
	copied := *thread
	copied.Messages = append([]core.ChatMessage(nil), thread.Messages...)
	s.threads[k] = &copied

	return nil
}

func (s *MemoryThreadStore) AppendMessages(ctx context.Context, projectID, env, threadID string, messages []core.ChatMessage, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := threadKey(projectID, env, threadID)
	th, exists := s.threads[k]
	if !exists {
		return ErrThreadNotFound
	}

	now := time.Now().UTC()
	if !th.ExpiresAt.IsZero() && th.ExpiresAt.Before(now) {
		delete(s.threads, k)
		return ErrThreadNotFound
	}

	th.Messages = append(th.Messages, messages...)
	th.UpdatedAt = now

	if ttl > 0 {
		th.TTLSeconds = int(ttl.Seconds())
		th.ExpiresAt = now.Add(ttl)
	} else if th.TTLSeconds > 0 {
		th.ExpiresAt = now.Add(time.Duration(th.TTLSeconds) * time.Second)
	}

	return nil
}

func (s *MemoryThreadStore) DeleteThread(ctx context.Context, projectID, env, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := threadKey(projectID, env, threadID)
	delete(s.threads, k)
	return nil
}

func (s *MemoryThreadStore) DeleteThreadsBySubject(ctx context.Context, projectID, env, issuer, subject string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, th := range s.threads {
		if th.ProjectID == projectID && th.Environment == env && th.Issuer == issuer && th.Subject == subject {
			delete(s.threads, k)
		}
	}
	return nil
}

// ----------------------------------------------------------------------
// 2. RedisThreadStore (Redis / Valkey)
// ----------------------------------------------------------------------

// RedisThreadStore stores conversation threads in Redis with native TTL.
type RedisThreadStore struct {
	client redis.UniversalClient
}

// NewRedisThreadStore creates a Redis-backed thread store.
func NewRedisThreadStore(client redis.UniversalClient) *RedisThreadStore {
	return &RedisThreadStore{client: client}
}

func redisThreadKey(projectID, env, threadID string) string {
	return fmt.Sprintf("th:%s:%s:%s", projectID, env, threadID)
}

func redisUserThreadsKey(projectID, env, issuer, subject string) string {
	return fmt.Sprintf("th:sub:%s:%s:%s:%s", projectID, env, issuer, subject)
}

func (s *RedisThreadStore) GetThread(ctx context.Context, projectID, env, threadID string) (*core.Thread, error) {
	if s.client == nil {
		return nil, errors.New("redis thread store: nil client")
	}

	key := redisThreadKey(projectID, env, threadID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrThreadNotFound
		}
		return nil, fmt.Errorf("redis get thread: %w", err)
	}

	var th core.Thread
	if err := json.Unmarshal(data, &th); err != nil {
		return nil, fmt.Errorf("redis decode thread: %w", err)
	}

	return &th, nil
}

func (s *RedisThreadStore) CreateThread(ctx context.Context, thread *core.Thread) error {
	if s.client == nil {
		return errors.New("redis thread store: nil client")
	}

	if thread.ID == "" {
		thread.ID = GenerateThreadID()
	}

	now := time.Now().UTC()
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = now

	if thread.TTLSeconds <= 0 {
		thread.TTLSeconds = 86400
	}
	ttl := time.Duration(thread.TTLSeconds) * time.Second
	thread.ExpiresAt = now.Add(ttl)

	data, err := json.Marshal(thread)
	if err != nil {
		return fmt.Errorf("redis encode thread: %w", err)
	}

	key := redisThreadKey(thread.ProjectID, thread.Environment, thread.ID)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, key, data, ttl)

	if thread.Subject != "" {
		subKey := redisUserThreadsKey(thread.ProjectID, thread.Environment, thread.Issuer, thread.Subject)
		pipe.SAdd(ctx, subKey, thread.ID)
		pipe.Expire(ctx, subKey, ttl*2)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis create thread exec: %w", err)
	}

	return nil
}

func (s *RedisThreadStore) AppendMessages(ctx context.Context, projectID, env, threadID string, messages []core.ChatMessage, ttl time.Duration) error {
	th, err := s.GetThread(ctx, projectID, env, threadID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	th.Messages = append(th.Messages, messages...)
	th.UpdatedAt = now

	if ttl <= 0 {
		if th.TTLSeconds > 0 {
			ttl = time.Duration(th.TTLSeconds) * time.Second
		} else {
			ttl = 24 * time.Hour
		}
	}
	th.TTLSeconds = int(ttl.Seconds())
	th.ExpiresAt = now.Add(ttl)

	data, err := json.Marshal(th)
	if err != nil {
		return fmt.Errorf("redis encode thread: %w", err)
	}

	key := redisThreadKey(projectID, env, threadID)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, key, data, ttl)

	if th.Subject != "" {
		subKey := redisUserThreadsKey(projectID, env, th.Issuer, th.Subject)
		pipe.SAdd(ctx, subKey, threadID)
		pipe.Expire(ctx, subKey, ttl*2)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis append thread exec: %w", err)
	}

	return nil
}

func (s *RedisThreadStore) DeleteThread(ctx context.Context, projectID, env, threadID string) error {
	if s.client == nil {
		return errors.New("redis thread store: nil client")
	}

	th, _ := s.GetThread(ctx, projectID, env, threadID)
	key := redisThreadKey(projectID, env, threadID)

	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	if th != nil && th.Subject != "" {
		subKey := redisUserThreadsKey(projectID, env, th.Issuer, th.Subject)
		pipe.SRem(ctx, subKey, threadID)
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisThreadStore) DeleteThreadsBySubject(ctx context.Context, projectID, env, issuer, subject string) error {
	if s.client == nil {
		return errors.New("redis thread store: nil client")
	}

	subKey := redisUserThreadsKey(projectID, env, issuer, subject)
	threadIDs, err := s.client.SMembers(ctx, subKey).Result()
	if err != nil {
		return fmt.Errorf("redis smembers subject threads: %w", err)
	}

	if len(threadIDs) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for _, id := range threadIDs {
		pipe.Del(ctx, redisThreadKey(projectID, env, id))
	}
	pipe.Del(ctx, subKey)

	_, err = pipe.Exec(ctx)
	return err
}

// ----------------------------------------------------------------------
// 3. PostgresThreadStore (Durable PostgreSQL)
// ----------------------------------------------------------------------

// PostgresThreadStore manages conversation threads in PostgreSQL.
type PostgresThreadStore struct {
	queries *db.Queries
}

// NewPostgresThreadStore creates a Postgres-backed thread store.
func NewPostgresThreadStore(queries *db.Queries) *PostgresThreadStore {
	return &PostgresThreadStore{queries: queries}
}

func (s *PostgresThreadStore) GetThread(ctx context.Context, projectID, env, threadID string) (*core.Thread, error) {
	if s.queries == nil {
		return nil, errors.New("postgres thread store: nil queries")
	}

	row, err := s.queries.GetConversationThread(ctx, db.GetConversationThreadParams{
		ProjectID:   projectID,
		Environment: env,
		ID:          threadID,
	})
	if err != nil {
		return nil, ErrThreadNotFound
	}

	var msgs []core.ChatMessage
	if len(row.Messages) > 0 {
		_ = json.Unmarshal(row.Messages, &msgs)
	}

	var meta map[string]any
	if len(row.Metadata) > 0 {
		_ = json.Unmarshal(row.Metadata, &meta)
	}

	return &core.Thread{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		Environment: row.Environment,
		AIAppID:     row.AiAppID,
		Issuer:      row.Issuer,
		Subject:     row.Subject,
		Messages:    msgs,
		Metadata:    meta,
		TTLSeconds:  int(row.TtlSeconds),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
		ExpiresAt:   row.ExpiresAt.Time,
	}, nil
}

func (s *PostgresThreadStore) CreateThread(ctx context.Context, thread *core.Thread) error {
	if s.queries == nil {
		return errors.New("postgres thread store: nil queries")
	}

	if thread.ID == "" {
		thread.ID = GenerateThreadID()
	}

	now := time.Now().UTC()
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = now

	if thread.TTLSeconds <= 0 {
		thread.TTLSeconds = 86400
	}
	expiresAt := now.Add(time.Duration(thread.TTLSeconds) * time.Second)
	thread.ExpiresAt = expiresAt

	msgsBytes, _ := json.Marshal(thread.Messages)
	metaBytes, _ := json.Marshal(thread.Metadata)

	_, err := s.queries.CreateConversationThread(ctx, db.CreateConversationThreadParams{
		ID:          thread.ID,
		ProjectID:   thread.ProjectID,
		Environment: thread.Environment,
		AiAppID:     thread.AIAppID,
		Issuer:      thread.Issuer,
		Subject:     thread.Subject,
		Messages:    msgsBytes,
		Metadata:    metaBytes,
		TtlSeconds:  int32(thread.TTLSeconds),
		ExpiresAt: pgtype.Timestamptz{
			Time:  expiresAt,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("postgres create thread: %w", err)
	}

	return nil
}

func (s *PostgresThreadStore) AppendMessages(ctx context.Context, projectID, env, threadID string, messages []core.ChatMessage, ttl time.Duration) error {
	th, err := s.GetThread(ctx, projectID, env, threadID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	th.Messages = append(th.Messages, messages...)
	th.UpdatedAt = now

	if ttl > 0 {
		th.TTLSeconds = int(ttl.Seconds())
	}
	expiresAt := now.Add(time.Duration(th.TTLSeconds) * time.Second)
	th.ExpiresAt = expiresAt

	msgsBytes, _ := json.Marshal(th.Messages)
	metaBytes, _ := json.Marshal(th.Metadata)

	_, err = s.queries.UpdateConversationThreadMessages(ctx, db.UpdateConversationThreadMessagesParams{
		ProjectID:   projectID,
		Environment: env,
		ID:          threadID,
		Messages:    msgsBytes,
		Metadata:    metaBytes,
		ExpiresAt: pgtype.Timestamptz{
			Time:  expiresAt,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("postgres append messages: %w", err)
	}

	return nil
}

func (s *PostgresThreadStore) DeleteThread(ctx context.Context, projectID, env, threadID string) error {
	if s.queries == nil {
		return errors.New("postgres thread store: nil queries")
	}

	return s.queries.DeleteConversationThread(ctx, db.DeleteConversationThreadParams{
		ProjectID:   projectID,
		Environment: env,
		ID:          threadID,
	})
}

func (s *PostgresThreadStore) DeleteThreadsBySubject(ctx context.Context, projectID, env, issuer, subject string) error {
	if s.queries == nil {
		return errors.New("postgres thread store: nil queries")
	}

	return s.queries.DeleteConversationThreadsBySubject(ctx, db.DeleteConversationThreadsBySubjectParams{
		ProjectID:   projectID,
		Environment: env,
		Issuer:      issuer,
		Subject:     subject,
	})
}
