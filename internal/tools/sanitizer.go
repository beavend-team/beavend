package tools

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/beavend/beavend/internal/core"
)

const (
	// DefaultMaxBytes is the default size cap for tool results (20KB).
	DefaultMaxBytes = 20 * 1024
	// HardMaxBytes is the absolute ceiling for any tool result regardless of tool config (1MB).
	HardMaxBytes = 1024 * 1024
)

var (
	creditCardRegex = regexp.MustCompile(`(?:\d[ -]*?){13,16}`)
	ssnRegex        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	emailRegex      = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	toolInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(ignore|disregard|forget|bypass|override)\s+(all\s+)?(previous|prior|above|system)\s+(instructions|prompts|rules|commands|constraints)\b`),
		regexp.MustCompile(`(?i)\b(enter|enable|activate|now in)\s+(developer|dan|unrestricted|god|jailbreak|chaos)\s+mode\b`),
		regexp.MustCompile(`(?i)\b(do\s+anything\s+now|you\s+can\s+do\s+anything\s+now|dan\s+prompt)\b`),
		regexp.MustCompile(`(?i)<\|im_start\|>\s*(system|assistant)`),
		regexp.MustCompile(`(?i)(---|===|\[\[?|<|\{\{?)\s*(system|instruction|admin|root|developer)\s*(---|===|\]\]?|>|\}\}?)`),
	}
)

// Sanitizer processes tool execution results to enforce size limits, redact sensitive data,
// and wrap external data into safe untrusted boundary blocks.
type Sanitizer struct{}

// NewSanitizer creates a new result sanitizer.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{}
}

// Sanitize applies truncation, redactions, injection checking, and untrusted delimiter wrapping.
func (s *Sanitizer) Sanitize(res core.ToolResult, cfg core.ResultConfig) core.ToolResult {
	content := res.Content

	// 1. Scan for prompt injection attempts in tool results
	for _, pat := range toolInjectionPatterns {
		if pat.MatchString(content) {
			res.IsError = true
			content = "[BLOCKED: tool result contained potential prompt injection]"
			break
		}
	}

	// 2. Truncate if exceeds max_bytes (bounded by HardMaxBytes)
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	} else if maxBytes > HardMaxBytes {
		maxBytes = HardMaxBytes
	}

	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n...[truncated]"
	}

	// 3. Redaction
	content = s.applyRedactions(content, cfg.Redact)

	// 4. Wrap as untrusted external data block
	status := "success"
	if res.IsError {
		status = "error"
	}

	wrapped := fmt.Sprintf("<tool_result tool_id=%q call_id=%q status=%q>\n%s\n</tool_result>",
		res.ToolID, res.CallID, status, content)

	res.Content = wrapped
	return res
}

func (s *Sanitizer) applyRedactions(content string, redactRules []string) string {
	if len(redactRules) == 0 {
		return content
	}

	for _, rule := range redactRules {
		switch strings.ToLower(strings.TrimSpace(rule)) {
		case "card_number", "credit_card", "card":
			content = creditCardRegex.ReplaceAllString(content, "[REDACTED_CARD]")
		case "ssn", "social_security":
			content = ssnRegex.ReplaceAllString(content, "[REDACTED_SSN]")
		case "email":
			content = emailRegex.ReplaceAllString(content, "[REDACTED_EMAIL]")
		default:
			// Treat as custom regex pattern
			if re, err := regexp.Compile(rule); err == nil {
				content = re.ReplaceAllString(content, "[REDACTED]")
			}
		}
	}

	return content
}
