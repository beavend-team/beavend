package stages

import (
	"context"
	"errors"
	"testing"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/routing"
)

type dummyAdapter struct{}

func (d *dummyAdapter) Chat(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}
func (d *dummyAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	return nil, nil
}
func (d *dummyAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}
func (d *dummyAdapter) Health(_ context.Context) error {
	return nil
}

func setupRoutingTestSnapshot() *core.Snapshot {
	snap := core.NewSnapshot("proj_test", "dev", 1)
	snap.Models["fast"] = &core.ModelRouting{
		ID:           "mr_fast",
		LogicalModel: "fast",
		Deployment:   "gemini-2.5-flash",
		Adapter:      "litellm",
	}
	snap.Apps["app_1"] = &core.AIApp{
		ID:             "app_1",
		ModelRoutingID: "fast",
	}
	return snap
}

func TestModelRoutingStageSuccess(t *testing.T) {
	snap := setupRoutingTestSnapshot()
	resolver := routing.NewResolver()
	resolver.RegisterAdapter("litellm", &dummyAdapter{})

	stage, err := NewModelRoutingStage(resolver)
	if err != nil {
		t.Fatalf("new stage: %v", err)
	}

	state := &core.ReqState{
		RequestID:   "req_1",
		ProjectID:   "proj_test",
		Environment: "dev",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		ChatRequest: &core.ChatRequest{},
	}

	if err := stage.Handle(context.Background(), state); err != nil {
		t.Fatalf("handle failed: %v", err)
	}

	if state.ResolvedModel == nil {
		t.Fatalf("expected resolved model to be populated")
	}
	if state.ResolvedModel.LogicalModel != "fast" {
		t.Fatalf("expected logical model fast, got %s", state.ResolvedModel.LogicalModel)
	}
	if state.ChatRequest.Model != "gemini-2.5-flash" {
		t.Fatalf("expected deployment gemini-2.5-flash, got %s", state.ChatRequest.Model)
	}
	if state.Error != nil {
		t.Fatalf("expected nil state.Error, got %+v", state.Error)
	}
}

func TestModelRoutingStageMissingSnapshot(t *testing.T) {
	resolver := routing.NewResolver()
	stage, _ := NewModelRoutingStage(resolver)

	state := &core.ReqState{
		RequestID: "req_2",
		AIAppID:   "app_1",
		Snapshot:  nil,
		AIApp:     &core.AIApp{ID: "app_1"},
	}

	err := stage.Handle(context.Background(), state)
	if !errors.Is(err, ErrMissingSnapshot) {
		t.Fatalf("expected ErrMissingSnapshot, got %v", err)
	}
	if state.Error == nil || state.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error response, got %+v", state.Error)
	}
}

func TestModelRoutingStageMissingAIApp(t *testing.T) {
	snap := setupRoutingTestSnapshot()
	resolver := routing.NewResolver()
	stage, _ := NewModelRoutingStage(resolver)

	state := &core.ReqState{
		RequestID: "req_3",
		Snapshot:  snap,
		AIApp:     nil,
	}

	err := stage.Handle(context.Background(), state)
	if !errors.Is(err, ErrMissingAIApp) {
		t.Fatalf("expected ErrMissingAIApp, got %v", err)
	}
	if state.Error == nil || state.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error response, got %+v", state.Error)
	}
}

func TestModelRoutingStageModelNotFound(t *testing.T) {
	snap := core.NewSnapshot("proj_test", "dev", 1)
	snap.Apps["app_missing"] = &core.AIApp{
		ID:             "app_missing",
		ModelRoutingID: "nonexistent",
	}

	resolver := routing.NewResolver()
	stage, _ := NewModelRoutingStage(resolver)

	state := &core.ReqState{
		RequestID: "req_4",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_missing"],
	}

	err := stage.Handle(context.Background(), state)
	if err == nil {
		t.Fatalf("expected error for missing model")
	}
	if state.Error == nil || state.Error.Code != "not_found" {
		t.Fatalf("expected not_found error code, got %+v", state.Error)
	}
}

func TestModelRoutingStageAdapterNotFound(t *testing.T) {
	snap := setupRoutingTestSnapshot()
	// Deliberately do not register "litellm" adapter
	resolver := routing.NewResolver()
	stage, _ := NewModelRoutingStage(resolver)

	state := &core.ReqState{
		RequestID: "req_5",
		Snapshot:  snap,
		AIApp:     snap.Apps["app_1"],
	}

	err := stage.Handle(context.Background(), state)
	if err == nil {
		t.Fatalf("expected error for missing adapter")
	}
	if state.Error == nil || state.Error.Code != "provider_error" {
		t.Fatalf("expected provider_error error code, got %+v", state.Error)
	}
}

func TestModelRoutingStageContextCancellation(t *testing.T) {
	resolver := routing.NewResolver()
	stage, _ := NewModelRoutingStage(resolver)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := stage.Handle(ctx, &core.ReqState{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
