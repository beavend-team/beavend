package limits

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserKey(t *testing.T) {
	tests := []struct {
		name        string
		projectID   string
		environment string
		identity    *core.Identity
		wantPrefix  string
		wantTag     bool
	}{
		{
			name:        "nil identity",
			projectID:   "proj-1",
			environment: "prod",
			identity:    nil,
			wantPrefix:  "{lim:proj-1:prod:anonymous}",
			wantTag:     true,
		},
		{
			name:        "empty identity",
			projectID:   "proj-1",
			environment: "prod",
			identity:    &core.Identity{},
			wantPrefix:  "{lim:proj-1:prod:anonymous}",
			wantTag:     true,
		},
		{
			name:        "valid authenticated identity",
			projectID:   "proj-1",
			environment: "prod",
			identity: &core.Identity{
				Issuer:  "https://auth.example.com",
				Subject: "user-12345",
			},
			wantPrefix: "{lim:proj-1:prod:",
			wantTag:    true,
		},
		{
			name:        "default fallback for empty project and env",
			projectID:   "",
			environment: "",
			identity:    nil,
			wantPrefix:  "{lim:default:default:anonymous}",
			wantTag:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := UserKey(tt.projectID, tt.environment, tt.identity)
			assert.True(t, strings.HasPrefix(key, tt.wantPrefix), "expected prefix %q, got %q", tt.wantPrefix, key)
			if tt.wantTag {
				assert.True(t, strings.HasPrefix(key, "{lim:"), "key must start with hash tag {lim:")
				assert.Contains(t, key, "}", "key must close hash tag with }")
			}
		})
	}

	// Determinism check: same identity produces identical key
	id := &core.Identity{Issuer: "https://auth.example.com", Subject: "user-abc"}
	k1 := UserKey("p", "e", id)
	k2 := UserKey("p", "e", id)
	assert.Equal(t, k1, k2)
}

func TestRuleKey(t *testing.T) {
	userKey := "{lim:p:e:123}"
	rule := core.LimitRule{
		Resource: "inference",
		Window:   60 * time.Second,
		Max:      10,
	}

	rk := RuleKey(userKey, rule)
	assert.Equal(t, "{lim:p:e:123}:inference:60", rk)

	// Ensure hash tag is preserved untouched
	assert.True(t, strings.HasPrefix(rk, userKey))
}

