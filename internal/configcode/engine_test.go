package configcode

import (
	"context"
	"testing"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store/db"
	pgx "github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSyncPublisher struct {
	lastEvent synckit.RevisionEvent
}

func (m *mockSyncPublisher) Publish(_ context.Context, ev synckit.RevisionEvent) error {
	m.lastEvent = ev
	return nil
}

type testStore struct {
	project         *db.Projects
	environment     *db.Environments
	authConfigBytes []byte
	prompts         map[string]db.Prompts
	promptVersions  map[string]db.PromptVersions
	policies        map[string]db.Policies
	policyVersions  map[string]db.PolicyVersions
	tools           map[string]db.Tools
	toolVersions    map[string]db.ToolVersions
	models          map[string]db.ModelRoutings
	providers       map[string]db.ProviderConfigs
	apps            map[string]db.AiApps
	appVersions     map[string]db.AiAppVersions
	revision        int64
}

func newTestStore() *testStore {
	return &testStore{
		prompts:        make(map[string]db.Prompts),
		promptVersions: make(map[string]db.PromptVersions),
		policies:       make(map[string]db.Policies),
		policyVersions: make(map[string]db.PolicyVersions),
		tools:          make(map[string]db.Tools),
		toolVersions:   make(map[string]db.ToolVersions),
		models:         make(map[string]db.ModelRoutings),
		providers:      make(map[string]db.ProviderConfigs),
		apps:           make(map[string]db.AiApps),
		appVersions:    make(map[string]db.AiAppVersions),
		revision:       1,
	}
}

func (s *testStore) GetProject(_ context.Context, id string) (db.Projects, error) {
	if s.project != nil && s.project.ID == id {
		return *s.project, nil
	}
	return db.Projects{}, pgx.ErrNoRows
}

func (s *testStore) CreateProject(_ context.Context, arg db.CreateProjectParams) (db.Projects, error) {
	s.project = &db.Projects{
		ID:          arg.ID,
		Name:        arg.Name,
		Description: arg.Description,
	}
	return *s.project, nil
}

func (s *testStore) GetEnvironment(_ context.Context, arg db.GetEnvironmentParams) (db.Environments, error) {
	if s.environment != nil && s.environment.ProjectID == arg.ProjectID && s.environment.Name == arg.Name {
		return *s.environment, nil
	}
	return db.Environments{}, pgx.ErrNoRows
}

func (s *testStore) CreateEnvironment(_ context.Context, arg db.CreateEnvironmentParams) (db.Environments, error) {
	s.environment = &db.Environments{
		ProjectID:   arg.ProjectID,
		Name:        arg.Name,
		Description: arg.Description,
	}
	return *s.environment, nil
}

func (s *testStore) UpdateEnvironmentAuthConfig(_ context.Context, arg db.UpdateEnvironmentAuthConfigParams) (db.Environments, error) {
	s.authConfigBytes = arg.AuthConfig
	if s.environment != nil {
		s.environment.AuthConfig = arg.AuthConfig
		return *s.environment, nil
	}
	return db.Environments{}, nil
}

func (s *testStore) GetSnapshotEnvironment(_ context.Context, _ db.GetSnapshotEnvironmentParams) ([]byte, error) {
	return s.authConfigBytes, nil
}

func (s *testStore) GetSnapshotPrompts(_ context.Context, _ db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error) {
	var rows []db.GetSnapshotPromptsRow
	for _, p := range s.prompts {
		if pv, ok := s.promptVersions[p.ID]; ok {
			rows = append(rows, db.GetSnapshotPromptsRow{
				PromptID:   p.ID,
				PromptName: p.Name,
				Version:    p.ActiveVersion,
				Template:   pv.Template,
				Variables:  pv.Variables,
			})
		}
	}
	return rows, nil
}

func (s *testStore) GetSnapshotPolicies(_ context.Context, _ db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error) {
	var rows []db.GetSnapshotPoliciesRow
	for _, p := range s.policies {
		if pv, ok := s.policyVersions[p.ID]; ok {
			rows = append(rows, db.GetSnapshotPoliciesRow{
				PolicyID:    p.ID,
				Name:        p.Name,
				Description: p.Description,
				Expression:  pv.Expression,
			})
		}
	}
	return rows, nil
}

