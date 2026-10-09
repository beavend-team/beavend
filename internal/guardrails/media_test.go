package guardrails_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
)

func TestMediaGuardrail_SizeAndMime(t *testing.T) {
	ctx := context.Background()

	mg := guardrails.NewMediaGuardrail(guardrails.MediaGuardrailConfig{
		MaxSizeBytes:   1024 * 1024, // 1MB
		AllowedMimes:   []string{"image/png", "image/jpeg", "audio/*"},
		MaxDurationSec: 60.0,
	})

	// 1. Valid media item
	validRef := &core.MediaRef{
		MediaID:  "med_1",
		MimeType: "image/png",
		Size:     500 * 1024,
	}
	err := mg.CheckMedia(ctx, validRef)
	require.NoError(t, err)

	// 2. Size exceeded
	largeRef := &core.MediaRef{
		MediaID:  "med_2",
		MimeType: "image/png",
		Size:     2 * 1024 * 1024, // 2MB
	}
	err = mg.CheckMedia(ctx, largeRef)
	require.Error(t, err)
	var viol *guardrails.Violation
	assert.ErrorAs(t, err, &viol)
	assert.Equal(t, "size", viol.Type)

	// 3. Disallowed MIME type
	badMimeRef := &core.MediaRef{
		MediaID:  "med_3",
		MimeType: "application/x-executable",
		Size:     100,
	}
	err = mg.CheckMedia(ctx, badMimeRef)
	require.Error(t, err)
	assert.ErrorAs(t, err, &viol)
	assert.Equal(t, "media_type", viol.Type)

	// 4. Wildcard MIME matches
	audioRef := &core.MediaRef{
		MediaID:  "med_4",
		MimeType: "audio/mpeg",
		Size:     200 * 1024,
	}
	err = mg.CheckMedia(ctx, audioRef)
	require.NoError(t, err)

	// 5. Duration check
	item := &core.GeneratedItem{
		Index: 0,
		Media: audioRef,
	}
	err = mg.CheckGeneratedItem(ctx, item, 120.0) // 120s > 60s
	require.Error(t, err)
	assert.ErrorAs(t, err, &viol)
	assert.Equal(t, "duration", viol.Type)
}
