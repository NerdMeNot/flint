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

// safeCompileOpts returns expr-lang options that sandbox the expression environment.
// Only whitelisted functions and the provided environment variables are available.
// No access to system calls, file I/O, or network.
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

// EvalExpr evaluates a single expression string against the given context.
// The expression can be a bare expression or wrapped in ${{ }}.
//
// Expressions run in a sandboxed environment — only safe string/collection
// builtins are available. No system calls, file I/O, or network access.
func EvalExpr(expression string, ctx ExprContext) (any, error) {
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

// unwrapTemplate strips ${{ }} wrapping if present, returning the inner expression.
func unwrapTemplate(s string) string {
	s = strings.TrimSpace(s)
	if m := exprPattern.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}
