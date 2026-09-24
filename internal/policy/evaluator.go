package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/beavend/beavend/internal/core"
)

// ContextWithSnapshot returns a new context containing the Snapshot.
func ContextWithSnapshot(ctx context.Context, snap *core.Snapshot) context.Context {
	return core.ContextWithSnapshot(ctx, snap)
}

// SnapshotFromContext extracts the Snapshot from context if present.
func SnapshotFromContext(ctx context.Context) (*core.Snapshot, bool) {
	return core.SnapshotFromContext(ctx)
}

// Evaluator evaluates compiled CEL programs and policy definitions.
type Evaluator struct {
	compiler *Compiler
}

// NewEvaluator creates a new Evaluator. If compiler is nil, a default compiler is created.
func NewEvaluator(compiler *Compiler) (*Evaluator, error) {
	if compiler == nil {
		var err error
		compiler, err = NewCompiler()
		if err != nil {
			return nil, err
		}
	}
	return &Evaluator{compiler: compiler}, nil
}

// Compiler returns the compiler used by this Evaluator.
func (e *Evaluator) Compiler() *Compiler {
	return e.compiler
}

// BuildActivation builds the CEL activation variables from Identity and metadata.
func BuildActivation(id *core.Identity, meta map[string]any) map[string]any {
	var claimsMap map[string]any
	idMap := map[string]any{
		"project":     "",
		"environment": "",
		"issuer":      "",
		"subject":     "",
		"audience":    "",
	}

	if id != nil {
		if id.Claims != nil {
			claimsMap = id.Claims
		} else {
			claimsMap = make(map[string]any)
		}
		idMap["project"] = id.Project
		idMap["environment"] = id.Environment
		idMap["issuer"] = id.Issuer
		idMap["subject"] = id.Subject
		idMap["audience"] = id.Audience
	} else {
		claimsMap = make(map[string]any)
	}

	metaMap := meta
	if metaMap == nil {
		metaMap = make(map[string]any)
	}

	return map[string]any{
		"claims":   claimsMap,
		"identity": idMap,
		"meta":     metaMap,
	}
}

// EvaluateProgram evaluates a pre-compiled cel.Program against identity and metadata.
func (e *Evaluator) EvaluateProgram(ctx context.Context, prg cel.Program, id *core.Identity, meta map[string]any) (bool, error) {
	if prg == nil {
		return false, fmt.Errorf("nil cel program")
	}

	activation := BuildActivation(id, meta)
	val, _, err := prg.ContextEval(ctx, activation)
	if err != nil {
		return false, fmt.Errorf("cel eval: %w", err)
	}

	b, ok := val.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expected bool result, got %T (%v)", val.Value(), val)
	}

	return b, nil
}

// EvaluatePolicy evaluates a policy from a Snapshot by policyID or policy name or inline expression.
func (e *Evaluator) EvaluatePolicy(ctx context.Context, snap *core.Snapshot, policyID string, id *core.Identity, meta map[string]any) (bool, error) {
	trimmed := strings.TrimSpace(policyID)
	if trimmed == "" {
		// Empty policy allows access by default
		return true, nil
	}

	if snap != nil {
		// 1. Look up in Snapshot.CompiledPolicies
		if snap.CompiledPolicies != nil {
			if rawPrg, exists := snap.CompiledPolicies[trimmed]; exists {
				if prg, ok := rawPrg.(cel.Program); ok {
					return e.EvaluateProgram(ctx, prg, id, meta)
				}
			}
		}

		// 2. Fallback: check if it's a policy definition in snap.Policies
		if snap.Policies != nil {
			if pDef, exists := snap.Policies[trimmed]; exists && pDef != nil {
				if strings.TrimSpace(pDef.Expression) == "" {
					return true, nil
				}
				// Check if already compiled under pDef.ID or pDef.Name
				if snap.CompiledPolicies != nil {
					if rawPrg, exists := snap.CompiledPolicies[pDef.ID]; exists {
						if prg, ok := rawPrg.(cel.Program); ok {
							return e.EvaluateProgram(ctx, prg, id, meta)
						}
					}
					if pDef.Name != "" {
						if rawPrg, exists := snap.CompiledPolicies[pDef.Name]; exists {
							if prg, ok := rawPrg.(cel.Program); ok {
								return e.EvaluateProgram(ctx, prg, id, meta)
							}
						}
					}
				}
				prg, err := e.compiler.Compile(pDef.Expression)
				if err != nil {
					return false, fmt.Errorf("compile snapshot policy %q: %w", trimmed, err)
				}
				return e.EvaluateProgram(ctx, prg, id, meta)
			}
		}
	}

	// 3. If not found in snapshot, attempt to compile as inline expression
	prg, err := e.compiler.Compile(trimmed)
	if err != nil {
		return false, fmt.Errorf("policy %q not found or invalid: %w", trimmed, err)
	}

	return e.EvaluateProgram(ctx, prg, id, meta)
}

// Evaluate satisfies core.PolicyEvaluator. It extracts the Snapshot from ctx if available,
// falling back to inline expression evaluation.
func (e *Evaluator) Evaluate(ctx context.Context, policyID string, id *core.Identity, meta map[string]any) (bool, error) {
	snap, _ := SnapshotFromContext(ctx)
	return e.EvaluatePolicy(ctx, snap, policyID, id, meta)
}

// SnapshotEvaluator binds an Evaluator to a specific core.Snapshot and satisfies core.PolicyEvaluator.
type SnapshotEvaluator struct {
	snap      *core.Snapshot
	evaluator *Evaluator
}

// NewSnapshotEvaluator creates a new SnapshotEvaluator.
func NewSnapshotEvaluator(snap *core.Snapshot, evaluator *Evaluator) *SnapshotEvaluator {
	return &SnapshotEvaluator{
		snap:      snap,
		evaluator: evaluator,
	}
}

// Evaluate evaluates a policy by ID, name, or inline expression against the bound Snapshot.
func (s *SnapshotEvaluator) Evaluate(ctx context.Context, policyID string, id *core.Identity, meta map[string]any) (bool, error) {
	return s.evaluator.EvaluatePolicy(ctx, s.snap, policyID, id, meta)
}
