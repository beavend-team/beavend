package configcode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store/db"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ResourceAction describes the operation needed for a resource.
type ResourceAction string

const (
	ActionCreate    ResourceAction = "create"
	ActionUpdate    ResourceAction = "update"
	ActionUnchanged ResourceAction = "unchanged"
)

// ResourceDiff describes the diff on a single resource.
type ResourceDiff struct {
	Type    string         `json:"type"` // "project", "auth", "app", "prompt", "model", "tool", "policy"
	ID      string         `json:"id"`
	Action  ResourceAction `json:"action"`
	Changes []string       `json:"changes,omitempty"`
}

// DiffSummary provides aggregate counts for a diff plan.
type DiffSummary struct {
	TotalCreated   int `json:"total_created"`
	TotalUpdated   int `json:"total_updated"`
	TotalUnchanged int `json:"total_unchanged"`
}

// DiffReport is the detailed plan of changes to apply.
type DiffReport struct {
	ProjectID   string         `json:"project_id"`
	Environment string         `json:"environment"`
	Resources   []ResourceDiff `json:"resources"`
	Summary     DiffSummary    `json:"summary"`
}

// QuerySource captures all database operations needed by the configcode Engine.
type QuerySource interface {
	GetProject(ctx context.Context, id string) (db.Projects, error)
	CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Projects, error)
	GetEnvironment(ctx context.Context, arg db.GetEnvironmentParams) (db.Environments, error)
	CreateEnvironment(ctx context.Context, arg db.CreateEnvironmentParams) (db.Environments, error)
	UpdateEnvironmentAuthConfig(ctx context.Context, arg db.UpdateEnvironmentAuthConfigParams) (db.Environments, error)
	GetSnapshotEnvironment(ctx context.Context, arg db.GetSnapshotEnvironmentParams) ([]byte, error)
	GetSnapshotPrompts(ctx context.Context, arg db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error)
	GetSnapshotPolicies(ctx context.Context, arg db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error)
	GetSnapshotTools(ctx context.Context, arg db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error)
	GetSnapshotModelRoutings(ctx context.Context, arg db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error)
	ListProviderConfigs(ctx context.Context, arg db.ListProviderConfigsParams) ([]db.ProviderConfigs, error)
	GetSnapshotApps(ctx context.Context, arg db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error)
	GetPrompt(ctx context.Context, id string) (db.Prompts, error)
	CreatePrompt(ctx context.Context, arg db.CreatePromptParams) (db.Prompts, error)
	CreatePromptVersion(ctx context.Context, arg db.CreatePromptVersionParams) (db.PromptVersions, error)
	GetActivePromptVersion(ctx context.Context, id string) (db.PromptVersions, error)
	UpdatePromptActiveVersion(ctx context.Context, arg db.UpdatePromptActiveVersionParams) (db.Prompts, error)
	GetPolicy(ctx context.Context, id string) (db.Policies, error)
	CreatePolicy(ctx context.Context, arg db.CreatePolicyParams) (db.Policies, error)
	CreatePolicyVersion(ctx context.Context, arg db.CreatePolicyVersionParams) (db.PolicyVersions, error)
	GetActivePolicyVersion(ctx context.Context, id string) (db.PolicyVersions, error)
	UpdatePolicyActiveVersion(ctx context.Context, arg db.UpdatePolicyActiveVersionParams) (db.Policies, error)
	GetTool(ctx context.Context, id string) (db.Tools, error)
	CreateTool(ctx context.Context, arg db.CreateToolParams) (db.Tools, error)
	CreateToolVersion(ctx context.Context, arg db.CreateToolVersionParams) (db.ToolVersions, error)
	GetActiveToolVersion(ctx context.Context, id string) (db.ToolVersions, error)
	UpdateToolActiveVersion(ctx context.Context, arg db.UpdateToolActiveVersionParams) (db.Tools, error)
	UpsertProviderConfig(ctx context.Context, arg db.UpsertProviderConfigParams) (db.ProviderConfigs, error)
	UpsertModelRouting(ctx context.Context, arg db.UpsertModelRoutingParams) (db.ModelRoutings, error)
	GetAIApp(ctx context.Context, id string) (db.AiApps, error)
	CreateAIApp(ctx context.Context, arg db.CreateAIAppParams) (db.AiApps, error)
	CreateAIAppVersion(ctx context.Context, arg db.CreateAIAppVersionParams) (db.AiAppVersions, error)
	GetActiveAIAppVersion(ctx context.Context, id string) (db.GetActiveAIAppVersionRow, error)
	UpdateAIAppActiveVersion(ctx context.Context, arg db.UpdateAIAppActiveVersionParams) (db.AiApps, error)
	IncrementProjectRevision(ctx context.Context, arg db.IncrementProjectRevisionParams) (int64, error)
}

// SkillQuerySource is an optional interface that QuerySource implementations can provide
// to support exporting and importing skills via configuration as code.
type SkillQuerySource interface {
	GetSnapshotSkills(ctx context.Context, arg db.GetSnapshotSkillsParams) ([]db.GetSnapshotSkillsRow, error)
	GetSkill(ctx context.Context, id string) (db.Skills, error)
	CreateSkill(ctx context.Context, arg db.CreateSkillParams) (db.Skills, error)
	CreateSkillVersion(ctx context.Context, arg db.CreateSkillVersionParams) (db.SkillVersions, error)
	GetActiveSkillVersion(ctx context.Context, id string) (db.SkillVersions, error)
	UpdateSkillActiveVersion(ctx context.Context, arg db.UpdateSkillActiveVersionParams) (db.Skills, error)
}

// PromptFragmentsSource is an optional interface to export and apply prompt fragments.
type PromptFragmentsSource interface {
	GetSnapshotPromptFragments(ctx context.Context, projectID, env, promptID string) ([]core.PromptFragmentRef, error)
	UpsertPromptFragments(ctx context.Context, projectID, env, promptID string, fragments []core.PromptFragmentRef) error
}

// AppAPIKeySource is an optional interface to export and apply app api keys.
type AppAPIKeySource interface {
	GetSnapshotAppAPIKeys(ctx context.Context, arg db.GetSnapshotAppAPIKeysParams) ([]db.AppApiKeys, error)
	UpsertAppAPIKey(ctx context.Context, key AppAPIKeySpec, projectID, env string) error
}

// KnowledgeDocSource is an optional interface to export and apply knowledge doc metadata.
type KnowledgeDocSource interface {
	GetSnapshotKnowledgeDocs(ctx context.Context, projectID, env string) ([]core.KnowledgeDocMeta, error)
	UpsertKnowledgeDoc(ctx context.Context, doc KnowledgeDocSpec, projectID, env string) error
}

// SkillGovernanceSource is an optional interface for skill status and change note.
type SkillGovernanceSource interface {
	GetSkillGovernance(ctx context.Context, skillID string) (status string, changeNote string, err error)
	UpdateSkillGovernance(ctx context.Context, skillID, status, changeNote string) error
}

