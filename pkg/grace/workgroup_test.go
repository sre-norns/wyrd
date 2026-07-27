package grace_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/stretchr/testify/require"
)

// concurrencyTracker keeps track of the maximum number of work items running at the same time.
type concurrencyTracker struct {
	running atomic.Int32
	peak    atomic.Int32
}

func (t *concurrencyTracker) enter() {
	running := t.running.Add(1)
	for {
		peak := t.peak.Load()
		if running <= peak || t.peak.CompareAndSwap(peak, running) {
			return
		}
	}
}

func (t *concurrencyTracker) leave() {
	t.running.Add(-1)
}

func TestWorkgroup_AllItemsAreExecuted(t *testing.T) {
	testCases := map[string]struct {
		nWorkers int
		nItems   int
	}{
		"single-worker":    {nWorkers: 1, nItems: 13},
		"few-workers":      {nWorkers: 3, nItems: 32},
		"idle-workers":     {nWorkers: 8, nItems: 2},
		"no-work":          {nWorkers: 4, nItems: 0},
		"zero-workers":     {nWorkers: 0, nItems: 7},
		"negative-workers": {nWorkers: -3, nItems: 7},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var executed atomic.Int32
			tracker := &concurrencyTracker{}

			wg := grace.NewWorkgroup(context.Background(), test.nWorkers)
			for range test.nItems {
				require.NoError(t, wg.Go(func(_ context.Context) error {
					tracker.enter()
					defer tracker.leave()

					executed.Add(1)
					time.Sleep(time.Millisecond)

					return nil
				}))
			}

			require.NoError(t, wg.Wait())
			require.Equal(t, int32(test.nItems), executed.Load(), "all work items must be executed")
			require.LessOrEqual(t, tracker.peak.Load(), int32(max(test.nWorkers, 1)), "concurrency must be limited")
		})
	}
}

func TestWorkgroup_CollectingGroupRunsAllItems(t *testing.T) {
	testCases := map[string]struct {
		nWorkers int
		nItems   int
	}{
		"single-worker": {nWorkers: 1, nItems: 11},
		"few-workers":   {nWorkers: 4, nItems: 27},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var executed atomic.Int32
			tracker := &concurrencyTracker{}

			wg := grace.NewCollectingWorkgroup(context.Background(), test.nWorkers)
			for range test.nItems {
				require.NoError(t, wg.Go(func(_ context.Context) error {
					tracker.enter()
					defer tracker.leave()

					executed.Add(1)
					time.Sleep(time.Millisecond)

					return nil
				}))
			}

			require.NoError(t, wg.Wait())
			require.Equal(t, int32(test.nItems), executed.Load())
			require.LessOrEqual(t, tracker.peak.Load(), int32(test.nWorkers))
		})
	}
}

func TestWorkgroup_FailFast_ReportsFirstError(t *testing.T) {
	expectedErr := errors.New("work item has failed")

	var executed atomic.Int32
	wg := grace.NewWorkgroup(context.Background(), 2)
	require.NoError(t, wg.Go(func(_ context.Context) error {
		executed.Add(1)
		return expectedErr
	}))
	accepted := int32(1)

	// The group is canceled asynchronously, so some items may be accepted before the failure is observed.
	for range 100 {
		if err := wg.Go(func(ctx context.Context) error {
			executed.Add(1)
			<-ctx.Done()

			return ctx.Err()
		}); err != nil {
			require.ErrorIs(t, err, expectedErr, "Go must report the cause of the group cancellation")
			break
		}
		accepted++
	}

	require.ErrorIs(t, wg.Wait(), expectedErr)
	require.Equal(t, accepted, executed.Load(), "every accepted work item must be invoked")
}

func TestWorkgroup_FailFast_CancelsWorkInFlight(t *testing.T) {
	expectedErr := errors.New("nope")

	started := make(chan struct{})
	wg := grace.NewWorkgroup(context.Background(), 2)

	require.NoError(t, wg.Go(func(ctx context.Context) error {
		close(started)
		<-ctx.Done() // Must be canceled by the failure of the item below.

		return ctx.Err()
	}))
	require.NoError(t, wg.Go(func(_ context.Context) error {
		<-started
		return expectedErr
	}))

	require.ErrorIs(t, wg.Wait(), expectedErr)
	require.ErrorIs(t, wg.Context().Err(), context.Canceled, "group context must be canceled when Wait returns")
}

