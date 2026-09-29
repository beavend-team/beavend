package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// EngineConfig holds dependencies for the memory and context engine.
type EngineConfig struct {
	DefaultProvider core.MemoryProvider
	ThreadStore     core.ThreadStore
	Embedder        core.ProviderAdapter // Optional: to vectorize text queries on the fly
}

// Engine coordinates server-side conversation threads and user-scoped semantic memory.
type Engine struct {
	defaultProvider core.MemoryProvider
	threadStore     core.ThreadStore
	embedder        core.ProviderAdapter
}

// NewEngine constructs a new Memory Engine.
func NewEngine(cfg EngineConfig) *Engine {
	provider := cfg.DefaultProvider
	if provider == nil {
		provider = NewLocalMemoryProvider()
	}

	threadStore := cfg.ThreadStore
	if threadStore == nil {
		threadStore = NewMemoryThreadStore()
	}

	return &Engine{
		defaultProvider: provider,
		threadStore:     threadStore,
		embedder:        cfg.Embedder,
	}
}

// DefaultProvider returns the configured default MemoryProvider.
func (e *Engine) DefaultProvider() core.MemoryProvider {
	return e.defaultProvider
}

// ThreadStore returns the configured ThreadStore.
func (e *Engine) ThreadStore() core.ThreadStore {
	return e.threadStore
}

// ResolveProvider returns the appropriate MemoryProvider for an AI App.
// If the app configures an implementor-hosted HTTP memory provider, it instantiates an HTTPMemoryProvider.
func (e *Engine) ResolveProvider(app *core.AIApp) (core.MemoryProvider, error) {
	if app == nil || !app.Memory.Enabled {
		return e.defaultProvider, nil
	}

	if app.Memory.ProviderType == "http" && app.Memory.HTTPURL != "" {
		return NewHTTPMemoryProvider(HTTPMemoryProviderConfig{
			EndpointURL: app.Memory.HTTPURL,
			AuthToken:   app.Memory.HTTPAuthToken,
			Timeout:     5 * time.Second,
		})
	}

	return e.defaultProvider, nil
}

// PurgeSubjectData implements the GDPR privacy purge hook for context, threads, and memories.
func (e *Engine) PurgeSubjectData(ctx context.Context, projectID, env, issuer, subject string) error {
	var errs []error

	if e.defaultProvider != nil {
		if err := e.defaultProvider.DeleteBySubject(ctx, projectID, issuer, subject); err != nil {
			errs = append(errs, fmt.Errorf("memory delete by subject: %w", err))
		}
	}

	if e.threadStore != nil {
		if err := e.threadStore.DeleteThreadsBySubject(ctx, projectID, env, issuer, subject); err != nil {
			errs = append(errs, fmt.Errorf("threads delete by subject: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("purge subject memory errors: %v", errs)
	}

	slog.InfoContext(ctx, "privacy purge: memories and threads purged",
		"project_id", projectID,
		"env", env,
		"issuer", issuer,
		"subject", subject,
	)

	return nil
}
