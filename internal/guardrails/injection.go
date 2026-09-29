package guardrails

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// InjectionPattern represents a regex pattern targeted at catching prompt injection or jailbreak attempts.
type InjectionPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// InjectionGuardrail detects adversarial prompt injection and jailbreak signatures.
type InjectionGuardrail struct {
	patterns []InjectionPattern
}

// NewInjectionGuardrail instantiates an injection scanner with standard adversarial patterns.
func NewInjectionGuardrail() *InjectionGuardrail {
	patterns := []InjectionPattern{
		{
			Name:    "override_instructions",
			Pattern: regexp.MustCompile(`(?i)\b(ignore|disregard|forget|bypass|override)\s+(all\s+)?(previous|prior|above|system)\s+(instructions|prompts|rules|commands|constraints)\b`),
		},
		{
			Name:    "developer_mode_jailbreak",
			Pattern: regexp.MustCompile(`(?i)\b(enter|enable|activate|now in)\s+(developer|dan|unrestricted|god|jailbreak|chaos)\s+mode\b`),
		},
		{
			Name:    "do_anything_now",
			Pattern: regexp.MustCompile(`(?i)\b(do\s+anything\s+now|you\s+can\s+do\s+anything\s+now|dan\s+prompt)\b`),
		},
		{
			Name:    "system_delimiter_spoofing",
			Pattern: regexp.MustCompile(`(?i)(---|===|\[\[?|<|\{\{?)\s*(system|instruction|admin|root|developer)\s*(---|===|\]\]?|>|\}\}?)`),
		},
		{
			Name:    "roleplay_hijack",
			Pattern: regexp.MustCompile(`(?i)\b(you\s+are\s+no\s+longer\s+an?\s+ai|pretend\s+you\s+have\s+no\s+(rules|ethics|guidelines|restrictions))\b`),
		},
		{
			Name:    "raw_chatml_injection",
			Pattern: regexp.MustCompile(`(?i)<\|im_start\|>\s*(system|assistant)`),
		},
	}

	return &InjectionGuardrail{
		patterns: patterns,
	}
}

// CheckInput scans input text for prompt injection signatures.
func (ig *InjectionGuardrail) CheckInput(_ context.Context, text string) error {
	normalized := strings.Join(strings.Fields(text), " ")

	for _, p := range ig.patterns {
		if loc := p.Pattern.FindStringIndex(normalized); loc != nil {
			matched := normalized[loc[0]:loc[1]]
			return &Violation{
				Type:    "injection",
				Message: fmt.Sprintf("potential prompt injection detected: %s", p.Name),
				Details: map[string]any{
					"rule":    p.Name,
					"matched": matched,
				},
			}
		}
	}

	return nil
}

// CheckOutput returns nil for injection checks on model completions.
func (ig *InjectionGuardrail) CheckOutput(_ context.Context, _ string) error {
	return nil
}
