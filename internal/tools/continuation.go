package tools

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
)

var (
	// ErrStateNotFound indicates suspended state was not found or has expired.
	ErrStateNotFound = errors.New("suspended state not found or expired")
	// ErrTokenInvalidFormat indicates malformed confirmation token format.
	ErrTokenInvalidFormat = errors.New("invalid confirmation token format")
	// ErrTokenSignatureInvalid indicates confirmation token signature verification failure.
	ErrTokenSignatureInvalid = errors.New("invalid confirmation token signature")
	// ErrTokenExpired indicates confirmation token has expired.
	ErrTokenExpired = errors.New("confirmation token expired")
	// ErrSubjectMismatch indicates the user attempting resumption does not match the suspended user.
	ErrSubjectMismatch = errors.New("user identity mismatch on continuation")
	// ErrArgsHashMismatch indicates tool arguments were tampered with.
	ErrArgsHashMismatch = errors.New("tool arguments hash mismatch")
)

const (
	// DefaultSuspensionTTL is the 5-minute continuation TTL per ADR-007.
	DefaultSuspensionTTL = 5 * time.Minute
)

// SuspendedState holds frozen agent loop execution state in Redis.
type SuspendedState struct {
	ProjectID        string             `json:"project_id"`
	Environment      string             `json:"environment"`
	RequestID        string             `json:"request_id"`
	AIAppID          string             `json:"ai_app_id"`
	ThreadID         string             `json:"thread_id,omitempty"`
	Subject          string             `json:"subject"`
	Issuer           string             `json:"issuer"`
	Messages         []core.ChatMessage `json:"messages"`
	PendingToolCall  core.ToolCall      `json:"pending_tool_call"`
	PendingToolCalls []core.ToolCall    `json:"pending_tool_calls,omitempty"`
	BoundaryNonce    string             `json:"boundary_nonce,omitempty"`
	ToolDefinitionID string             `json:"tool_definition_id"`
	SnapshotRevision int64              `json:"snapshot_revision"`
	CreatedAt        time.Time          `json:"created_at"`
	IsStreaming      bool               `json:"is_streaming"`
}

// ConfirmationTokenClaims represents the payload bound to a signed confirmation token.
type ConfirmationTokenClaims struct {
	ProjectID   string `json:"pid"`
	Environment string `json:"env,omitempty"`
	AIAppID     string `json:"app,omitempty"`
	RequestID   string `json:"rid"`
	ToolID      string `json:"tid"`
	ArgsHash    string `json:"ah"`
	Issuer      string `json:"iss,omitempty"`
	Subject     string `json:"sub"`
	ExpiresAt   int64  `json:"exp"`
}

// SuspendedStateStore defines persistence for suspended request continuation.
type SuspendedStateStore interface {
	Save(ctx context.Context, state *SuspendedState, ttl time.Duration) error
	Get(ctx context.Context, projectID, requestID string) (*SuspendedState, error)
	Delete(ctx context.Context, projectID, requestID string) error
	Consume(ctx context.Context, projectID, env, reqID string) (*SuspendedState, error)
}

func suspendedKey(projectID, env, requestID string) string {
	if env != "" {
		return fmt.Sprintf("pend:%s:%s:%s", projectID, env, requestID)
	}
	return fmt.Sprintf("pend:%s:%s", projectID, requestID)
}

func suspendedAAD(projectID, env, requestID string) string {
	return fmt.Sprintf("%s:%s:%s", projectID, env, requestID)
}

// RedisSuspendedStateStore implements SuspendedStateStore using Redis.
type RedisSuspendedStateStore struct {
	client redis.UniversalClient
	encKey []byte
}

// NewRedisSuspendedStateStore creates a new RedisSuspendedStateStore.
func NewRedisSuspendedStateStore(client redis.UniversalClient, encKey ...[]byte) *RedisSuspendedStateStore {
	var key []byte
	if len(encKey) > 0 && len(encKey[0]) > 0 {
		key = encKey[0]
	} else {
		key = secrets.DeriveKey("beavend-continuation-aes256gcm-default-master-key")
	}
	return &RedisSuspendedStateStore{
		client: client,
		encKey: key,
	}
}

// WithEncryptionKey overrides the AES-256 encryption key.
func (s *RedisSuspendedStateStore) WithEncryptionKey(key []byte) *RedisSuspendedStateStore {
	s.encKey = key
	return s
}

