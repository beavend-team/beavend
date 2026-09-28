package tools

import (
	"strings"

	"github.com/beavend/beavend/internal/core"
)

// InjectIdentityBindings injects server-verified identity claims into the tool arguments.
// Any value provided by the caller/LLM for bound keys is strictly discarded and overwritten
// to prevent confused-deputy attacks.
func InjectIdentityBindings(args map[string]any, bindings map[string]string, id *core.Identity) map[string]any {
	if args == nil {
		args = make(map[string]any)
	}
	if len(bindings) == 0 {
		return args
	}

	for argKey, bindingPath := range bindings {
		// Strictly discard any caller- or LLM-supplied value for bound keys
		delete(args, argKey)
		if id != nil {
			val := resolveBindingValue(bindingPath, id)
			if val != nil {
				args[argKey] = val
			}
		}
	}

	return args
}

// resolveBindingValue extracts a value from Identity based on a path expression.
// Supported expressions:
// - "identity.subject", "subject", "sub"
// - "identity.issuer", "issuer", "iss"
// - "identity.project", "project"
// - "identity.environment", "environment", "env"
// - "identity.audience", "audience", "aud"
// - "identity.claims.<key>", "claims.<key>"
func resolveBindingValue(path string, id *core.Identity) any {
	clean := strings.TrimSpace(path)
	clean = strings.TrimPrefix(clean, "identity.")

	switch clean {
	case "subject", "sub":
		return id.Subject
	case "issuer", "iss":
		return id.Issuer
	case "project":
		return id.Project
	case "environment", "env":
		return id.Environment
	case "audience", "aud":
		return id.Audience
	default:
		// Check claims prefix: "claims.<key>" or direct claim name
		claimKey := strings.TrimPrefix(clean, "claims.")
		if val, exists := id.Claim(claimKey); exists {
			return val
		}
		// Try direct path in claims
		if val, exists := id.Claim(clean); exists {
			return val
		}
		return nil
	}
}
