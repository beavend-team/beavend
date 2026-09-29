package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SchemaGuardrail enforces that assistant completions conform to a specified JSON Schema.
type SchemaGuardrail struct {
	schema *jsonschema.Schema
}

// NewSchemaGuardrail creates a guardrail with a compiled JSON Schema.
func NewSchemaGuardrail(schema *jsonschema.Schema) *SchemaGuardrail {
	return &SchemaGuardrail{
		schema: schema,
	}
}

// CompileSchema compiles a raw JSON Schema string using jsonschema.Compiler.
func CompileSchema(schemaID, rawSchema string) (*jsonschema.Schema, error) {
	if strings.TrimSpace(rawSchema) == "" {
		return nil, nil
	}

	compiler := jsonschema.NewCompiler()
	url := fmt.Sprintf("schema://beavend/app/%s.json", schemaID)
	if err := compiler.AddResource(url, strings.NewReader(rawSchema)); err != nil {
		return nil, fmt.Errorf("add json schema resource: %w", err)
	}

	compiled, err := compiler.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile json schema: %w", err)
	}

	return compiled, nil
}

// CheckInput is a no-op for schema guardrails.
func (sg *SchemaGuardrail) CheckInput(_ context.Context, _ string) error {
	return nil
}

// CheckOutput validates that completion text matches the required JSON Schema.
func (sg *SchemaGuardrail) CheckOutput(_ context.Context, text string) error {
	if sg.schema == nil {
		return nil
	}

	cleanJSON, err := ExtractCleanJSON(text)
	if err != nil {
		return &Violation{
			Type:    "schema",
			Message: fmt.Sprintf("completion is not valid JSON: %v", err),
			Details: map[string]any{
				"raw_output": text,
			},
		}
	}

	var parsed any
	dec := json.NewDecoder(strings.NewReader(cleanJSON))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return &Violation{
			Type:    "schema",
			Message: fmt.Sprintf("failed to parse JSON: %v", err),
			Details: map[string]any{
				"clean_json": cleanJSON,
			},
		}
	}

	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return &Violation{
			Type:    "schema",
			Message: "response contains multiple JSON values or trailing data",
			Details: map[string]any{
				"clean_json": cleanJSON,
			},
		}
	}

	if err := sg.schema.Validate(parsed); err != nil {
		return &Violation{
			Type:    "schema",
			Message: fmt.Sprintf("response violates required JSON Schema: %v", err),
			Details: map[string]any{
				"validation_error": err.Error(),
			},
		}
	}

	return nil
}

// ExtractCleanJSON strips Markdown code fences (e.g. ```json ... ```) and whitespace.
func ExtractCleanJSON(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", fmt.Errorf("empty JSON payload")
	}

	// Strip leading ```json or ```
	if strings.HasPrefix(trimmed, "```") {
		firstLineEnd := strings.Index(trimmed, "\n")
		if firstLineEnd != -1 {
			trimmed = strings.TrimSpace(trimmed[firstLineEnd+1:])
		} else {
			trimmed = strings.TrimPrefix(trimmed, "```")
		}

		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSuffix(trimmed, "```")
			trimmed = strings.TrimSpace(trimmed)
		}
	}

	// Check if valid JSON syntax
	var dummy any
	if err := json.Unmarshal([]byte(trimmed), &dummy); err != nil {
		// Sometimes there is commentary text before or after the JSON block
		start := bytes.IndexAny([]byte(trimmed), "{[")
		end := bytes.LastIndexAny([]byte(trimmed), "}]")
		if start != -1 && end != -1 && end > start {
			extracted := trimmed[start : end+1]
			if err2 := json.Unmarshal([]byte(extracted), &dummy); err2 == nil {
				return extracted, nil
			}
		}
		return "", err
	}

	return trimmed, nil
}