// Save serializes, encrypts with AES-256-GCM, and stores suspended state in Redis with the configured TTL.
func (s *RedisSuspendedStateStore) Save(ctx context.Context, state *SuspendedState, ttl time.Duration) error {
	if state == nil {
		return errors.New("nil suspended state")
	}
	if ttl <= 0 {
		ttl = DefaultSuspensionTTL
	}

	key := suspendedKey(state.ProjectID, state.Environment, state.RequestID)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal suspended state: %w", err)
	}

	aad := []byte(suspendedAAD(state.ProjectID, state.Environment, state.RequestID))
	ciphertext, nonce, err := secrets.EncryptWithAAD(s.encKey, data, aad)
	if err != nil {
		return fmt.Errorf("encrypt suspended state: %w", err)
	}

	payload := append(nonce, ciphertext...)

	// Save with both key formats if environment is specified so legacy Get works
	if state.Environment != "" {
		legacyKey := fmt.Sprintf("pend:%s:%s", state.ProjectID, state.RequestID)
		_ = s.client.Set(ctx, legacyKey, payload, ttl)
	}

	return s.client.Set(ctx, key, payload, ttl).Err()
}

// Get retrieves and deserializes suspended state from Redis.
func (s *RedisSuspendedStateStore) Get(ctx context.Context, projectID, requestID string) (*SuspendedState, error) {
	key := fmt.Sprintf("pend:%s:%s", projectID, requestID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrStateNotFound
		}
		return nil, fmt.Errorf("redis get suspended state: %w", err)
	}

	return s.decodeState(data, projectID, "", requestID)
}

// Consume retrieves and deletes suspended state atomically using Redis GETDEL.
func (s *RedisSuspendedStateStore) Consume(ctx context.Context, projectID, env, reqID string) (*SuspendedState, error) {
	key := suspendedKey(projectID, env, reqID)
	data, err := s.client.GetDel(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if env != "" {
				legacyKey := fmt.Sprintf("pend:%s:%s", projectID, reqID)
				legacyData, legacyErr := s.client.GetDel(ctx, legacyKey).Bytes()
				if legacyErr == nil {
					return s.decodeState(legacyData, projectID, env, reqID)
				}
			}
			return nil, ErrStateNotFound
		}
		return nil, fmt.Errorf("redis getdel suspended state: %w", err)
	}

	// Also delete legacy key if it existed
	if env != "" {
		legacyKey := fmt.Sprintf("pend:%s:%s", projectID, reqID)
		_ = s.client.Del(ctx, legacyKey)
	}

	return s.decodeState(data, projectID, env, reqID)
}

func (s *RedisSuspendedStateStore) decodeState(data []byte, projectID, env, reqID string) (*SuspendedState, error) {
	var state SuspendedState

	// Direct JSON unmarshal (if stored unencrypted)
	if len(data) > 0 && data[0] == '{' {
		if err := json.Unmarshal(data, &state); err == nil {
			return &state, nil
		}
	}

	// Attempt AES-256-GCM decryption
	if len(data) >= 12 {
		nonce := data[:12]
		ciphertext := data[12:]
		aad := []byte(suspendedAAD(projectID, env, reqID))
		plaintext, err := secrets.DecryptWithAAD(s.encKey, ciphertext, nonce, aad)
		if err == nil {
			if err := json.Unmarshal(plaintext, &state); err == nil {
				return &state, nil
			}
		}

		// If env was empty or mismatched, try common environments
		for _, testEnv := range []string{"", "default", "prod", "dev", "staging", "production", "development"} {
			testAAD := []byte(suspendedAAD(projectID, testEnv, reqID))
			if pt, err := secrets.DecryptWithAAD(s.encKey, ciphertext, nonce, testAAD); err == nil {
				if err := json.Unmarshal(pt, &state); err == nil {
					return &state, nil
				}
			}
		}
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal suspended state: %w", err)
	}

	return &state, nil
}

// Delete removes suspended state from Redis upon resumption or completion.
func (s *RedisSuspendedStateStore) Delete(ctx context.Context, projectID, requestID string) error {
	key := fmt.Sprintf("pend:%s:%s", projectID, requestID)
	return s.client.Del(ctx, key).Err()
}

// InMemorySuspendedStateStore provides thread-safe in-memory storage with TTL for testing.
type InMemorySuspendedStateStore struct {
	mu    sync.RWMutex
	items map[string]inMemoryItem
}

type inMemoryItem struct {
	state     SuspendedState
	expiresAt time.Time
}

// NewInMemorySuspendedStateStore creates a new in-memory continuation store.
func NewInMemorySuspendedStateStore() *InMemorySuspendedStateStore {
	return &InMemorySuspendedStateStore{
		items: make(map[string]inMemoryItem),
	}
}

