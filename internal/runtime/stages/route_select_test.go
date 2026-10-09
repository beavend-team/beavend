package stages

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/routing"
)

func setupRouteSelectSnapshot() *core.Snapshot {
	snap := core.NewSnapshot("proj_test", "dev", 1)
	snap.Models["text-model"] = &core.ModelRouting{
		ID:           "mr_text",
		LogicalModel: "text-model",
		Deployment:   "gpt-4o-mini",
		Adapter:      "litellm",
	}
	snap.Models["vision-model"] = &core.ModelRouting{
		ID:           "mr_vision",
		LogicalModel: "vision-model",
		Deployment:   "gpt-4o",
		Adapter:      "litellm",
	}
	snap.Apps["app_1"] = &core.AIApp{
		ID: "app_1",
		ModelRoutes: &core.ModelRoutes{
			Default: "text-model",
			Rules: []core.ModelRouteRule{
				{
					When: core.RouteCondition{InputHas: []string{"image"}},
					Use:  "vision-model",
				},
			},
		},
	}
	return snap
}

func TestRouteSelectStageSuccess(t *testing.T) {
	snap := setupRouteSelectSnapshot()
	resolver := routing.NewResolver()
	stage, err := NewRouteSelectStage(resolver)
	require.NoError(t, err)

	// 1. Default route selection
	state := &core.ReqState{
		RequestID:   "req_1",
		ProjectID:   "proj_test",
		Environment: "dev",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		ChatRequest: &core.ChatRequest{},
	}

	err = stage.Handle(context.Background(), state)
	require.NoError(t, err)
	assert.Equal(t, "text-model", state.SelectedModel)

	// 2. Multimodal image input route selection
	stateImage := &core.ReqState{
		RequestID:   "req_2",
		ProjectID:   "proj_test",
		Environment: "dev",
		AIAppID:     "app_1",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_1"],
		ChatRequest: &core.ChatRequest{
			Attachments: []core.Attachment{{Type: "image", MimeType: "image/png"}},
		},
	}

	err = stage.Handle(context.Background(), stateImage)
	require.NoError(t, err)
	assert.Equal(t, "vision-model", stateImage.SelectedModel)
}

func TestRouteSelectStageSkipsWhenAlreadySelected(t *testing.T) {
	snap := setupRouteSelectSnapshot()
	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	state := &core.ReqState{
		RequestID:     "req_3",
		Snapshot:      snap,
		AIApp:         snap.Apps["app_1"],
		SelectedModel: "pre-selected-model",
	}

	err := stage.Handle(context.Background(), state)
	require.NoError(t, err)
	assert.Equal(t, "pre-selected-model", state.SelectedModel)
}

func TestRouteSelectStageSkipsOnExistingResponse(t *testing.T) {
	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	state := &core.ReqState{
		RequestID:    "req_4",
		ChatResponse: &core.ChatResponse{ID: "cached_1"},
	}

	err := stage.Handle(context.Background(), state)
	require.NoError(t, err)
	assert.Empty(t, state.SelectedModel)
}

func TestRouteSelectStageMissingSnapshot(t *testing.T) {
	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	state := &core.ReqState{
		RequestID: "req_5",
		AIAppID:   "app_1",
		Snapshot:  nil,
	}

	err := stage.Handle(context.Background(), state)
	require.ErrorIs(t, err, ErrMissingSnapshot)
	require.NotNil(t, state.Error)
	assert.Equal(t, "invalid_argument", state.Error.Code)
}

func TestRouteSelectStageMissingAIApp(t *testing.T) {
	snap := setupRouteSelectSnapshot()
	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	state := &core.ReqState{
		RequestID: "req_6",
		Snapshot:  snap,
		AIAppID:   "nonexistent_app",
	}

	err := stage.Handle(context.Background(), state)
	require.ErrorIs(t, err, ErrMissingAIApp)
	require.NotNil(t, state.Error)
	assert.Equal(t, "invalid_argument", state.Error.Code)
}

func TestRouteSelectStageNoMatchError(t *testing.T) {
	snap := core.NewSnapshot("proj_test", "dev", 1)
	snap.Apps["app_empty"] = &core.AIApp{
		ID:          "app_empty",
		ModelRoutes: &core.ModelRoutes{}, // No default, no rules
	}

	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	state := &core.ReqState{
		RequestID:   "req_7",
		Snapshot:    snap,
		AIApp:       snap.Apps["app_empty"],
		ChatRequest: &core.ChatRequest{},
	}

	err := stage.Handle(context.Background(), state)
	require.ErrorIs(t, err, ErrRouteSelectFailed)
	require.NotNil(t, state.Error)
	assert.Equal(t, "not_found", state.Error.Code)
}

func TestRouteSelectStageContextCancellation(t *testing.T) {
	resolver := routing.NewResolver()
	stage, _ := NewRouteSelectStage(resolver)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := stage.Handle(ctx, &core.ReqState{})
	require.ErrorIs(t, err, context.Canceled)
}