func TestEstimateSlidingWindow_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		prevCount   int64
		currCount   int64
		cost        int64
		maxLimit    int64
		windowSec   int64
		elapsedSec  int64
		wantAllowed bool
		minRetrySec int64
	}{
		{
			name:        "first request in empty window passes",
			prevCount:   0,
			currCount:   0,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  10,
			wantAllowed: true,
		},
		{
			name:        "request within limit passes",
			prevCount:   0,
			currCount:   3,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  10,
			wantAllowed: true,
		},
		{
			name:        "burst exactly reaching max limit passes",
			prevCount:   0,
			currCount:   4,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  30,
			wantAllowed: true,
		},
		{
			name:        "burst exceeding max limit is rejected",
			prevCount:   0,
			currCount:   5,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  20,
			wantAllowed: false,
			minRetrySec: 40,
		},
		{
			name: "previous window decayed weight allows request",
			// window 60s, elapsed 45s => weight = 15/60 = 0.25
			// prevCount 8 => floor(8 * 0.25) = 2
			// currCount = 2, cost = 1 => 2 + 2 + 1 = 5 <= 5 => passes
			prevCount:   8,
			currCount:   2,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  45,
			wantAllowed: true,
		},
		{
			name: "previous window high weight blocks request",
			// window 60s, elapsed 15s => weight = 45/60 = 0.75
			// prevCount 8 => floor(8 * 0.75) = 6
			// currCount = 0, cost = 1 => 6 + 0 + 1 = 7 > 5 => rejected
			prevCount:   8,
			currCount:   0,
			cost:        1,
			maxLimit:    5,
			windowSec:   60,
			elapsedSec:  15,
			wantAllowed: false,
			minRetrySec: 45,
		},
		{
			name:        "zero max limit always rejected",
			prevCount:   0,
			currCount:   0,
			cost:        1,
			maxLimit:    0,
			windowSec:   60,
			elapsedSec:  10,
			wantAllowed: false,
			minRetrySec: 60,
		},
		{
			name:        "cost greater than max limit always rejected",
			prevCount:   0,
			currCount:   0,
			cost:        100,
			maxLimit:    50,
			windowSec:   60,
			elapsedSec:  10,
			wantAllowed: false,
			minRetrySec: 50,
		},
		{
			name:       "token request admitted when under token limit",
			prevCount:  1000,
			currCount:  2000,
			cost:       1500,
			maxLimit:   10000,
			windowSec:  86400,
			elapsedSec: 43200, // weight = 0.5 -> prev = 500
			// 500 + 2000 + 1500 = 4000 <= 10000 -> passes
			wantAllowed: true,
		},
		{
			name:       "token request rejected when over token limit",
			prevCount:  5000,
			currCount:  4000,
			cost:       5000,
			maxLimit:   10000,
			windowSec:  86400,
			elapsedSec: 43200, // weight = 0.5 -> prev = 2500
			// 2500 + 4000 + 5000 = 11500 > 10000 -> rejected
			wantAllowed: false,
			minRetrySec: 43200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, retryAfter := EstimateSlidingWindow(
				tt.prevCount,
				tt.currCount,
				tt.cost,
				tt.maxLimit,
				tt.windowSec,
				tt.elapsedSec,
			)

			assert.Equal(t, tt.wantAllowed, allowed)
			if !tt.wantAllowed {
				assert.GreaterOrEqual(t, int64(retryAfter.Seconds()), tt.minRetrySec)
			} else {
				assert.Equal(t, time.Duration(0), retryAfter)
			}
		})
	}
}

func TestMemoryLimitStore_MultiWindowAndReconciliation(t *testing.T) {
	ctx := context.Background()
	currentTime := time.Unix(1700000000, 0)
	timeFn := func() time.Time { return currentTime }

	store := NewMemoryLimitStore(timeFn)

	rules := []core.LimitRule{
		{
			Resource: "inference",
			Window:   60 * time.Second,
			Max:      2, // 2 requests per minute
		},
		{
			Resource: "inference",
			Window:   24 * time.Hour,
			Max:      5, // 5 requests per day
		},
		{
			Resource: "tokens",
			Window:   24 * time.Hour,
			Max:      1000, // 1000 tokens per day
		},
	}

	key := "{lim:test:dev:user1}"

	// Request 1: 300 tokens estimated
	allowed, retry, err := store.CheckAndDebit(ctx, key, rules, 300)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, time.Duration(0), retry)

	// Request 2: 400 tokens estimated (cumulative 700 tokens, 2 requests)
	allowed, retry, err = store.CheckAndDebit(ctx, key, rules, 400)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, time.Duration(0), retry)

	// Request 3: 1-minute inference limit (2) is reached!
	allowed, retry, err = store.CheckAndDebit(ctx, key, rules, 100)
	require.NoError(t, err)
	assert.False(t, allowed, "should be rate limited by 1-minute limit")
	assert.Greater(t, retry, time.Duration(0))

	// Reconcile Request 1: estimated 300, but actual was only 100 (refund 200 tokens)
	err = store.ReconcileTokens(ctx, key, 300, 100)
	require.NoError(t, err)

	// Advance time by 61 seconds (1-minute window passes)
	currentTime = currentTime.Add(61 * time.Second)

	// Request 4: now allowed under 1-minute window, and cumulative tokens = 700 - 200 = 500
	// 500 + 400 tokens = 900 <= 1000 -> allowed
	allowed, retry, err = store.CheckAndDebit(ctx, key, rules, 400)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, time.Duration(0), retry)

	// Request 5: 300 tokens would push total to 900 + 300 = 1200 > 1000 -> rejected by tokens
	allowed, retry, err = store.CheckAndDebit(ctx, key, rules, 300)
	require.NoError(t, err)
	assert.False(t, allowed, "should be blocked by 24h token limit")
	assert.Greater(t, retry, time.Duration(0))
}