func TestWorkgroup_Collecting_ReportsAllErrors(t *testing.T) {
	errs := []error{
		errors.New("first"),
		errors.New("second"),
		errors.New("third"),
	}

	var executed atomic.Int32
	wg := grace.NewCollectingWorkgroup(context.Background(), 2)
	for _, err := range errs {
		require.NoError(t, wg.Go(func(_ context.Context) error {
			executed.Add(1)
			return err
		}))
	}
	require.NoError(t, wg.Go(func(_ context.Context) error {
		executed.Add(1)
		return nil
	}))

	got := wg.Wait()
	require.Error(t, got)
	require.Equal(t, int32(len(errs)+1), executed.Load(), "an error must not stop other work items")
	for _, err := range errs {
		require.ErrorIs(t, got, err)
	}
}

func TestWorkgroup_ParentContextCancellation(t *testing.T) {
	testCases := map[string]struct {
		newGroup func(ctx context.Context, nWorkers int) *grace.Workgroup
	}{
		"fail-fast":  {newGroup: grace.NewWorkgroup},
		"collecting": {newGroup: grace.NewCollectingWorkgroup},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			started := make(chan struct{})
			wg := test.newGroup(ctx, 1)
			require.NoError(t, wg.Go(func(itemCtx context.Context) error {
				close(started)
				<-itemCtx.Done()

				return nil
			}))

			<-started
			cancel()

			var skipped atomic.Int32
			for range 10 {
				if err := wg.Go(func(_ context.Context) error {
					skipped.Add(1)
					return nil
				}); err != nil {
					require.ErrorIs(t, err, context.Canceled)
				}
			}

			require.ErrorIs(t, wg.Wait(), context.Canceled, "cancellation of the parent context must be reported")
			require.Equal(t, int32(0), skipped.Load(), "no work item must be started after cancellation")
		})
	}
}

func TestWorkgroup_WaitIsIdempotent(t *testing.T) {
	expectedErr := errors.New("kaboom")

	wg := grace.NewWorkgroup(context.Background(), 3)
	require.NoError(t, wg.Go(func(_ context.Context) error {
		return expectedErr
	}))

	require.ErrorIs(t, wg.Wait(), expectedErr)
	require.ErrorIs(t, wg.Wait(), expectedErr)

	noErrGroup := grace.NewCollectingWorkgroup(context.Background(), 2)
	require.NoError(t, noErrGroup.Wait())
	require.NoError(t, noErrGroup.Wait())
}

func TestWorkgroup_ConcurrentProducers(t *testing.T) {
	const nProducers = 8
	const nItemsPerProducer = 16

	var executed atomic.Int32
	tracker := &concurrencyTracker{}

	wg := grace.NewCollectingWorkgroup(context.Background(), 3)

	var producers sync.WaitGroup
	producers.Add(nProducers)
	for range nProducers {
		go func() {
			defer producers.Done()
			for range nItemsPerProducer {
				_ = wg.Go(func(_ context.Context) error {
					tracker.enter()
					defer tracker.leave()

					executed.Add(1)

					return nil
				})
			}
		}()
	}
	producers.Wait()

	require.NoError(t, wg.Wait())
	require.Equal(t, int32(nProducers*nItemsPerProducer), executed.Load())
	require.LessOrEqual(t, tracker.peak.Load(), int32(3))
}

func TestWorkgroup_GoAfterWaitIsRejectedWithoutPanic(t *testing.T) {
	for range 1_000 {
		wg := grace.NewWorkgroup(context.Background(), 1)
		require.NoError(t, wg.Wait())

		var err error
		require.NotPanics(t, func() {
			err = wg.Go(func(_ context.Context) error {
				return nil
			})
		})
		require.ErrorIs(t, err, grace.ErrWorkgroupClosed)
		require.Same(t, context.Cause(wg.Context()), err)
	}
}

func TestWorkgroup_AcceptedWorkIsInvokedAfterCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	wg := grace.NewWorkgroup(parent, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, wg.Go(func(_ context.Context) error {
		close(started)
		<-release
		return nil
	}))
	<-started

	var invoked atomic.Int32
	var observedCancellation atomic.Bool
	require.NoError(t, wg.Go(func(ctx context.Context) error {
		observedCancellation.Store(errors.Is(ctx.Err(), context.Canceled))
		invoked.Add(1)
		return nil
	}))

	cancel()
	close(release)

	require.ErrorIs(t, wg.Wait(), context.Canceled)
	require.Equal(t, int32(1), invoked.Load())
	require.True(t, observedCancellation.Load())
}