// Save stores state in memory with expiration.
func (m *InMemorySuspendedStateStore) Save(_ context.Context, state *SuspendedState, ttl time.Duration) error {
	if state == nil {
		return errors.New("nil suspended state")
	}
	if ttl <= 0 {
		ttl = DefaultSuspensionTTL
	}

	key := fmt.Sprintf("pend:%s:%s", state.ProjectID, state.RequestID)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[key] = inMemoryItem{
		state:     *state,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// Get retrieves state if present and not expired.
func (m *InMemorySuspendedStateStore) Get(_ context.Context, projectID, requestID string) (*SuspendedState, error) {
	key := fmt.Sprintf("pend:%s:%s", projectID, requestID)
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.items[key]
	if !exists {
		return nil, ErrStateNotFound
	}

	if time.Now().After(item.expiresAt) {
		return nil, ErrStateNotFound
	}

	cp := item.state
	return &cp, nil
}

// Delete removes state from memory.
func (m *InMemorySuspendedStateStore) Delete(_ context.Context, projectID, requestID string) error {
	key := fmt.Sprintf("pend:%s:%s", projectID, requestID)
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.items, key)
	return nil
}

// Consume atomically retrieves and deletes state from memory.
func (m *InMemorySuspendedStateStore) Consume(_ context.Context, projectID, env, reqID string) (*SuspendedState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := suspendedKey(projectID, env, reqID)
	item, exists := m.items[key]
	if !exists {
		legacyKey := fmt.Sprintf("pend:%s:%s", projectID, reqID)
		item, exists = m.items[legacyKey]
		if !exists {
			return nil, ErrStateNotFound
		}
		delete(m.items, legacyKey)
	} else {
		delete(m.items, key)
	}

	if time.Now().After(item.expiresAt) {
		return nil, ErrStateNotFound
	}

	cp := item.state
	return &cp, nil
}

// ContinuationManager generates and validates HMAC-signed confirmation tokens
// and manages suspended state persistence.
type ContinuationManager struct {
	store   SuspendedStateStore
	hmacKey []byte
}

// NewContinuationManager creates a new ContinuationManager.
func NewContinuationManager(store SuspendedStateStore, hmacKey []byte) *ContinuationManager {
	if store == nil {
		store = NewInMemorySuspendedStateStore()
	}
	if len(hmacKey) == 0 {
		hmacKey = []byte("default-beavend-hmac-continuation-secret")
	}
	return &ContinuationManager{
		store:   store,
		hmacKey: hmacKey,
	}
}

// Store returns the underlying SuspendedStateStore.
func (cm *ContinuationManager) Store() SuspendedStateStore {
	return cm.store
}

// ComputeArgsHash generates a canonical SHA-256 hex string of the arguments.
func ComputeArgsHash(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		trimmed = "{}"
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		canonicalJSON, err := json.Marshal(parsed)
		if err == nil {
			h := sha256.Sum256(canonicalJSON)
			return hex.EncodeToString(h[:])
		}
	}
	h := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(h[:])
}

// IssueConfirmationToken signs and generates a URL-safe confirmation token.
func (cm *ContinuationManager) IssueConfirmationToken(state *SuspendedState, toolCall core.ToolCall, ttl time.Duration) (string, error) {
	if state == nil {
		return "", errors.New("nil state")
	}
	if ttl == 0 {
		ttl = DefaultSuspensionTTL
	}

	claims := ConfirmationTokenClaims{
		ProjectID:   state.ProjectID,
		Environment: state.Environment,
		AIAppID:     state.AIAppID,
		RequestID:   state.RequestID,
		ToolID:      toolCall.ID,
		ArgsHash:    ComputeArgsHash(toolCall.Arguments),
		Issuer:      state.Issuer,
		Subject:     state.Subject,
		ExpiresAt:   time.Now().Add(ttl).Unix(),
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal token claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, cm.hmacKey)
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s.%s", payloadB64, sigB64), nil
}

// ValidateConfirmationToken validates token signature, expiration, and extracts claims.
func (cm *ContinuationManager) ValidateConfirmationToken(tokenStr string) (*ConfirmationTokenClaims, error) {
	parts := splitToken(tokenStr)
	if len(parts) != 2 {
		return nil, ErrTokenInvalidFormat
	}

	payloadB64, sigB64 := parts[0], parts[1]

	expectedMAC := hmac.New(sha256.New, cm.hmacKey)
	expectedMAC.Write([]byte(payloadB64))
	expectedSig := expectedMAC.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrTokenInvalidFormat)
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrTokenSignatureInvalid
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload encoding", ErrTokenInvalidFormat)
	}

	var claims ConfirmationTokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalidFormat, err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	return &claims, nil
}

