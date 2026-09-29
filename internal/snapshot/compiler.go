package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/store/db"
)

// QuerySource provides data needed to compile a configuration snapshot.
// Matches the methods generated on *db.Queries.
type QuerySource interface {
	GetSnapshotRevision(ctx context.Context, arg db.GetSnapshotRevisionParams) (int64, error)
	GetSnapshotApps(ctx context.Context, arg db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error)
	GetSnapshotPrompts(ctx context.Context, arg db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error)
	GetSnapshotPolicies(ctx context.Context, arg db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error)
	GetSnapshotTools(ctx context.Context, arg db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error)
	GetSnapshotModelRoutings(ctx context.Context, arg db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error)
	ListAllProjectEnvironments(ctx context.Context) ([]db.ListAllProjectEnvironmentsRow, error)
}

// Compiler compiles raw store records into an in-memory immutable Snapshot.
type Compiler struct {
	queries        QuerySource
	policyCompiler *policy.Compiler
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
			_ = json.Unmarshal(m.FallbackDeployments, &fallbacks)
		}

		mRouting := &core.ModelRouting{
			ID:                  m.ID,
			ProjectID:           m.ProjectID,
			Environment:         m.Environment,
			LogicalModel:        m.LogicalModel,
			Deployment:          m.Deployment,
			Adapter:             m.Adapter,
			FallbackDeployments: fallbacks,
		}
		snap.Models[m.LogicalModel] = mRouting
		if m.ID != "" && m.ID != m.LogicalModel {
			snap.Models[m.ID] = mRouting
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

		appObj := &core.AIApp{
			ID:             a.AppID,
			ProjectID:      a.ProjectID,
			Environment:    a.Environment,
			Name:           a.Name,
			Description:    a.Description,
			AccessPolicy:   a.AccessPolicy,
			PromptID:       a.PromptID,
			PromptVersion:  int(a.PromptVersion),
			ModelRoutingID: a.ModelRoutingID,
			ToolIDs:        toolIDs,
			Limits:         limits,
			Cache:          cacheConfig,
			Guardrails:     guardrails,
			ResponseSchema: a.ResponseSchema,
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

	return snap, nil
}