func TestWorkgroup_ConcurrentGoAndWaitAreLinearizable(t *testing.T) {
	const (
		iterations = 100
		producers  = 32
	)

	type submission struct {
		err      error
		panicked bool
	}

	for range iterations {
		parent, cancel := context.WithCancel(context.Background())
		wg := grace.NewCollectingWorkgroup(parent, 4)
		start := make(chan struct{})
		submissions := make(chan submission, producers)
		waitResult := make(chan error, 1)
		cancelDone := make(chan struct{})
		var executed atomic.Int32

		for range producers {
			go func() {
				<-start
				result := submission{}
				defer func() {
					if recover() != nil {
						result.panicked = true
					}
					submissions <- result
				}()

				result.err = wg.Go(func(_ context.Context) error {
					executed.Add(1)
					return nil
				})
			}()
		}
		go func() {
			<-start
			waitResult <- wg.Wait()
		}()
		go func() {
			defer close(cancelDone)
			<-start
			cancel()
		}()

		close(start)

		accepted := 0
		rejected := make([]error, 0, producers)
		for range producers {
			result := <-submissions
			require.False(t, result.panicked)
			if result.err == nil {
				accepted++
			} else {
				rejected = append(rejected, result.err)
			}
		}

		waitErr := <-waitResult
		<-cancelDone
		terminalCause := context.Cause(wg.Context())
		if waitErr == nil {
			require.ErrorIs(t, terminalCause, grace.ErrWorkgroupClosed)
		} else {
			require.Same(t, terminalCause, waitErr)
			require.ErrorIs(t, waitErr, context.Canceled)
		}
		for _, err := range rejected {
			require.Same(t, terminalCause, err)
		}
		require.Equal(t, producers, accepted+len(rejected))
		require.Equal(t, int32(accepted), executed.Load())
	}
}

func TestWorkgroup_FailFastExposesOneTerminalCause(t *testing.T) {
	const iterations = 1_000

	for range iterations {
		wg := grace.NewWorkgroup(context.Background(), 8)
		start := make(chan struct{})
		for i := range 8 {
			workErr := errors.New("worker failure " + string(rune('a'+i)))
			require.NoError(t, wg.Go(func(_ context.Context) error {
				<-start
				return workErr
			}))
		}

		close(start)
		<-wg.Context().Done()

		goErr := wg.Go(func(_ context.Context) error {
			return nil
		})
		waitErr := wg.Wait()
		cause := context.Cause(wg.Context())

		require.Same(t, cause, goErr)
		require.Same(t, cause, waitErr)
	}
}

func TestWorkgroup_ConcurrentWaitersReturnOneResult(t *testing.T) {
	firstErr := errors.New("first")
	secondErr := errors.New("second")

	wg := grace.NewCollectingWorkgroup(context.Background(), 2)
	require.NoError(t, wg.Go(func(_ context.Context) error {
		return firstErr
	}))
	require.NoError(t, wg.Go(func(_ context.Context) error {
		return secondErr
	}))

	const waiters = 32
	start := make(chan struct{})
	results := make(chan error, waiters)
	for range waiters {
		go func() {
			<-start
			results <- wg.Wait()
		}()
	}

	close(start)

	firstResult := <-results
	require.ErrorIs(t, firstResult, firstErr)
	require.ErrorIs(t, firstResult, secondErr)
	for range waiters - 1 {
		require.Same(t, firstResult, <-results)
	}
}

func TestWorkgroup_NilWorkIsRejected(t *testing.T) {
	wg := grace.NewWorkgroup(context.Background(), 1)

	require.ErrorIs(t, wg.Go(nil), grace.ErrNilWorkItem)
	require.NoError(t, wg.Wait())
}

func TestWorkgroup_NilParentUsesBackgroundContext(t *testing.T) {
	var wg *grace.Workgroup
	require.NotPanics(t, func() {
		wg = grace.NewWorkgroup(nil, 1)
	})

	var executed atomic.Bool
	require.NoError(t, wg.Go(func(_ context.Context) error {
		executed.Store(true)
		return nil
	}))
	require.NoError(t, wg.Wait())
	require.True(t, executed.Load())
}