func (s *testStore) GetSnapshotTools(_ context.Context, _ db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error) {
	var rows []db.GetSnapshotToolsRow
	for _, t := range s.tools {
		if tv, ok := s.toolVersions[t.ID]; ok {
			rows = append(rows, db.GetSnapshotToolsRow{
				ToolID:           t.ID,
				Name:             t.Name,
				Description:      t.Description,
				Version:          t.ActiveVersion,
				SideEffect:       tv.SideEffect,
				InputSchema:      tv.InputSchema,
				IdentityBindings: tv.IdentityBindings,
				Executor:         tv.Executor,
				DownstreamAuth:   tv.DownstreamAuth,
				Confirmation:     tv.Confirmation,
				Authorization:    tv.Authorization,
				ResultConfig:     tv.ResultConfig,
				CacheConfig:      tv.CacheConfig,
			})
		}
	}
	return rows, nil
}

func (s *testStore) GetSnapshotModelRoutings(_ context.Context, _ db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error) {
	var rows []db.ModelRoutings
	for _, m := range s.models {
		rows = append(rows, m)
	}
	return rows, nil
}

func (s *testStore) ListProviderConfigs(_ context.Context, _ db.ListProviderConfigsParams) ([]db.ProviderConfigs, error) {
	var rows []db.ProviderConfigs
	for _, p := range s.providers {
		rows = append(rows, p)
	}
	return rows, nil
}

func (s *testStore) GetSnapshotApps(_ context.Context, _ db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error) {
	var rows []db.GetSnapshotAppsRow
	for _, a := range s.apps {
		if av, ok := s.appVersions[a.ID]; ok {
			rows = append(rows, db.GetSnapshotAppsRow{
				AppID:          a.ID,
				Name:           a.Name,
				Description:    a.Description,
				AccessPolicy:   av.AccessPolicy,
				PromptID:       av.PromptID,
				PromptVersion:  av.PromptVersion,
				ModelRoutingID: av.ModelRoutingID,
				ToolIds:        av.ToolIds,
				Limits:         av.Limits,
				CacheConfig:    av.CacheConfig,
				Guardrails:         av.Guardrails,
				MemoryConfig:   av.MemoryConfig,
				ResponseSchema: av.ResponseSchema,
				AuthConfig:     a.AuthConfig,
				ModelRoutes:    a.ModelRoutes,
			})
		}
	}
	return rows, nil
}

func (s *testStore) GetPrompt(_ context.Context, id string) (db.Prompts, error) {
	if p, ok := s.prompts[id]; ok {
		return p, nil
	}
	return db.Prompts{}, pgx.ErrNoRows
}

func (s *testStore) CreatePrompt(_ context.Context, arg db.CreatePromptParams) (db.Prompts, error) {
	p := db.Prompts{
		ID:            arg.ID,
		Name:          arg.Name,
		Description:   arg.Description,
		ActiveVersion: arg.ActiveVersion,
	}
	s.prompts[arg.ID] = p
	return p, nil
}

func (s *testStore) CreatePromptVersion(_ context.Context, arg db.CreatePromptVersionParams) (db.PromptVersions, error) {
	pv := db.PromptVersions{
		ID:        arg.ID,
		PromptID:  arg.PromptID,
		Version:   arg.Version,
		Template:  arg.Template,
		Variables: arg.Variables,
	}
	s.promptVersions[arg.PromptID] = pv
	return pv, nil
}

func (s *testStore) GetActivePromptVersion(_ context.Context, id string) (db.PromptVersions, error) {
	if pv, ok := s.promptVersions[id]; ok {
		return pv, nil
	}
	return db.PromptVersions{}, pgx.ErrNoRows
}

func (s *testStore) UpdatePromptActiveVersion(_ context.Context, arg db.UpdatePromptActiveVersionParams) (db.Prompts, error) {
	if p, ok := s.prompts[arg.ID]; ok {
		p.ActiveVersion = arg.ActiveVersion
		s.prompts[arg.ID] = p
		return p, nil
	}
	return db.Prompts{}, pgx.ErrNoRows
}

