// Package flinterr provides typed error classification for Flint.
//
// Every error in Flint carries an ErrorKind that classifies its nature,
// enabling consistent retry decisions and HTTP status mapping at the
// handler layer.
package flinterr

import (
	"errors"
	"fmt"
)

// ErrorKind classifies the nature of an error.
type ErrorKind int

const (
	// KindTransient indicates a retryable failure (network, timeout, temporary unavailability).
	KindTransient ErrorKind = iota
	// KindInvalidInput indicates bad input from the caller (validation failure, malformed request).
	KindInvalidInput
	// KindNotFound indicates the requested resource does not exist.
	KindNotFound
	// KindConflict indicates a conflict (duplicate, optimistic lock failure).
	KindConflict
	// KindForbidden indicates the caller is not allowed to perform this action.
	KindForbidden
	// KindInternal indicates an unexpected internal error.
	KindInternal
)

func (k ErrorKind) String() string {
	switch k {
	case KindTransient:
		return "transient"
	case KindInvalidInput:
		return "invalid_input"
	case KindNotFound:
		return "not_found"
	case KindConflict:
		return "conflict"
	case KindForbidden:
		return "forbidden"
	case KindInternal:
		return "internal"
	default:
		return "unknown"
	}
}

// Error is a classified error carrying a Kind, human-readable message, and optional cause.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("flint: %s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("flint: %s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// New creates a new Error with the given kind and message.
func New(kind ErrorKind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

// Wrap creates a new Error wrapping an existing error.
func Wrap(kind ErrorKind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

// Convenience constructors.

func NewTransient(msg string) *Error                { return New(KindTransient, msg) }
func NewInvalidInput(msg string) *Error             { return New(KindInvalidInput, msg) }
func NewNotFound(msg string) *Error                 { return New(KindNotFound, msg) }
func NewConflict(msg string) *Error                 { return New(KindConflict, msg) }
func NewForbidden(msg string) *Error                { return New(KindForbidden, msg) }
func NewInternal(msg string) *Error                 { return New(KindInternal, msg) }
func WrapTransient(msg string, err error) *Error    { return Wrap(KindTransient, msg, err) }
func WrapInvalidInput(msg string, err error) *Error { return Wrap(KindInvalidInput, msg, err) }
func WrapNotFound(msg string, err error) *Error     { return Wrap(KindNotFound, msg, err) }
func WrapInternal(msg string, err error) *Error     { return Wrap(KindInternal, msg, err) }

// IsTransient returns true if the error (or any error in its chain) is a transient flinterr.Error.
func IsTransient(err error) bool {
	return IsKind(err, KindTransient)
}

// IsNotFound returns true if the error (or any error in its chain) is a not-found flinterr.Error.
func IsNotFound(err error) bool {
	return IsKind(err, KindNotFound)
}

// IsKind returns true if the error (or any error in its chain) is a flinterr.Error with the given kind.
func IsKind(err error, kind ErrorKind) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == kind
	}
	return false
}

// Kind extracts the ErrorKind from an error. Returns KindInternal if the error
// is not a flinterr.Error.
func Kind(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}
