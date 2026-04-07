package pipeline

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
)

// exprPattern matches ${{ ... }} template expressions.
var exprPattern = regexp.MustCompile(`\$\{\{\s*(.*?)\s*\}\}`)

// ExprContext holds the variables available during expression evaluation.
type ExprContext map[string]any

// FileHasher computes file hashes for the hashFiles() expression function.
// At parse/validate time, a no-op implementation is used.
// At runtime, the engine provides a real implementation.
type FileHasher interface {
	HashFiles(pattern string) (string, error)
}

// safeCompileOpts returns expr-lang options that sandbox the expression environment.
func safeCompileOpts(env map[string]any) []expr.Option {
	return []expr.Option{
		expr.Env(env),
		expr.DisableAllBuiltins(),
		expr.EnableBuiltin("len"),
		expr.EnableBuiltin("all"),
		expr.EnableBuiltin("any"),
		expr.EnableBuiltin("one"),
		expr.EnableBuiltin("none"),
		expr.EnableBuiltin("filter"),
		expr.EnableBuiltin("map"),
		expr.EnableBuiltin("count"),
		expr.EnableBuiltin("contains"),
		expr.EnableBuiltin("startsWith"),
		expr.EnableBuiltin("endsWith"),
		expr.EnableBuiltin("trim"),
		expr.EnableBuiltin("upper"),
		expr.EnableBuiltin("lower"),
		expr.EnableBuiltin("split"),
		expr.EnableBuiltin("join"),
		expr.EnableBuiltin("string"),
		expr.EnableBuiltin("int"),
		expr.EnableBuiltin("float"),
	}
}

// MaxExpressionLength is the maximum allowed length of an expression string
// to protect against pathological inputs.
const MaxExpressionLength = 4096 // Prevent pathological regex/parsing; ~4KB max

// EvalExpr evaluates a single expression string against the given context.
// The expression can be a bare expression or wrapped in ${{ }}.
//
// Expressions run in a sandboxed environment — only safe string/collection
// builtins are available. No system calls, file I/O, or network access.
func EvalExpr(expression string, ctx ExprContext) (any, error) {
	if len(expression) > MaxExpressionLength {
		return nil, fmt.Errorf("%w: expression exceeds %d characters", ErrInvalidExpr, MaxExpressionLength)
	}

	expression = unwrapTemplate(expression)
	if expression == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpr)
	}

	env := make(map[string]any, len(ctx))
	for k, v := range ctx {
		env[k] = v
	}

	program, err := expr.Compile(expression, safeCompileOpts(env)...)
	if err != nil {
		return nil, fmt.Errorf("%w: compile %q: %v", ErrInvalidExpr, expression, err)
	}

	result, err := expr.Run(program, env)
	if err != nil {
		return nil, fmt.Errorf("%w: eval %q: %v", ErrInvalidExpr, expression, err)
	}

	return result, nil
}

// EvalCondition evaluates a ${{ }} expression as a boolean.
// Used for if: conditions on steps.
func EvalCondition(expression string, ctx ExprContext) (bool, error) {
	result, err := EvalExpr(expression, ctx)
	if err != nil {
		return false, err
	}

	switch v := result.(type) {
	case bool:
		return v, nil
	case nil:
		return false, nil
	default:
		return false, fmt.Errorf("%w: %q evaluated to %T, expected bool", ErrInvalidExpr, expression, result)
	}
}

// Interpolate replaces all ${{ ... }} templates in a string with their evaluated values.
func Interpolate(s string, ctx ExprContext) (string, error) {
	var firstErr error
	result := exprPattern.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		val, err := EvalExpr(match, ctx)
		if err != nil {
			firstErr = err
			return match
		}
		return fmt.Sprintf("%v", val)
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}

// BuildRuntimeContext creates an ExprContext with the standard variables
// available during pipeline execution.
func BuildRuntimeContext(opts RuntimeContextOpts) ExprContext {
	ctx := ExprContext{
		"branch":      opts.Branch,
		"commitSha":   opts.CommitSha,
		"shortSha":    truncate(opts.CommitSha, 7),
		"tag":         opts.Tag,
		"environment": opts.Environment,
		"triggeredBy": opts.TriggeredBy,
		"triggerType": opts.TriggerType,
		"status":      opts.Status,
		"project": map[string]any{
			"name": opts.ProjectName,
			"repo": opts.ProjectRepo,
		},
		"run": map[string]any{
			"id": opts.RunID,
		},
	}

	if opts.Inputs != nil {
		ctx["inputs"] = opts.Inputs
	} else {
		ctx["inputs"] = map[string]any{}
	}

	if opts.Env != nil {
		ctx["env"] = opts.Env
	} else {
		ctx["env"] = map[string]any{}
	}

	if opts.Secrets != nil {
		ctx["secrets"] = opts.Secrets
	} else {
		ctx["secrets"] = map[string]any{}
	}

	if opts.Matrix != nil {
		ctx["matrix"] = opts.Matrix
	} else {
		ctx["matrix"] = map[string]any{}
	}

	if opts.Steps != nil {
		ctx["steps"] = opts.Steps
	} else {
		ctx["steps"] = map[string]any{}
	}

	// Webhook context.
	webhook := map[string]any{}
	if opts.WebhookBody != nil {
		webhook["body"] = opts.WebhookBody
	}
	if opts.WebhookHeaders != nil {
		headers := make(map[string]any, len(opts.WebhookHeaders))
		for k, v := range opts.WebhookHeaders {
			headers[k] = v
		}
		webhook["headers"] = headers
	}
	ctx["webhook"] = webhook

	// hashFiles function — uses FileHasher if provided, otherwise returns a
	// deterministic placeholder. The placeholder uses the pattern as input so
	// different patterns produce different cache keys even at validate time.
	if opts.FileHasher != nil {
		ctx["hashFiles"] = func(pattern string) string {
			hash, err := opts.FileHasher.HashFiles(pattern)
			if err != nil {
				return "#ERR:hashFiles(" + pattern + ")"
			}
			return hash
		}
	} else {
		ctx["hashFiles"] = func(pattern string) string {
			return "placeholder:" + pattern
		}
	}

	return ctx
}

// RuntimeContextOpts provides the values for building a runtime expression context.
type RuntimeContextOpts struct {
	Branch      string
	CommitSha   string
	Tag         string
	Environment string
	TriggeredBy string
	TriggerType string
	Status      string
	ProjectName string
	ProjectRepo string
	RunID       string
	Inputs      map[string]any
	Env         map[string]any
	Secrets     map[string]any
	Matrix      map[string]any
	Steps       map[string]any
	FileHasher  FileHasher
	// Webhook context (only for webhook-triggered runs).
	WebhookBody    map[string]any
	WebhookHeaders map[string]string
}

// unwrapTemplate strips ${{ }} wrapping if present, returning the inner expression.
func unwrapTemplate(s string) string {
	s = strings.TrimSpace(s)
	if m := exprPattern.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
