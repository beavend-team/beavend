package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beavend/beavend/internal/configcode"
	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/control/auth"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyStore struct {
	configcode.QuerySource
}

func (d *dummyStore) GetProject(_ context.Context, id string) (db.Projects, error) {
	return db.Projects{ID: id, Name: "Demo Project"}, nil
}
func (d *dummyStore) CreateProject(_ context.Context, arg db.CreateProjectParams) (db.Projects, error) {
	return db.Projects{ID: arg.ID, Name: arg.Name}, nil
}
func (d *dummyStore) GetEnvironment(_ context.Context, arg db.GetEnvironmentParams) (db.Environments, error) {
	return db.Environments{ProjectID: arg.ProjectID, Name: arg.Name}, nil
}
func (d *dummyStore) CreateEnvironment(_ context.Context, arg db.CreateEnvironmentParams) (db.Environments, error) {
	return db.Environments{ProjectID: arg.ProjectID, Name: arg.Name}, nil
}
func (d *dummyStore) UpdateEnvironmentAuthConfig(_ context.Context, arg db.UpdateEnvironmentAuthConfigParams) (db.Environments, error) {
	return db.Environments{ProjectID: arg.ProjectID, Name: arg.Name, AuthConfig: arg.AuthConfig}, nil
}
func (d *dummyStore) GetSnapshotEnvironment(_ context.Context, _ db.GetSnapshotEnvironmentParams) ([]byte, error) {
	authCfg := core.AuthConfig{
		Provider: "firebase",
		Issuer:   "https://securetoken.google.com/demo",
		Audience: []string{"demo"},
	}
	b, _ := json.Marshal(authCfg)
	return b, nil
}
func (d *dummyStore) GetSnapshotPrompts(_ context.Context, _ db.GetSnapshotPromptsParams) ([]db.GetSnapshotPromptsRow, error) {
	return nil, nil
}
func (d *dummyStore) GetSnapshotPolicies(_ context.Context, _ db.GetSnapshotPoliciesParams) ([]db.GetSnapshotPoliciesRow, error) {
	return nil, nil
}
func (d *dummyStore) GetSnapshotTools(_ context.Context, _ db.GetSnapshotToolsParams) ([]db.GetSnapshotToolsRow, error) {
	return nil, nil
}
func (d *dummyStore) GetSnapshotModelRoutings(_ context.Context, _ db.GetSnapshotModelRoutingsParams) ([]db.ModelRoutings, error) {
	return nil, nil
}
func (d *dummyStore) ListProviderConfigs(_ context.Context, _ db.ListProviderConfigsParams) ([]db.ProviderConfigs, error) {
	return nil, nil
}
func (d *dummyStore) GetSnapshotApps(_ context.Context, _ db.GetSnapshotAppsParams) ([]db.GetSnapshotAppsRow, error) {
	return nil, nil
}
func (d *dummyStore) IncrementProjectRevision(_ context.Context, _ db.IncrementProjectRevisionParams) (int64, error) {
	return 2, nil
}

func TestHandler_ExportAndImportConfig(t *testing.T) {
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))

	// Create test engine with mock store
	engineStore := &dummyStore{}
	cfgEngine := configcode.NewEngine(engineStore, &mockPublisher{}, []byte("test-key-32-chars-long-security!"))
	svc.SetConfigEngine(cfgEngine)

	h := NewHandler(svc)

	r := chi.NewRouter()
	r.Get("/projects/{project_id}/environments/{name}/export", h.HandleExportConfig)
	r.Post("/projects/{project_id}/environments/{name}/import", h.HandleImportConfig)

	// 1. Import config in dry-run mode
	sampleYAML := `
beavend_version: "0.1"
project:
  id: "proj_demo"
  name: "Demo Project"
environment: "staging"
auth:
  provider: "firebase"
  issuer: "https://securetoken.google.com/demo"
  audience: ["demo"]
`
	req := httptest.NewRequest(http.MethodPost, "/projects/proj_demo/environments/staging/import?dry_run=true", bytes.NewBufferString(sampleYAML))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var diff configcode.DiffReport
	err := json.Unmarshal(rec.Body.Bytes(), &diff)
	require.NoError(t, err)
	assert.Equal(t, "proj_demo", diff.ProjectID)
	assert.Equal(t, "staging", diff.Environment)
	assert.Equal(t, 2, diff.Summary.TotalUnchanged) // project + auth unchanged

	// 2. Import config in apply mode
	req = httptest.NewRequest(http.MethodPost, "/projects/proj_demo/environments/staging/import", bytes.NewBufferString(sampleYAML))
	// Add admin context
	user := &auth.AdminUser{ID: "adm_1", Role: auth.RoleOwner}
	req = req.WithContext(auth.WithAdminUser(context.Background(), user))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var report configcode.DiffReport
	err = json.Unmarshal(rec.Body.Bytes(), &report)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Summary.TotalUnchanged)

	// Verify audit log entry
	entries := auditor.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "import_configuration", entries[0].Action)

	// 3. Export config back
	req = httptest.NewRequest(http.MethodGet, "/projects/proj_demo/environments/staging/export", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-yaml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "proj_demo-staging.yaml")
	assert.Contains(t, rec.Body.String(), "beavend_version: \"0.1\"")
	assert.Contains(t, rec.Body.String(), "proj_demo")
}
