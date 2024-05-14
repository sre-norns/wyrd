package grace

import (
	"context"
	"errors"
	"log"
)

// FatalOnError logs and error and terminates the process using [log.Fatal] if err is not nil.
// It does nothing if err is nil.
func FatalOnError(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

// SuccessRequired is similar to [FatalOnError] in that it checks if error passed is not nil,
// and logs error with description prefixing the error message and Exit(1)
// It does nothing if err is nil.
func SuccessRequired(err error, description string) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("%s: %v", description, err)
	}
}
