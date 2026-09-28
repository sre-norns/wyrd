package manifest

import "net/http"

// StatusError is an error that knows how an API should report it: an HTTP
// status and a stable problem code (ADR 0001 §6).
//
// Packages declare their sentinel errors as StatusErrors so that an HTTP layer
// can map any of them to a response without importing -- or even knowing about
// -- the package that returned it. Compare with errors.Is as usual: each
// sentinel is a distinct pointer.
type StatusError struct {
	// Status is the HTTP status an API should respond with.
	Status int
	// Code is stable, kebab-case, and what a client branches on. Messages may
	// change; codes do not.
	Code string
	// Message is the human-readable description.
	Message string
}

// NewStatusError returns a sentinel error reported with status and code.
func NewStatusError(status int, code, message string) *StatusError {
	return &StatusError{Status: status, Code: code, Message: message}
}

// Error implements error.
func (e *StatusError) Error() string {
	return e.Message
}

// HTTPStatus returns the status an API should respond with.
func (e *StatusError) HTTPStatus() int {
	if e.Status == 0 {
		return http.StatusInternalServerError
	}
	return e.Status
}

// ProblemCode returns the stable code a client branches on.
func (e *StatusError) ProblemCode() string {
	return e.Code
}