func (s *testStore) GetPolicy(_ context.Context, id string) (db.Policies, error) {
	if pol, ok := s.policies[id]; ok {
		return pol, nil
	}
	return db.Policies{}, pgx.ErrNoRows
}

func (s *testStore) CreatePolicy(_ context.Context, arg db.CreatePolicyParams) (db.Policies, error) {
	p := db.Policies{
		ID:            arg.ID,
		Name:          arg.Name,
		Description:   arg.Description,
		ActiveVersion: arg.ActiveVersion,
	}
	s.policies[arg.ID] = p
	return p, nil
}

func (s *testStore) CreatePolicyVersion(_ context.Context, arg db.CreatePolicyVersionParams) (db.PolicyVersions, error) {
	pv := db.PolicyVersions{
		ID:         arg.ID,
		PolicyID:   arg.PolicyID,
		Version:    arg.Version,
		Expression: arg.Expression,
	}
	s.policyVersions[arg.PolicyID] = pv
	return pv, nil
}

func (s *testStore) GetActivePolicyVersion(_ context.Context, id string) (db.PolicyVersions, error) {
	if pv, ok := s.policyVersions[id]; ok {
		return pv, nil
	}
	return db.PolicyVersions{}, pgx.ErrNoRows
}

func (s *testStore) UpdatePolicyActiveVersion(_ context.Context, arg db.UpdatePolicyActiveVersionParams) (db.Policies, error) {
	if p, ok := s.policies[arg.ID]; ok {
		p.ActiveVersion = arg.ActiveVersion
		s.policies[arg.ID] = p
		return p, nil
	}
	return db.Policies{}, pgx.ErrNoRows
}

func (s *testStore) GetTool(_ context.Context, id string) (db.Tools, error) {
	if t, ok := s.tools[id]; ok {
		return t, nil
	}
	return db.Tools{}, pgx.ErrNoRows
}

func (s *testStore) CreateTool(_ context.Context, arg db.CreateToolParams) (db.Tools, error) {
	t := db.Tools{
		ID:            arg.ID,
		Name:          arg.Name,
		Description:   arg.Description,
		ActiveVersion: arg.ActiveVersion,
	}
	s.tools[arg.ID] = t
	return t, nil
}

func (s *testStore) CreateToolVersion(_ context.Context, arg db.CreateToolVersionParams) (db.ToolVersions, error) {
	tv := db.ToolVersions{
		ID:               arg.ID,
		ToolID:           arg.ToolID,
		Version:          arg.Version,
		SideEffect:       arg.SideEffect,
		InputSchema:      arg.InputSchema,
		IdentityBindings: arg.IdentityBindings,
		Executor:         arg.Executor,
		DownstreamAuth:   arg.DownstreamAuth,
		Confirmation:     arg.Confirmation,
		Authorization:    arg.Authorization,
		ResultConfig:     arg.ResultConfig,
		CacheConfig:      arg.CacheConfig,
	}
	s.toolVersions[arg.ToolID] = tv
	return tv, nil
}

func (s *testStore) GetActiveToolVersion(_ context.Context, id string) (db.ToolVersions, error) {
	if tv, ok := s.toolVersions[id]; ok {
		return tv, nil
	}
	return db.ToolVersions{}, pgx.ErrNoRows
}

func (s *testStore) UpdateToolActiveVersion(_ context.Context, arg db.UpdateToolActiveVersionParams) (db.Tools, error) {
	if t, ok := s.tools[arg.ID]; ok {
		t.ActiveVersion = arg.ActiveVersion
		s.tools[arg.ID] = t
		return t, nil
	}
	return db.Tools{}, pgx.ErrNoRows
}

func (s *testStore) UpsertProviderConfig(_ context.Context, arg db.UpsertProviderConfigParams) (db.ProviderConfigs, error) {
	p := db.ProviderConfigs{
		ID:              arg.ID,
		ProviderName:    arg.ProviderName,
		BaseUrl:         arg.BaseUrl,
		EncryptedApiKey: arg.EncryptedApiKey,
	}
	s.providers[arg.ProviderName] = p
	return p, nil
}