// ValidateResumption verifies that the caller subject matches the token,
// arguments match the signed hash, and retrieves the suspended state.
func (cm *ContinuationManager) ValidateResumption(ctx context.Context, tokenStr string, callerID *core.Identity) (*SuspendedState, *ConfirmationTokenClaims, error) {
	claims, err := cm.ValidateConfirmationToken(tokenStr)
	if err != nil {
		return nil, nil, err
	}

	// 1. Verify caller Subject (claim #8)
	if claims.Subject != "" {
		if callerID == nil || callerID.Subject == "" {
			return nil, nil, fmt.Errorf("%w: token bound to subject %q but caller has no subject", ErrSubjectMismatch, claims.Subject)
		}
		if claims.Subject != callerID.Subject {
			return nil, nil, fmt.Errorf("%w: token subject %q != caller %q", ErrSubjectMismatch, claims.Subject, callerID.Subject)
		}
	}

	// 2. Verify ProjectID isolation (claim #1)
	if callerID != nil && callerID.Project != "" && claims.ProjectID != "" {
		if callerID.Project != claims.ProjectID {
			return nil, nil, fmt.Errorf("%w: token project %q != caller project %q", ErrSubjectMismatch, claims.ProjectID, callerID.Project)
		}
	}

	// 3. Verify Environment isolation (claim #2)
	if callerID != nil && callerID.Environment != "" && claims.Environment != "" {
		if callerID.Environment != claims.Environment {
			return nil, nil, fmt.Errorf("%w: token environment %q != caller environment %q", ErrSubjectMismatch, claims.Environment, callerID.Environment)
		}
	}

	// 4. Verify Issuer (claim #7)
	if callerID != nil && callerID.Issuer != "" && claims.Issuer != "" {
		if callerID.Issuer != claims.Issuer {
			return nil, nil, fmt.Errorf("%w: token issuer %q != caller issuer %q", ErrSubjectMismatch, claims.Issuer, callerID.Issuer)
		}
	}

	// Atomically retrieve and consume suspended state (Control #10: eliminate TOCTOU replay window)
	state, err := cm.store.Consume(ctx, claims.ProjectID, claims.Environment, claims.RequestID)
	if err != nil {
		return nil, nil, err
	}

	// Verify state consistency across bound claims
	if claims.ProjectID != "" && state.ProjectID != "" && state.ProjectID != claims.ProjectID {
		return nil, nil, fmt.Errorf("%w: state project %q != token project %q", ErrSubjectMismatch, state.ProjectID, claims.ProjectID)
	}
	if claims.Environment != "" && state.Environment != "" && state.Environment != claims.Environment {
		return nil, nil, fmt.Errorf("%w: state environment %q != token environment %q", ErrSubjectMismatch, state.Environment, claims.Environment)
	}
	if claims.AIAppID != "" && state.AIAppID != "" && state.AIAppID != claims.AIAppID {
		return nil, nil, fmt.Errorf("%w: state app %q != token app %q", ErrSubjectMismatch, state.AIAppID, claims.AIAppID)
	}
	if claims.Subject != "" && state.Subject != "" && state.Subject != claims.Subject {
		return nil, nil, fmt.Errorf("%w: state subject %q != token subject %q", ErrSubjectMismatch, state.Subject, claims.Subject)
	}
	if claims.Issuer != "" && state.Issuer != "" && state.Issuer != claims.Issuer {
		return nil, nil, fmt.Errorf("%w: state issuer %q != token issuer %q", ErrSubjectMismatch, state.Issuer, claims.Issuer)
	}

	// Verify tool ID and arguments integrity (claim #5: ToolID, claim #6: ArgsHash)
	matched := false
	var targetCall core.ToolCall

	if state.PendingToolCall.ID == claims.ToolID {
		matched = true
		targetCall = state.PendingToolCall
	} else {
		for _, call := range state.PendingToolCalls {
			if call.ID == claims.ToolID {
				matched = true
				targetCall = call
				break
			}
		}
	}

	if !matched && claims.ToolID != "" {
		return nil, nil, fmt.Errorf("tool ID mismatch: token had %q", claims.ToolID)
	}

	expectedHash := ComputeArgsHash(targetCall.Arguments)
	if expectedHash != claims.ArgsHash {
		return nil, nil, fmt.Errorf("%w: expected %s, token had %s", ErrArgsHashMismatch, expectedHash, claims.ArgsHash)
	}

	return state, claims, nil
}

func splitToken(token string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	parts = append(parts, token[start:])
	return parts
}
