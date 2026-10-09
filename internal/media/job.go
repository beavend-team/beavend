package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrJobNotFound is returned when an asynchronous media job is not found or expired.
	ErrJobNotFound = errors.New("media job: not found")
)

// GenerateJobID generates a unique identifier for media jobs with prefix "job_".
func GenerateJobID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "job_" + hex.EncodeToString(b)
}

// MediaJob represents an asynchronous media generation job (e.g. video generation).
type MediaJob struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"project_id"`
	Environment string               `json:"environment"`
	AIAppID     string               `json:"ai_app_id"`
	Model       string               `json:"model"`
	Modality    string               `json:"modality"`
	Prompt      string               `json:"prompt"`
	Status      string               `json:"status"` // "queued", "processing", "completed", "failed"
	Result      *core.GenerateResponse `json:"result,omitempty"`
	Error       string               `json:"error,omitempty"`
	WebhookURL  string               `json:"webhook_url,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// JobStore defines persistence for asynchronous media jobs.
type JobStore interface {
	CreateJob(ctx context.Context, job *MediaJob, ttl time.Duration) error
	GetJob(ctx context.Context, projectID, jobID string) (*MediaJob, error)
	UpdateJob(ctx context.Context, job *MediaJob, ttl time.Duration) error
}

// RedisJobStore implements JobStore using Redis with automatic TTL expiration.
type RedisJobStore struct {
	client redis.UniversalClient
}

// NewRedisJobStore constructs a RedisJobStore.
func NewRedisJobStore(client redis.UniversalClient) *RedisJobStore {
	return &RedisJobStore{client: client}
}

func jobKey(projectID, jobID string) string {
	if projectID == "" {
		projectID = "default"
	}
	return fmt.Sprintf("job:%s:%s", projectID, jobID)
}

// CreateJob persists a new MediaJob in Redis.
func (s *RedisJobStore) CreateJob(ctx context.Context, job *MediaJob, ttl time.Duration) error {
	if s.client == nil {
		return errors.New("nil redis client")
	}
	if job == nil {
		return errors.New("nil job")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return s.client.Set(ctx, jobKey(job.ProjectID, job.ID), data, ttl).Err()
}

// GetJob retrieves a MediaJob by ID.
func (s *RedisJobStore) GetJob(ctx context.Context, projectID, jobID string) (*MediaJob, error) {
	if s.client == nil {
		return nil, errors.New("nil redis client")
	}
	data, err := s.client.Get(ctx, jobKey(projectID, jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("redis get job: %w", err)
	}
	var job MediaJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal job: %w", err)
	}
	return &job, nil
}

// UpdateJob updates an existing MediaJob in Redis.
func (s *RedisJobStore) UpdateJob(ctx context.Context, job *MediaJob, ttl time.Duration) error {
	return s.CreateJob(ctx, job, ttl)
}

// InMemoryJobStore provides an in-memory JobStore for unit testing.
type InMemoryJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*MediaJob
}

// NewInMemoryJobStore constructs an InMemoryJobStore.
func NewInMemoryJobStore() *InMemoryJobStore {
	return &InMemoryJobStore{
		jobs: make(map[string]*MediaJob),
	}
}

// CreateJob saves a job in memory.
func (m *InMemoryJobStore) CreateJob(_ context.Context, job *MediaJob, _ time.Duration) error {
	if job == nil {
		return errors.New("nil job")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *job
	m.jobs[jobKey(job.ProjectID, job.ID)] = &copied
	return nil
}

// GetJob retrieves a job from memory.
func (m *InMemoryJobStore) GetJob(_ context.Context, projectID, jobID string) (*MediaJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[jobKey(projectID, jobID)]
	if !ok {
		return nil, ErrJobNotFound
	}
	copied := *j
	return &copied, nil
}

// UpdateJob updates a job in memory.
func (m *InMemoryJobStore) UpdateJob(ctx context.Context, job *MediaJob, ttl time.Duration) error {
	return m.CreateJob(ctx, job, ttl)
}

var (
	_ JobStore = (*RedisJobStore)(nil)
	_ JobStore = (*InMemoryJobStore)(nil)
)