func TestTierResolver_TableDriven(t *testing.T) {
	resolver, err := NewTierResolver(nil, nil)
	require.NoError(t, err)

	rules := []core.LimitRule{
		{
			Resource: "inference",
			Window:   24 * time.Hour,
			Max:      30, // default free
		},
		{
			Resource:      "inference",
			Window:        24 * time.Hour,
			Max:           500, // pro tier
			TierCondition: "claims.plan == 'pro'",
		},
		{
			Resource:      "inference",
			Window:        24 * time.Hour,
			Max:           2000, // enterprise tier
			TierCondition: "claims.plan == 'enterprise'",
		},
		{
			Resource: "inference",
			Window:   1 * time.Minute,
			Max:      5, // burst limit for everyone
		},
		{
			Resource: "tokens",
			Window:   24 * time.Hour,
			Max:      100000, // default tokens
		},
		{
			Resource:      "tokens",
			Window:        24 * time.Hour,
			Max:           1000000, // pro tokens
			TierCondition: "claims.plan == 'pro' || claims.plan == 'enterprise'",
		},
	}

	tests := []struct {
		name          string
		identity      *core.Identity
		wantInf24hMax int64
		wantTok24hMax int64
		wantInf1mMax  int64
	}{
		{
			name:          "nil identity resolves to default rules",
			identity:      nil,
			wantInf24hMax: 30,
			wantTok24hMax: 100000,
			wantInf1mMax:  5,
		},
		{
			name: "free plan resolves to default rules",
			identity: &core.Identity{
				Claims: map[string]any{"plan": "free"},
			},
			wantInf24hMax: 30,
			wantTok24hMax: 100000,
			wantInf1mMax:  5,
		},
		{
			name: "pro plan resolves to pro rules",
			identity: &core.Identity{
				Claims: map[string]any{"plan": "pro"},
			},
			wantInf24hMax: 500,
			wantTok24hMax: 1000000,
			wantInf1mMax:  5,
		},
		{
			name: "enterprise plan resolves to enterprise inference and pro/enterprise tokens",
			identity: &core.Identity{
				Claims: map[string]any{"plan": "enterprise"},
			},
			wantInf24hMax: 2000,
			wantTok24hMax: 1000000,
			wantInf1mMax:  5,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, err := resolver.ResolveRules(ctx, rules, tt.identity, nil)
			require.NoError(t, err)

			var inf24h, tok24h, inf1m *core.LimitRule
			for _, r := range active {
				rCopy := r
				if r.Resource == "inference" && r.Window == 24*time.Hour {
					inf24h = &rCopy
				}
				if r.Resource == "tokens" && r.Window == 24*time.Hour {
					tok24h = &rCopy
				}
				if r.Resource == "inference" && r.Window == 1*time.Minute {
					inf1m = &rCopy
				}
			}

			require.NotNil(t, inf24h, "missing inference 24h rule")
			assert.Equal(t, tt.wantInf24hMax, inf24h.Max)

			require.NotNil(t, tok24h, "missing tokens 24h rule")
			assert.Equal(t, tt.wantTok24hMax, tok24h.Max)

			require.NotNil(t, inf1m, "missing inference 1m rule")
			assert.Equal(t, tt.wantInf1mMax, inf1m.Max)
		})
	}
}

func TestEstimateRequestTokens(t *testing.T) {
	// Nil request
	assert.Equal(t, DefaultMaxOutputTokens, EstimateRequestTokens(nil))

	// Request with max tokens specified
	maxTok := 500
	req := &core.ChatRequest{
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello world!"}, // ~12 chars + overhead
		},
		MaxTokens: &maxTok,
	}

	est := EstimateRequestTokens(req)
	// promptEstimate > 0 + 500
	assert.GreaterOrEqual(t, est, int64(501))

	// Request without max tokens specifies default
	reqNoMax := &core.ChatRequest{
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	estDefault := EstimateRequestTokens(reqNoMax)
	assert.Greater(t, estDefault, DefaultMaxOutputTokens)
}

