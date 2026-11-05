package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrSchemaValidationFailed indicates tool arguments do not satisfy the configured JSON Schema.
	ErrSchemaValidationFailed = errors.New("tool arguments failed schema validation")
	// ErrToolValidationFailed indicates a tool definition violated security or schema constraints.
	ErrToolValidationFailed = errors.New("tool definition validation failed")
)

// ValidateToolDefinition verifies security, executor, and schema constraints on a ToolDefinition.
func ValidateToolDefinition(toolDef core.ToolDefinition) error {
	// 1. Validate InputSchema
	if len(toolDef.InputSchema) > 0 {
		schemaBytes, err := json.Marshal(toolDef.InputSchema)
		if err != nil {
			return fmt.Errorf("%w: marshal input schema: %v", ErrToolValidationFailed, err)
		}
		trimmed := strings.TrimSpace(string(schemaBytes))
		if trimmed != "" && trimmed != "{}" && trimmed != "null" {
			jsCompiler := jsonschema.NewCompiler()
			schemaURL := fmt.Sprintf("schema://beavend/tool/%s.json", toolDef.ID)
			if err := jsCompiler.AddResource(schemaURL, bytes.NewReader(schemaBytes)); err != nil {
				return fmt.Errorf("%w: invalid input schema: %v", ErrToolValidationFailed, err)
			}
			if _, err := jsCompiler.Compile(schemaURL); err != nil {
				return fmt.Errorf("%w: compile input schema: %v", ErrToolValidationFailed, err)
			}
		}
	}

	// 2. Validate Executor Security Constraints
	execCfg := toolDef.Executor
	switch execCfg.Type {
	case "mcp":
		if strings.EqualFold(execCfg.Transport, "stdio") || strings.TrimSpace(execCfg.Command) != "" {
			return fmt.Errorf("%w: mcp stdio transport is disabled to prevent host execution: only remote http or sse transport is permitted", ErrToolValidationFailed)
		}
		if strings.TrimSpace(execCfg.URL) == "" {
			return fmt.Errorf("%w: mcp executor requires a non-empty url", ErrToolValidationFailed)
		}
	case "sql_template":
		if strings.TrimSpace(execCfg.Template) == "" {
			return fmt.Errorf("%w: sql_template executor requires a non-empty template", ErrToolValidationFailed)
		}
		if err := ValidateReadOnlySQL(execCfg.Template); err != nil {
			return fmt.Errorf("%w: %v", ErrToolValidationFailed, err)
		}
		dsn := strings.TrimSpace(execCfg.URL)
		if dsn == "" && execCfg.Headers != nil {
			dsn = strings.TrimSpace(execCfg.Headers["dsn"])
		}
		if dsn != "" {
			if err := ValidateConnectionTarget(dsn, false); err != nil {
				return fmt.Errorf("%w: database connection blocked by security policy: %v", ErrToolValidationFailed, err)
			}
		}
	case "wasm":
		b64 := strings.TrimSpace(execCfg.WASM.BinaryBase64)
		u := strings.TrimSpace(execCfg.WASM.BinaryURL)
		if b64 == "" && u == "" {
			return fmt.Errorf("%w: wasm executor requires binary_base64 or binary_url", ErrToolValidationFailed)
		}
		if b64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return fmt.Errorf("%w: invalid wasm binary_base64: %v", ErrToolValidationFailed, err)
			}
			if len(decoded) < 8 || string(decoded[:4]) != "\x00asm" {
				return fmt.Errorf("%w: invalid wasm binary: missing required \\x00asm header", ErrToolValidationFailed)
			}
		}
		if u != "" {
			parsed, err := url.Parse(u)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("%w: wasm binary_url must be a valid http or https URL", ErrToolValidationFailed)
			}
		}
		if execCfg.WASM.MaxMemoryMB > 64 {
			return fmt.Errorf("%w: wasm max_memory_mb cannot exceed 64MB", ErrToolValidationFailed)
		}
	}

	return nil
}

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
