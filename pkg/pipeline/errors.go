package pipeline

import (
	"errors"
	"fmt"
)

// Sentinel errors for pipeline parsing and validation.
var (
	ErrInvalidPipeline = errors.New("invalid pipeline")
	ErrCycleDetected   = errors.New("dependency cycle detected")
	ErrInvalidExpr     = errors.New("invalid expression")
)

// Severity indicates how critical a validation issue is.
type Severity string

const (
	SeverityError   Severity = "error"   // blocks runs
	SeverityWarning Severity = "warning" // informational, doesn't block
)

// ParseError is a structured error from pipeline parsing or validation.
type ParseError struct {
	Line       int    // 0 if unknown
	Column     int    // 0 if unknown
	Field      string // e.g., "steps[2].dependsOn"
	Message    string
	Suggestion string   // e.g., "did you mean 'build'?"
	Severity   Severity // default: error
	Err        error
}

func (e *ParseError) Error() string {
	var loc string
	if e.Line > 0 {
		loc = fmt.Sprintf("line %d", e.Line)
		if e.Column > 0 {
			loc = fmt.Sprintf("line %d, col %d", e.Line, e.Column)
		}
		if e.Field != "" {
			loc += ", " + e.Field
		}
	} else if e.Field != "" {
		loc = e.Field
	}

	msg := e.Message
	if e.Suggestion != "" {
		msg += " (" + e.Suggestion + ")"
	}

	if loc != "" {
		return fmt.Sprintf("%s: %s", loc, msg)
	}
	return msg
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

func (e *ParseError) Is(target error) bool {
	return target == ErrInvalidPipeline || target == e.Err
}

// ValidationResult holds all issues found during rich pipeline validation.
type ValidationResult struct {
	Issues []ValidationIssue
}

// Valid returns true if there are no error-severity issues.
func (r *ValidationResult) Valid() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Errors returns only error-severity issues.
func (r *ValidationResult) Errors() []ValidationIssue {
	var errs []ValidationIssue
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			errs = append(errs, issue)
		}
	}
	return errs
}

// Warnings returns only warning-severity issues.
func (r *ValidationResult) Warnings() []ValidationIssue {
	var warns []ValidationIssue
	for _, issue := range r.Issues {
		if issue.Severity == SeverityWarning {
			warns = append(warns, issue)
		}
	}
	return warns
}

// Error codes for programmatic handling of validation issues.
const (
	CodeSyntaxError       = "SYNTAX_ERROR"
	CodeMissingField      = "MISSING_FIELD"
	CodeDuplicateField    = "DUPLICATE_FIELD"
	CodeInvalidValue      = "INVALID_VALUE"
	CodeUnknownRef        = "UNKNOWN_REF"
	CodeCycleDetected     = "CYCLE_DETECTED"
	CodeEnvMismatch       = "ENV_MISMATCH"
	CodeTriggerConflict   = "TRIGGER_CONFLICT"
	CodeInvalidExpression = "INVALID_EXPRESSION"
	CodeInvalidApprover   = "INVALID_APPROVER"
	CodeBoundsExceeded    = "BOUNDS_EXCEEDED"
)

// ValidationIssue is a single validation finding with position info and error code.
type ValidationIssue struct {
	Code       string // programmatic error code (e.g., UNKNOWN_REF)
	Line       int
	Column     int
	Field      string
	Message    string
	Suggestion string
	Severity   Severity
}

func (v ValidationIssue) String() string {
	var loc string
	if v.Line > 0 {
		loc = fmt.Sprintf("line %d", v.Line)
		if v.Column > 0 {
			loc = fmt.Sprintf("line %d, col %d", v.Line, v.Column)
		}
	}
	if v.Field != "" {
		if loc != "" {
			loc += ", "
		}
		loc += v.Field
	}

	msg := v.Message
	if v.Suggestion != "" {
		msg += "\n  " + v.Suggestion
	}

	if loc != "" {
		return fmt.Sprintf("%s: %s", loc, msg)
	}
	return msg
}

// Helper constructors for ParseError.
func newParseError(field, message string) *ParseError {
	return &ParseError{Field: field, Message: message, Severity: SeverityError, Err: ErrInvalidPipeline}
}

func wrapParseError(field, message string, err error) *ParseError {
	return &ParseError{Field: field, Message: message, Severity: SeverityError, Err: err}
}
