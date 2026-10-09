package guardrails

import (
	"context"
	"fmt"
	"strings"

	"github.com/beavend/beavend/internal/core"
)

// MediaGuardrail enforces size, MIME type, and dimension/duration constraints on generated or ingested media.
type MediaGuardrail struct {
	MaxSizeBytes   int64
	AllowedMimes   []string
	MaxDurationSec float64
}

// MediaGuardrailConfig configures MediaGuardrail.
type MediaGuardrailConfig struct {
	MaxSizeBytes   int64
	AllowedMimes   []string
	MaxDurationSec float64
}

// NewMediaGuardrail creates a new MediaGuardrail.
func NewMediaGuardrail(cfg MediaGuardrailConfig) *MediaGuardrail {
	maxBytes := cfg.MaxSizeBytes
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024 // 50MB default limit for media items
	}
	return &MediaGuardrail{
		MaxSizeBytes:   maxBytes,
		AllowedMimes:   cfg.AllowedMimes,
		MaxDurationSec: cfg.MaxDurationSec,
	}
}

// CheckMedia validates a core.MediaRef or generated media attributes against configured constraints.
func (m *MediaGuardrail) CheckMedia(_ context.Context, media *core.MediaRef) error {
	if media == nil {
		return nil
	}

	if m.MaxSizeBytes > 0 && media.Size > m.MaxSizeBytes {
		return &Violation{
			Type:    "size",
			Message: fmt.Sprintf("media size %d bytes exceeds maximum allowed limit %d bytes", media.Size, m.MaxSizeBytes),
			Details: map[string]any{
				"actual_bytes": media.Size,
				"max_bytes":    m.MaxSizeBytes,
				"media_id":     media.MediaID,
			},
		}
	}

	if len(m.AllowedMimes) > 0 && media.MimeType != "" {
		allowed := false
		lowerMime := strings.ToLower(strings.TrimSpace(media.MimeType))
		for _, am := range m.AllowedMimes {
			lowerAllowed := strings.ToLower(strings.TrimSpace(am))
			if lowerAllowed == lowerMime || strings.HasPrefix(lowerMime, strings.TrimSuffix(lowerAllowed, "*")) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &Violation{
				Type:    "media_type",
				Message: fmt.Sprintf("media MIME type %q is not permitted by guardrails", media.MimeType),
				Details: map[string]any{
					"mime_type":     media.MimeType,
					"allowed_mimes": m.AllowedMimes,
				},
			}
		}
	}

	return nil
}

// CheckGeneratedItem validates a generated media item.
func (m *MediaGuardrail) CheckGeneratedItem(ctx context.Context, item *core.GeneratedItem, duration float64) error {
	if item == nil {
		return nil
	}

	if item.Media != nil {
		if err := m.CheckMedia(ctx, item.Media); err != nil {
			return err
		}
	} else if m.MaxSizeBytes > 0 && int64(len(item.Data)) > m.MaxSizeBytes {
		return &Violation{
			Type:    "size",
			Message: fmt.Sprintf("generated media payload %d bytes exceeds maximum allowed limit %d bytes", len(item.Data), m.MaxSizeBytes),
			Details: map[string]any{
				"actual_bytes": len(item.Data),
				"max_bytes":    m.MaxSizeBytes,
			},
		}
	}

	if m.MaxDurationSec > 0 && duration > m.MaxDurationSec {
		return &Violation{
			Type:    "duration",
			Message: fmt.Sprintf("generated media duration %.2f seconds exceeds maximum allowed %.2f seconds", duration, m.MaxDurationSec),
			Details: map[string]any{
				"actual_duration": duration,
				"max_duration":    m.MaxDurationSec,
			},
		}
	}

	return nil
}
