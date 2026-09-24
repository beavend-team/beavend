package policy

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

// Compiler compiles CEL expressions into executable cel.Program instances.
type Compiler struct {
	env *cel.Env
}

// CompilerOption allows customizing the Compiler environment.
type CompilerOption func(*compilerConfig)

type compilerConfig struct {
	envOpts []cel.EnvOption
}

// WithEnvOptions appends additional CEL environment options.
func WithEnvOptions(opts ...cel.EnvOption) CompilerOption {
	return func(c *compilerConfig) {
		c.envOpts = append(c.envOpts, opts...)
	}
}

// NewCompiler initializes a new Compiler with standard variables and extensions.
// Standard environment variables:
// - claims: map[string]dyn (caller's JWT claims)
// - identity: map[string]dyn (caller's project, environment, issuer, subject, audience)
// - meta: map[string]dyn (request and runtime metadata)
func NewCompiler(opts ...CompilerOption) (*Compiler, error) {
	cfg := &compilerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	baseOpts := []cel.EnvOption{
		cel.Variable("claims", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("identity", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("meta", cel.MapType(cel.StringType, cel.DynType)),
		cel.CrossTypeNumericComparisons(true),
		cel.OptionalTypes(),
		ext.Strings(),
		ext.Lists(),
		ext.Sets(),
	}

	if len(cfg.envOpts) > 0 {
		baseOpts = append(baseOpts, cfg.envOpts...)
	}

	env, err := cel.NewEnv(baseOpts...)
	if err != nil {
		return nil, fmt.Errorf("create cel env: %w", err)
	}

	return &Compiler{env: env}, nil
}

// Env returns the underlying CEL environment.
func (c *Compiler) Env() *cel.Env {
	return c.env
}

// Compile parses and type-checks a CEL expression, ensuring it returns boolean,
// and returns an optimized, reusable cel.Program.
func (c *Compiler) Compile(expr string) (cel.Program, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, fmt.Errorf("empty expression")
	}

	ast, iss := c.env.Compile(trimmed)
	if iss.Err() != nil {
		return nil, fmt.Errorf("compile cel: %w", iss.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("expression must evaluate to bool, got %v", ast.OutputType())
	}

	prg, err := c.env.Program(ast, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		return nil, fmt.Errorf("build cel program: %w", err)
	}

	return prg, nil
}
