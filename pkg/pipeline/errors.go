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

// ParseError is a structured error from pipeline parsing or validation.
type ParseError struct {
	Field   string // e.g., "steps[2].name", "pipeline"
	Message string
	Err     error
}

func (e *ParseError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

func (e *ParseError) Is(target error) bool {
	if target == ErrInvalidPipeline {
		return true
	}
	return false
}

func newParseError(field, message string) *ParseError {
	return &ParseError{Field: field, Message: message, Err: ErrInvalidPipeline}
}

func wrapParseError(field, message string, err error) *ParseError {
	return &ParseError{Field: field, Message: message, Err: err}
}
