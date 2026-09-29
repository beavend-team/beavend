package guardrails

import (
	"context"
	"fmt"
)

// Violation represents a specific safety or policy violation detected by a guardrail.
type Violation struct {
	Type    string         `json:"type"`    // "size", "pii", "injection", "schema"
	Message string         `json:"message"` // User-safe descriptive message
	Details map[string]any `json:"details,omitempty"`
}

func (v *Violation) Error() string {
	return fmt.Sprintf("guardrail violation [%s]: %s", v.Type, v.Message)
}

// Engine coordinates execution of multiple input and output guardrails.
type Engine struct {
	pii       *PIIGuardrail
	injection *InjectionGuardrail
	size      *SizeGuardrail
	schema    *SchemaGuardrail
}

// Config specifies options for constructing a guardrail Engine.
type Config struct {
	MaxInputBytes    int
	MaxOutputBytes   int
	PIIRedaction     bool
	PIIBlock         bool
	InjectionDefense bool
	ResponseSchema   string
}

// NewEngine creates a new guardrail engine configured according to the provided settings.
func NewEngine(cfg Config) *Engine {
	e := &Engine{}

	if cfg.MaxInputBytes > 0 || cfg.MaxOutputBytes > 0 {
		e.size = NewSizeGuardrail(cfg.MaxInputBytes, cfg.MaxOutputBytes)
	}

	if cfg.PIIRedaction || cfg.PIIBlock {
		mode := PIIModeRedact
		if cfg.PIIBlock {
			mode = PIIModeBlock
		}
		e.pii = NewPIIGuardrail(mode)
	}

	if cfg.InjectionDefense {
		e.injection = NewInjectionGuardrail()
	}

	return e
}

// SetSchemaGuardrail attaches a structured JSON Schema validator.
func (e *Engine) SetSchemaGuardrail(sg *SchemaGuardrail) {
	e.schema = sg
}

// CheckInput executes configured input guardrails against text (e.g. user messages).
func (e *Engine) CheckInput(ctx context.Context, text string) error {
	if e.size != nil {
		if err := e.size.CheckInput(ctx, text); err != nil {
			return err
		}
	}

	if e.injection != nil {
		if err := e.injection.CheckInput(ctx, text); err != nil {
			return err
		}
	}

	if e.pii != nil && e.pii.Mode() == PIIModeBlock {
		if err := e.pii.CheckInput(ctx, text); err != nil {
			return err
		}
	}

	return nil
}

// RedactInput sanitizes PII in input text if PII redaction is enabled.
func (e *Engine) RedactInput(text string) (string, []PIIMatch) {
	if e.pii != nil && e.pii.Mode() == PIIModeRedact {
		return e.pii.Redact(text)
	}
	return text, nil
}

// CheckOutput executes configured output guardrails against model completions.
func (e *Engine) CheckOutput(ctx context.Context, text string) error {
	if e.size != nil {
		if err := e.size.CheckOutput(ctx, text); err != nil {
			return err
		}
	}

	if e.pii != nil && e.pii.Mode() == PIIModeBlock {
		if err := e.pii.CheckOutput(ctx, text); err != nil {
			return err
		}
	}

	if e.schema != nil {
		if err := e.schema.CheckOutput(ctx, text); err != nil {
			return err
		}
	}

	return nil
}

// RedactOutput sanitizes PII in model completion if PII redaction is enabled.
func (e *Engine) RedactOutput(text string) (string, []PIIMatch) {
	if e.pii != nil && e.pii.Mode() == PIIModeRedact {
		return e.pii.Redact(text)
	}
	return text, nil
}

// EvaluateOutput evaluates text against output size, output PII, and JSON schema, returning sanitized text.
func (e *Engine) EvaluateOutput(ctx context.Context, text string) (string, error) {
	if err := e.CheckOutput(ctx, text); err != nil {
		return "", err
	}

	sanitized := text
	if e.schema != nil {
		if cleanJSON, err := ExtractCleanJSON(sanitized); err == nil {
			sanitized = cleanJSON
		}
	}

	if e.pii != nil && e.pii.Mode() == PIIModeRedact {
		sanitized, _ = e.pii.Redact(sanitized)
	}

	return sanitized, nil
}
