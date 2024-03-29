package grace

import "fmt"

// Error represents actionable error interface.
type Error interface {
	// Text message to show to users of what was expected to happen
	WhatExpected() string

	// Text message to show to users of what actually happened
	WhatHappened() string

	// Text message with Call To Action - what a user can be doing to resolve the error
	WhatToDo() string
}

// ActionableError simple implementation of actionable Error interface
type ActionableError struct {
	expected     string
	got          string
	callToAction string
}

// WhatExpected returns string message of what was expected to happen in response to an action.
func (e *ActionableError) WhatExpected() string {
	return e.expected
}

// WhatHappened returns string message of what actually has happened.
func (e *ActionableError) WhatHappened() string {
	return e.got
}

// WhatToDo returns string message of what action(s) user can take to fix the error.
func (e *ActionableError) WhatToDo() string {
	return e.callToAction
}

// Error method is an implementation of the standard `error` interface
func (e *ActionableError) Error() string {
	return fmt.Sprintf("expected: %s, got: %s; What to do: %s", e.expected, e.got, e.callToAction)
}

// RaiseError returns an instance of an actionable error [ActionableError]
func RaiseError(
	expected, got, cta string,
) Error {
	return &ActionableError{
		expected:     expected,
		got:          got,
		callToAction: cta,
	}
}
