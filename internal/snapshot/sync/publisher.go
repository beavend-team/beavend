package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Publisher broadcasts revision invalidation events.
type Publisher interface {
	Publish(ctx context.Context, event RevisionEvent) error
}

// RedisPublisher publishes revision events to Redis pub/sub.
type RedisPublisher struct {
	client redis.UniversalClient
}

// NewRedisPublisher creates a new Redis revision publisher.
func NewRedisPublisher(client redis.UniversalClient) *RedisPublisher {
	return &RedisPublisher{client: client}
}

// Publish serializes and publishes the RevisionEvent to channel cfg:{project_id}.
func (p *RedisPublisher) Publish(ctx context.Context, event RevisionEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal revision event: %w", err)
	}

	channel := ChannelForProject(event.ProjectID)
	if err := p.client.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("publish to channel %s: %w", channel, err)
	}

	return nil
}
