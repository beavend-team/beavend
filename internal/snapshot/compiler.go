package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/secrets"
	"github.com/beavend/beavend/internal/store/db"
)

// QuerySource provides data needed to compile a configuration snapshot.
// Matches the methods generated on *db.Queries.
type QuerySource interface {
	GetSnapshotRevision(ctx context.Context, arg db.GetSnapshotRevisionParams) (int64, error)
	GetSnapshotEnvironment(ctx context.Context, arg db.GetSnapshotEnvironmentParams) ([]byte, error)
	GetSnapshotApps(ctx context.Context, arg db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error)
	GetSnapshotPrompts(ctx context.Context, arg db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error)
	GetSnapshotPolicies(ctx context.Context, arg db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error)
	GetSnapshotTools(ctx context.Context, arg db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error)
	GetSnapshotModelRoutings(ctx context.Context, arg db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error)
	GetSnapshotAppAPIKeys(ctx context.Context, arg db.GetSnapshotAppAPIKeysParams) ([]db.AppApiKeys, error)
	ListAllProjectEnvironments(ctx context.Context) ([]db.ListAllProjectEnvironmentsRow, error)
}

// ProviderQuerySource provides upstream model provider configs from store.
type ProviderQuerySource interface {
	ListProviderConfigs(ctx context.Context, arg db.ListProviderConfigsParams) ([]db.ProviderConfigs, error)
}

// Compiler compiles raw store records into an in-memory immutable Snapshot.
type Compiler struct {
	queries        QuerySource
	policyCompiler *policy.Compiler
	masterKey      []byte
}

// NewCompiler creates a new Snapshot Compiler.
func NewCompiler(queries QuerySource) (*Compiler, error) {
	pc, err := policy.NewCompiler()
	if err != nil {
		return nil, fmt.Errorf("init cel policy compiler: %w", err)
	}

	return &Compiler{
		queries:        queries,
		policyCompiler: pc,
	}, nil
}

// SetMasterKey configures the envelope encryption master key used to decrypt provider credentials.
func (c *Compiler) SetMasterKey(key []byte) {
	c.masterKey = key
}

