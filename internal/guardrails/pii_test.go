package guardrails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPIIGuardrail_CreditCard(t *testing.T) {
	g := NewPIIGuardrail(PIIModeRedact)

	// Valid Visa (passes Luhn)
	validVisa := "Please charge 4532015112830366 for my subscription"
	redacted, matches := g.Redact(validVisa)
	assert.Len(t, matches, 1)
	assert.Equal(t, PIICreditCard, matches[0].Type)
	assert.Contains(t, redacted, "[REDACTED_CREDIT_CARD]")
	assert.NotContains(t, redacted, "4532015112830366")

	// Invalid credit card (fails Luhn) -> should NOT be matched as credit card
	invalidCC := "Order number is 4532015112830367 thank you"
	redacted2, matches2 := g.Redact(invalidCC)
	assert.Empty(t, matches2)
	assert.Equal(t, invalidCC, redacted2)
}

func TestPIIGuardrail_SSN_Email_Phone_Token(t *testing.T) {
	g := NewPIIGuardrail(PIIModeRedact)

	input := "User john.doe@example.com (SSN: 123-45-6789, Phone: (555) 234-5678) used token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.doNotLeak"
	redacted, matches := g.Redact(input)

	require.NotEmpty(t, matches)
	assert.Contains(t, redacted, "[REDACTED_EMAIL]")
	assert.Contains(t, redacted, "[REDACTED_SSN]")
	assert.Contains(t, redacted, "[REDACTED_PHONE]")
	assert.Contains(t, redacted, "[REDACTED_TOKEN]")

	assert.NotContains(t, redacted, "john.doe@example.com")
	assert.NotContains(t, redacted, "123-45-6789")
	assert.NotContains(t, redacted, "555")
	assert.NotContains(t, redacted, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
}

func TestPIIGuardrail_SSN_Validation(t *testing.T) {
	g := NewPIIGuardrail(PIIModeRedact)

	// Valid SSN
	valid := "SSN is 219-45-6789"
	redacted, matches := g.Redact(valid)
	assert.Len(t, matches, 1)
	assert.Equal(t, "[REDACTED_SSN]", redacted[7:])

	// Invalid area 000, 666, 900+ or group 00 or serial 0000
	invalidInputs := []string{
		"Code 000-45-6789",
		"Code 666-45-6789",
		"Code 950-45-6789",
		"Code 123-00-6789",
		"Code 123-45-0000",
	}

	for _, inv := range invalidInputs {
		red, m := g.Redact(inv)
		assert.Empty(t, m, "should not match invalid SSN: %s", inv)
		assert.Equal(t, inv, red)
	}
}

func TestPIIGuardrail_BlockMode(t *testing.T) {
	ctx := context.Background()
	g := NewPIIGuardrail(PIIModeBlock)

	cleanInput := "Hello, how can I help you today?"
	assert.NoError(t, g.CheckInput(ctx, cleanInput))
	assert.NoError(t, g.CheckOutput(ctx, cleanInput))

	taintedInput := "Contact me at alice@wonderland.org please"
	err := g.CheckInput(ctx, taintedInput)
	require.Error(t, err)
	var v *Violation
	require.ErrorAs(t, err, &v)
	assert.Equal(t, "pii", v.Type)
	assert.Equal(t, "email", v.Details["pii_type"])

	errOut := g.CheckOutput(ctx, taintedInput)
	require.Error(t, errOut)
}

func TestPIIGuardrail_LuhnValidation(t *testing.T) {
	tests := []struct {
		num   string
		valid bool
	}{
		{"49927398716", true},
		{"49927398717", false},
		{"1234567812345670", true},
		{"1234567812345678", false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.valid, isValidLuhn(tc.num), "num: %s", tc.num)
	}
}

func TestPIIGuardrail_APIKey(t *testing.T) {
	g := NewPIIGuardrail(PIIModeRedact)

	// #nosec G101 -- test dummy key
	input := "Here is my key: " + "sk_live_" + "1234567890abcdef1234567890"
	redacted, matches := g.Redact(input)
	assert.NotEmpty(t, matches)
	assert.True(t, strings.Contains(redacted, "[REDACTED_API_KEY]"))
	assert.False(t, strings.Contains(redacted, "sk_live_"))
}
