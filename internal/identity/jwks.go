package identity

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"golang.org/x/sync/singleflight"
)

const (
	defaultJWKSTTL            = 1 * time.Hour
	defaultMinRefreshInterval = 5 * time.Second
	defaultHTTPClientTimeout  = 10 * time.Second
)

type jwksCacheEntry struct {
	set       jwk.Set
	expiresAt time.Time
	fetchedAt time.Time
}

// JWKSOptions configures the JWKSManager.
type JWKSOption func(*JWKSManager)

// WithHTTPClient sets a custom HTTP client for remote JWKS fetches.
func WithHTTPClient(client *http.Client) JWKSOption {
	return func(m *JWKSManager) {
		if client != nil {
			m.client = client
		}
	}
}

// WithDefaultTTL sets the cache TTL for JWKS sets.
func WithDefaultTTL(ttl time.Duration) JWKSOption {
	return func(m *JWKSManager) {
		if ttl > 0 {
			m.defaultTTL = ttl
		}
	}
}

// WithMinRefreshInterval sets the minimum cooldown between consecutive forced refreshes.
func WithMinRefreshInterval(interval time.Duration) JWKSOption {
	return func(m *JWKSManager) {
		if interval >= 0 {
			m.minRefreshInterval = interval
		}
	}
}

// JWKSManager manages in-memory cached JWKS keysets with single-flight remote fetching.
type JWKSManager struct {
	client             *http.Client
	defaultTTL         time.Duration
	minRefreshInterval time.Duration

	mu    sync.RWMutex
	cache map[string]*jwksCacheEntry
	sf    singleflight.Group
}

// NewJWKSManager creates a new thread-safe JWKS cache manager.
func NewJWKSManager(opts ...JWKSOption) *JWKSManager {
	m := &JWKSManager{
		client: &http.Client{
			Timeout: defaultHTTPClientTimeout,
		},
		defaultTTL:         defaultJWKSTTL,
		minRefreshInterval: defaultMinRefreshInterval,
		cache:              make(map[string]*jwksCacheEntry),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Get returns the cached JWKS keyset for the specified URL, fetching it if missing or expired.
func (m *JWKSManager) Get(ctx context.Context, jwksURL string) (jwk.Set, error) {
	if jwksURL == "" {
		return nil, fmt.Errorf("%w: empty jwks url", ErrAuthConfigMissing)
	}

	m.mu.RLock()
	entry, found := m.cache[jwksURL]
	if found && time.Now().Before(entry.expiresAt) {
		set := entry.set
		m.mu.RUnlock()
		return set, nil
	}
	m.mu.RUnlock()

	// Not in cache or expired, fetch via single-flight
	return m.Refresh(ctx, jwksURL)
}

// Refresh performs a single-flight fetch of the JWKS keyset, updating the in-memory cache.
func (m *JWKSManager) Refresh(ctx context.Context, jwksURL string) (jwk.Set, error) {
	if jwksURL == "" {
		return nil, fmt.Errorf("%w: empty jwks url", ErrAuthConfigMissing)
	}

	parsedURL, err := url.Parse(jwksURL)
	if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return nil, fmt.Errorf("%w: invalid jwks url %q", ErrAuthConfigMissing, jwksURL)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	res, err, _ := m.sf.Do(jwksURL, func() (any, error) {
		// Re-check cache inside singleflight: another concurrent call might have just refreshed it
		m.mu.RLock()
		if entry, found := m.cache[jwksURL]; found {
			// If refreshed within minRefreshInterval, return cached set to avoid spamming remote IdP
			if time.Since(entry.fetchedAt) < m.minRefreshInterval && entry.set != nil {
				m.mu.RUnlock()
				return entry.set, nil
			}
		}
		m.mu.RUnlock()

		// Use dedicated timeout context for background fetch so one client disconnect doesn't abort for all
		timeout := defaultHTTPClientTimeout
		if m.client != nil && m.client.Timeout > 0 {
			timeout = m.client.Timeout
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		set, err := jwk.Fetch(fetchCtx, jwksURL, jwk.WithHTTPClient(m.client))
		if err != nil {
			return nil, fmt.Errorf("fetch jwks from %s: %w", jwksURL, err)
		}

		now := time.Now()
		m.mu.Lock()
		m.cache[jwksURL] = &jwksCacheEntry{
			set:       set,
			expiresAt: now.Add(m.defaultTTL),
			fetchedAt: now,
		}
		m.mu.Unlock()

		return set, nil
	})

	if err != nil {
		return nil, err
	}

	return res.(jwk.Set), nil
}

// SetManual injects a JWKS set directly into the cache. Useful for tests or static keysets.
func (m *JWKSManager) SetManual(jwksURL string, set jwk.Set, ttl time.Duration) {
	if ttl <= 0 {
		ttl = m.defaultTTL
	}
	now := time.Now()
	m.mu.Lock()
	m.cache[jwksURL] = &jwksCacheEntry{
		set:       set,
		expiresAt: now.Add(ttl),
		fetchedAt: now,
	}
	m.mu.Unlock()
}

// Clear removes all cached JWKS keysets.
func (m *JWKSManager) Clear() {
	m.mu.Lock()
	m.cache = make(map[string]*jwksCacheEntry)
	m.mu.Unlock()
}
