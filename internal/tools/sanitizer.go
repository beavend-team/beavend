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

	closingToolResultRegex = regexp.MustCompile(`(?i)</\s*tool_result\b`)
)

// SanitizeOpt configures result sanitization options.
type SanitizeOpt func(*SanitizeOptions)

// SanitizeOptions holds options for sanitizing tool results or content.
type SanitizeOptions struct {
	Nonce    string
	ToolID   string
	CallID   string
	Status   string
	MaxBytes int
	Redact   []string
}

// WithNonce sets a boundary nonce on the tool result delimiter.
func WithNonce(nonce string) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.Nonce = nonce
	}
}

// WithToolID sets the tool identifier on the delimiter.
func WithToolID(toolID string) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.ToolID = toolID
	}
}

// WithCallID sets the invocation call identifier on the delimiter.
func WithCallID(callID string) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.CallID = callID
	}
}

// WithStatus sets the status string ("success" or "error") on the delimiter.
func WithStatus(status string) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.Status = status
	}
}

// WithMaxBytes sets the maximum content byte size cap.
func WithMaxBytes(maxBytes int) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.MaxBytes = maxBytes
	}
}

// WithRedact sets the list of sensitive data patterns to redact.
func WithRedact(redact []string) SanitizeOpt {
	return func(o *SanitizeOptions) {
		o.Redact = redact
	}
}

// Sanitizer processes tool execution results to enforce size limits, redact sensitive data,
// escape XML tag breakouts, and wrap external data into safe untrusted boundary blocks.
type Sanitizer struct{}

// NewSanitizer creates a new result sanitizer.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{}
}

// Sanitize applies truncation, redactions, injection checking, XML escaping, and untrusted delimiter wrapping.
func (s *Sanitizer) Sanitize(res core.ToolResult, cfg core.ResultConfig, opts ...SanitizeOpt) core.ToolResult {
	options := &SanitizeOptions{
		ToolID:   res.ToolID,
		CallID:   res.CallID,
		MaxBytes: cfg.MaxBytes,
		Redact:   cfg.Redact,
	}
	if res.IsError {
		options.Status = "error"
	} else {
		options.Status = "success"
	}
	for _, opt := range opts {
		opt(options)
	}

	content := res.Content

	// 1. Scan for prompt injection attempts in tool results
	for _, pat := range toolInjectionPatterns {
		if pat.MatchString(content) {
			res.IsError = true
			options.Status = "error"
			content = "[BLOCKED: tool result contained potential prompt injection]"
			break
		}
	}

	// 2. Truncate if exceeds max_bytes (bounded by HardMaxBytes)
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	} else if maxBytes > HardMaxBytes {
		maxBytes = HardMaxBytes
	}

	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n...[truncated]"
	}

	// 3. Redaction
	content = s.applyRedactions(content, options.Redact)

	// 4. Escape XML breakout tags inside content to prevent boundary tampering
	content = EscapeClosingToolResultTags(content)

	// 5. Wrap as untrusted external data block with optional nonce
	res.Content = WrapToolResult(content, options.Nonce, options.ToolID, options.CallID, options.Status)
	return res
}

// SanitizeContent sanitizes a raw string content with options, applying XML escaping and delimiter wrapping.
func (s *Sanitizer) SanitizeContent(content string, opts ...SanitizeOpt) string {
	options := &SanitizeOptions{
		Status: "success",
	}
	for _, opt := range opts {
		opt(options)
	}

	for _, pat := range toolInjectionPatterns {
		if pat.MatchString(content) {
			options.Status = "error"
			content = "[BLOCKED: tool result contained potential prompt injection]"
			break
		}
	}

	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	} else if maxBytes > HardMaxBytes {
		maxBytes = HardMaxBytes
	}

	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n...[truncated]"
	}

	content = s.applyRedactions(content, options.Redact)
	content = EscapeClosingToolResultTags(content)

	return WrapToolResult(content, options.Nonce, options.ToolID, options.CallID, options.Status)
}

// SanitizeString is an alias for SanitizeContent.
func (s *Sanitizer) SanitizeString(content string, opts ...SanitizeOpt) string {
	return s.SanitizeContent(content, opts...)
}

// EscapeClosingToolResultTags replaces any closing </tool_result tags to prevent XML breakout attacks.
func EscapeClosingToolResultTags(content string) string {
	return closingToolResultRegex.ReplaceAllString(content, "&lt;/tool_result")
}

// WrapToolResult formats content with XML boundaries, optionally including a cryptographic nonce.
func WrapToolResult(content, nonce, toolID, callID, status string) string {
	if status == "" {
		status = "success"
	}
	if nonce != "" {
		return fmt.Sprintf("<tool_result nonce=%q tool_id=%q call_id=%q status=%q>\n%s\n</tool_result>",
			nonce, toolID, callID, status, content)
	}
	return fmt.Sprintf("<tool_result tool_id=%q call_id=%q status=%q>\n%s\n</tool_result>",
		toolID, callID, status, content)
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
