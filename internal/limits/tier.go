package limits

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/google/cel-go/cel"
)

// TierResolver resolves active limit rules for a user identity by evaluating CEL tier conditions.
type TierResolver struct {
	compiler  *policy.Compiler
	evaluator *policy.Evaluator
	cache     sync.Map // expression string -> cel.Program
}

// NewTierResolver creates a new TierResolver instance.
func NewTierResolver(compiler *policy.Compiler, evaluator *policy.Evaluator) (*TierResolver, error) {
	if compiler == nil {
		var err error
		compiler, err = policy.NewCompiler()
		if err != nil {
			return nil, fmt.Errorf("create compiler: %w", err)
		}
	}
	if evaluator == nil {
		var err error
		evaluator, err = policy.NewEvaluator(compiler)
		if err != nil {
			return nil, fmt.Errorf("create evaluator: %w", err)
		}
	}

	return &TierResolver{
		compiler:  compiler,
		evaluator: evaluator,
	}, nil
}

// getOrCompileProgram returns a cached compiled cel.Program or compiles and caches it.
// It checks the Snapshot in context first to avoid compilation on the hot path.
func (r *TierResolver) getOrCompileProgram(ctx context.Context, expr string) (cel.Program, error) {
	expr = strings.TrimSpace(expr)
	if snap, ok := core.SnapshotFromContext(ctx); ok && snap != nil && snap.CompiledPolicies != nil {
		if rawPrg, exists := snap.CompiledPolicies[expr]; exists {
			if prg, ok := rawPrg.(cel.Program); ok {
				return prg, nil
			}
		}
	}

	if val, ok := r.cache.Load(expr); ok {
		return val.(cel.Program), nil
	}

	prg, err := r.compiler.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("compile tier condition %q: %w", expr, err)
	}

	r.cache.Store(expr, prg)
	return prg, nil
}

// ResolveRules selects the active limit rules applicable to the given identity.
//
// Rules with an empty TierCondition are default/unconditional rules.
// Rules with a TierCondition are evaluated; if true, they override any default
// rule for the same (resource, window) pair.
func (r *TierResolver) ResolveRules(ctx context.Context, rules []core.LimitRule, id *core.Identity, meta map[string]any) ([]core.LimitRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	type ruleKey struct {
		resource string
		window   int64 // seconds
	}

	defaults := make(map[ruleKey]core.LimitRule)
	matched := make(map[ruleKey]core.LimitRule)
	seenKeys := make([]ruleKey, 0, len(rules))

	for _, rule := range rules {
		windowSec := int64(rule.Window.Seconds())
		if windowSec <= 0 {
			windowSec = 1
		}
		res := strings.TrimSpace(rule.Resource)
		if res == "" {
			res = "inference"
		}
		k := ruleKey{resource: res, window: windowSec}

		condition := strings.TrimSpace(rule.TierCondition)
		if condition == "" {
			// Default rule
			if _, exists := defaults[k]; !exists {
				defaults[k] = rule
				seenKeys = append(seenKeys, k)
			}
			continue
		}

		// Conditional tier rule
		prg, err := r.getOrCompileProgram(ctx, condition)
		if err != nil {
			return nil, err
		}

		applies, err := r.evaluator.EvaluateProgram(ctx, prg, id, meta)
		if err != nil {
			// If condition evaluation fails (e.g. missing claim), it does not apply to this user
			applies = false
		}

		if applies {
			if _, alreadyMatched := matched[k]; !alreadyMatched {
				matched[k] = rule
				// If not seen in defaults, track order
				hasSeen := false
				for _, sk := range seenKeys {
					if sk == k {
						hasSeen = true
						break
					}
				}
				if !hasSeen {
					seenKeys = append(seenKeys, k)
				}
			}
		}
	}

	// Assemble final list: matched tier rule overrides default rule
	resolved := make([]core.LimitRule, 0, len(seenKeys))
	for _, k := range seenKeys {
		if rule, ok := matched[k]; ok {
			resolved = append(resolved, rule)
		} else if rule, ok := defaults[k]; ok {
			resolved = append(resolved, rule)
		}
	}

	return resolved, nil
}
