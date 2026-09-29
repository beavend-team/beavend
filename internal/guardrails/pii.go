package guardrails

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// PIIMode defines whether PII should be redacted in-place or cause a block error.
type PIIMode string

const (
	// PIIModeRedact replaces sensitive strings with placeholders (e.g. [REDACTED_EMAIL]).
	PIIModeRedact PIIMode = "redact"
	// PIIModeBlock returns an error rejecting the request or response.
	PIIModeBlock PIIMode = "block"
)

// PIIType represents the category of personally identifiable information.
type PIIType string

const (
	PIICreditCard PIIType = "credit_card"
	PIISSN        PIIType = "ssn"
	PIIEmail      PIIType = "email"
	PIIPhone      PIIType = "phone"
	PIIToken      PIIType = "token"
)

// PIIMatch records an identified sensitive token and its location.
type PIIMatch struct {
	Type  PIIType `json:"type"`
	Value string  `json:"-"`
	Start int     `json:"start"`
	End   int     `json:"end"`
}

var (
	// Email regex
	emailRegex = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)

	// US SSN regex (format: XXX-XX-XXXX)
	ssnRegex = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

	// Phone numbers (US/International standard formats with mandatory delimiters or parenthesis)
	phoneRegex = regexp.MustCompile(`\b(?:\+?1[-. ]?)?\(?[0-9]{3}\)?[-. ][0-9]{3}[-. ][0-9]{4}\b|\b(?:\+?1[-. ]?)?\([0-9]{3}\)[-. ]?[0-9]{3}[-. ][0-9]{4}\b`)

	// Bearer JWT tokens or secret keys (e.g. eyJ..., sk_live_..., ghp_...)
	jwtRegex    = regexp.MustCompile(`\beyJ[A-Za-z0-9-_=]+\.[A-Za-z0-9-_=]+\.[A-Za-z0-9-_.+/=]+\b`)
	apiKeyRegex = regexp.MustCompile(`\b(?:sk|pk|ghp|gho)(?:_[a-zA-Z0-9]+)?_[0-9a-zA-Z]{16,}\b`)

	// Candidate credit card numbers (13-19 digits, optional spaces or dashes)
	creditCardCandidateRegex = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
)

// PIIGuardrail scans text for sensitive personal and credential information.
type PIIGuardrail struct {
	mode PIIMode
}

// NewPIIGuardrail instantiates a PII guardrail in either redact or block mode.
func NewPIIGuardrail(mode PIIMode) *PIIGuardrail {
	if mode == "" {
		mode = PIIModeRedact
	}
	return &PIIGuardrail{mode: mode}
}

// Mode returns the active operational mode.
func (p *PIIGuardrail) Mode() PIIMode {
	return p.mode
}

// Detect finds all PII occurrences in the supplied text.
func (p *PIIGuardrail) Detect(text string) []PIIMatch {
	var matches []PIIMatch

	// 1. Credit Cards (with Luhn check to prevent false positives)
	ccMatches := creditCardCandidateRegex.FindAllStringIndex(text, -1)
	for _, loc := range ccMatches {
		candidate := text[loc[0]:loc[1]]
		cleanDigits := sanitizeDigits(candidate)
		if len(cleanDigits) >= 13 && len(cleanDigits) <= 19 && isValidLuhn(cleanDigits) {
			matches = append(matches, PIIMatch{
				Type:  PIICreditCard,
				Value: candidate,
				Start: loc[0],
				End:   loc[1],
			})
		}
	}

	// 2. SSN
	ssnMatches := ssnRegex.FindAllStringIndex(text, -1)
	for _, loc := range ssnMatches {
		matches = append(matches, PIIMatch{
			Type:  PIISSN,
			Value: text[loc[0]:loc[1]],
			Start: loc[0],
			End:   loc[1],
		})
	}

	// 3. Email
	emailMatches := emailRegex.FindAllStringIndex(text, -1)
	for _, loc := range emailMatches {
		matches = append(matches, PIIMatch{
			Type:  PIIEmail,
			Value: text[loc[0]:loc[1]],
			Start: loc[0],
			End:   loc[1],
		})
	}

	// 4. Phone
	phoneMatches := phoneRegex.FindAllStringIndex(text, -1)
	for _, loc := range phoneMatches {
		matches = append(matches, PIIMatch{
			Type:  PIIPhone,
			Value: text[loc[0]:loc[1]],
			Start: loc[0],
			End:   loc[1],
		})
	}

	// 5. Tokens & API Keys
	jwtMatches := jwtRegex.FindAllStringIndex(text, -1)
	for _, loc := range jwtMatches {
		matches = append(matches, PIIMatch{
			Type:  PIIToken,
			Value: text[loc[0]:loc[1]],
			Start: loc[0],
			End:   loc[1],
		})
	}

	apiKeyMatches := apiKeyRegex.FindAllStringIndex(text, -1)
	for _, loc := range apiKeyMatches {
		matches = append(matches, PIIMatch{
			Type:  PIIToken,
			Value: text[loc[0]:loc[1]],
			Start: loc[0],
			End:   loc[1],
		})
	}

	return matches
}

