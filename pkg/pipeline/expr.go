package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
)

// exprCtxVar is the env key under which the evaluation deadline context is
// injected. Prefixed to avoid colliding with any user-facing context variable.
const exprCtxVar = "__flintctx"

// exprEvalTimeout bounds how long a single expression may run, defending against
// pathological inputs. Var (not const) so tests can tighten it.
var exprEvalTimeout = time.Second

// exprPattern matches ${{ ... }} template expressions.
var exprPattern = regexp.MustCompile(`\$\{\{\s*(.*?)\s*\}\}`)

// ExprContext holds the variables available during expression evaluation.
type ExprContext map[string]any

// reservedContextVars are the built-in top-level variable names available in
// ${{ }} expressions (see BuildRuntimeContext). User-chosen names — manual
// input names and matrix dimension keys — that collide with these are flagged
// to avoid confusing shadowing.
var reservedContextVars = map[string]bool{
	"branch": true, "commitSha": true, "shortSha": true, "tag": true,
	"environment": true, "triggeredBy": true, "triggerType": true, "status": true,
	"project": true, "run": true, "inputs": true, "env": true, "secrets": true,
	"matrix": true, "steps": true, "webhook": true, "hashFiles": true,
	"success": true, "failure": true, "always": true,
	"fromJSON": true, "toJSON": true, "format": true,
}

// IsReservedContextVar reports whether name collides with a built-in expression
// context variable.
func IsReservedContextVar(name string) bool { return reservedContextVars[name] }

// FileHasher computes file hashes for the hashFiles() expression function.
// At parse/validate time, a no-op implementation is used.
// At runtime, the engine provides a real implementation.
type FileHasher interface {
	HashFiles(pattern string) (string, error)
}

// StdExprFuncs are the context-independent helper functions available in every
// expression environment — validation and runtime alike. They carry no data
// (unlike hashFiles or the status functions), so a single definition can be
// merged into every context builder, which keeps validation and runtime from
// drifting. fromJSON is the linchpin for runtime fan-out (a step emits a JSON
// array; a downstream expression parses it).
func StdExprFuncs() map[string]any {
	return map[string]any{
		// fromJSON parses a JSON string into a value (object → map, array →
		// slice, scalar → scalar). Invalid JSON is a loud error, not a silent
		// nil, so a malformed output fails the expression rather than the step.
		"fromJSON": func(s string) (any, error) {
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return nil, fmt.Errorf("fromJSON: %w", err)
			}
			return v, nil
		},
		// toJSON serializes a value to a compact JSON string.
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("toJSON: %w", err)
			}
			return string(b), nil
		},
		// format substitutes {0}, {1}, … in the template with the positional
		// arguments (GitHub Actions style). A literal brace is written {{ or }}.
		"format": func(format string, args ...any) string {
			return formatTemplate(format, args)
		},
	}
}

// formatTemplate implements the format() placeholder substitution.
func formatTemplate(format string, args []any) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		switch {
		case c == '{' && i+1 < len(format) && format[i+1] == '{':
			b.WriteByte('{')
			i++
		case c == '}' && i+1 < len(format) && format[i+1] == '}':
			b.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(format[i:], '}')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			idx, err := strconv.Atoi(format[i+1 : i+end])
			if err != nil || idx < 0 || idx >= len(args) {
				b.WriteString(format[i : i+end+1]) // leave the placeholder verbatim
			} else {
				fmt.Fprintf(&b, "%v", args[idx])
			}
			i += end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// safeCompileOpts returns expr-lang options that sandbox the expression environment.
func safeCompileOpts(env map[string]any) []expr.Option {
	return []expr.Option{
		expr.Env(env),
		expr.WithContext(exprCtxVar),
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

	timeoutCtx, cancel := context.WithTimeout(context.Background(), exprEvalTimeout)
	defer cancel()

	env := make(map[string]any, len(ctx)+1)
	maps.Copy(env, ctx)
	env[exprCtxVar] = timeoutCtx

	program, err := expr.Compile(expression, safeCompileOpts(env)...)
	if err != nil {
		return nil, fmt.Errorf("%w: compile %q: %v", ErrInvalidExpr, expression, err)
	}

	// Run on a goroutine so EvalExpr returns within the deadline even if the
	// program is pathological; WithContext lets the VM observe cancellation.
	type evalResult struct {
		val any
		err error
	}
	done := make(chan evalResult, 1)
	go func() {
		val, err := expr.Run(program, env)
		done <- evalResult{val, err}
	}()

	select {
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("%w: evaluation timed out after %s", ErrInvalidExpr, exprEvalTimeout)
	case r := <-done:
		if r.err != nil {
			if timeoutCtx.Err() != nil {
				return nil, fmt.Errorf("%w: evaluation timed out after %s", ErrInvalidExpr, exprEvalTimeout)
			}
			return nil, fmt.Errorf("%w: eval %q: %v", ErrInvalidExpr, expression, r.err)
		}
		return r.val, nil
	}
}

// compileExpr type-checks an expression against the given context without
// running it. It returns a non-nil error only for compile/type errors, which
// makes it the right tool for validation (no fragile error-string matching and
// no evaluation side effects).
func compileExpr(expression string, ctx ExprContext) error {
	if len(expression) > MaxExpressionLength {
		return fmt.Errorf("%w: expression exceeds %d characters", ErrInvalidExpr, MaxExpressionLength)
	}
	expression = unwrapTemplate(expression)
	if expression == "" {
		return fmt.Errorf("%w: empty expression", ErrInvalidExpr)
	}

	env := make(map[string]any, len(ctx)+1)
	maps.Copy(env, ctx)
	env[exprCtxVar] = context.Background()

	if _, err := expr.Compile(expression, safeCompileOpts(env)...); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidExpr, err)
	}
	return nil
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

	maps.Copy(ctx, StdExprFuncs())

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
