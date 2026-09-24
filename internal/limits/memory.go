package limits

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// MemoryLimitStore is a thread-safe, in-memory implementation of core.LimitStore.
// It uses the exact sliding window counter algorithm without requiring Redis.
type MemoryLimitStore struct {
	mu           sync.Mutex
	counters     map[string]int64
	tokenWindows map[string]map[int64]struct{} // userKey -> set of windowSeconds
	nowFunc      func() time.Time
}

// NewMemoryLimitStore creates a new in-memory limit store.
func NewMemoryLimitStore(nowFunc func() time.Time) *MemoryLimitStore {
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &MemoryLimitStore{
		counters:     make(map[string]int64),
		tokenWindows: make(map[string]map[int64]struct{}),
		nowFunc:      nowFunc,
	}
}

// CheckAndDebit evaluates and debits limits in memory atomically.
func (m *MemoryLimitStore) CheckAndDebit(ctx context.Context, key string, rules []core.LimitRule, requestedTokens int64) (bool, time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}

	if len(rules) == 0 {
		return true, 0, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	nowSec := m.nowFunc().Unix()
	var maxRetry time.Duration
	allowed := true

	// First pass: check all rules
	for _, r := range rules {
		windowSec := int64(r.Window.Seconds())
		if windowSec <= 0 {
			windowSec = 1
		}
		cost := int64(1)
		if r.Resource == "tokens" {
			cost = requestedTokens
		}

		baseKey := RuleKey(key, r)
		bucket := nowSec / windowSec
		currKey := baseKey + ":" + timeBucketStr(bucket)
		prevKey := baseKey + ":" + timeBucketStr(bucket-1)

		currVal := m.counters[currKey]
		prevVal := m.counters[prevKey]

		elapsed := nowSec % windowSec
		ruleAllowed, retry := EstimateSlidingWindow(prevVal, currVal, cost, r.Max, windowSec, elapsed)
		if !ruleAllowed {
			allowed = false
			if retry > maxRetry {
				maxRetry = retry
			}
		}
	}

	if !allowed {
		return false, maxRetry, nil
	}

	// Second pass: debit all rules
	for _, r := range rules {
		windowSec := int64(r.Window.Seconds())
		if windowSec <= 0 {
			windowSec = 1
		}
		cost := int64(1)
		if r.Resource == "tokens" {
			cost = requestedTokens
			// Track token window for reconciliation
			if m.tokenWindows[key] == nil {
				m.tokenWindows[key] = make(map[int64]struct{})
			}
			m.tokenWindows[key][windowSec] = struct{}{}
		}

		baseKey := RuleKey(key, r)
		bucket := nowSec / windowSec
		currKey := baseKey + ":" + timeBucketStr(bucket)
		m.counters[currKey] += cost
	}

	return true, 0, nil
}

// ReconcileTokens adjusts token counters by actual - estimated.
func (m *MemoryLimitStore) ReconcileTokens(ctx context.Context, key string, estimatedTokens, actualTokens int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	delta := actualTokens - estimatedTokens
	if delta == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	nowSec := m.nowFunc().Unix()
	windows, exists := m.tokenWindows[key]
	if !exists {
		return nil
	}

	for windowSec := range windows {
		bucket := nowSec / windowSec
		ruleKey := RuleKey(key, core.LimitRule{Resource: "tokens", Window: time.Duration(windowSec) * time.Second})
		currKey := ruleKey + ":" + timeBucketStr(bucket)
		newVal := m.counters[currKey] + delta
		if newVal < 0 {
			newVal = 0
		}
		m.counters[currKey] = newVal
	}

	return nil
}

func timeBucketStr(b int64) string {
	return strconv.FormatInt(b, 10)
}
