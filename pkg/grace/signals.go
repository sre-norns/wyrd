package grace

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Standard OS signals that we want our applications to respect to shutdown nicely
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
var onlyOneSignalHandler = make(chan struct{}) // this is hack to ensure that `SetupSignalHandler` is only called once

// NewSignalHandlingContext registers handlers for shutdownSignals (usually SIGTERM and SIGINT).
// A context is created which will be canceled on one of these signals.
// In case of another signal received during cancellation, the application will be terminated with exit code 1.
func NewSignalHandlingContext() context.Context {
	close(onlyOneSignalHandler) // panics if called twice

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		cancel()
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return ctx
}
