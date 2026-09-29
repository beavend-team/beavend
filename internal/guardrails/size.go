package guardrails

import (
	"context"
	"fmt"
)

// SizeGuardrail verifies that input and output text payloads remain within configured byte limits.
type SizeGuardrail struct {
	maxInputBytes  int
	maxOutputBytes int
}

// NewSizeGuardrail creates a byte length validation guardrail.
func NewSizeGuardrail(maxInputBytes, maxOutputBytes int) *SizeGuardrail {
	return &SizeGuardrail{
		maxInputBytes:  maxInputBytes,
		maxOutputBytes: maxOutputBytes,
	}
}

// CheckInput validates that input text length does not exceed maxInputBytes.
func (s *SizeGuardrail) CheckInput(_ context.Context, text string) error {
	if s.maxInputBytes > 0 && len(text) > s.maxInputBytes {
		return &Violation{
			Type:    "size",
			Message: fmt.Sprintf("input size %d bytes exceeds maximum allowed %d bytes", len(text), s.maxInputBytes),
			Details: map[string]any{
				"actual_bytes": len(text),
				"max_bytes":    s.maxInputBytes,
			},
		}
	}
	return nil
}

// CheckOutput validates that output text length does not exceed maxOutputBytes.
func (s *SizeGuardrail) CheckOutput(_ context.Context, text string) error {
	if s.maxOutputBytes > 0 && len(text) > s.maxOutputBytes {
		return &Violation{
			Type:    "size",
			Message: fmt.Sprintf("output size %d bytes exceeds maximum allowed %d bytes", len(text), s.maxOutputBytes),
			Details: map[string]any{
				"actual_bytes": len(text),
				"max_bytes":    s.maxOutputBytes,
			},
		}
	}
	return nil
}