// ToolExtendedSource is an optional interface for tool parallel safety.
type ToolExtendedSource interface {
	GetToolParallelSafe(ctx context.Context, toolID string) (bool, error)
	SetToolParallelSafe(ctx context.Context, toolID string, parallelSafe bool) error
}

// ModelCapabilitiesSource is an optional interface for model capability flags and cached pricing.
type ModelCapabilitiesSource interface {
	GetModelCapabilities(ctx context.Context, projectID, env, logicalModel string) (cachedInputCost *float64, capabilities []string, err error)
	UpdateModelCapabilities(ctx context.Context, projectID, env, logicalModel string, cachedInputCost *float64, capabilities []string) error
}

// Engine handles exporting and importing configuration as code documents.
type Engine struct {
	queries   QuerySource
	publisher synckit.Publisher
	masterKey []byte
}

// NewEngine creates a new configcode Engine.
func NewEngine(queries QuerySource, publisher synckit.Publisher, masterKey []byte) *Engine {
	return &Engine{
		queries:   queries,
		publisher: publisher,
		masterKey: masterKey,
	}
}

// Export compiles all database entities for a project and environment into a ProjectDocument.
func (e *Engine) Export(ctx context.Context, projectID, env string) (*ProjectDocument, error) {
	if e.queries == nil {
		return nil, errors.New("database queries not configured")
	}

	proj, err := e.queries.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project %q: %w", projectID, err)
	}

	doc := &ProjectDocument{
		BeavendVersion: CurrentVersion,
		Project: ProjectMeta{
			ID:          proj.ID,
			Name:        proj.Name,
			Description: proj.Description,
		},
		Environment: env,
	}

	// 1. AuthConfig
	authBytes, err := e.queries.GetSnapshotEnvironment(ctx, db.GetSnapshotEnvironmentParams{
		ProjectID: projectID,
		Name:      env,
	})
	if err == nil && len(authBytes) > 0 && string(authBytes) != "{}" && string(authBytes) != "null" {
		var authCfg core.AuthConfig
		if err := json.Unmarshal(authBytes, &authCfg); err == nil && (authCfg.Provider != "" || authCfg.Issuer != "" || authCfg.JWKSURL != "") {
			doc.Auth = &authCfg
		}
	}

	// 2. Prompts
	prompts, err := e.queries.GetSnapshotPrompts(ctx, db.GetSnapshotPromptsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
		for _, p := range prompts {
			var vars []string
			if len(p.Variables) > 0 {
				_ = json.Unmarshal(p.Variables, &vars)
			}
			var frags []core.PromptFragmentRef
			if pfs, ok := e.queries.(PromptFragmentsSource); ok {
				if f, err := pfs.GetSnapshotPromptFragments(ctx, projectID, env, p.PromptID); err == nil {
					frags = f
				}
			}
			doc.Prompts = append(doc.Prompts, PromptSpec{
				ID:        p.PromptID,
				Name:      p.PromptName,
				Template:  p.Template,
				Variables: vars,
				Fragments: frags,
				Version:   int(p.Version),
			})
		}
	}

	// 2b. Skills
	if sq, ok := e.queries.(SkillQuerySource); ok {
		skills, err := sq.GetSnapshotSkills(ctx, db.GetSnapshotSkillsParams{
			ProjectID:   projectID,
			Environment: env,
		})
		if err == nil {
			for _, s := range skills {
				var vars []string
				if len(s.Variables) > 0 {
					_ = json.Unmarshal(s.Variables, &vars)
				}
				var status, changeNote string
				if sgs, ok := e.queries.(SkillGovernanceSource); ok {
					status, changeNote, _ = sgs.GetSkillGovernance(ctx, s.SkillID)
				}
				doc.Skills = append(doc.Skills, SkillSpec{
					ID:            s.SkillID,
					Name:          s.SkillName,
					Description:   s.SkillDescription,
					Source:        s.Source,
					InjectionMode: s.InjectionMode,
					Content:       s.Content,
					Variables:     vars,
					Version:       int(s.Version),
					Status:        status,
					ChangeNote:    changeNote,
				})
			}
		}
	}

	// 3. Policies
	policies, err := e.queries.GetSnapshotPolicies(ctx, db.GetSnapshotPoliciesParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
		for _, pol := range policies {
			doc.Policies = append(doc.Policies, PolicySpec{
				ID:          pol.PolicyID,
				Name:        pol.Name,
				Description: pol.Description,
				Expression:  pol.Expression,
			})
		}
	}

	// 4. Tools
	tools, err := e.queries.GetSnapshotTools(ctx, db.GetSnapshotToolsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
		for _, t := range tools {
			var inputSchema map[string]any
			if len(t.InputSchema) > 0 {
				_ = json.Unmarshal(t.InputSchema, &inputSchema)
			}
			var identityBindings map[string]string
			if len(t.IdentityBindings) > 0 {
				_ = json.Unmarshal(t.IdentityBindings, &identityBindings)
			}
			var executor core.ExecutorConfig
			if len(t.Executor) > 0 {
				_ = json.Unmarshal(t.Executor, &executor)
			}
			var downstreamAuth *core.DownstreamAuthConfig
			if len(t.DownstreamAuth) > 0 {
				var da core.DownstreamAuthConfig
				if err := json.Unmarshal(t.DownstreamAuth, &da); err == nil && da.Mode != "" {
					downstreamAuth = &da
				}
			}
			var resultCfg *core.ResultConfig
			if len(t.ResultConfig) > 0 {
				var rc core.ResultConfig
				if err := json.Unmarshal(t.ResultConfig, &rc); err == nil {
					resultCfg = &rc
				}
			}
			var cacheCfg *core.CacheConfig
			if len(t.CacheConfig) > 0 {
				var cc core.CacheConfig
				if err := json.Unmarshal(t.CacheConfig, &cc); err == nil && cc.Enabled {
					cacheCfg = &cc
				}
			}

			var parallelSafe bool
			if tes, ok := e.queries.(ToolExtendedSource); ok {
				parallelSafe, _ = tes.GetToolParallelSafe(ctx, t.ToolID)
			}

			doc.Tools = append(doc.Tools, ToolSpec{
				ID:               t.ToolID,
				Name:             t.Name,
				Description:      t.Description,
				Version:          int(t.Version),
				SideEffect:       t.SideEffect,
				ParallelSafe:     parallelSafe,
				InputSchema:      inputSchema,
				IdentityBindings: identityBindings,
				Executor:         executor,
				DownstreamAuth:   downstreamAuth,
				Confirmation:     t.Confirmation,
				Authorization:    t.Authorization,
				Result:           resultCfg,
				Cache:            cacheCfg,
			})
		}
	}

	// 5. Models & Providers
	providerMap := make(map[string]db.ProviderConfigs)
	providers, _ := e.queries.ListProviderConfigs(ctx, db.ListProviderConfigsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	for _, p := range providers {
		providerMap[p.ProviderName] = p
	}

	models, err := e.queries.GetSnapshotModelRoutings(ctx, db.GetSnapshotModelRoutingsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
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

			var provSpec *ProviderSpec
			if prov, ok := providerMap[m.Adapter]; ok {
				provSpec = &ProviderSpec{
					ProviderName: prov.ProviderName,
					BaseURL:      prov.BaseUrl,
					APIKey:       MaskedSecretPlaceholder,
				}
			}

			var cachedCost *float64
			var caps []string
			if mcs, ok := e.queries.(ModelCapabilitiesSource); ok {
				cachedCost, caps, _ = mcs.GetModelCapabilities(ctx, projectID, env, m.LogicalModel)
			}

			doc.Models = append(doc.Models, ModelSpec{
				ID:                   m.ID,
				LogicalModel:         m.LogicalModel,
				Deployment:           m.Deployment,
				Adapter:              m.Adapter,
				FallbackDeployments:  fallbacks,
				InputCostPer1M:       numericToFloatPtr(m.InputCostPer1m),
				CachedInputCostPer1M: cachedCost,
				OutputCostPer1M:      numericToFloatPtr(m.OutputCostPer1m),
				InputModalities:      m.InputModalities,
				OutputModalities:     m.OutputModalities,
				Capabilities:         caps,
				ProviderConfig:       provSpec,
			})
		}
	}

	// 6. Apps
	apps, err := e.queries.GetSnapshotApps(ctx, db.GetSnapshotAppsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil {
		for _, a := range apps {
			var toolIDs []string
			if len(a.ToolIds) > 0 {
				_ = json.Unmarshal(a.ToolIds, &toolIDs)
			}
			var skillIDs []string
			if len(a.SkillIds) > 0 {
				_ = json.Unmarshal(a.SkillIds, &skillIDs)
			}
			var limits []core.LimitRule
			if len(a.Limits) > 0 {
				_ = json.Unmarshal(a.Limits, &limits)
			}
			var cacheCfg *core.CacheConfig
			if len(a.CacheConfig) > 0 {
				var cc core.CacheConfig
				if err := json.Unmarshal(a.CacheConfig, &cc); err == nil && cc.Enabled {
					cacheCfg = &cc
				}
			}
			var guardrails *core.GuardrailConfig
			if len(a.Guardrails) > 0 {
				var gc core.GuardrailConfig
				if err := json.Unmarshal(a.Guardrails, &gc); err == nil {
					guardrails = &gc
				}
			}
			var memory *core.MemoryConfig
			if len(a.MemoryConfig) > 0 {
				var mc core.MemoryConfig
				if err := json.Unmarshal(a.MemoryConfig, &mc); err == nil && mc.Enabled {
					memory = &mc
				}
			}
			var searchCfg *core.SearchConfig
			if len(a.SearchConfig) > 0 {
				var sc core.SearchConfig
				if err := json.Unmarshal(a.SearchConfig, &sc); err == nil && sc.Enabled {
					searchCfg = &sc
				}
			}

			var appAuth *core.AuthConfig
			if len(a.AuthConfig) > 0 && string(a.AuthConfig) != "{}" && string(a.AuthConfig) != "null" {
				var ac core.AuthConfig
				if err := json.Unmarshal(a.AuthConfig, &ac); err == nil && (ac.Provider != "" || ac.Issuer != "" || ac.JWKSURL != "") {
					appAuth = &ac
				}
			}

			var modelRoutes *core.ModelRoutes
			if len(a.ModelRoutes) > 0 && string(a.ModelRoutes) != "{}" && string(a.ModelRoutes) != "null" {
				var mr core.ModelRoutes
				if err := json.Unmarshal(a.ModelRoutes, &mr); err == nil {
					modelRoutes = &mr
				}
			}

			doc.Apps = append(doc.Apps, AppSpec{
				ID:             a.AppID,
				Name:           a.Name,
				Description:    a.Description,
				AccessPolicy:   a.AccessPolicy,
				PromptID:       a.PromptID,
				PromptVersion:  int(a.PromptVersion),
				ModelRoutingID: a.ModelRoutingID,
				ModelRoutes:    modelRoutes,
				ToolIDs:        toolIDs,
				SkillIDs:       skillIDs,
				Limits:         limits,
				Cache:          cacheCfg,
				Guardrails:     guardrails,
				Memory:         memory,
				Search:         searchCfg,
				ResponseSchema: a.ResponseSchema,
				Auth:           appAuth,
			})
		}
	}

	// 7. API Keys
	if aks, ok := e.queries.(AppAPIKeySource); ok {
		keys, err := aks.GetSnapshotAppAPIKeys(ctx, db.GetSnapshotAppAPIKeysParams{
			ProjectID:   projectID,
			Environment: env,
		})
		if err == nil {
			for _, k := range keys {
				doc.APIKeys = append(doc.APIKeys, AppAPIKeySpec{
					ID:        k.ID,
					AppID:     k.AppID,
					Name:      k.Name,
					KeyPrefix: k.KeyPrefix,
				})
			}
		}
	}

	// 8. Knowledge Docs
	if kds, ok := e.queries.(KnowledgeDocSource); ok {
		kDocs, err := kds.GetSnapshotKnowledgeDocs(ctx, projectID, env)
		if err == nil {
			for _, kd := range kDocs {
				doc.Knowledge = append(doc.Knowledge, KnowledgeDocSpec{
					ID:          kd.ID,
					AppID:       kd.AppID,
					Title:       kd.Title,
					Description: kd.Description,
					SourceURL:   kd.SourceURL,
					Tags:        kd.Tags,
					Revision:    kd.Revision,
				})
			}
		}
	}

	return doc, nil
}