func (s *testStore) UpsertModelRouting(_ context.Context, arg db.UpsertModelRoutingParams) (db.ModelRoutings, error) {
	m := db.ModelRoutings{
		ID:                  arg.ID,
		LogicalModel:        arg.LogicalModel,
		Deployment:          arg.Deployment,
		Adapter:             arg.Adapter,
		FallbackDeployments: arg.FallbackDeployments,
		InputModalities:     arg.InputModalities,
		OutputModalities:    arg.OutputModalities,
	}
	s.models[arg.LogicalModel] = m
	return m, nil
}

func (s *testStore) GetAIApp(_ context.Context, id string) (db.AiApps, error) {
	if a, ok := s.apps[id]; ok {
		return a, nil
	}
	return db.AiApps{}, pgx.ErrNoRows
}

func (s *testStore) CreateAIApp(_ context.Context, arg db.CreateAIAppParams) (db.AiApps, error) {
	a := db.AiApps{
		ID:            arg.ID,
		Name:          arg.Name,
		Description:   arg.Description,
		ActiveVersion: arg.ActiveVersion,
		AuthConfig:    arg.AuthConfig,
		ModelRoutes:   arg.ModelRoutes,
	}
	s.apps[arg.ID] = a
	return a, nil
}

func (s *testStore) CreateAIAppVersion(_ context.Context, arg db.CreateAIAppVersionParams) (db.AiAppVersions, error) {
	av := db.AiAppVersions{
		ID:             arg.ID,
		AppID:          arg.AppID,
		Version:        arg.Version,
		AccessPolicy:   arg.AccessPolicy,
		PromptID:       arg.PromptID,
		PromptVersion:  arg.PromptVersion,
		ModelRoutingID: arg.ModelRoutingID,
		ToolIds:        arg.ToolIds,
		Limits:         arg.Limits,
		CacheConfig:    arg.CacheConfig,
		Guardrails:     arg.Guardrails,
		ResponseSchema: arg.ResponseSchema,
		MemoryConfig:   arg.MemoryConfig,
	}
	s.appVersions[arg.AppID] = av
	return av, nil
}

func (s *testStore) GetActiveAIAppVersion(_ context.Context, id string) (db.GetActiveAIAppVersionRow, error) {
	if av, ok := s.appVersions[id]; ok {
		return db.GetActiveAIAppVersionRow{
			AppID:          av.AppID,
			Version:        av.Version,
			AccessPolicy:   av.AccessPolicy,
			PromptID:       av.PromptID,
			PromptVersion:  av.PromptVersion,
			ModelRoutingID: av.ModelRoutingID,
			ToolIds:        av.ToolIds,
			Limits:         av.Limits,
			CacheConfig:    av.CacheConfig,
			Guardrails:     av.Guardrails,
			ResponseSchema: av.ResponseSchema,
		}, nil
	}
	return db.GetActiveAIAppVersionRow{}, pgx.ErrNoRows
}

func (s *testStore) UpdateAIAppActiveVersion(_ context.Context, arg db.UpdateAIAppActiveVersionParams) (db.AiApps, error) {
	if a, ok := s.apps[arg.ID]; ok {
		a.ActiveVersion = arg.ActiveVersion
		a.AuthConfig = arg.AuthConfig
		s.apps[arg.ID] = a
		return a, nil
	}
	return db.AiApps{}, pgx.ErrNoRows
}

func (s *testStore) IncrementProjectRevision(_ context.Context, _ db.IncrementProjectRevisionParams) (int64, error) {
	s.revision++
	return s.revision, nil
}

