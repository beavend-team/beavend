package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaGuardrail(t *testing.T) {
	ctx := context.Background()

	rawSchema := `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"answer": {"type": "string"},
			"confidence": {"type": "number", "minimum": 0, "maximum": 1}
		},
		"required": ["answer", "confidence"]
	}`

	schema, err := CompileSchema("test_app", rawSchema)
	require.NoError(t, err)
	require.NotNil(t, schema)

	sg := NewSchemaGuardrail(schema)

	// Valid JSON
	validJSON := `{"answer": "Paris is the capital of France", "confidence": 0.98}`
	assert.NoError(t, sg.CheckOutput(ctx, validJSON))

	// Valid JSON inside markdown fence
	markdownJSON := "```json\n{\"answer\": \"Tokyo\", \"confidence\": 0.99}\n```"
	assert.NoError(t, sg.CheckOutput(ctx, markdownJSON))

	// Invalid schema (missing required field)
	missingFieldJSON := `{"answer": "Berlin"}`
	errMissing := sg.CheckOutput(ctx, missingFieldJSON)
	require.Error(t, errMissing)
	var vMissing *Violation
	require.ErrorAs(t, errMissing, &vMissing)
	assert.Equal(t, "schema", vMissing.Type)

	// Invalid schema (confidence out of bounds)
	invalidValueJSON := `{"answer": "Rome", "confidence": 1.5}`
	errInvalid := sg.CheckOutput(ctx, invalidValueJSON)
	require.Error(t, errInvalid)

	// Not JSON at all
	notJSON := "Here is the answer: Paris"
	errNotJSON := sg.CheckOutput(ctx, notJSON)
	require.Error(t, errNotJSON)

	// Trailing data or multiple JSON payloads
	multiJSON := `{"answer": "Paris", "confidence": 0.9} {"answer": "London", "confidence": 0.8}`
	errMulti := sg.CheckOutput(ctx, multiJSON)
	require.Error(t, errMulti)
	var vMulti *Violation
	require.ErrorAs(t, errMulti, &vMulti)
	assert.Contains(t, vMulti.Message, "after top-level value")
}

func TestExtractCleanJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "plain json",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "markdown fence json",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "markdown fence without tag",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "surrounding commentary",
			input:    "Here is the result:\n```json\n{\"key\": \"value\"}\n```\nHope that helps!",
			expected: `{"key": "value"}`,
		},
		{
			name:    "empty string",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "non-json",
			input:   "some plain text without json",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractCleanJSON(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.JSONEq(t, tc.expected, got)
			}
		})
	}
}