// Diff compares the incoming ProjectDocument with the current database state and computes a DiffReport.
func (e *Engine) Diff(ctx context.Context, doc *ProjectDocument) (*DiffReport, error) {
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate document: %w", err)
	}

	report := &DiffReport{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	}

	// 1. Project
	proj, err := e.queries.GetProject(ctx, doc.Project.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "project",
				ID:     doc.Project.ID,
				Action: ActionCreate,
			})
		} else {
			return nil, fmt.Errorf("diff project: %w", err)
		}
	} else {
		var changes []string
		if proj.Name != doc.Project.Name {
			changes = append(changes, fmt.Sprintf("name: %q -> %q", proj.Name, doc.Project.Name))
		}
		if proj.Description != doc.Project.Description {
			changes = append(changes, fmt.Sprintf("description: %q -> %q", proj.Description, doc.Project.Description))
		}
		action := ActionUnchanged
		if len(changes) > 0 {
			action = ActionUpdate
		}
		report.Resources = append(report.Resources, ResourceDiff{
			Type:    "project",
			ID:      doc.Project.ID,
			Action:  action,
			Changes: changes,
		})
	}

	// 2. AuthConfig
	if doc.Auth != nil {
		authBytes, _ := e.queries.GetSnapshotEnvironment(ctx, db.GetSnapshotEnvironmentParams{
			ProjectID: doc.Project.ID,
			Name:      doc.Environment,
		})
		action := ActionCreate
		var changes []string
		if len(authBytes) > 0 && string(authBytes) != "{}" && string(authBytes) != "null" {
			var existingAuth core.AuthConfig
			if err := json.Unmarshal(authBytes, &existingAuth); err == nil {
				if !reflect.DeepEqual(existingAuth, *doc.Auth) {
					action = ActionUpdate
					changes = append(changes, fmt.Sprintf("provider: %s -> %s", existingAuth.Provider, doc.Auth.Provider))
				} else {
					action = ActionUnchanged
				}
			}
		}
		report.Resources = append(report.Resources, ResourceDiff{
			Type:    "auth",
			ID:      doc.Environment,
			Action:  action,
			Changes: changes,
		})
	}

	// 3. Prompts
	existingPrompts, _ := e.queries.GetSnapshotPrompts(ctx, db.GetSnapshotPromptsParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	promptMap := make(map[string]db.GetSnapshotPromptsRow)
	for _, p := range existingPrompts {
		promptMap[p.PromptID] = p
	}

	for _, pSpec := range doc.Prompts {
		if existing, ok := promptMap[pSpec.ID]; ok {
			var changes []string
			if strings.TrimSpace(existing.Template) != strings.TrimSpace(pSpec.Template) {
				changes = append(changes, "template changed")
			}
			if pfs, ok := e.queries.(PromptFragmentsSource); ok {
				existingFrags, _ := pfs.GetSnapshotPromptFragments(ctx, doc.Project.ID, doc.Environment, pSpec.ID)
				if !reflect.DeepEqual(existingFrags, pSpec.Fragments) {
					changes = append(changes, "fragments changed")
				}
			}
			action := ActionUnchanged
			if len(changes) > 0 {
				action = ActionUpdate
			}
			report.Resources = append(report.Resources, ResourceDiff{
				Type:    "prompt",
				ID:      pSpec.ID,
				Action:  action,
				Changes: changes,
			})
		} else {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "prompt",
				ID:     pSpec.ID,
				Action: ActionCreate,
			})
		}
	}

	// 3b. Skills
	if sq, ok := e.queries.(SkillQuerySource); ok && len(doc.Skills) > 0 {
		existingSkills, _ := sq.GetSnapshotSkills(ctx, db.GetSnapshotSkillsParams{
			ProjectID:   doc.Project.ID,
			Environment: doc.Environment,
		})
		skillMap := make(map[string]db.GetSnapshotSkillsRow)
		for _, s := range existingSkills {
			skillMap[s.SkillID] = s
		}

		for _, sSpec := range doc.Skills {
			if existing, ok := skillMap[sSpec.ID]; ok {
				var changes []string
				if strings.TrimSpace(existing.Content) != strings.TrimSpace(sSpec.Content) {
					changes = append(changes, "content changed")
				}
				if existing.InjectionMode != sSpec.InjectionMode && sSpec.InjectionMode != "" {
					changes = append(changes, fmt.Sprintf("injection_mode: %s -> %s", existing.InjectionMode, sSpec.InjectionMode))
				}
				if sgs, ok := sq.(SkillGovernanceSource); ok {
					st, cn, _ := sgs.GetSkillGovernance(ctx, sSpec.ID)
					if sSpec.Status != "" && st != sSpec.Status {
						changes = append(changes, fmt.Sprintf("status: %s -> %s", st, sSpec.Status))
					}
					if sSpec.ChangeNote != "" && cn != sSpec.ChangeNote {
						changes = append(changes, "change_note changed")
					}
				}
				action := ActionUnchanged
				if len(changes) > 0 {
					action = ActionUpdate
				}
				report.Resources = append(report.Resources, ResourceDiff{
					Type:    "skill",
					ID:      sSpec.ID,
					Action:  action,
					Changes: changes,
				})
			} else {
				report.Resources = append(report.Resources, ResourceDiff{
					Type:   "skill",
					ID:     sSpec.ID,
					Action: ActionCreate,
				})
			}
		}
	}

	// 4. Policies
	existingPolicies, _ := e.queries.GetSnapshotPolicies(ctx, db.GetSnapshotPoliciesParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	policyMap := make(map[string]db.GetSnapshotPoliciesRow)
	for _, pol := range existingPolicies {
		policyMap[pol.PolicyID] = pol
	}

	for _, polSpec := range doc.Policies {
		if existing, ok := policyMap[polSpec.ID]; ok {
			var changes []string
			if strings.TrimSpace(existing.Expression) != strings.TrimSpace(polSpec.Expression) {
				changes = append(changes, "expression changed")
			}
			action := ActionUnchanged
			if len(changes) > 0 {
				action = ActionUpdate
			}
			report.Resources = append(report.Resources, ResourceDiff{
				Type:    "policy",
				ID:      polSpec.ID,
				Action:  action,
				Changes: changes,
			})
		} else {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "policy",
				ID:     polSpec.ID,
				Action: ActionCreate,
			})
		}
	}

	// 5. Tools
	existingTools, _ := e.queries.GetSnapshotTools(ctx, db.GetSnapshotToolsParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	toolMap := make(map[string]db.GetSnapshotToolsRow)
	for _, t := range existingTools {
		toolMap[t.ToolID] = t
	}

	for _, tSpec := range doc.Tools {
		if existing, ok := toolMap[tSpec.ID]; ok {
			var changes []string
			if existing.SideEffect != tSpec.SideEffect {
				changes = append(changes, fmt.Sprintf("side_effect: %s -> %s", existing.SideEffect, tSpec.SideEffect))
			}
			if existing.Confirmation != tSpec.Confirmation {
				changes = append(changes, fmt.Sprintf("confirmation: %s -> %s", existing.Confirmation, tSpec.Confirmation))
			}
			if existing.Authorization != tSpec.Authorization {
				changes = append(changes, fmt.Sprintf("authorization: %s -> %s", existing.Authorization, tSpec.Authorization))
			}
			if tes, ok := e.queries.(ToolExtendedSource); ok {
				ps, _ := tes.GetToolParallelSafe(ctx, tSpec.ID)
				if ps != tSpec.ParallelSafe {
					changes = append(changes, fmt.Sprintf("parallel_safe: %v -> %v", ps, tSpec.ParallelSafe))
				}
			}
			action := ActionUnchanged
			if len(changes) > 0 {
				action = ActionUpdate
			}
			report.Resources = append(report.Resources, ResourceDiff{
				Type:    "tool",
				ID:      tSpec.ID,
				Action:  action,
				Changes: changes,
			})
		} else {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "tool",
				ID:     tSpec.ID,
				Action: ActionCreate,
			})
		}
	}

	// 6. Models
	existingModels, _ := e.queries.GetSnapshotModelRoutings(ctx, db.GetSnapshotModelRoutingsParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	modelMap := make(map[string]db.ModelRoutings)
	for _, m := range existingModels {
		modelMap[m.LogicalModel] = m
	}

	for _, mSpec := range doc.Models {
		if existing, ok := modelMap[mSpec.LogicalModel]; ok {
			var changes []string
			if existing.Deployment != mSpec.Deployment {
				changes = append(changes, fmt.Sprintf("deployment: %s -> %s", existing.Deployment, mSpec.Deployment))
			}
			if existing.Adapter != mSpec.Adapter {
				changes = append(changes, fmt.Sprintf("adapter: %s -> %s", existing.Adapter, mSpec.Adapter))
			}
			if mcs, ok := e.queries.(ModelCapabilitiesSource); ok {
				cached, caps, _ := mcs.GetModelCapabilities(ctx, doc.Project.ID, doc.Environment, mSpec.LogicalModel)
				if (cached == nil && mSpec.CachedInputCostPer1M != nil) || (cached != nil && mSpec.CachedInputCostPer1M == nil) || (cached != nil && mSpec.CachedInputCostPer1M != nil && *cached != *mSpec.CachedInputCostPer1M) {
					changes = append(changes, "cached_input_cost changed")
				}
				if !stringSlicesEqualUnordered(caps, mSpec.Capabilities) {
					changes = append(changes, "capabilities changed")
				}
			}
			action := ActionUnchanged
			if len(changes) > 0 {
				action = ActionUpdate
			}
			report.Resources = append(report.Resources, ResourceDiff{
				Type:    "model",
				ID:      mSpec.LogicalModel,
				Action:  action,
				Changes: changes,
			})
		} else {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "model",
				ID:     mSpec.LogicalModel,
				Action: ActionCreate,
			})
		}
	}

	// 7. Apps
	existingApps, _ := e.queries.GetSnapshotApps(ctx, db.GetSnapshotAppsParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	appMap := make(map[string]db.GetSnapshotAppsRow)
	for _, a := range existingApps {
		appMap[a.AppID] = a
	}

	for _, aSpec := range doc.Apps {
		if existing, ok := appMap[aSpec.ID]; ok {
			var changes []string
			if existing.PromptID != aSpec.PromptID {
				changes = append(changes, fmt.Sprintf("prompt_id: %s -> %s", existing.PromptID, aSpec.PromptID))
			}
			if existing.ModelRoutingID != aSpec.ModelRoutingID {
				changes = append(changes, fmt.Sprintf("model_routing_id: %s -> %s", existing.ModelRoutingID, aSpec.ModelRoutingID))
			}
			if existing.AccessPolicy != aSpec.AccessPolicy {
				changes = append(changes, fmt.Sprintf("access_policy: %s -> %s", existing.AccessPolicy, aSpec.AccessPolicy))
			}
			var existingAuth *core.AuthConfig
			if len(existing.AuthConfig) > 0 && string(existing.AuthConfig) != "{}" && string(existing.AuthConfig) != "null" {
				var ac core.AuthConfig
				_ = json.Unmarshal(existing.AuthConfig, &ac)
				existingAuth = &ac
			}
			if (existingAuth == nil && aSpec.Auth != nil) || (existingAuth != nil && aSpec.Auth == nil) || (existingAuth != nil && aSpec.Auth != nil && existingAuth.Provider != aSpec.Auth.Provider) {
				changes = append(changes, "auth provider changed")
			}
			var existingSkillIDs []string
			if len(existing.SkillIds) > 0 {
				_ = json.Unmarshal(existing.SkillIds, &existingSkillIDs)
			}
			if !stringSlicesEqualUnordered(existingSkillIDs, aSpec.SkillIDs) {
				changes = append(changes, "attached skills changed")
			}
			action := ActionUnchanged
			if len(changes) > 0 {
				action = ActionUpdate
			}
			report.Resources = append(report.Resources, ResourceDiff{
				Type:    "app",
				ID:      aSpec.ID,
				Action:  action,
				Changes: changes,
			})
		} else {
			report.Resources = append(report.Resources, ResourceDiff{
				Type:   "app",
				ID:     aSpec.ID,
				Action: ActionCreate,
			})
		}
	}

	// 8. API Keys
	if aks, ok := e.queries.(AppAPIKeySource); ok {
		existingKeys, _ := aks.GetSnapshotAppAPIKeys(ctx, db.GetSnapshotAppAPIKeysParams{
			ProjectID:   doc.Project.ID,
			Environment: doc.Environment,
		})
		keyMap := make(map[string]db.AppApiKeys)
		for _, k := range existingKeys {
			keyMap[k.ID] = k
		}

		allKeys := append([]AppAPIKeySpec(nil), doc.APIKeys...)
		for _, a := range doc.Apps {
			allKeys = append(allKeys, a.APIKeys...)
		}

		for _, kSpec := range allKeys {
			if existing, ok := keyMap[kSpec.ID]; ok {
				var changes []string
				if existing.Name != kSpec.Name {
					changes = append(changes, fmt.Sprintf("name: %s -> %s", existing.Name, kSpec.Name))
				}
				action := ActionUnchanged
				if len(changes) > 0 {
					action = ActionUpdate
				}
				report.Resources = append(report.Resources, ResourceDiff{
					Type:    "api_key",
					ID:      kSpec.ID,
					Action:  action,
					Changes: changes,
				})
			} else {
				report.Resources = append(report.Resources, ResourceDiff{
					Type:   "api_key",
					ID:     kSpec.ID,
					Action: ActionCreate,
				})
			}
		}
	}

	// 9. Knowledge Docs
	if kds, ok := e.queries.(KnowledgeDocSource); ok {
		existingDocs, _ := kds.GetSnapshotKnowledgeDocs(ctx, doc.Project.ID, doc.Environment)
		docMap := make(map[string]core.KnowledgeDocMeta)
		for _, d := range existingDocs {
			docMap[d.ID] = d
		}

		for _, kSpec := range doc.Knowledge {
			if existing, ok := docMap[kSpec.ID]; ok {
				var changes []string
				if existing.Title != kSpec.Title {
					changes = append(changes, fmt.Sprintf("title: %s -> %s", existing.Title, kSpec.Title))
				}
				action := ActionUnchanged
				if len(changes) > 0 {
					action = ActionUpdate
				}
				report.Resources = append(report.Resources, ResourceDiff{
					Type:    "knowledge",
					ID:      kSpec.ID,
					Action:  action,
					Changes: changes,
				})
			} else {
				report.Resources = append(report.Resources, ResourceDiff{
					Type:   "knowledge",
					ID:     kSpec.ID,
					Action: ActionCreate,
				})
			}
		}
	}

	// Summary calculation
	for _, r := range report.Resources {
		switch r.Action {
		case ActionCreate:
			report.Summary.TotalCreated++
		case ActionUpdate:
			report.Summary.TotalUpdated++
		case ActionUnchanged:
			report.Summary.TotalUnchanged++
		}
	}

	return report, nil
}

