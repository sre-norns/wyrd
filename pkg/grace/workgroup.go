package grace

import (
	"context"
	"errors"
	"sync"
)

// WorkItem is a single unit of work to be executed by a [Workgroup].
// The context given to a work item is the group context: it is canceled when the parent context is canceled,
// when the group is done, or - in case of a fail-fast group - when any other work item returns an error.
// Long running work items are expected to observe that context and to return early once it is canceled.
type WorkItem func(ctx context.Context) error

// errWorkgroupFinished is the cancellation cause used to release the group context once all work is done.
var errWorkgroupFinished = errors.New("workgroup: all work items have finished")

// Workgroup is a group of goroutines working on a stream of [WorkItem]s with a limited concurrency:
// no more than a fixed number of work items are executed at any given time.
//
// A group is created with one of the two error handling policies:
//   - fail-fast, see [NewWorkgroup]: the first work item to return an error cancels the group;
//   - collect-all, see [NewCollectingWorkgroup]: all work items are executed and all errors are reported.
//
// A zero value of Workgroup is not usable, use one of the constructors.
type Workgroup struct {
	wg   sync.WaitGroup
	stop sync.Once

	// ctx is the group context, given to each work item and canceled when the group is done.
	ctx    context.Context
	cancel context.CancelCauseFunc

	// failFast selects the error handling policy: cancel the group on the first error if set.
	failFast   bool
	workstream chan WorkItem

	mu   sync.Mutex
	errs []error
}

// NewWorkgroup creates a new fail-fast [Workgroup] that executes no more than nWorkers work items concurrently.
// The first work item to return a non-nil error cancels the group context:
// work items already in-flight are expected to observe that cancellation and to return,
// work items still queued are dropped and never executed, and [Workgroup.Go] stops accepting new work.
// [Workgroup.Wait] reports that first error.
//
// The group context is derived from ctx, thus cancellation of ctx also cancels the group.
// nWorkers is capped at the minimum of 1: a non-positive value results in a group of a single worker.
func NewWorkgroup(ctx context.Context, nWorkers int) *Workgroup {
	return newWorkgroup(ctx, nWorkers, true)
}

// NewCollectingWorkgroup creates a new [Workgroup] that executes no more than nWorkers work items concurrently
// and runs every work item submitted, regardless of errors returned by others.
// [Workgroup.Wait] reports all collected errors joined with [errors.Join].
//
// The group context is derived from ctx, thus cancellation of ctx cancels the group:
// work items still queued when that happens are dropped and never executed.
// nWorkers is capped at the minimum of 1: a non-positive value results in a group of a single worker.
func NewCollectingWorkgroup(ctx context.Context, nWorkers int) *Workgroup {
	return newWorkgroup(ctx, nWorkers, false)
}

func newWorkgroup(ctx context.Context, nWorkers int, failFast bool) *Workgroup {
	if nWorkers < 1 {
		nWorkers = 1
	}

	groupCtx, cancel := context.WithCancelCause(ctx)
	result := &Workgroup{
		ctx:        groupCtx,
		cancel:     cancel,
		failFast:   failFast,
		workstream: make(chan WorkItem, nWorkers),
	}

	result.wg.Add(nWorkers)
	for range nWorkers {
		go result.run()
	}

	return result
}

// Context returns the group context: the same context that is passed to each [WorkItem].
// It is canceled when the group is canceled and when [Workgroup.Wait] returns.
func (w *Workgroup) Context() context.Context {
	return w.ctx
}

// Go schedules a work item for execution by the group.
// It blocks while all workers are busy and the queue of pending work items is full.
//
// It returns a non-nil error if the group has been canceled, in which case the work item is not scheduled
// and will never be executed. The error returned is the cause of the cancellation:
// the first error reported by a work item of a fail-fast group, or the cause of the parent context cancellation.
//
// Go must not be called after [Workgroup.Wait].
func (w *Workgroup) Go(work WorkItem) error {
	select {
	case <-w.ctx.Done():
		return context.Cause(w.ctx)
	case w.workstream <- work:
		return nil
	}
}

// Wait stops the group from accepting new work items, waits for all scheduled work items to finish
// and returns the error accumulated according to the group error handling policy:
// the first error reported for a fail-fast group, all errors joined with [errors.Join] for a collecting one.
// If no work item failed but the group has been canceled by its parent context,
// the cause of that cancellation is returned.
//
// Wait cancels the group context before returning, thus it must be called to release resources
// held by the group, even when errors are of no interest.
// It is safe to call Wait more than once: subsequent calls return the same error.
func (w *Workgroup) Wait() error {
	w.stop.Do(func() {
		close(w.workstream)
	})
	w.wg.Wait()

	err := w.result()
	w.cancel(errWorkgroupFinished)

	return err
}

func (w *Workgroup) run() {
	defer w.wg.Done()

	for work := range w.workstream {
		// Note: the queue is drained even when the group is canceled, so that a producer
		// blocked in [Workgroup.Go] is never left waiting for a worker that is not coming back.
		if w.ctx.Err() != nil {
			continue
		}

		if err := work(w.ctx); err != nil {
			w.collect(err)
		}
	}
}

func (w *Workgroup) collect(err error) {
	w.mu.Lock()
	w.errs = append(w.errs, err)
	w.mu.Unlock()

	if w.failFast {
		w.cancel(err)
	}
}

func (w *Workgroup) result() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.errs) == 0 {
		// No work item failed, but the group might have been canceled by the caller of Wait or by the parent context.
		if cause := context.Cause(w.ctx); !errors.Is(cause, errWorkgroupFinished) {
			return cause
		}

		return nil
	}

	if w.failFast {
		return w.errs[0]
	}

	return errors.Join(w.errs...)
}
