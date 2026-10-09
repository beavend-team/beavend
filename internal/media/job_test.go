package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/media"
)

func TestInMemoryJobStore(t *testing.T) {
	ctx := context.Background()
	store := media.NewInMemoryJobStore()

	job := &media.MediaJob{
		ID:          media.GenerateJobID(),
		ProjectID:   "proj_test",
		Environment: "prod",
		AIAppID:     "app_video",
		Model:       "luma-ray",
		Modality:    "video",
		Prompt:      "a cinematic flying drone shot",
		Status:      "queued",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err := store.CreateJob(ctx, job, time.Hour)
	require.NoError(t, err)

	fetched, err := store.GetJob(ctx, "proj_test", job.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", fetched.Status)
	assert.Equal(t, job.Prompt, fetched.Prompt)

	// Update job
	fetched.Status = "completed"
	fetched.Result = &core.GenerateResponse{
		ID:       "vid_123",
		Model:    "luma-ray",
		Modality: "video",
		Items: []core.GeneratedItem{
			{URL: "https://cdn.example.com/video.mp4"},
		},
	}
	err = store.UpdateJob(ctx, fetched, time.Hour)
	require.NoError(t, err)

	updated, err := store.GetJob(ctx, "proj_test", job.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", updated.Status)
	require.NotNil(t, updated.Result)
	assert.Equal(t, "vid_123", updated.Result.ID)

	// Non-existent
	_, err = store.GetJob(ctx, "proj_test", "non_existent")
	assert.ErrorIs(t, err, media.ErrJobNotFound)
}
