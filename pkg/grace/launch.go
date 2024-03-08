package grace

import (
	"context"
	"errors"
	"log"
)

func FatalOnError(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
