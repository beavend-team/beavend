package limits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/redis/go-redis/v9"
)

var (
	// ErrLimitStoreUnavailable is returned when the counter backend cannot be reached in fail-closed mode.
	ErrLimitStoreUnavailable = errors.New("limit store unavailable")
)

// RedisLimitStoreOptions provides configuration options for RedisLimitStore.
type RedisLimitStoreOptions struct {
	FailClosed bool
	NowFunc    func() time.Time
}

// RedisLimitStore enforces multi-window sliding limits via Redis Lua scripts.
type RedisLimitStore struct {
	client          redis.UniversalClient
	checkScript     *redis.Script
	reconcileScript *redis.Script
	failClosed      bool
	nowFunc         func() time.Time
}

// NewRedisLimitStore constructs a new RedisLimitStore.
func NewRedisLimitStore(client redis.UniversalClient, opts *RedisLimitStoreOptions) *RedisLimitStore {
	failClosed := true
	nowFn := time.Now
	if opts != nil {
		failClosed = opts.FailClosed
		if opts.NowFunc != nil {
			nowFn = opts.NowFunc
		}
	}

	return &RedisLimitStore{
		client:          client,
		checkScript:     redis.NewScript(CheckAndDebitScript),
		reconcileScript: redis.NewScript(ReconcileScript),
		failClosed:      failClosed,
		nowFunc:         nowFn,
	}
}

// CheckAndDebit evaluates multiple limit rules atomically for the given key in a single round-trip.
func (s *RedisLimitStore) CheckAndDebit(ctx context.Context, key string, rules []core.LimitRule, requestedTokens int64) (bool, time.Duration, error) {
	if len(rules) == 0 {
		return true, 0, nil
	}

	if s.client == nil {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: nil redis client", ErrLimitStoreUnavailable)
		}
		slog.WarnContext(ctx, "nil redis client, failing open")
		return true, 0, nil
	}

	nowSec := s.nowFunc().Unix()
	ruleCount := len(rules)

	// KEYS: [ruleKey1, ruleKey2, ..., tokenWindowsKey]
	keys := make([]string, ruleCount+1)
	for i, r := range rules {
		keys[i] = RuleKey(key, r)
	}
	keys[ruleCount] = TokenWindowsKey(key)

	// ARGV: [now, rule_count, (window, max, cost, is_token)...]
	argv := make([]any, 2+ruleCount*4)
	argv[0] = nowSec
	argv[1] = ruleCount

	for i, r := range rules {
		windowSec := int64(r.Window.Seconds())
		if windowSec <= 0 {
			windowSec = 1
		}
		cost := int64(1)
		isToken := int64(0)
		if r.Resource == "tokens" {
			cost = requestedTokens
			if cost <= 0 {
				cost = 1
			}
			isToken = 1
		}
		maxLimit := r.Max
		if maxLimit < 0 {
			maxLimit = 0
		}

		offset := 2 + i*4
		argv[offset] = windowSec
		argv[offset+1] = maxLimit
		argv[offset+2] = cost
		argv[offset+3] = isToken
	}

	res, err := s.checkScript.Run(ctx, s.client, keys, argv...).Result()
	if err != nil {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: redis script execution: %w", ErrLimitStoreUnavailable, err)
		}
		slog.WarnContext(ctx, "redis limit check error, failing open", "error", err)
		return true, 0, nil
	}

	results, ok := res.([]any)
	if !ok || len(results) < 2 {
		if s.failClosed {
			return false, 0, fmt.Errorf("%w: invalid redis response: %v", ErrLimitStoreUnavailable, res)
		}
		return true, 0, nil
	}

	allowedInt, _ := results[0].(int64)
	retrySec, _ := results[1].(int64)

	if allowedInt == 1 {
		return true, 0, nil
	}

	retryAfter := time.Duration(retrySec) * time.Second
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	return false, retryAfter, nil
}

// ReconcileTokens reconciles estimated token reservations with actual usage.
func (s *RedisLimitStore) ReconcileTokens(ctx context.Context, key string, estimatedTokens, actualTokens int64) error {
	delta := actualTokens - estimatedTokens
	if delta == 0 {
		return nil
	}

	if s.client == nil {
		if s.failClosed {
			return fmt.Errorf("%w: nil redis client", ErrLimitStoreUnavailable)
		}
		return nil
	}

	nowSec := s.nowFunc().Unix()
	keys := []string{
		TokenWindowsKey(key),
		key + ":tokens",
	}

	_, err := s.reconcileScript.Run(ctx, s.client, keys, nowSec, delta).Result()
	if err != nil {
		if s.failClosed {
			return fmt.Errorf("%w: redis reconciliation error: %w", ErrLimitStoreUnavailable, err)
		}
		slog.WarnContext(ctx, "redis token reconciliation error, ignored due to fail_open", "error", err)
		return nil
	}

	return nil
}