func TestWorkgroup_ContextPreservesParentMetadata(t *testing.T) {
	type contextKey struct{}

	expectedValue := "parent value"
	expectedDeadline := time.Now().Add(time.Hour)
	parent := context.WithValue(context.Background(), contextKey{}, expectedValue)
	parent, cancel := context.WithDeadline(parent, expectedDeadline)
	defer cancel()

	wg := grace.NewWorkgroup(parent, 1)
	deadline, ok := wg.Context().Deadline()
	require.True(t, ok)
	require.Equal(t, expectedDeadline, deadline)
	require.Equal(t, expectedValue, wg.Context().Value(contextKey{}))
	require.NoError(t, wg.Wait())
}

func TestWorkgroup_RealFailureIsPreservedAfterParentCancellation(t *testing.T) {
	expectedErr := errors.New("worker failed while shutting down")
	parent, cancel := context.WithCancel(context.Background())
	wg := grace.NewWorkgroup(parent, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, wg.Go(func(_ context.Context) error {
		close(started)
		<-release
		return expectedErr
	}))
	<-started

	cancel()
	<-wg.Context().Done()
	close(release)

	waitErr := wg.Wait()
	cause := context.Cause(wg.Context())
	goErr := wg.Go(func(_ context.Context) error {
		return nil
	})

	require.Same(t, cause, waitErr)
	require.Same(t, cause, goErr)
	require.ErrorIs(t, cause, expectedErr)
	require.ErrorIs(t, cause, context.Canceled)
}

func TestWorkgroup_FirstRealFailureWins(t *testing.T) {
	firstErr := errors.New("first worker failure")
	secondErr := errors.New("second worker failure")
	wg := grace.NewWorkgroup(context.Background(), 2)

	releaseFirst := make(chan struct{})
	require.NoError(t, wg.Go(func(_ context.Context) error {
		<-releaseFirst
		return firstErr
	}))
	require.NoError(t, wg.Go(func(_ context.Context) error {
		<-wg.Context().Done()
		return secondErr
	}))

	close(releaseFirst)

	waitErr := wg.Wait()
	require.Same(t, context.Cause(wg.Context()), waitErr)
	require.ErrorIs(t, waitErr, firstErr)
	require.NotErrorIs(t, waitErr, secondErr)
}

func TestWorkgroup_CancellationBeforeSubmission(t *testing.T) {
	testCases := map[string]struct {
		newGroup func(ctx context.Context, nWorkers int) *grace.Workgroup
	}{
		"fail-fast":  {newGroup: grace.NewWorkgroup},
		"collecting": {newGroup: grace.NewCollectingWorkgroup},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			cancel()
			wg := test.newGroup(parent, 1)

			var executed atomic.Bool
			goErr := wg.Go(func(_ context.Context) error {
				executed.Store(true)
				return nil
			})
			waitErr := wg.Wait()
			cause := context.Cause(wg.Context())

			require.Same(t, cause, goErr)
			require.Same(t, cause, waitErr)
			require.ErrorIs(t, cause, context.Canceled)
			require.False(t, executed.Load())
		})
	}
}

func TestWorkgroup_CancellationDuringBlockedSubmission(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	wg := grace.NewWorkgroup(parent, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, wg.Go(func(_ context.Context) error {
		close(started)
		<-release
		return nil
	}))
	<-started

	var queuedExecuted atomic.Bool
	require.NoError(t, wg.Go(func(_ context.Context) error {
		queuedExecuted.Store(true)
		return nil
	}))

	submitting := make(chan struct{})
	submissionResult := make(chan error, 1)
	go func() {
		close(submitting)
		submissionResult <- wg.Go(func(_ context.Context) error {
			return nil
		})
	}()
	<-submitting

	select {
	case err := <-submissionResult:
		cancel()
		close(release)
		_ = wg.Wait()
		t.Fatalf("submission returned before cancellation: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	cancel()
	goErr := <-submissionResult
	close(release)
	waitErr := wg.Wait()

	require.Same(t, context.Cause(wg.Context()), goErr)
	require.Same(t, context.Cause(wg.Context()), waitErr)
	require.ErrorIs(t, waitErr, context.Canceled)
	require.True(t, queuedExecuted.Load(), "work accepted before cancellation must still be invoked")
}
