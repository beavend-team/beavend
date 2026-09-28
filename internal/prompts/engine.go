package prompts

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/beavend/beavend/internal/core"
)

var (
	// varPattern matches {{ variable_name }} or {{ nested.variable.name }}
	varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
)

// RenderOptions configures prompt template rendering.
type RenderOptions struct {
	// Strict causes rendering to return an error if a variable is missing.
	Strict bool
	// KeepMissingPlaceholders retains the original {{var}} placeholder when missing (if Strict is false).
	KeepMissingPlaceholders bool
}

// Engine provides prompt templating, variable extraction, and interpolation.
type Engine struct {
	defaultOpts RenderOptions
}

// NewEngine creates a new prompt template engine.
func NewEngine(opts ...RenderOptions) *Engine {
	opt := RenderOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	return &Engine{defaultOpts: opt}
}

// ExtractVariables scans a prompt template and returns an ordered slice of unique variable names.
func ExtractVariables(template string) []string {
	matches := varPattern.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	vars := make([]string, 0, len(matches))

	for _, m := range matches {
		if len(m) > 1 {
			name := m[1]
			if _, exists := seen[name]; !exists {
				seen[name] = struct{}{}
				vars = append(vars, name)
			}
		}
	}

	return vars
}

// Render interpolates variables into the template string using the default options.
func (e *Engine) Render(template string, vars map[string]any) (string, error) {
	return e.RenderWithOptions(template, vars, e.defaultOpts)
}

// Render renders a template using default non-strict options.
func Render(template string, vars map[string]any) (string, error) {
	return NewEngine().Render(template, vars)
}

// RenderWithOptions interpolates variables into the template string with explicit options.
func (e *Engine) RenderWithOptions(template string, vars map[string]any, opts RenderOptions) (string, error) {
	if template == "" {
		return "", nil
	}

	var renderErr error

	rendered := varPattern.ReplaceAllStringFunc(template, func(match string) string {
		if renderErr != nil {
			return match
		}

		submatches := varPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		varPath := submatches[1]
		val, found := resolveVariable(varPath, vars)
		if !found {
			if opts.Strict {
				renderErr = fmt.Errorf("missing required variable %q", varPath)
				return match
			}
			if opts.KeepMissingPlaceholders {
				return match
			}
			return ""
		}

		return formatValue(val)
	})

	if renderErr != nil {
		return "", renderErr
	}

	return rendered, nil
}

// BuildVariableContext constructs a hierarchical variable context from Identity, metadata, and app config.
func BuildVariableContext(snap *core.Snapshot, app *core.AIApp, id *core.Identity, metadata map[string]any) map[string]any {
	ctx := make(map[string]any)

	// Ingress / Snapshot context
	if snap != nil {
		ctx["project_id"] = snap.ProjectID
		ctx["environment"] = snap.Environment
		ctx["project"] = map[string]any{
			"id":  snap.ProjectID,
			"env": snap.Environment,
		}
	}

	// AI App context
	if app != nil {
		ctx["app_id"] = app.ID
		ctx["app_name"] = app.Name
		ctx["app"] = map[string]any{
			"id":          app.ID,
			"name":        app.Name,
			"description": app.Description,
		}
	}

	// Request metadata
	if len(metadata) > 0 {
		ctx["metadata"] = metadata
		for k, v := range metadata {
			if _, exists := ctx[k]; !exists {
				ctx[k] = v
			}
		}
	}

	// Identity context
	if id != nil {
		ctx["user_id"] = id.Subject
		ctx["subject"] = id.Subject
		ctx["issuer"] = id.Issuer

		userMap := map[string]any{
			"id":      id.Subject,
			"subject": id.Subject,
			"issuer":  id.Issuer,
			"claims":  id.Claims,
		}

		if len(id.Claims) > 0 {
			ctx["claims"] = id.Claims

			// Flatten common standard claims into userMap and ctx
			for _, k := range []string{"email", "name", "role", "plan", "locale"} {
				if v, ok := id.Claims[k]; ok {
					userMap[k] = v
					if _, exists := ctx[k]; !exists {
						ctx[k] = v
					}
				}
			}
		}

		ctx["user"] = userMap
	}

	return ctx
}

func resolveVariable(path string, vars map[string]any) (any, bool) {
	if vars == nil {
		return nil, false
	}

	// 1. Direct key match (e.g. vars["user.email"] if flat)
	if val, ok := vars[path]; ok {
		return val, true
	}

	// 2. Nested dot-path lookup (e.g. user.email -> vars["user"]["email"])
	parts := strings.Split(path, ".")
	if len(parts) == 1 {
		return nil, false
	}

	var current any = vars
	for _, part := range parts {
		switch m := current.(type) {
		case map[string]any:
			val, ok := m[part]
			if !ok {
				return nil, false
			}
			current = val
		case map[string]string:
			val, ok := m[part]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}

	return current, true
}

func formatValue(val any) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%t", v)
	case []byte:
		return string(v)
	case map[string]any, map[string]string, []any, []string:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