// Apply executes the declarative changes from the document against the database.
func (e *Engine) Apply(ctx context.Context, doc *ProjectDocument) (*DiffReport, error) {
	diff, err := e.Diff(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("compute diff: %w", err)
	}

	// 1. Ensure Project exists
	_, err = e.queries.GetProject(ctx, doc.Project.ID)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		_, err = e.queries.CreateProject(ctx, db.CreateProjectParams{
			ID:          doc.Project.ID,
			Name:        doc.Project.Name,
			Description: doc.Project.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("create project: %w", err)
		}
	}

	// 2. Ensure Environment exists
	_, err = e.queries.GetEnvironment(ctx, db.GetEnvironmentParams{
		ProjectID: doc.Project.ID,
		Name:      doc.Environment,
	})
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		_, err = e.queries.CreateEnvironment(ctx, db.CreateEnvironmentParams{
			ProjectID:   doc.Project.ID,
			Name:        doc.Environment,
			Description: fmt.Sprintf("%s environment", doc.Environment),
		})
		if err != nil {
			return nil, fmt.Errorf("create environment: %w", err)
		}
	}

	// 3. Update AuthConfig if present
	if doc.Auth != nil {
		authBytes, _ := json.Marshal(doc.Auth)
		_, err = e.queries.UpdateEnvironmentAuthConfig(ctx, db.UpdateEnvironmentAuthConfigParams{
			ProjectID:  doc.Project.ID,
			Name:       doc.Environment,
			AuthConfig: authBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("update auth config: %w", err)
		}
	}

	// 4. Upsert Prompts
	for _, pSpec := range doc.Prompts {
		varsBytes, _ := json.Marshal(pSpec.Variables)
		_, err := e.queries.GetPrompt(ctx, pSpec.ID)
		if err != nil && errors.Is(err, pgx.ErrNoRows) {
			_, err = e.queries.CreatePrompt(ctx, db.CreatePromptParams{
				ID:            pSpec.ID,
				ProjectID:     doc.Project.ID,
				Environment:   doc.Environment,
				Name:          pSpec.Name,
				Description:   pSpec.Description,
				ActiveVersion: 1,
			})
			if err != nil {
				return nil, fmt.Errorf("create prompt %s: %w", pSpec.ID, err)
			}
			_, err = e.queries.CreatePromptVersion(ctx, db.CreatePromptVersionParams{
				ID:          fmt.Sprintf("%s_v1", pSpec.ID),
				PromptID:    pSpec.ID,
				ProjectID:   doc.Project.ID,
				Environment: doc.Environment,
				Version:     1,
				Template:    pSpec.Template,
				Variables:   varsBytes,
				IsActive:    true,
			})
			if err != nil {
				return nil, fmt.Errorf("create prompt version %s_v1: %w", pSpec.ID, err)
			}
		} else if err == nil {
			// Check if new version needed
			activeV, err := e.queries.GetActivePromptVersion(ctx, pSpec.ID)
			if err == nil && (activeV.Template != pSpec.Template || string(activeV.Variables) != string(varsBytes)) {
				newV := activeV.Version + 1
				_, err = e.queries.CreatePromptVersion(ctx, db.CreatePromptVersionParams{
					ID:          fmt.Sprintf("%s_v%d", pSpec.ID, newV),
					PromptID:    pSpec.ID,
					ProjectID:   doc.Project.ID,
					Environment: doc.Environment,
					Version:     newV,
					Template:    pSpec.Template,
					Variables:   varsBytes,
					IsActive:    true,
				})
				if err != nil {
					return nil, fmt.Errorf("create prompt version %s_v%d: %w", pSpec.ID, newV, err)
				}
				_, _ = e.queries.UpdatePromptActiveVersion(ctx, db.UpdatePromptActiveVersionParams{
					ID:            pSpec.ID,
					ActiveVersion: newV,
				})
			}
		}
		if pfs, ok := e.queries.(PromptFragmentsSource); ok && len(pSpec.Fragments) > 0 {
			_ = pfs.UpsertPromptFragments(ctx, doc.Project.ID, doc.Environment, pSpec.ID, pSpec.Fragments)
		}
	}

	// 4b. Upsert Skills
	if sq, ok := e.queries.(SkillQuerySource); ok {
		for _, sSpec := range doc.Skills {
			varsBytes, _ := json.Marshal(sSpec.Variables)
			if len(varsBytes) == 0 {
				varsBytes = []byte("[]")
			}
			injMode := sSpec.InjectionMode
			if injMode == "" {
				injMode = core.SkillInjectionAlways
			}
			src := sSpec.Source
			if src == "" {
				src = "manual"
			}

			_, err := sq.GetSkill(ctx, sSpec.ID)
			if err != nil && errors.Is(err, pgx.ErrNoRows) {
				_, err = sq.CreateSkill(ctx, db.CreateSkillParams{
					ID:            sSpec.ID,
					ProjectID:     doc.Project.ID,
					Environment:   doc.Environment,
					Name:          sSpec.Name,
					Description:   sSpec.Description,
					Source:        src,
					InjectionMode: injMode,
					ActiveVersion: 1,
				})
				if err != nil {
					return nil, fmt.Errorf("create skill %s: %w", sSpec.ID, err)
				}
				_, err = sq.CreateSkillVersion(ctx, db.CreateSkillVersionParams{
					ID:          fmt.Sprintf("%s_v1", sSpec.ID),
					SkillID:     sSpec.ID,
					ProjectID:   doc.Project.ID,
					Environment: doc.Environment,
					Version:     1,
					Content:     sSpec.Content,
					Variables:   varsBytes,
					IsActive:    true,
				})
				if err != nil {
					return nil, fmt.Errorf("create skill version %s_v1: %w", sSpec.ID, err)
				}
			} else if err == nil {
				activeV, err := sq.GetActiveSkillVersion(ctx, sSpec.ID)
				if err == nil && (activeV.Content != sSpec.Content || string(activeV.Variables) != string(varsBytes)) {
					newV := activeV.Version + 1
					_, err = sq.CreateSkillVersion(ctx, db.CreateSkillVersionParams{
						ID:          fmt.Sprintf("%s_v%d", sSpec.ID, newV),
						SkillID:     sSpec.ID,
						ProjectID:   doc.Project.ID,
						Environment: doc.Environment,
						Version:     newV,
						Content:     sSpec.Content,
						Variables:   varsBytes,
						IsActive:    true,
					})
					if err != nil {
						return nil, fmt.Errorf("create skill version %s_v%d: %w", sSpec.ID, newV, err)
					}
					_, _ = sq.UpdateSkillActiveVersion(ctx, db.UpdateSkillActiveVersionParams{
						ID:            sSpec.ID,
						ActiveVersion: newV,
					})
				}
			}
			if sgs, ok := sq.(SkillGovernanceSource); ok && (sSpec.Status != "" || sSpec.ChangeNote != "") {
				_ = sgs.UpdateSkillGovernance(ctx, sSpec.ID, sSpec.Status, sSpec.ChangeNote)
			}
		}
	}

	// 5. Upsert Policies
	for _, polSpec := range doc.Policies {
		_, err := e.queries.GetPolicy(ctx, polSpec.ID)
		if err != nil && errors.Is(err, pgx.ErrNoRows) {
			_, err = e.queries.CreatePolicy(ctx, db.CreatePolicyParams{
				ID:            polSpec.ID,
				ProjectID:     doc.Project.ID,
				Environment:   doc.Environment,
				Name:          polSpec.Name,
				Description:   polSpec.Description,
				ActiveVersion: 1,
			})
			if err != nil {
				return nil, fmt.Errorf("create policy %s: %w", polSpec.ID, err)
			}
			_, err = e.queries.CreatePolicyVersion(ctx, db.CreatePolicyVersionParams{
				ID:          fmt.Sprintf("%s_v1", polSpec.ID),
				PolicyID:    polSpec.ID,
				ProjectID:   doc.Project.ID,
				Environment: doc.Environment,
				Version:     1,
				Expression:  polSpec.Expression,
				IsActive:    true,
			})
			if err != nil {
				return nil, fmt.Errorf("create policy version %s_v1: %w", polSpec.ID, err)
			}
		} else if err == nil {
			activeV, err := e.queries.GetActivePolicyVersion(ctx, polSpec.ID)
			if err == nil && activeV.Expression != polSpec.Expression {
				newV := activeV.Version + 1
				_, err = e.queries.CreatePolicyVersion(ctx, db.CreatePolicyVersionParams{
					ID:          fmt.Sprintf("%s_v%d", polSpec.ID, newV),
					PolicyID:    polSpec.ID,
					ProjectID:   doc.Project.ID,
					Environment: doc.Environment,
					Version:     newV,
					Expression:  polSpec.Expression,
					IsActive:    true,
				})
				if err != nil {
					return nil, fmt.Errorf("create policy version %s_v%d: %w", polSpec.ID, newV, err)
				}
				_, _ = e.queries.UpdatePolicyActiveVersion(ctx, db.UpdatePolicyActiveVersionParams{
					ID:            polSpec.ID,
					ActiveVersion: newV,
				})
			}
		}
	}

	// 6. Upsert Tools
	for _, tSpec := range doc.Tools {
		schemaBytes, _ := json.Marshal(tSpec.InputSchema)
		bindingsBytes, _ := json.Marshal(tSpec.IdentityBindings)
		execBytes, _ := json.Marshal(tSpec.Executor)
		authBytes, _ := json.Marshal(tSpec.DownstreamAuth)
		resBytes, _ := json.Marshal(tSpec.Result)
		cacheBytes, _ := json.Marshal(tSpec.Cache)

		_, err := e.queries.GetTool(ctx, tSpec.ID)
		if err != nil && errors.Is(err, pgx.ErrNoRows) {
			_, err = e.queries.CreateTool(ctx, db.CreateToolParams{
				ID:            tSpec.ID,
				ProjectID:     doc.Project.ID,
				Environment:   doc.Environment,
				Name:          tSpec.Name,
				Description:   tSpec.Description,
				ActiveVersion: 1,
			})
			if err != nil {
				return nil, fmt.Errorf("create tool %s: %w", tSpec.ID, err)
			}
			_, err = e.queries.CreateToolVersion(ctx, db.CreateToolVersionParams{
				ID:               fmt.Sprintf("%s_v1", tSpec.ID),
				ToolID:           tSpec.ID,
				ProjectID:        doc.Project.ID,
				Environment:      doc.Environment,
				Version:          1,
				SideEffect:       tSpec.SideEffect,
				InputSchema:      schemaBytes,
				IdentityBindings: bindingsBytes,
				Executor:         execBytes,
				DownstreamAuth:   authBytes,
				Confirmation:     tSpec.Confirmation,
				Authorization:    tSpec.Authorization,
				ResultConfig:     resBytes,
				CacheConfig:      cacheBytes,
				IsActive:         true,
			})
			if err != nil {
				return nil, fmt.Errorf("create tool version %s_v1: %w", tSpec.ID, err)
			}
		} else if err == nil {
			activeV, err := e.queries.GetActiveToolVersion(ctx, tSpec.ID)
			if err == nil {
				newV := activeV.Version + 1
				_, err = e.queries.CreateToolVersion(ctx, db.CreateToolVersionParams{
					ID:               fmt.Sprintf("%s_v%d", tSpec.ID, newV),
					ToolID:           tSpec.ID,
					ProjectID:        doc.Project.ID,
					Environment:      doc.Environment,
					Version:          newV,
					SideEffect:       tSpec.SideEffect,
					InputSchema:      schemaBytes,
					IdentityBindings: bindingsBytes,
					Executor:         execBytes,
					DownstreamAuth:   authBytes,
					Confirmation:     tSpec.Confirmation,
					Authorization:    tSpec.Authorization,
					ResultConfig:     resBytes,
					CacheConfig:      cacheBytes,
					IsActive:         true,
				})
				if err != nil {
					return nil, fmt.Errorf("create tool version %s_v%d: %w", tSpec.ID, newV, err)
				}
				_, _ = e.queries.UpdateToolActiveVersion(ctx, db.UpdateToolActiveVersionParams{
					ID:            tSpec.ID,
					ActiveVersion: newV,
				})
			}
		}
		if tes, ok := e.queries.(ToolExtendedSource); ok {
			_ = tes.SetToolParallelSafe(ctx, tSpec.ID, tSpec.ParallelSafe)
		}
	}

	// 7. Upsert Models & Providers
	for _, mSpec := range doc.Models {
		// Provider config
		if mSpec.ProviderConfig != nil {
			apiKey := ResolveSecret(mSpec.ProviderConfig.APIKey)
			if apiKey != "" && apiKey != MaskedSecretPlaceholder && len(e.masterKey) == 32 {
				cipher, nonce, err := secrets.Encrypt(e.masterKey, []byte(apiKey))
				if err == nil {
					_, _ = e.queries.UpsertProviderConfig(ctx, db.UpsertProviderConfigParams{
						ID:               fmt.Sprintf("%s_%s_%s", doc.Project.ID, doc.Environment, mSpec.ProviderConfig.ProviderName),
						ProjectID:        doc.Project.ID,
						Environment:      doc.Environment,
						ProviderName:     mSpec.ProviderConfig.ProviderName,
						BaseUrl:          mSpec.ProviderConfig.BaseURL,
						EncryptedApiKey:  cipher,
						Nonce:            nonce,
						KeyID:            "v1",
					})
				}
			}
		}

		fallbacksBytes, _ := json.Marshal(mSpec.FallbackDeployments)
		modelID := mSpec.ID
		if modelID == "" {
			modelID = fmt.Sprintf("%s_%s_%s", doc.Project.ID, doc.Environment, mSpec.LogicalModel)
		}
		inMods := mSpec.InputModalities
		if inMods == nil {
			inMods = []string{}
		}
		outMods := mSpec.OutputModalities
		if outMods == nil {
			outMods = []string{}
		}
		_, err = e.queries.UpsertModelRouting(ctx, db.UpsertModelRoutingParams{
			ID:                  modelID,
			ProjectID:           doc.Project.ID,
			Environment:         doc.Environment,
			LogicalModel:        mSpec.LogicalModel,
			Deployment:          mSpec.Deployment,
			Adapter:             mSpec.Adapter,
			FallbackDeployments: fallbacksBytes,
			InputCostPer1m:      floatPtrToNumeric(mSpec.InputCostPer1M),
			OutputCostPer1m:     floatPtrToNumeric(mSpec.OutputCostPer1M),
			InputModalities:     inMods,
			OutputModalities:    outMods,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert model routing %s: %w", mSpec.LogicalModel, err)
		}
		if mcs, ok := e.queries.(ModelCapabilitiesSource); ok && (mSpec.CachedInputCostPer1M != nil || len(mSpec.Capabilities) > 0) {
			_ = mcs.UpdateModelCapabilities(ctx, doc.Project.ID, doc.Environment, mSpec.LogicalModel, mSpec.CachedInputCostPer1M, mSpec.Capabilities)
		}
	}

	// 8. Upsert Apps
	for _, aSpec := range doc.Apps {
		toolsBytes, _ := json.Marshal(aSpec.ToolIDs)
		skillBytes, _ := json.Marshal(aSpec.SkillIDs)
		if len(skillBytes) == 0 {
			skillBytes = []byte("[]")
		}
		limitsBytes, _ := json.Marshal(aSpec.Limits)
		cacheBytes, _ := json.Marshal(aSpec.Cache)
		guardBytes, _ := json.Marshal(aSpec.Guardrails)
		memBytes, _ := json.Marshal(aSpec.Memory)
		if len(memBytes) == 0 {
			memBytes = []byte("{}")
		}

		var authBytes []byte
		if aSpec.Auth != nil {
			authBytes, _ = json.Marshal(aSpec.Auth)
		}

		var routesBytes []byte
		if aSpec.ModelRoutes != nil {
			routesBytes, _ = json.Marshal(aSpec.ModelRoutes)
		} else if aSpec.ModelRoutingID != "" {
			routesBytes, _ = json.Marshal(core.ModelRoutes{Default: aSpec.ModelRoutingID})
		}

		_, err := e.queries.GetAIApp(ctx, aSpec.ID)
		if err != nil && errors.Is(err, pgx.ErrNoRows) {
			_, err = e.queries.CreateAIApp(ctx, db.CreateAIAppParams{
				ID:            aSpec.ID,
				ProjectID:     doc.Project.ID,
				Environment:   doc.Environment,
				Name:          aSpec.Name,
				Description:   aSpec.Description,
				ActiveVersion: 1,
				AuthConfig:    authBytes,
				ModelRoutes:   routesBytes,
			})
			if err != nil {
				return nil, fmt.Errorf("create ai app %s: %w", aSpec.ID, err)
			}
			var searchBytes []byte
			if aSpec.Search != nil {
				searchBytes, _ = json.Marshal(aSpec.Search)
			} else {
				searchBytes = []byte("{}")
			}

			_, err = e.queries.CreateAIAppVersion(ctx, db.CreateAIAppVersionParams{
				ID:             fmt.Sprintf("%s_v1", aSpec.ID),
				AppID:          aSpec.ID,
				ProjectID:      doc.Project.ID,
				Environment:    doc.Environment,
				Version:        1,
				AccessPolicy:   aSpec.AccessPolicy,
				PromptID:       aSpec.PromptID,
				PromptVersion:  int32(aSpec.PromptVersion),
				ModelRoutingID: aSpec.ModelRoutingID,
				ToolIds:        toolsBytes,
				SkillIds:       skillBytes,
				Limits:         limitsBytes,
				CacheConfig:    cacheBytes,
				Guardrails:     guardBytes,
				ResponseSchema: aSpec.ResponseSchema,
				IsActive:       true,
				MemoryConfig:   memBytes,
				SearchConfig:   searchBytes,
			})
			if err != nil {
				return nil, fmt.Errorf("create ai app version %s_v1: %w", aSpec.ID, err)
			}
		} else if err == nil {
			activeV, err := e.queries.GetActiveAIAppVersion(ctx, aSpec.ID)
			if err == nil {
				var searchBytes []byte
				if aSpec.Search != nil {
					searchBytes, _ = json.Marshal(aSpec.Search)
				} else {
					searchBytes = []byte("{}")
				}

				newV := activeV.Version + 1
				_, err = e.queries.CreateAIAppVersion(ctx, db.CreateAIAppVersionParams{
					ID:             fmt.Sprintf("%s_v%d", aSpec.ID, newV),
					AppID:          aSpec.ID,
					ProjectID:      doc.Project.ID,
					Environment:    doc.Environment,
					Version:        newV,
					AccessPolicy:   aSpec.AccessPolicy,
					PromptID:       aSpec.PromptID,
					PromptVersion:  int32(aSpec.PromptVersion),
					ModelRoutingID: aSpec.ModelRoutingID,
					ToolIds:        toolsBytes,
					SkillIds:       skillBytes,
					Limits:         limitsBytes,
					CacheConfig:    cacheBytes,
					Guardrails:     guardBytes,
					ResponseSchema: aSpec.ResponseSchema,
					IsActive:       true,
					MemoryConfig:   memBytes,
					SearchConfig:   searchBytes,
				})
				if err != nil {
					return nil, fmt.Errorf("create ai app version %s_v%d: %w", aSpec.ID, newV, err)
				}
				_, _ = e.queries.UpdateAIAppActiveVersion(ctx, db.UpdateAIAppActiveVersionParams{
					ID:            aSpec.ID,
					ActiveVersion: newV,
					AuthConfig:    authBytes,
					ModelRoutes:   routesBytes,
				})
			}
		}
	}

	// 8b. Upsert API Keys
	if aks, ok := e.queries.(AppAPIKeySource); ok {
		allKeys := append([]AppAPIKeySpec(nil), doc.APIKeys...)
		for _, a := range doc.Apps {
			allKeys = append(allKeys, a.APIKeys...)
		}
		for _, k := range allKeys {
			_ = aks.UpsertAppAPIKey(ctx, k, doc.Project.ID, doc.Environment)
		}
	}

	// 8c. Upsert Knowledge Docs
	if kds, ok := e.queries.(KnowledgeDocSource); ok {
		for _, kd := range doc.Knowledge {
			_ = kds.UpsertKnowledgeDoc(ctx, kd, doc.Project.ID, doc.Environment)
		}
	}

	// 9. Increment revision and publish notification
	newRev, err := e.queries.IncrementProjectRevision(ctx, db.IncrementProjectRevisionParams{
		ProjectID:   doc.Project.ID,
		Environment: doc.Environment,
	})
	if err == nil && e.publisher != nil {
		_ = e.publisher.Publish(ctx, synckit.RevisionEvent{
			ProjectID:   doc.Project.ID,
			Environment: doc.Environment,
			Revision:    newRev,
		})
	}

	return diff, nil
}

func generateID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%x", prefix, b)
}

func floatPtrToNumeric(f *float64) pgtype.Numeric {
	if f == nil {
		return pgtype.Numeric{Valid: false}
	}
	var num pgtype.Numeric
	_ = num.Scan(fmt.Sprintf("%.6f", *f))
	return num
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

func stringSlicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