// Redact replaces any identified PII in the text with corresponding redaction tokens.
func (p *PIIGuardrail) Redact(text string) (string, []PIIMatch) {
	matches := p.Detect(text)
	if len(matches) == 0 {
		return text, nil
	}

	result := text

	// Redact tokens
	result = jwtRegex.ReplaceAllString(result, "[REDACTED_TOKEN]")
	result = apiKeyRegex.ReplaceAllString(result, "[REDACTED_API_KEY]")

	// Redact credit cards
	ccMatches := creditCardCandidateRegex.FindAllStringIndex(result, -1)
	for i := len(ccMatches) - 1; i >= 0; i-- {
		loc := ccMatches[i]
		candidate := result[loc[0]:loc[1]]
		digits := sanitizeDigits(candidate)
		if len(digits) >= 13 && len(digits) <= 19 && isValidLuhn(digits) {
			result = result[:loc[0]] + "[REDACTED_CREDIT_CARD]" + result[loc[1]:]
		}
	}

	// Redact SSN
	result = ssnRegex.ReplaceAllString(result, "[REDACTED_SSN]")

	// Redact Email
	result = emailRegex.ReplaceAllString(result, "[REDACTED_EMAIL]")

	// Redact Phone
	result = phoneRegex.ReplaceAllString(result, "[REDACTED_PHONE]")

	return result, matches
}

// CheckInput validates that input text does not contain PII (when in block mode).
func (p *PIIGuardrail) CheckInput(_ context.Context, text string) error {
	if p.mode != PIIModeBlock {
		return nil
	}

	matches := p.Detect(text)
	if len(matches) > 0 {
		return &Violation{
			Type:    "pii",
			Message: fmt.Sprintf("input contains sensitive personal data (%s)", matches[0].Type),
			Details: map[string]any{
				"match_count": len(matches),
				"pii_type":    string(matches[0].Type),
			},
		}
	}
	return nil
}

// CheckOutput validates that output text does not contain PII (when in block mode).
func (p *PIIGuardrail) CheckOutput(_ context.Context, text string) error {
	if p.mode != PIIModeBlock {
		return nil
	}

	matches := p.Detect(text)
	if len(matches) > 0 {
		return &Violation{
			Type:    "pii",
			Message: fmt.Sprintf("output contains sensitive personal data (%s)", matches[0].Type),
			Details: map[string]any{
				"match_count": len(matches),
				"pii_type":    string(matches[0].Type),
			},
		}
	}
	return nil
}

// sanitizeDigits extracts only numeric characters from a string.
func sanitizeDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isValidLuhn implements the Luhn formula (mod 10 algorithm) to validate credit card numbers.
func isValidLuhn(number string) bool {
	sum := 0
	alternate := false

	for i := len(number) - 1; i >= 0; i-- {
		n := int(number[i] - '0')
		if n < 0 || n > 9 {
			return false
		}

		if alternate {
			n *= 2
			if n > 9 {
				n = (n % 10) + 1
			}
		}
		sum += n
		alternate = !alternate
	}

	return sum%10 == 0
}
