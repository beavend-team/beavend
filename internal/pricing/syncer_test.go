package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncer_HTTPFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"synced-model": {
				"input_cost_per_token": 0.000001,
				"output_cost_per_token": 0.000005
			}
		}`))
	}))
	defer ts.Close()

	cat := NewCatalog()
	syncer := NewSyncer(cat, SyncerConfig{
		CatalogURL: ts.URL,
		Interval:   100 * time.Millisecond,
		HTTPClient: ts.Client(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := syncer.SyncOnce(ctx); err != nil {
		t.Fatalf("SyncOnce failed: %v", err)
	}

	p, found := cat.Lookup("synced-model")
	if !found {
		t.Fatal("expected synced-model to be present after sync")
	}
	if p.InputCostPer1M != 1.0 || p.OutputCostPer1M != 5.0 {
		t.Fatalf("unexpected rates: %+v", p)
	}
}

func TestSyncer_FileFetch(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "prices.json")
	err := os.WriteFile(filePath, []byte(`{
		"file-model": {
			"input_cost_per_1m": 4.5,
			"output_cost_per_1m": 9.0
		}
	}`), 0644)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	cat := NewCatalog()
	syncer := NewSyncer(cat, SyncerConfig{
		CatalogURL: "file://" + filePath,
	})

	if err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce file failed: %v", err)
	}

	p, found := cat.Lookup("file-model")
	if !found {
		t.Fatal("expected file-model to be found")
	}
	if p.InputCostPer1M != 4.5 || p.OutputCostPer1M != 9.0 {
		t.Fatalf("unexpected rates: %+v", p)
	}
}
