package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/providers/openaicompat"
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
	AdapterName  string
	Snapshot     *core.Snapshot
}

// Resolver resolves logical models to concrete deployments and provider adapters.
type Resolver struct {
	mu              sync.RWMutex
	adapters        map[string]core.ProviderAdapter
	adapterFactory  func(prov *core.ProviderConfig) (core.ProviderAdapter, error)
	cachedAdapters  map[string]core.ProviderAdapter
	policyEvaluator *policy.Evaluator
}

// NewResolver creates a new Resolver with empty adapters.
func NewResolver() *Resolver {
	return &Resolver{
		adapters:       make(map[string]core.ProviderAdapter),
		cachedAdapters: make(map[string]core.ProviderAdapter),
	}
}

// RegisterAdapter registers a provider adapter for an adapter identifier (e.g. "litellm", "openai_compat").
func (r *Resolver) RegisterAdapter(name string, adapter core.ProviderAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = adapter
}

// SetProviderAdapterFactory registers a custom factory for creating adapters from provider configs.
func (r *Resolver) SetProviderAdapterFactory(factory func(prov *core.ProviderConfig) (core.ProviderAdapter, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapterFactory = factory
}

// SetPolicyEvaluator configures an explicit policy evaluator for routing rule and override evaluations.
func (r *Resolver) SetPolicyEvaluator(evaluator *policy.Evaluator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policyEvaluator = evaluator
}

func (r *Resolver) getEvaluator() (*policy.Evaluator, error) {
	r.mu.RLock()
	ev := r.policyEvaluator
	r.mu.RUnlock()
	if ev != nil {
		return ev, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policyEvaluator != nil {
		return r.policyEvaluator, nil
	}
	newEv, err := policy.NewEvaluator(nil)
	if err != nil {
		return nil, fmt.Errorf("init policy evaluator for routing: %w", err)
	}
	r.policyEvaluator = newEv
	return newEv, nil
}

// GetAdapter retrieves the adapter for the given name.
func (r *Resolver) GetAdapter(name string) (core.ProviderAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// GetAdapterWithSnapshot retrieves or instantiates an adapter, looking into the Snapshot's configured providers.
func (r *Resolver) GetAdapterWithSnapshot(name string, snap *core.Snapshot) (core.ProviderAdapter, bool) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = "litellm"
	}

	// 1. Direct registered adapter check (e.g. "litellm", "openai_compat")
	if a, ok := r.GetAdapter(trimmedName); ok {
		return a, true
	}

	// 2. Snapshot configured provider (e.g. "openrouter", "groq", "openai", "together")
	if snap != nil {
		if prov, found := snap.Provider(trimmedName); found && prov != nil {
			cacheKey := fmt.Sprintf("%s:%s:%s:%s", prov.ProjectID, prov.Environment, prov.ProviderName, prov.BaseURL)
			r.mu.RLock()
			cached, exists := r.cachedAdapters[cacheKey]
			r.mu.RUnlock()
			if exists && cached != nil {
				return cached, true
			}

			var newAdapter core.ProviderAdapter
			if r.adapterFactory != nil {
				var err error
				newAdapter, err = r.adapterFactory(prov)
				if err != nil {
					newAdapter = nil
				}
			}
			if newAdapter == nil {
				newAdapter, _ = openaicompat.NewAdapter(openaicompat.Config{
					BaseURL: prov.BaseURL,
					APIKey:  prov.APIKey,
				})
			}
			if newAdapter != nil {
				r.mu.Lock()
				r.cachedAdapters[cacheKey] = newAdapter
				r.mu.Unlock()
				return newAdapter, true
			}
		}
	}

	// 3. Fallbacks
	if strings.Contains(strings.ToLower(trimmedName), "openai") {
		if a, ok := r.GetAdapter("openai_compat"); ok {
			return a, true
		}
		if a, ok := r.GetAdapter("openaicompat"); ok {
			return a, true
		}
	}
	if a, ok := r.GetAdapter("litellm"); ok {
		return a, true
	}

	return nil, false
}

// ResolveModel resolves the concrete provider adapter and deployment for a pre-selected logical model.
func (r *Resolver) ResolveModel(snap *core.Snapshot, logicalModel string) (*ResolvedRoute, error) {
	if snap == nil {
		return nil, errors.New("nil snapshot")
	}

	trimmedModel := strings.TrimSpace(logicalModel)
	if trimmedModel == "" {
		return nil, fmt.Errorf("%w: empty logical model", ErrModelNotFound)
	}

	mRouting, ok := snap.Model(trimmedModel)
	if !ok {
		return nil, fmt.Errorf("%w: logical model %q not found in snapshot", ErrModelNotFound, trimmedModel)
	}

	adapterName := mRouting.Adapter
	if adapterName == "" {
		adapterName = "litellm" // Default provider adapter
	}

	adapter, ok := r.GetAdapterWithSnapshot(adapterName, snap)
	if !ok {
		return nil, fmt.Errorf("%w: no adapter registered for %q", ErrAdapterNotFound, adapterName)
	}

	return &ResolvedRoute{
		ModelRouting: mRouting,
		Adapter:      adapter,
		Deployment:   mRouting.Deployment,
		AdapterName:  adapterName,
		Snapshot:     snap,
	}, nil
}

// Resolve maps the request and app configuration to a concrete model deployment and adapter.
// Resolves entirely in memory from *core.Snapshot without touching PostgreSQL.
func (r *Resolver) Resolve(snap *core.Snapshot, app *core.AIApp, req *core.ChatRequest) (*ResolvedRoute, error) {
	if snap == nil {
		return nil, errors.New("nil snapshot")
	}

	logicalModel, err := r.SelectLogicalModel(context.Background(), snap, app, req, nil, nil)
	if err != nil {
		return nil, err
	}

	return r.ResolveModel(snap, logicalModel)
}

// SelectLogicalModel selects the logical model name for a request based on:
// 1. Explicit request model override (policy-gated)
// 2. Per-app routing rules (first match wins)
// 3. App default model
// Returns ErrModelNotFound if no route matches or model not found.
func (r *Resolver) SelectLogicalModel(
	ctx context.Context,
	snap *core.Snapshot,
	app *core.AIApp,
	req *core.ChatRequest,
	id *core.Identity,
	meta map[string]any,
) (string, error) {
	if snap == nil {
		return "", errors.New("nil snapshot")
	}
	if app == nil {
		return "", errors.New("nil ai app")
	}

	// 1. Explicit request model override (policy-gated)
	if req != nil && strings.TrimSpace(req.Model) != "" {
		reqModel := strings.TrimSpace(req.Model)
		if _, ok := snap.Model(reqModel); !ok {
			return "", fmt.Errorf("%w: requested model %q not found in snapshot", ErrModelNotFound, reqModel)
		}

		// Policy gating check if "allow_model_override" policy exists
		if snap.Policies != nil {
			if _, hasPolicy := snap.Policies["allow_model_override"]; hasPolicy {
				evaluator, err := r.getEvaluator()
				if err != nil {
					return "", fmt.Errorf("evaluator error: %w", err)
				}
				combinedMeta := make(map[string]any)
				if meta != nil {
					for k, v := range meta {
						combinedMeta[k] = v
					}
				}
				if req.Metadata != nil {
					for k, v := range req.Metadata {
						combinedMeta[k] = v
					}
				}
				combinedMeta["requested_model"] = reqModel
				allowed, evalErr := evaluator.EvaluatePolicy(ctx, snap, "allow_model_override", id, combinedMeta)
				if evalErr != nil || !allowed {
					return "", fmt.Errorf("model override %q denied by policy", reqModel)
				}
			}
		}

		return reqModel, nil
	}

	// 2. Rule evaluation (first match wins)
	if app.ModelRoutes != nil && len(app.ModelRoutes.Rules) > 0 {
		inputMods := requestInputModalities(req)
		for _, rule := range app.ModelRoutes.Rules {
			if r.matchCondition(ctx, snap, rule.When, req, inputMods, id, meta) {
				useModel := strings.TrimSpace(rule.Use)
				if useModel == "" {
					continue
				}
				if _, ok := snap.Model(useModel); !ok {
					return "", fmt.Errorf("%w: rule target model %q not found in snapshot", ErrModelNotFound, useModel)
				}
				return useModel, nil
			}
		}
	}

	// 3. AI App default model routing
	defaultModel := ""
	if app.ModelRoutes != nil && strings.TrimSpace(app.ModelRoutes.Default) != "" {
		defaultModel = strings.TrimSpace(app.ModelRoutes.Default)
	} else if strings.TrimSpace(app.ModelRoutingID) != "" {
		defaultModel = strings.TrimSpace(app.ModelRoutingID)
	}

	if defaultModel != "" {
		if _, ok := snap.Model(defaultModel); !ok {
			return "", fmt.Errorf("%w: default model %q not found in snapshot", ErrModelNotFound, defaultModel)
		}
		return defaultModel, nil
	}

	return "", fmt.Errorf("%w: no matching route for app %q", ErrModelNotFound, appID(app))
}

func (r *Resolver) matchCondition(
	ctx context.Context,
	snap *core.Snapshot,
	cond core.RouteCondition,
	req *core.ChatRequest,
	inputMods map[string]bool,
	id *core.Identity,
	meta map[string]any,
) bool {
	// If rule condition is completely empty, it matches unconditionally
	if len(cond.InputHas) == 0 && len(cond.Output) == 0 && len(cond.Metadata) == 0 && cond.Expr == "" {
		return true
	}

	// 1. Input modalities check
	if len(cond.InputHas) > 0 {
		for _, reqMod := range cond.InputHas {
			if !inputMods[strings.ToLower(strings.TrimSpace(reqMod))] {
				return false
			}
		}
	}

	// 2. Output modalities check
	if len(cond.Output) > 0 {
		reqOutputMods := make(map[string]bool)
		if req != nil && len(req.OutputModalities) > 0 {
			for _, m := range req.OutputModalities {
				reqOutputMods[strings.ToLower(strings.TrimSpace(m))] = true
			}
		} else {
			reqOutputMods["text"] = true
		}
		for _, reqMod := range cond.Output {
			if !reqOutputMods[strings.ToLower(strings.TrimSpace(reqMod))] {
				return false
			}
		}
	}

	// 3. Metadata check
	if len(cond.Metadata) > 0 {
		if req == nil || req.Metadata == nil {
			return false
		}
		for k, expectedVal := range cond.Metadata {
			if actualVal, ok := req.Metadata[k]; !ok || actualVal != expectedVal {
				return false
			}
		}
	}

	// 4. CEL Expression check
	if cond.Expr != "" {
		evaluator, err := r.getEvaluator()
		if err != nil {
			return false
		}
		combinedMeta := make(map[string]any)
		if meta != nil {
			for k, v := range meta {
				combinedMeta[k] = v
			}
		}
		if req != nil && req.Metadata != nil {
			for k, v := range req.Metadata {
				combinedMeta[k] = v
			}
		}
		matched, evalErr := evaluator.EvaluatePolicy(ctx, snap, cond.Expr, id, combinedMeta)
		if evalErr != nil || !matched {
			return false
		}
	}

	return true
}

func requestInputModalities(req *core.ChatRequest) map[string]bool {
	mods := make(map[string]bool)
	if req == nil {
		return mods
	}
	if len(req.Messages) > 0 {
		mods["text"] = true
	}
	for _, att := range req.Attachments {
		if t := strings.ToLower(strings.TrimSpace(att.Type)); t != "" {
			mods[t] = true
		}
		mime := strings.ToLower(strings.TrimSpace(att.MimeType))
		switch {
		case strings.HasPrefix(mime, "image/"):
			mods["image"] = true
		case strings.HasPrefix(mime, "audio/"):
			mods["audio"] = true
		case strings.HasPrefix(mime, "video/"):
			mods["video"] = true
		case strings.HasPrefix(mime, "application/pdf"):
			mods["document"] = true
		}
	}
	if req.Metadata != nil {
		if m := strings.ToLower(strings.TrimSpace(req.Metadata["modality"])); m != "" {
			mods[m] = true
		}
		if m := strings.ToLower(strings.TrimSpace(req.Metadata["input_modality"])); m != "" {
			mods[m] = true
		}
	}
	return mods
}

// ParseFallback parses a fallback deployment string into (adapterName, deployment).
// Supported formats:
// 1. JSON object string: {"provider":"groq","deployment":"llama-3"} or {"adapter":"groq","deployment":"llama-3"}
// 2. Colon delimiter: "provider:deployment" (e.g. "openai:gpt-4o-mini")
// 3. Slash delimiter: "provider/deployment" (e.g. "groq/llama-3.3-70b-versatile", "openrouter/meta-llama/llama-3-8b")
// 4. Bare deployment name: "gpt-4o-mini" (defaults to defaultAdapter)
func (r *Resolver) ParseFallback(fb string, defaultAdapter string, snap *core.Snapshot) (string, string) {
	fb = strings.TrimSpace(fb)
	if fb == "" {
		return defaultAdapter, ""
	}

	// 1. Check if JSON object string
	if strings.HasPrefix(fb, "{") && strings.HasSuffix(fb, "}") {
		var obj struct {
			Adapter    string `json:"adapter"`
			Provider   string `json:"provider"`
			Deployment string `json:"deployment"`
			Model      string `json:"model"`
		}
		if err := json.Unmarshal([]byte(fb), &obj); err == nil {
			dep := obj.Deployment
			if dep == "" {
				dep = obj.Model
			}
			ad := obj.Adapter
			if ad == "" {
				ad = obj.Provider
			}
			if ad == "" {
				ad = defaultAdapter
			}
			if dep != "" {
				return ad, dep
			}
		}
	}

	// 2. Check for colon delimiter "provider:deployment"
	if idx := strings.Index(fb, ":"); idx != -1 {
		prefix := strings.TrimSpace(fb[:idx])
		suffix := strings.TrimSpace(fb[idx+1:])
		if prefix != "" && suffix != "" {
			return prefix, suffix
		}
	}

	// 3. Check for slash delimiter "provider/deployment"
	if idx := strings.Index(fb, "/"); idx != -1 {
		prefix := strings.TrimSpace(fb[:idx])
		suffix := strings.TrimSpace(fb[idx+1:])
		if prefix != "" && suffix != "" {
			// Check if prefix is a known provider in the snapshot
			if snap != nil {
				if _, ok := snap.Provider(prefix); ok {
					return prefix, suffix
				}
			}
			// Check if prefix is a registered adapter
			r.mu.RLock()
			_, isRegistered := r.adapters[prefix]
			r.mu.RUnlock()
			if isRegistered {
				return prefix, suffix
			}
			// Check common standard provider/adapter identifiers
			switch strings.ToLower(prefix) {
			case "litellm", "openai", "openaicompat", "openai_compat", "anthropic",
				"groq", "openrouter", "together", "ollama", "gemini", "azure", "mistral",
				"bedrock", "vertex", "deepseek", "cohere", "voyage", "aws", "cloudflare":
				return prefix, suffix
			}
		}
	}

	// 4. Bare deployment name -> use defaultAdapter
	return defaultAdapter, fb
}

// ResolveFallback attempts to find an unattempted fallback deployment.
// It supports failover across multiple providers and adapters rather than being locked to the primary model's provider.
// It filters fallback models by capability compatibility (ADR-017 §3 & T083 item 3).
func (r *Resolver) ResolveFallback(current *ResolvedRoute, attemptedDeployments map[string]bool, snap ...*core.Snapshot) (*ResolvedRoute, error) {
	if current == nil || current.ModelRouting == nil {
		return nil, errors.New("nil current route")
	}

	var activeSnap *core.Snapshot
	if len(snap) > 0 && snap[0] != nil {
		activeSnap = snap[0]
	} else if current.Snapshot != nil {
		activeSnap = current.Snapshot
	}

	mRouting := current.ModelRouting
	defaultAdapterName := mRouting.Adapter
	if defaultAdapterName == "" {
		defaultAdapterName = "litellm"
	}

	for _, fb := range mRouting.FallbackDeployments {
		adapterName, deployment := r.ParseFallback(fb, defaultAdapterName, activeSnap)
		if deployment == "" {
			continue
		}

		// Capability filtering (ADR-017 §3 & T083 item 3):
		// Fallback model must provide equal or superset capabilities of the primary model
		if activeSnap != nil {
			var fbModel *core.ModelRouting
			if m, ok := activeSnap.Model(fb); ok {
				fbModel = m
			} else if m, ok := activeSnap.Model(deployment); ok {
				fbModel = m
			}
			if fbModel != nil {
				if fbModel.Deployment != "" {
					deployment = fbModel.Deployment
				}
				if fbModel.Adapter != "" {
					adapterName = fbModel.Adapter
				}
				if len(mRouting.InputModalities) > 0 && len(fbModel.InputModalities) > 0 {
					incompatible := false
					for _, reqIn := range mRouting.InputModalities {
						if !containsString(fbModel.InputModalities, reqIn) {
							incompatible = true
							break
						}
					}
					if incompatible {
						continue
					}
				}
				if len(mRouting.OutputModalities) > 0 && len(fbModel.OutputModalities) > 0 {
					incompatible := false
					for _, reqOut := range mRouting.OutputModalities {
						if !containsString(fbModel.OutputModalities, reqOut) {
							incompatible = true
							break
						}
					}
					if incompatible {
						continue
					}
				}
			}
		}

		// Check if already attempted under any key representation
		if attemptedDeployments[fb] || attemptedDeployments[deployment] || attemptedDeployments[adapterName+"/"+deployment] {
			continue
		}

		// Resolve provider adapter for the fallback provider
		var adapter core.ProviderAdapter
		if adapterName == current.AdapterName && current.Adapter != nil {
			adapter = current.Adapter
		} else if activeSnap != nil {
			var ok bool
			adapter, ok = r.GetAdapterWithSnapshot(adapterName, activeSnap)
			if !ok {
				continue
			}
		} else if a, ok := r.GetAdapter(adapterName); ok {
			adapter = a
		} else if adapterName == defaultAdapterName && current.Adapter != nil {
			adapter = current.Adapter
		} else {
			continue
		}

		return &ResolvedRoute{
			ModelRouting: mRouting,
			Adapter:      adapter,
			Deployment:   deployment,
			AdapterName:  adapterName,
			Snapshot:     activeSnap,
		}, nil
	}

	return nil, ErrNoAvailableDeployments
}

func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}
	return false
}

// ResolveGenerator resolves a core.Generator implementation for a given logical model.
func (r *Resolver) ResolveGenerator(ctx context.Context, logicalModel string, snap *core.Snapshot) (core.Generator, string, error) {
	route, err := r.ResolveModel(snap, logicalModel)
	if err != nil {
		return nil, "", err
	}
	gen, ok := route.Adapter.(core.Generator)
	if !ok {
		return nil, "", fmt.Errorf("adapter %q for model %q does not support Generator interface", route.AdapterName, logicalModel)
	}
	return gen, route.Deployment, nil
}

func appID(app *core.AIApp) string {
	if app != nil {
		return app.ID
	}
	return ""
}