func TestEngine_ApplyAndExportRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore()
	publisher := &mockSyncPublisher{}
	masterKey := secrets.DeriveKey("test-master-key-32-byte-phrase")

	engine := NewEngine(store, publisher, masterKey)

	doc := &ProjectDocument{
		BeavendVersion: CurrentVersion,
		Project: ProjectMeta{
			ID:          "proj_retail",
			Name:        "Retail Assistant Project",
			Description: "Retail AI customer app",
		},
		Environment: "staging",
		Auth: &core.AuthConfig{
			Provider: "firebase",
			Issuer:   "https://securetoken.google.com/retail-app",
			Audience: []string{"retail-app"},
		},
		Apps: []AppSpec{
			{
				ID:             "order_assistant",
				Name:           "Order Assistant",
				AccessPolicy:   "auth_policy",
				PromptID:       "retail_prompt",
				ModelRoutingID: "fast",
				ToolIDs:        []string{"get_order_status"},
				Auth: &core.AuthConfig{
					Provider: "anonymous",
				},
			},
		},
		Prompts: []PromptSpec{
			{
				ID:       "retail_prompt",
				Name:     "Retail Prompt",
				Template: "Help user track their retail orders.",
			},
		},
		Policies: []PolicySpec{
			{
				ID:         "auth_policy",
				Name:       "Auth Policy",
				Expression: "claims.email_verified == true",
			},
		},
		Tools: []ToolSpec{
			{
				ID:   "get_order_status",
				Name: "Get Order Status",
				Executor: core.ExecutorConfig{
					Type:   "http",
					Method: "GET",
					URL:    "https://api.retail.com/orders/{order_id}",
				},
			},
		},
		Models: []ModelSpec{
			{
				ID:           "fast",
				LogicalModel: "fast",
				Deployment:   "gpt-4o-mini",
				Adapter:      "litellm",
				ProviderConfig: &ProviderSpec{
					ProviderName: "litellm",
					BaseURL:      "http://localhost:4000",
					APIKey:       "sk-test-key-litellm",
				},
			},
		},
	}

	// 1. Dry run diff
	diff, err := engine.Diff(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, "proj_retail", diff.ProjectID)
	assert.Equal(t, "staging", diff.Environment)
	assert.Equal(t, 7, diff.Summary.TotalCreated)
	assert.Equal(t, 0, diff.Summary.TotalUpdated)

	// Verify DB is untouched after diff
	_, err = store.GetProject(ctx, "proj_retail")
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	// 2. Apply
	report, err := engine.Apply(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, 7, report.Summary.TotalCreated)
	assert.Equal(t, int64(2), store.revision)
	assert.Equal(t, "proj_retail", publisher.lastEvent.ProjectID)
	assert.Equal(t, "staging", publisher.lastEvent.Environment)

	// 3. Export back and verify
	exported, err := engine.Export(ctx, "proj_retail", "staging")
	require.NoError(t, err)
	assert.Equal(t, doc.Project.ID, exported.Project.ID)
	assert.Equal(t, doc.Environment, exported.Environment)
	assert.Equal(t, doc.Auth.Provider, exported.Auth.Provider)
	require.Len(t, exported.Apps, 1)
	assert.Equal(t, "order_assistant", exported.Apps[0].ID)
	require.NotNil(t, exported.Apps[0].Auth)
	assert.Equal(t, "anonymous", exported.Apps[0].Auth.Provider)
	require.Len(t, exported.Prompts, 1)
	assert.Equal(t, "retail_prompt", exported.Prompts[0].ID)
	require.Len(t, exported.Policies, 1)
	assert.Equal(t, "auth_policy", exported.Policies[0].ID)
	require.Len(t, exported.Tools, 1)
	assert.Equal(t, "get_order_status", exported.Tools[0].ID)
	require.Len(t, exported.Models, 1)
	assert.Equal(t, "fast", exported.Models[0].LogicalModel)
	assert.Equal(t, MaskedSecretPlaceholder, exported.Models[0].ProviderConfig.APIKey)

	// 4. Update a prompt and re-diff
	doc.Prompts[0].Template = "Updated template for retail orders."
	updateDiff, err := engine.Diff(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, 1, updateDiff.Summary.TotalUpdated)
	assert.Equal(t, 6, updateDiff.Summary.TotalUnchanged)

	// 5. Apply update
	updateReport, err := engine.Apply(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, 1, updateReport.Summary.TotalUpdated)
	assert.Equal(t, int64(3), store.revision)
}
