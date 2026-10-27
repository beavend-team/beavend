package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/memory"
)

// KnowledgeSearchBackend defines the interface for retrieving passages by keyword and vector search.
type KnowledgeSearchBackend interface {
	SearchKeyword(ctx context.Context, appID, projectID, env, query string, limit int) ([]core.SearchResult, error)
	SearchVector(ctx context.Context, appID, projectID, env, query string, limit int) ([]core.SearchResult, error)
}

// MemoryKnowledgeSearchBackend provides an in-memory test implementation of KnowledgeSearchBackend.
type MemoryKnowledgeSearchBackend struct {
	mu             sync.RWMutex
	KeywordResults map[string][]core.SearchResult
	VectorResults  map[string][]core.SearchResult
}

// NewMemoryKnowledgeSearchBackend creates a new MemoryKnowledgeSearchBackend.
func NewMemoryKnowledgeSearchBackend() *MemoryKnowledgeSearchBackend {
	return &MemoryKnowledgeSearchBackend{
		KeywordResults: make(map[string][]core.SearchResult),
		VectorResults:  make(map[string][]core.SearchResult),
	}
}

func (b *MemoryKnowledgeSearchBackend) SearchKeyword(_ context.Context, _, _, _, query string, limit int) ([]core.SearchResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	res := b.KeywordResults[q]
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

func (b *MemoryKnowledgeSearchBackend) SearchVector(_ context.Context, _, _, _, query string, limit int) ([]core.SearchResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	res := b.VectorResults[q]
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

// ComputeKnowledgeCacheKey generates a cache key scoped to (ai_app_id, project_id, env, kb_revision, query_hash).
func ComputeKnowledgeCacheKey(appID, projectID, env string, kbRevision int, query string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(query))))
	return fmt.Sprintf("kb:search:%s:%s:%s:%d:%s", appID, projectID, env, kbRevision, hex.EncodeToString(h[:]))
}

// KnowledgeSearchExecutor implements core.Executor for the knowledge_search tool.
type KnowledgeSearchExecutor struct {
	backend    KnowledgeSearchBackend
	cacheStore core.CacheStore
	kbRevision int
}

// NewKnowledgeSearchExecutor creates a new KnowledgeSearchExecutor.
func NewKnowledgeSearchExecutor(backend KnowledgeSearchBackend, cacheStore core.CacheStore, kbRevision int) *KnowledgeSearchExecutor {
	if kbRevision <= 0 {
		kbRevision = 1
	}
	return &KnowledgeSearchExecutor{
		backend:    backend,
		cacheStore: cacheStore,
		kbRevision: kbRevision,
	}
}

func (e *KnowledgeSearchExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if call.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				Content: fmt.Sprintf("invalid arguments json: %v", err),
				IsError: true,
			}, nil
		}
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "query parameter cannot be empty",
			IsError: true,
		}, nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}

	snap, ok := core.SnapshotFromContext(ctx)
	appID, _ := core.AppIDFromContext(ctx)
	var projectID, env string
	kbRevision := e.kbRevision
	if ok && snap != nil {
		projectID = snap.ProjectID
		env = snap.Environment
		if snap.Revision > 0 {
			kbRevision = int(snap.Revision)
		}
	}
	if appID == "" || projectID == "" || env == "" {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "tenant context missing: appID, projectID, and environment are required",
			IsError: true,
		}, nil
	}
	if kbRevision <= 0 {
		kbRevision = 1
	}

	// 1. Check scoped cache
	cacheKey := ComputeKnowledgeCacheKey(appID, projectID, env, kbRevision, query)
	if e.cacheStore != nil {
		if cached, err := e.cacheStore.Get(ctx, cacheKey); err == nil && len(cached) > 0 {
			var cachedRes core.ToolResult
			if err := json.Unmarshal(cached, &cachedRes); err == nil {
				cachedRes.CallID = call.ID
				return cachedRes, nil
			}
		}
	}

	if e.backend == nil {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "knowledge search backend not configured",
			IsError: true,
		}, nil
	}

	// 2. Query keyword and vector backends
	kwResults, err := e.backend.SearchKeyword(ctx, appID, projectID, env, query, limit*2)
	if err != nil {
		kwResults = nil
	}
	vecResults, err := e.backend.SearchVector(ctx, appID, projectID, env, query, limit*2)
	if err != nil {
		vecResults = nil
	}

	// 3. Reciprocal Rank Fusion
	merged := memory.HybridRRF(kwResults, vecResults, memory.DefaultRRFConstant)
	if len(merged) > limit {
		merged = merged[:limit]
	}

	if len(merged) == 0 {
		res := core.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("No relevant knowledge base documents found for query %q.", query),
		}
		return res, nil
	}

	// 4. Format and sanitize passages
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Retrieved %d relevant knowledge passages for query %q:\n\n", len(merged), query))
	for idx, item := range merged {
		escapedContent := EscapeClosingToolResultTags(item.Content)
		title := item.ID
		if t, ok := item.Metadata["title"].(string); ok && t != "" {
			title = t
		}
		sb.WriteString(fmt.Sprintf("%d. [%s] (Score: %.4f)\n%s\n\n", idx+1, title, item.Score, escapedContent))
	}

	finalContent := strings.TrimSpace(sb.String())
	result := core.ToolResult{
		CallID:  call.ID,
		Content: finalContent,
	}

	// 5. Store in cache (15m TTL)
	if e.cacheStore != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = e.cacheStore.Set(ctx, cacheKey, data, 15*time.Minute)
		}
	}

	return result, nil
}

