package limits

import (
	"github.com/beavend/beavend/internal/core"
)

const (
	// DefaultMaxOutputTokens is the conservative upper bound for output tokens
	// if max_tokens is unspecified in ChatRequest.
	DefaultMaxOutputTokens int64 = 4096

	// ApproxCharsPerToken is an industry-standard heuristic for token estimation (3.5 - 4 chars per token).
	ApproxCharsPerToken = 3.5
)

// EstimateRequestTokens computes a conservative upper bound for token consumption
// prior to invoking the AI provider:
//
//	estimated = prompt_tokens_estimate + requested_max_tokens (or default)
func EstimateRequestTokens(req *core.ChatRequest) int64 {
	if req == nil {
		return DefaultMaxOutputTokens
	}

	var promptChars int
	for _, msg := range req.Messages {
		promptChars += len(msg.Role) + len(msg.Content) + 8 // message overhead
	}

	promptEstimate := int64(float64(promptChars) / ApproxCharsPerToken)
	if promptEstimate < 1 {
		promptEstimate = 1
	}

	outputEstimate := DefaultMaxOutputTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		outputEstimate = int64(*req.MaxTokens)
	}

	return promptEstimate + outputEstimate
}
