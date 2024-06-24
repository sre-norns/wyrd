package manifest

import (
	"fmt"
	"strings"
)

type ErrorSet []error

func (e ErrorSet) String() string {
	if e == nil {
		return "<nil ErrorSet>"
	}

	if len(e) == 0 {
		return "<empty ErrorSet>"
	}

	if len(e) == 1 {
		return e[0].Error()
	}

	s := make([]string, 0, len(e)+1)
	s = append(s, fmt.Sprintf("Multiple Errors[%d]", len(e)))
	for _, er := range e {
		s = append(s, er.Error())
	}

	return strings.Join(s, "\n")
}

func (e ErrorSet) Error() string {
	return e.String()
}

func AsMultiErrorOrNil(e ...error) error {
	if len(e) == 0 {
		return nil
	}

	nonNilErrors := ErrorSet{}
	for _, er := range e {
		if er != nil {
			nonNilErrors = append(nonNilErrors, er)
		}
	}

	if len(nonNilErrors) == 0 {
		return nil
	}

	return nonNilErrors
}
