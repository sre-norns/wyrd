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
		"single-worker":   {nWorkers: 1, nItems: 13},
		"few-workers":     {nWorkers: 3, nItems: 32},
		"idle-workers":    {nWorkers: 8, nItems: 2},
		"no-work":         {nWorkers: 4, nItems: 0},
		"invalid-workers": {nWorkers: 0, nItems: 7},
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

	// Note: the group is canceled asynchronously, thus some of the items below may still be accepted and executed.
	for range 100 {
		if err := wg.Go(func(ctx context.Context) error {
			executed.Add(1)
			<-ctx.Done()

			return ctx.Err()
		}); err != nil {
			require.ErrorIs(t, err, expectedErr, "Go must report the cause of the group cancellation")
			break
		}
	}

	require.ErrorIs(t, wg.Wait(), expectedErr)
	require.Less(t, executed.Load(), int32(101), "queued work items must be dropped once the group is canceled")
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
