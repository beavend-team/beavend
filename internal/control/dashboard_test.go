package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestDashboardHandler_NilFS(t *testing.T) {
	handler := DashboardHandler(nil)
	req := httptest.NewRequest("GET", "/dashboard/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Beavend Dashboard")
}

func TestDashboardHandler_WithFilesAndSPAFallback(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><body><h1>Root Dashboard</h1></body></html>"),
		},
		"assets/style.css": &fstest.MapFile{
			Data: []byte("body { color: red; }"),
		},
	}

	handler := DashboardHandler(mockFS)

	// 1. Root /dashboard/ serves index.html
	req1 := httptest.NewRequest("GET", "/dashboard/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "Root Dashboard")

	// 2. Direct asset /dashboard/assets/style.css serves CSS
	req2 := httptest.NewRequest("GET", "/dashboard/assets/style.css", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "body { color: red; }")

	// 3. Client-side route /dashboard/apps falls back to index.html
	req3 := httptest.NewRequest("GET", "/dashboard/apps", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusOK, rec3.Code)
	assert.Contains(t, rec3.Body.String(), "Root Dashboard")
}
