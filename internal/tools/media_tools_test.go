package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
	"github.com/beavend/beavend/internal/media"
)

type mockGenerator struct {
	generateFn func(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error)
}

func (m *mockGenerator) Generate(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, req)
	}
	return &core.GenerateResponse{}, nil
}

type mockResolver struct {
	gen        core.Generator
	deployment string
	err        error
}

func (m *mockResolver) ResolveGenerator(_ context.Context, _ string, _ *core.Snapshot) (core.Generator, string, error) {
	return m.gen, m.deployment, m.err
}

func TestMediaToolExecutor_Image(t *testing.T) {
	mediaStore, err := media.NewLocalMediaStore(media.LocalMediaStoreConfig{
		BaseDir:    t.TempDir(),
		SigningKey: "secret-key",
		BaseURL:    "http://localhost:8080/v1/media",
	})
	if err != nil {
		t.Fatalf("NewLocalMediaStore failed: %v", err)
	}

	mockGen := &mockGenerator{
		generateFn: func(_ context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
			if req.Modality != "image" {
				t.Fatalf("expected modality image, got %s", req.Modality)
			}
			return &core.GenerateResponse{
				Model: req.Model,
				Items: []core.GeneratedItem{
					{
						Index:        0,
						Data:         []byte("fake-png-data"),
						MimeType:     "image/png",
						RevisedPrompt: "A beautiful scenery (enhanced)",
					},
				},
				Usage: core.UsageStats{
					Images: 1,
				},
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	resolver := &mockResolver{
		gen:        mockGen,
		deployment: "dall-e-3",
	}

	cfg := MediaToolExecutorConfig{
		MediaStore:   mediaStore,
		Resolver:     resolver,
		Snapshot:     snap,
		LogicalModel: "image",
		ProjectID:    "proj_1",
		Environment:  "dev",
		AIAppID:      "app_1",
	}
	exec := NewMediaToolExecutor(cfg)

	call := core.ToolCall{
		ID:        "call_1",
		Name:      "generate_image",
		Arguments: `{"prompt":"A beautiful scenery","size":"1024x1024"}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned error: %s", res.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(res.Content), &output); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if output["status"] != "success" {
		t.Errorf("expected status success, got %v", output["status"])
	}
	urlStr, ok := output["url"].(string)
	if !ok || urlStr == "" {
		t.Errorf("expected non-empty url, got %v", output["url"])
	}
}

func TestMediaToolExecutor_Speech(t *testing.T) {
	mediaStore, err := media.NewLocalMediaStore(media.LocalMediaStoreConfig{
		BaseDir:    t.TempDir(),
		SigningKey: "secret-key",
		BaseURL:    "http://localhost:8080/v1/media",
	})
	if err != nil {
		t.Fatalf("NewLocalMediaStore failed: %v", err)
	}

	mockGen := &mockGenerator{
		generateFn: func(_ context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
			if req.Modality != "audio" {
				t.Fatalf("expected modality audio, got %s", req.Modality)
			}
			return &core.GenerateResponse{
				Model: req.Model,
				Items: []core.GeneratedItem{
					{
						Index:    0,
						Data:     []byte("fake-mp3-audio-bytes"),
						MimeType: "audio/mpeg",
					},
				},
				Usage: core.UsageStats{
					AudioSeconds: 4.5,
				},
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	resolver := &mockResolver{
		gen:        mockGen,
		deployment: "tts-1",
	}

	cfg := MediaToolExecutorConfig{
		MediaStore:   mediaStore,
		Resolver:     resolver,
		Snapshot:     snap,
		LogicalModel: "tts",
		ProjectID:    "proj_1",
		Environment:  "dev",
		AIAppID:      "app_1",
	}
	exec := NewMediaToolExecutor(cfg)

	call := core.ToolCall{
		ID:        "call_2",
		Name:      "generate_speech",
		Arguments: `{"text":"Hello world! Welcome to Beavend."}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned error: %s", res.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(res.Content), &output); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if output["status"] != "success" {
		t.Errorf("expected status success, got %v", output["status"])
	}
	if output["duration_secs"] != 4.5 {
		t.Errorf("expected duration_secs 4.5, got %v", output["duration_secs"])
	}
}

func TestMediaToolExecutor_VideoAsync(t *testing.T) {
	mediaStore, err := media.NewLocalMediaStore(media.LocalMediaStoreConfig{
		BaseDir:    t.TempDir(),
		SigningKey: "secret-key",
		BaseURL:    "http://localhost:8080/v1/media",
	})
	if err != nil {
		t.Fatalf("NewLocalMediaStore failed: %v", err)
	}

	jobStore := media.NewInMemoryJobStore()

	webhookDone := make(chan struct{})
	var webhookPayload map[string]any
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&webhookPayload)
		w.WriteHeader(http.StatusOK)
		close(webhookDone)
	}))
	defer webhookServer.Close()

	mockGen := &mockGenerator{
		generateFn: func(_ context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
			if req.Modality != "video" {
				t.Fatalf("expected modality video, got %s", req.Modality)
			}
			return &core.GenerateResponse{
				Model: req.Model,
				Items: []core.GeneratedItem{
					{
						Index:    0,
						Data:     []byte("fake-mp4-video-stream"),
						MimeType: "video/mp4",
					},
				},
				Usage: core.UsageStats{
					VideoSeconds: 5.0,
				},
			}, nil
		},
	}

	snap := core.NewSnapshot("proj_1", "dev", 1)
	resolver := &mockResolver{
		gen:        mockGen,
		deployment: "sora",
	}

	cfg := MediaToolExecutorConfig{
		MediaStore:   mediaStore,
		JobStore:     jobStore,
		Resolver:     resolver,
		Snapshot:     snap,
		LogicalModel: "video",
		ProjectID:    "proj_1",
		Environment:  "dev",
		AIAppID:      "app_video",
		SSRFClient:   webhookServer.Client(), // allow safe client for httptest
	}
	exec := NewMediaToolExecutor(cfg)

	call := core.ToolCall{
		ID:   "call_3",
		Name: "generate_video",
		Arguments: `{"prompt":"A panda skateboarding","duration":5.0,"webhook_url":"` + webhookServer.URL + `"}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned error: %s", res.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(res.Content), &output); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if output["status"] != "queued" {
		t.Errorf("expected status queued, got %v", output["status"])
	}
	jobID, ok := output["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected non-empty job_id, got %v", output["job_id"])
	}

	// Wait for background job to finish & webhook notification
	select {
	case <-webhookDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook notification")
	}

	// Verify job status in jobStore
	job, err := jobStore.GetJob(context.Background(), "proj_1", jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected job status completed, got %s (error: %s)", job.Status, job.Error)
	}
	if job.Result == nil || len(job.Result.Items) == 0 {
		t.Fatal("expected job result with items")
	}
	if job.Result.Items[0].URL == "" {
		t.Fatal("expected signed URL in job result item")
	}
}

func TestMediaToolExecutor_GuardrailViolation(t *testing.T) {
	mockGen := &mockGenerator{
		generateFn: func(_ context.Context, _ core.GenerateRequest) (*core.GenerateResponse, error) {
			return &core.GenerateResponse{
				Items: []core.GeneratedItem{
					{
						Index:    0,
						Data:     make([]byte, 1024),
						MimeType: "application/x-executable", // Disallowed mime type
					},
				},
			}, nil
		},
	}

	guard := guardrails.NewMediaGuardrail(guardrails.MediaGuardrailConfig{
		AllowedMimes: []string{"image/png", "image/jpeg"},
	})

	cfg := MediaToolExecutorConfig{
		Resolver:  &mockResolver{gen: mockGen, deployment: "img-dep"},
		Snapshot:  core.NewSnapshot("p", "e", 1),
		Guardrail: guard,
	}
	exec := NewMediaToolExecutor(cfg)

	call := core.ToolCall{
		ID:        "call_4",
		Name:      "generate_image",
		Arguments: `{"prompt":"hack"}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true when guardrail fails")
	}
}
