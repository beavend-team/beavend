package routing

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrModelNotFound indicates the logical model or model routing was not found in the snapshot.
	ErrModelNotFound = errors.New("model_routing: model not found")
	// ErrAdapterNotFound indicates no adapter was registered for the specified adapter type.
	ErrAdapterNotFound = errors.New("model_routing: adapter not found")
	// ErrNoAvailableDeployments indicates all primary and fallback deployments have been exhausted.
	ErrNoAvailableDeployments = errors.New("model_routing: all deployments exhausted")
)

// ResolvedRoute holds the resolved model routing, concrete provider adapter, and deployment.
type ResolvedRoute struct {
	ModelRouting *core.ModelRouting
	Adapter      core.ProviderAdapter
	Deployment   string
}

// Resolver resolves logical models to concrete deployments and provider adapters.
type Resolver struct {
	mu       sync.RWMutex
	adapters map[string]core.ProviderAdapter
}

// NewResolver creates a new Resolver with empty adapters.
func NewResolver() *Resolver {
	return &Resolver{
		adapters: make(map[string]core.ProviderAdapter),
	}
}

// RegisterAdapter registers a provider adapter for an adapter identifier (e.g. "litellm", "openai_compat").
func (r *Resolver) RegisterAdapter(name string, adapter core.ProviderAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = adapter
}

// GetAdapter retrieves the adapter for the given name.
func (r *Resolver) GetAdapter(name string) (core.ProviderAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// Resolve maps the request and app configuration to a concrete model deployment and adapter.
// Resolves entirely in memory from *core.Snapshot without touching PostgreSQL.
func (r *Resolver) Resolve(snap *core.Snapshot, app *core.AIApp, req *core.ChatRequest) (*ResolvedRoute, error) {
	if snap == nil {
		return nil, errors.New("nil snapshot")
	}

	logicalModel := r.selectLogicalModel(snap, app, req)
	if logicalModel == "" {
		return nil, fmt.Errorf("%w: no logical model specified for app %q", ErrModelNotFound, appID(app))
	}

	mRouting, ok := snap.Model(logicalModel)
	if !ok {
		return nil, fmt.Errorf("%w: logical model %q not found in snapshot", ErrModelNotFound, logicalModel)
	}

	adapterName := mRouting.Adapter
	if adapterName == "" {
		adapterName = "litellm" // Default provider adapter
	}

	adapter, ok := r.GetAdapter(adapterName)
	if !ok {
		return nil, fmt.Errorf("%w: no adapter registered for %q", ErrAdapterNotFound, adapterName)
	}

	return &ResolvedRoute{
		ModelRouting: mRouting,
		Adapter:      adapter,
		Deployment:   mRouting.Deployment,
	}, nil
}

// ResolveFallback attempts to find an unattempted fallback deployment.
func (r *Resolver) ResolveFallback(current *ResolvedRoute, attemptedDeployments map[string]bool) (*ResolvedRoute, error) {
	if current == nil || current.ModelRouting == nil {
		return nil, errors.New("nil current route")
	}

	mRouting := current.ModelRouting
	for _, fb := range mRouting.FallbackDeployments {
		if !attemptedDeployments[fb] {
			return &ResolvedRoute{
				ModelRouting: mRouting,
				Adapter:      current.Adapter,
				Deployment:   fb,
			}, nil
		}
	}

	return nil, ErrNoAvailableDeployments
}

func (r *Resolver) selectLogicalModel(snap *core.Snapshot, app *core.AIApp, req *core.ChatRequest) string {
	// 1. Explicit request model override takes precedence
	if req != nil && req.Model != "" {
		return req.Model
	}

	// 2. Multimodal inspection: if request contains attachments with images, try "vision"
	if req != nil {
		if hasImageAttachment(req) {
			if _, ok := snap.Model("vision"); ok {
				return "vision"
			}
		}
		if req.Metadata != nil {
			if req.Metadata["modality"] == "image" {
				if _, ok := snap.Model("vision"); ok {
					return "vision"
				}
			}
			if req.Metadata["complexity"] == "high" {
				if _, ok := snap.Model("reasoning"); ok {
					return "reasoning"
				}
			}
		}
	}

	// 3. AI App default model routing
	if app != nil && app.ModelRoutingID != "" {
		return app.ModelRoutingID
	}

	// 4. Default fallback: "fast"
	if _, ok := snap.Model("fast"); ok {
		return "fast"
	}

	return ""
}

func hasImageAttachment(req *core.ChatRequest) bool {
	for _, att := range req.Attachments {
		if att.Type == "image" || strings.HasPrefix(strings.ToLower(att.MimeType), "image/") {
			return true
		}
	}
	return false
}

func appID(app *core.AIApp) string {
	if app != nil {
		return app.ID
	}
	return ""
}
