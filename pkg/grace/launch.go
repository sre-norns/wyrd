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