// Compile loads all configuration for a (projectID, env) from the store,
// pre-compiles expressions and schemas, validates referential integrity,
// and produces an immutable *core.Snapshot.
func (c *Compiler) Compile(ctx context.Context, projectID, env string) (*core.Snapshot, error) {
	// 1. Revision
	rev, err := c.queries.GetSnapshotRevision(ctx, db.GetSnapshotRevisionParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("fetch revision: %w", err)
	}
	if rev == 0 {
		rev = 1
	}

	snap := core.NewSnapshot(projectID, env, rev)

	// 1b. Environment AuthConfig
	authBytes, err := c.queries.GetSnapshotEnvironment(ctx, db.GetSnapshotEnvironmentParams{
		ProjectID: projectID,
		Name:      env,
	})
	if err == nil && len(authBytes) > 0 && string(authBytes) != "{}" && string(authBytes) != "null" {
		var authCfg core.AuthConfig
		if err := json.Unmarshal(authBytes, &authCfg); err == nil && (authCfg.Provider != "" || authCfg.Issuer != "" || authCfg.JWKSURL != "") {
			snap.AuthConfig = &authCfg
		}
	}

	// 2. Prompts
	prompts, err := c.queries.GetSnapshotPrompts(ctx, db.GetSnapshotPromptsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch prompts: %w", err)
	}

	for _, p := range prompts {
		var vars []string
		if len(p.Variables) > 0 {
			if err := json.Unmarshal(p.Variables, &vars); err != nil {
				return nil, fmt.Errorf("unmarshal prompt variables for %s: %w", p.PromptID, err)
			}
		}

		pv := &core.PromptVersion{
			ID:        p.ID,
			PromptID:  p.PromptID,
			Version:   int(p.Version),
			Template:  p.Template,
			Variables: vars,
			IsActive:  p.IsActive,
			CreatedAt: p.CreatedAt.Time,
		}
		snap.Prompts[p.PromptID] = pv
		if p.PromptName != "" && p.PromptName != p.PromptID {
			snap.Prompts[p.PromptName] = pv
		}
	}

	// 3. Policies & CEL compilation
	policies, err := c.queries.GetSnapshotPolicies(ctx, db.GetSnapshotPoliciesParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch policies: %w", err)
	}

	for _, pol := range policies {
		pDef := &core.PolicyDefinition{
			ID:          pol.PolicyID,
			Name:        pol.Name,
			Description: pol.Description,
			Expression:  pol.Expression,
		}
		snap.Policies[pol.PolicyID] = pDef
		if pol.Name != "" && pol.Name != pol.PolicyID {
			snap.Policies[pol.Name] = pDef
		}

		if strings.TrimSpace(pol.Expression) != "" {
			prg, err := c.policyCompiler.Compile(pol.Expression)
			if err != nil {
				return nil, fmt.Errorf("compile cel policy %s (%q): %w", pol.PolicyID, pol.Expression, err)
			}
			snap.CompiledPolicies[pol.PolicyID] = prg
			if pol.Name != "" && pol.Name != pol.PolicyID {
				snap.CompiledPolicies[pol.Name] = prg
			}
		}
	}

	// 4. Tools & JSON Schema compilation
	tools, err := c.queries.GetSnapshotTools(ctx, db.GetSnapshotToolsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch tools: %w", err)
	}

	for _, t := range tools {
		var inputSchema map[string]any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &inputSchema); err != nil {
				return nil, fmt.Errorf("unmarshal input schema for tool %s: %w", t.ToolID, err)
			}

			if len(inputSchema) > 0 {
				// Pre-compile JSON Schema
				jsCompiler := jsonschema.NewCompiler()
				schemaURL := fmt.Sprintf("schema://beavend/tool/%s.json", t.ToolID)
				if err := jsCompiler.AddResource(schemaURL, bytes.NewReader(t.InputSchema)); err != nil {
					return nil, fmt.Errorf("add json schema resource for tool %s: %w", t.ToolID, err)
				}
				compiledSchema, err := jsCompiler.Compile(schemaURL)
				if err != nil {
					return nil, fmt.Errorf("compile json schema for tool %s: %w", t.ToolID, err)
				}
				snap.CompiledSchemas[t.ToolID] = compiledSchema
				if t.Name != "" && t.Name != t.ToolID {
					snap.CompiledSchemas[t.Name] = compiledSchema
				}
			}
		}

		var identityBindings map[string]string
		if len(t.IdentityBindings) > 0 {
			_ = json.Unmarshal(t.IdentityBindings, &identityBindings)
		}

		var execConfig core.ExecutorConfig
		if len(t.Executor) > 0 {
			if err := json.Unmarshal(t.Executor, &execConfig); err != nil {
				return nil, fmt.Errorf("unmarshal executor config for tool %s: %w", t.ToolID, err)
			}
		}

		var downstreamAuth core.DownstreamAuthConfig
		if len(t.DownstreamAuth) > 0 {
			_ = json.Unmarshal(t.DownstreamAuth, &downstreamAuth)
		}

		var resConfig core.ResultConfig
		if len(t.ResultConfig) > 0 {
			_ = json.Unmarshal(t.ResultConfig, &resConfig)
		}

		var cacheConfig core.CacheConfig
		if len(t.CacheConfig) > 0 {
			_ = json.Unmarshal(t.CacheConfig, &cacheConfig)
		}

		tDef := &core.ToolDefinition{
			ID:               t.ToolID,
			Name:             t.Name,
			Description:      t.Description,
			Version:          int(t.Version),
			SideEffect:       t.SideEffect,
			InputSchema:      inputSchema,
			IdentityBindings: identityBindings,
			Executor:         execConfig,
			DownstreamAuth:   downstreamAuth,
			Confirmation:     t.Confirmation,
			Authorization:    t.Authorization,
			Result:           resConfig,
			Cache:            cacheConfig,
		}

		// Referential integrity: verify tool authorization policy exists or is valid inline CEL
		if t.Authorization != "" {
			if _, exists := snap.Policies[t.Authorization]; !exists {
				// Inline CEL expression: validate syntax and pre-compile
				prg, err := c.policyCompiler.Compile(t.Authorization)
				if err != nil {
					return nil, fmt.Errorf("tool %s invalid authorization policy %q: %w", t.ToolID, t.Authorization, err)
				}
				snap.CompiledPolicies[t.Authorization] = prg
			}
		}

		snap.Tools[t.ToolID] = tDef
		if t.Name != "" && t.Name != t.ToolID {
			snap.Tools[t.Name] = tDef
		}
	}

	// 5. Model Routings
	models, err := c.queries.GetSnapshotModelRoutings(ctx, db.GetSnapshotModelRoutingsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch model routings: %w", err)
	}

	for _, m := range models {
		var fallbacks []string
		if len(m.FallbackDeployments) > 0 {
			if err := json.Unmarshal(m.FallbackDeployments, &fallbacks); err != nil {
				var rawObjects []map[string]any
				if objErr := json.Unmarshal(m.FallbackDeployments, &rawObjects); objErr == nil {
					for _, obj := range rawObjects {
						prov, _ := obj["provider"].(string)
						if prov == "" {
							prov, _ = obj["adapter"].(string)
						}
						dep, _ := obj["deployment"].(string)
						if dep == "" {
							dep, _ = obj["model"].(string)
						}
						if prov != "" && dep != "" {
							fallbacks = append(fallbacks, prov+"/"+dep)
						} else if dep != "" {
							fallbacks = append(fallbacks, dep)
						}
					}
				}
			}
		}

		mRouting := &core.ModelRouting{
			ID:                  m.ID,
			ProjectID:           m.ProjectID,
			Environment:         m.Environment,
			LogicalModel:        m.LogicalModel,
			Deployment:          m.Deployment,
			Adapter:             m.Adapter,
			FallbackDeployments: fallbacks,
			InputCostPer1M:      numericToFloatPtr(m.InputCostPer1m),
			OutputCostPer1M:     numericToFloatPtr(m.OutputCostPer1m),
			InputModalities:     m.InputModalities,
			OutputModalities:    m.OutputModalities,
		}
		snap.Models[m.LogicalModel] = mRouting
		if m.ID != "" && m.ID != m.LogicalModel {
			snap.Models[m.ID] = mRouting
		}
	}

	// Validate fallback capability compatibility (ADR-017 §3)
	for _, mRouting := range snap.Models {
		for _, fb := range mRouting.FallbackDeployments {
			if fbModel, ok := snap.Models[fb]; ok {
				if len(mRouting.InputModalities) > 0 && len(fbModel.InputModalities) > 0 {
					for _, inMod := range mRouting.InputModalities {
						if !containsString(fbModel.InputModalities, inMod) {
							return nil, fmt.Errorf("model routing %s fallback %s does not support required input modality %q", mRouting.LogicalModel, fb, inMod)
						}
					}
				}
				if len(mRouting.OutputModalities) > 0 && len(fbModel.OutputModalities) > 0 {
					for _, outMod := range mRouting.OutputModalities {
						if !containsString(fbModel.OutputModalities, outMod) {
							return nil, fmt.Errorf("model routing %s fallback %s does not support required output modality %q", mRouting.LogicalModel, fb, outMod)
						}
					}
				}
			}
		}
	}

	// 5b. Provider Configurations
	if pqs, ok := c.queries.(ProviderQuerySource); ok {
		provConfigs, err := pqs.ListProviderConfigs(ctx, db.ListProviderConfigsParams{
			ProjectID:   projectID,
			Environment: env,
		})
		if err == nil {
			var derivedKey []byte
			if len(c.masterKey) > 0 {
				derivedKey = secrets.DeriveKey(string(c.masterKey))
			} else {
				derivedKey = secrets.DeriveKey("default-master-key-32-chars-long")
			}
			for _, pc := range provConfigs {
				var apiKey string
				if len(pc.EncryptedApiKey) > 0 && len(pc.Nonce) > 0 {
					if pt, decErr := secrets.Decrypt(derivedKey, pc.EncryptedApiKey, pc.Nonce); decErr == nil {
						apiKey = string(pt)
					}
				}
				pName := strings.ToLower(strings.TrimSpace(pc.ProviderName))
				pCfg := &core.ProviderConfig{
					ID:           pc.ID,
					ProjectID:    pc.ProjectID,
					Environment:  pc.Environment,
					ProviderName: pc.ProviderName,
					BaseURL:      pc.BaseUrl,
					APIKey:       apiKey,
				}
				snap.Providers[pName] = pCfg
				if pName != pc.ProviderName {
					snap.Providers[pc.ProviderName] = pCfg
				}
			}
		}
	}

	// 6. AI Apps & Referential Integrity Checks
	apps, err := c.queries.GetSnapshotApps(ctx, db.GetSnapshotAppsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch ai apps: %w", err)
	}

	for _, a := range apps {
		var toolIDs []string
		if len(a.ToolIds) > 0 {
			if err := json.Unmarshal(a.ToolIds, &toolIDs); err != nil {
				return nil, fmt.Errorf("unmarshal tool_ids for app %s: %w", a.AppID, err)
			}
		}

		// Referential integrity: verify tools exist
		for _, toolID := range toolIDs {
			// Built-in tools provided by Beavend runtime are always valid
			switch toolID {
			case "web_search", "generate_image", "generate_speech", "generate_video":
				continue
			}
			if _, exists := snap.Tools[toolID]; !exists {
				return nil, fmt.Errorf("app %s references non-existent tool %q", a.AppID, toolID)
			}
		}

		// Referential integrity: verify prompt exists
		if a.PromptID != "" {
			if _, exists := snap.Prompts[a.PromptID]; !exists {
				return nil, fmt.Errorf("app %s references non-existent prompt %q", a.AppID, a.PromptID)
			}
		}

		// Referential integrity: verify model routing exists
		if a.ModelRoutingID != "" {
			if _, exists := snap.Models[a.ModelRoutingID]; !exists {
				return nil, fmt.Errorf("app %s references non-existent model routing %q", a.AppID, a.ModelRoutingID)
			}
		}

		var modelRoutes *core.ModelRoutes
		if len(a.ModelRoutes) > 0 && string(a.ModelRoutes) != "{}" && string(a.ModelRoutes) != "null" {
			var mr core.ModelRoutes
			if err := json.Unmarshal(a.ModelRoutes, &mr); err != nil {
				return nil, fmt.Errorf("unmarshal model_routes for app %s: %w", a.AppID, err)
			}
			modelRoutes = &mr
		}

		// Legacy compatibility: apps without model_routes use ModelRoutingID as default
		if modelRoutes == nil || strings.TrimSpace(modelRoutes.Default) == "" {
			if a.ModelRoutingID != "" {
				if modelRoutes == nil {
					modelRoutes = &core.ModelRoutes{Default: a.ModelRoutingID}
				} else {
					modelRoutes.Default = a.ModelRoutingID
				}
			}
		}

		// Referential integrity: default model route must be present
		if modelRoutes == nil || strings.TrimSpace(modelRoutes.Default) == "" {
			return nil, fmt.Errorf("app %s missing default model route", a.AppID)
		}

		// Referential integrity: verify default model exists
		if _, exists := snap.Models[modelRoutes.Default]; !exists {
			return nil, fmt.Errorf("app %s references non-existent default model %q", a.AppID, modelRoutes.Default)
		}

		// Validate routing rules
		for _, rule := range modelRoutes.Rules {
			if strings.TrimSpace(rule.Use) == "" {
				return nil, fmt.Errorf("app %s route rule has empty model target 'use'", a.AppID)
			}
			targetModel, exists := snap.Models[rule.Use]
			if !exists {
				return nil, fmt.Errorf("app %s route rule references non-existent model %q", a.AppID, rule.Use)
			}

			// Validate input modalities if capability metadata is declared
			if len(targetModel.InputModalities) > 0 && len(rule.When.InputHas) > 0 {
				for _, reqIn := range rule.When.InputHas {
					if !containsString(targetModel.InputModalities, reqIn) {
						return nil, fmt.Errorf("app %s route rule model %q does not support requested input modality %q", a.AppID, rule.Use, reqIn)
					}
				}
			}

			// Validate output modalities if capability metadata is declared
			if len(targetModel.OutputModalities) > 0 && len(rule.When.Output) > 0 {
				for _, reqOut := range rule.When.Output {
					if !containsString(targetModel.OutputModalities, reqOut) {
						return nil, fmt.Errorf("app %s route rule model %q does not support requested output modality %q", a.AppID, rule.Use, reqOut)
					}
				}
			}

			// Pre-compile optional CEL expr
			if rule.When.Expr != "" {
				if _, exists := snap.CompiledPolicies[rule.When.Expr]; !exists {
					prg, err := c.policyCompiler.Compile(rule.When.Expr)
					if err != nil {
						return nil, fmt.Errorf("app %s invalid route rule cel expression %q: %w", a.AppID, rule.When.Expr, err)
					}
					snap.CompiledPolicies[rule.When.Expr] = prg
				}
			}
		}

		// Referential integrity: verify access policy exists or is valid inline CEL
		if a.AccessPolicy != "" {
			if _, exists := snap.Policies[a.AccessPolicy]; !exists {
				// Inline CEL expression: validate syntax and pre-compile
				prg, err := c.policyCompiler.Compile(a.AccessPolicy)
				if err != nil {
					return nil, fmt.Errorf("app %s invalid access policy %q: %w", a.AppID, a.AccessPolicy, err)
				}
				snap.CompiledPolicies[a.AccessPolicy] = prg
			}
		}

		var limits []core.LimitRule
		if len(a.Limits) > 0 {
			if err := json.Unmarshal(a.Limits, &limits); err != nil {
				return nil, fmt.Errorf("unmarshal limits for app %s: %w", a.AppID, err)
			}

			// Pre-compile any CEL tier conditions in limit rules
			for _, limit := range limits {
				if limit.TierCondition != "" {
					if _, exists := snap.CompiledPolicies[limit.TierCondition]; !exists {
						prg, err := c.policyCompiler.Compile(limit.TierCondition)
						if err != nil {
							return nil, fmt.Errorf("app %s invalid limit tier condition %q: %w", a.AppID, limit.TierCondition, err)
						}
						snap.CompiledPolicies[limit.TierCondition] = prg
					}
				}
			}
		}

		var cacheConfig core.CacheConfig
		if len(a.CacheConfig) > 0 {
			_ = json.Unmarshal(a.CacheConfig, &cacheConfig)
		}

		var guardrails core.GuardrailConfig
		if len(a.Guardrails) > 0 {
			_ = json.Unmarshal(a.Guardrails, &guardrails)
		}

		var memConfig core.MemoryConfig
		if len(a.MemoryConfig) > 0 {
			_ = json.Unmarshal(a.MemoryConfig, &memConfig)
		}

		var searchConfig core.SearchConfig
		if len(a.SearchConfig) > 0 {
			_ = json.Unmarshal(a.SearchConfig, &searchConfig)
		}

		var appAuthConfig *core.AuthConfig
		if len(a.AuthConfig) > 0 && string(a.AuthConfig) != "{}" && string(a.AuthConfig) != "null" {
			var ac core.AuthConfig
			if err := json.Unmarshal(a.AuthConfig, &ac); err == nil && (ac.Provider != "" || ac.Issuer != "" || ac.JWKSURL != "" || ac.AllowAnonymous) {
				appAuthConfig = &ac
			}
		}

		modelRoutingID := a.ModelRoutingID
		if modelRoutingID == "" && modelRoutes != nil {
			modelRoutingID = modelRoutes.Default
		}

		appObj := &core.AIApp{
			ID:             a.AppID,
			ProjectID:      a.ProjectID,
			Environment:    a.Environment,
			Name:           a.Name,
			Description:    a.Description,
			AccessPolicy:   a.AccessPolicy,
			PromptID:       a.PromptID,
			PromptVersion:  int(a.PromptVersion),
			ModelRoutingID: modelRoutingID,
			ModelRoutes:    modelRoutes,
			ToolIDs:        toolIDs,
			Limits:         limits,
			Cache:          cacheConfig,
			Guardrails:     guardrails,
			Memory:         memConfig,
			Search:         searchConfig,
			ResponseSchema: a.ResponseSchema,
			AuthConfig:     appAuthConfig,
			CreatedAt:      a.CreatedAt.Time,
			UpdatedAt:      time.Now().UTC(),
		}
		snap.Apps[a.AppID] = appObj
		if a.Name != "" && a.Name != a.AppID {
			snap.Apps[a.Name] = appObj
		}

		if strings.TrimSpace(a.ResponseSchema) != "" {
			jsCompiler := jsonschema.NewCompiler()
			schemaURL := fmt.Sprintf("schema://beavend/app/%s.json", a.AppID)
			if err := jsCompiler.AddResource(schemaURL, strings.NewReader(a.ResponseSchema)); err != nil {
				return nil, fmt.Errorf("add json schema resource for app %s: %w", a.AppID, err)
			}
			compiledSchema, err := jsCompiler.Compile(schemaURL)
			if err != nil {
				return nil, fmt.Errorf("compile response json schema for app %s: %w", a.AppID, err)
			}
			snap.CompiledSchemas["app_response:"+a.AppID] = compiledSchema
			if a.Name != "" && a.Name != a.AppID {
				snap.CompiledSchemas["app_response:"+a.Name] = compiledSchema
			}
		}
	}

	// 7. App API Keys
	apiKeys, err := c.queries.GetSnapshotAppAPIKeys(ctx, db.GetSnapshotAppAPIKeysParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
		for _, k := range apiKeys {
			var lastUsed, expires *time.Time
			if k.LastUsedAt.Valid {
				t := k.LastUsedAt.Time
				lastUsed = &t
			}
			if k.ExpiresAt.Valid {
				t := k.ExpiresAt.Time
				expires = &t
			}
			snap.AppAPIKeys[k.KeyHash] = &core.AppAPIKey{
				ID:          k.ID,
				AppID:       k.AppID,
				ProjectID:   k.ProjectID,
				Environment: k.Environment,
				Name:        k.Name,
				KeyPrefix:   k.KeyPrefix,
				KeyHash:     k.KeyHash,
				CreatedAt:   k.CreatedAt.Time,
				LastUsedAt:  lastUsed,
				ExpiresAt:   expires,
			}
		}
	}

	return snap, nil
}

func numericToFloatPtr(num pgtype.Numeric) *float64 {
	if !num.Valid {
		return nil
	}
	f, err := num.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	val := f.Float64
	return &val
}

func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, val) {
			return true
		}
	}
	return false
}