func TestRedisLimitStore_FailClosedAndOpen(t *testing.T) {
	ctx := context.Background()
	rules := []core.LimitRule{
		{Resource: "inference", Window: time.Minute, Max: 5},
	}

	// FailClosed = true with nil client
	storeClosed := NewRedisLimitStore(nil, &RedisLimitStoreOptions{FailClosed: true})
	allowed, _, err := storeClosed.CheckAndDebit(ctx, "{lim:test:dev:anon}", rules, 1)
	assert.Error(t, err)
	assert.False(t, allowed)
	assert.ErrorIs(t, err, ErrLimitStoreUnavailable)

	// FailClosed = false with nil client
	storeOpen := NewRedisLimitStore(nil, &RedisLimitStoreOptions{FailClosed: false})
	allowed, _, err = storeOpen.CheckAndDebit(ctx, "{lim:test:dev:anon}", rules, 1)
	assert.NoError(t, err)
	assert.True(t, allowed)

	// Reconcile with fail closed
	err = storeClosed.ReconcileTokens(ctx, "{lim:test:dev:anon}", 100, 50)
	assert.Error(t, err)

	// Reconcile with fail open
	err = storeOpen.ReconcileTokens(ctx, "{lim:test:dev:anon}", 100, 50)
	assert.NoError(t, err)
}

func TestPostgresLimitStore_FailClosedAndOpen(t *testing.T) {
	ctx := context.Background()
	rules := []core.LimitRule{
		{Resource: "inference", Window: time.Minute, Max: 5},
	}

	// FailClosed = true with nil pool
	storeClosed := NewPostgresLimitStore(nil, &PostgresLimitStoreOptions{FailClosed: true})
	allowed, _, err := storeClosed.CheckAndDebit(ctx, "{lim:test:dev:anon}", rules, 1)
	assert.Error(t, err)
	assert.False(t, allowed)
	assert.ErrorIs(t, err, ErrLimitStoreUnavailable)

	// FailClosed = false with nil pool
	storeOpen := NewPostgresLimitStore(nil, &PostgresLimitStoreOptions{FailClosed: false})
	allowed, _, err = storeOpen.CheckAndDebit(ctx, "{lim:test:dev:anon}", rules, 1)
	assert.NoError(t, err)
	assert.True(t, allowed)

	// Reconcile with fail closed
	err = storeClosed.ReconcileTokens(ctx, "{lim:test:dev:anon}", 100, 50)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitStoreUnavailable)

	// Reconcile with fail open
	err = storeOpen.ReconcileTokens(ctx, "{lim:test:dev:anon}", 100, 50)
	assert.NoError(t, err)
}

func TestTierResolver_SnapshotPrecompiled(t *testing.T) {
	resolver, err := NewTierResolver(nil, nil)
	require.NoError(t, err)

	compiler, err := policy.NewCompiler()
	require.NoError(t, err)

	prg, err := compiler.Compile("claims.tier == 'gold'")
	require.NoError(t, err)

	snap := core.NewSnapshot("proj-1", "prod", 1)
	snap.CompiledPolicies["claims.tier == 'gold'"] = prg

	ctx := core.ContextWithSnapshot(context.Background(), snap)

	rules := []core.LimitRule{
		{Resource: "inference", Window: time.Minute, Max: 10},
		{Resource: "inference", Window: time.Minute, Max: 100, TierCondition: "claims.tier == 'gold'"},
	}

	id := &core.Identity{Claims: map[string]any{"tier": "gold"}}
	resolved, err := resolver.ResolveRules(ctx, rules, id, nil)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, int64(100), resolved[0].Max)
}

func TestEstimateSlidingWindow_NegativeInputs(t *testing.T) {
	// Negative counts should be treated safely as 0
	allowed, _ := EstimateSlidingWindow(-5, -2, -1, 10, 60, -10)
	assert.True(t, allowed)
}
