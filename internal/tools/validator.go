package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrSchemaValidationFailed indicates tool arguments do not satisfy the configured JSON Schema.
	ErrSchemaValidationFailed = errors.New("tool arguments failed schema validation")
)

// ValidateArguments validates tool arguments against a pre-compiled JSON schema or raw schema map.
func ValidateArguments(args map[string]any, snap *core.Snapshot, toolDef *core.ToolDefinition) error {
	if toolDef == nil {
		return nil
	}

	// 1. Check if pre-compiled schema exists in snapshot
	if snap != nil && snap.CompiledSchemas != nil {
		if rawSchema, ok := snap.CompiledSchemas[toolDef.ID]; ok {
			if schema, valid := rawSchema.(*jsonschema.Schema); valid && schema != nil {
				return validateWithSchema(schema, args)
			}
		}
		if rawSchema, ok := snap.CompiledSchemas[toolDef.Name]; ok {
			if schema, valid := rawSchema.(*jsonschema.Schema); valid && schema != nil {
				return validateWithSchema(schema, args)
			}
		}
	}

	// 2. If no pre-compiled schema, but toolDef has InputSchema, compile on demand
	if len(toolDef.InputSchema) > 0 {
		schemaBytes, err := json.Marshal(toolDef.InputSchema)
		if err != nil {
			return fmt.Errorf("marshal input schema: %w", err)
		}

		jsCompiler := jsonschema.NewCompiler()
		schemaURL := fmt.Sprintf("schema://beavend/tool/%s.json", toolDef.ID)
		if err := jsCompiler.AddResource(schemaURL, bytes.NewReader(schemaBytes)); err != nil {
			return fmt.Errorf("add json schema resource: %w", err)
		}

		schema, err := jsCompiler.Compile(schemaURL)
		if err != nil {
			return fmt.Errorf("compile json schema: %w", err)
		}

		return validateWithSchema(schema, args)
	}

	return nil
}

func validateWithSchema(schema *jsonschema.Schema, args map[string]any) error {
	// jsonschema.Validate expects JSON-unmarshaled types (e.g. map[string]any)
	// Round-trip through JSON to ensure compatible types (floats, ints, bools)
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal arguments for validation: %w", err)
	}

	var jsonVal any
	if err := json.Unmarshal(data, &jsonVal); err != nil {
		return fmt.Errorf("unmarshal arguments for validation: %w", err)
	}

	if err := schema.Validate(jsonVal); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaValidationFailed, err)
	}

	return nil
}