// ProposeMemoryExecutor implements core.Executor for the propose_memory tool.
type ProposeMemoryExecutor struct {
	store memory.DocumentMemoryStore
}

// NewProposeMemoryExecutor creates a new ProposeMemoryExecutor.
func NewProposeMemoryExecutor(store memory.DocumentMemoryStore) *ProposeMemoryExecutor {
	return &ProposeMemoryExecutor{store: store}
}

func (e *ProposeMemoryExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	var args struct {
		TargetScope string `json:"target_scope"`
		Key         string `json:"key"`
		Value       string `json:"value"`
		Reason      string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("invalid arguments json: %v", err),
			IsError: true,
		}, nil
	}

	if args.TargetScope != core.DocumentMemoryScopeProject &&
		args.TargetScope != core.DocumentMemoryScopeApp &&
		args.TargetScope != core.DocumentMemoryScopeUser {
		return core.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("invalid target_scope %q: must be 'project', 'app', or 'user'", args.TargetScope),
			IsError: true,
		}, nil
	}

	if strings.TrimSpace(args.Key) == "" {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "memory key cannot be empty",
			IsError: true,
		}, nil
	}

	if len(args.Value) > core.MaxDocumentMemoryBytes {
		return core.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("memory value exceeds maximum size of %d bytes", core.MaxDocumentMemoryBytes),
			IsError: true,
		}, nil
	}

	if e.store == nil {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "document memory store not configured",
			IsError: true,
		}, nil
	}

	snap, ok := core.SnapshotFromContext(ctx)
	if !ok || snap == nil || snap.ProjectID == "" || snap.Environment == "" {
		return core.ToolResult{
			CallID:  call.ID,
			Content: "tenant context missing: projectID and environment are required",
			IsError: true,
		}, nil
	}
	appID, _ := core.AppIDFromContext(ctx)

	var targetID string
	switch args.TargetScope {
	case core.DocumentMemoryScopeUser:
		if strings.TrimSpace(id.Subject) == "" {
			return core.ToolResult{
				CallID:  call.ID,
				Content: "user identity required for user-scoped document memory proposal",
				IsError: true,
			}, nil
		}
		targetID = id.Subject
	case core.DocumentMemoryScopeApp:
		if appID == "" {
			return core.ToolResult{
				CallID:  call.ID,
				Content: "app context required for app-scoped document memory proposal",
				IsError: true,
			}, nil
		}
		targetID = appID
	case core.DocumentMemoryScopeProject:
		targetID = ""
	}

	prop := &core.DocumentMemoryProposal{
		ProjectID:   snap.ProjectID,
		Environment: snap.Environment,
		TargetScope: args.TargetScope,
		TargetID:    targetID,
		Key:         strings.TrimSpace(args.Key),
		Value:       args.Value,
		Reason:      args.Reason,
		ProposedBy:  id.Subject,
		CreatedAt:   time.Now().UTC(),
	}

	if err := e.store.CreateProposal(ctx, prop); err != nil {
		if errors.Is(err, memory.ErrMemoryTooLarge) {
			return core.ToolResult{
				CallID:  call.ID,
				Content: "memory value exceeds 2048 bytes limit",
				IsError: true,
			}, nil
		}
		return core.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("failed to create memory proposal: %v", err),
			IsError: true,
		}, nil
	}

	respMap := map[string]any{
		"status":       "proposal_created",
		"proposal_id":  prop.ID,
		"target_scope": prop.TargetScope,
		"key":          prop.Key,
		"note":         "Memory proposal submitted for admin review and approval.",
	}
	outJSON, _ := json.Marshal(respMap)

	return core.ToolResult{
		CallID:  call.ID,
		Content: string(outJSON),
	}, nil
}
