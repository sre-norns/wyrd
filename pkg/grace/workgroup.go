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

// ErrWorkgroupClosed identifies a [Workgroup.Go] call rejected because
// [Workgroup.Wait] stopped the group from accepting work. Check it with
// [errors.Is].
var ErrWorkgroupClosed = errors.New("workgroup: no longer accepting work")

// ErrNilWorkItem is returned when [Workgroup.Go] is called with a nil work item.
var ErrNilWorkItem = errors.New("workgroup: nil work item")

// detachedWorkgroupParent preserves a parent's values and deadline while making
// cancellation propagation explicit. This lets the Workgroup expose one stable
// cause even when a real worker failure is discovered after parent cancellation.
type detachedWorkgroupParent struct {
	context.Context
}

func (detachedWorkgroupParent) Done() <-chan struct{} {
	return nil
}

func (detachedWorkgroupParent) Err() error {
	return nil
}

// workgroupCause is a stable error tree shared by Go, Context, and Wait.
// It may gain a real failure after cancellation, but its identity never changes.
type workgroupCause struct {
	mu     sync.RWMutex
	causes []error
	real   bool
}

func (c *workgroupCause) Error() string {
	causes := c.snapshot()
	if len(causes) == 0 {
		return ErrWorkgroupClosed.Error()
	}

	return errors.Join(causes...).Error()
}

func (c *workgroupCause) Unwrap() []error {
	causes := c.snapshot()
	if len(causes) == 0 {
		return []error{ErrWorkgroupClosed}
	}

	return causes
}

func (c *workgroupCause) snapshot() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]error(nil), c.causes...)
}

func (c *workgroupCause) hasCause() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.causes) != 0
}

// addFailFast selects the first non-cancellation error. A cancellation-only
// cause remains in the tree so callers can still recognize shutdown.
func (c *workgroupCause) addFailFast(err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	real := !isCancellationOnly(err)
	if len(c.causes) == 0 {
		c.causes = append(c.causes, err)
		c.real = real
		return true
	}
	if c.real || !real {
		return false
	}

	c.causes = append([]error{err}, c.causes...)
	c.real = true
	return false
}

func (c *workgroupCause) addCollecting(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	real := !isCancellationOnly(err)
	if real && !c.real {
		c.causes = append([]error{err}, c.causes...)
	} else {
		c.causes = append(c.causes, err)
	}
	c.real = c.real || real
}

func isCancellationOnly(err error) bool {
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		errs := wrapped.Unwrap()
		if len(errs) == 0 {
			return false
		}
		for _, nested := range errs {
			if !isCancellationOnly(nested) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return isCancellationOnly(wrapped.Unwrap())
	default:
		return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}
}

// Workgroup is a group of goroutines working on a stream of [WorkItem]s with a limited concurrency:
// no more than a fixed number of work items are executed at any given time.
//
// A group is created with one of the two error handling policies:
//   - fail-fast, see [NewWorkgroup]: the first non-cancellation work item error is the primary cause;
//   - collect-all, see [NewCollectingWorkgroup]: all work items are executed and all errors are reported.
//
// Submission and closure are linearized: a call to [Workgroup.Go] either
// returns nil and its work item is invoked exactly once, or returns an error and
// the work item is not invoked. Calling [Workgroup.Go] concurrently with
// [Workgroup.Wait] is safe. Workgroup does not recover panics from work items.
//
// A zero value of Workgroup is not usable, use one of the constructors.
type Workgroup struct {
	wg     sync.WaitGroup
	stop   sync.Once
	finish sync.Once
	done   chan struct{}

	acceptMu sync.Mutex
	closed   bool

	// ctx is the group context, given to each work item and canceled when the group is done.
	ctx    context.Context
	cancel context.CancelCauseFunc
	parent context.Context

	stopParent func() bool
	parentOnce sync.Once
	parentDone chan struct{}

	// failFast selects the error handling policy: cancel the group on the first observed cause if set.
	failFast   bool
	workstream chan WorkItem

	cause   *workgroupCause
	waitErr error
}

// NewWorkgroup creates a new fail-fast [Workgroup] that executes no more than nWorkers work items concurrently.
// The first work item to return a non-nil error cancels the group context:
// work items already in-flight are expected to observe that cancellation and to return,
// work items already accepted are still invoked with the canceled group context,
// and [Workgroup.Go] stops accepting new work.
//
// If cancellation is observed before a real work item failure, the first later
// real failure from accepted work becomes the primary error while cancellation
// remains recognizable with [errors.Is]. [Workgroup.Go], [Workgroup.Context],
// and [Workgroup.Wait] expose the same stable causal error.
//
// The group context preserves the values and deadline of ctx, and cancellation
// of ctx cancels the group. A nil ctx is treated as [context.Background].
// nWorkers is capped at the minimum of 1: a non-positive value results in a group of a single worker.
func NewWorkgroup(ctx context.Context, nWorkers int) *Workgroup {
	return newWorkgroup(ctx, nWorkers, true)
}

// NewCollectingWorkgroup creates a new [Workgroup] that executes no more than nWorkers work items concurrently
// and runs every work item submitted, regardless of errors returned by others.
// [Workgroup.Wait] reports a multi-error containing all collected errors.
//
// Cancellation of ctx cancels the group and prevents new submissions, but every
// already accepted work item is still invoked with the canceled group context.
// The group context preserves the values and deadline of ctx. A nil ctx is
// treated as [context.Background].
// nWorkers is capped at the minimum of 1: a non-positive value results in a group of a single worker.
func NewCollectingWorkgroup(ctx context.Context, nWorkers int) *Workgroup {
	return newWorkgroup(ctx, nWorkers, false)
}

func newWorkgroup(ctx context.Context, nWorkers int, failFast bool) *Workgroup {
	if ctx == nil {
		ctx = context.Background()
	}
	if nWorkers < 1 {
		nWorkers = 1
	}

	groupCtx, cancel := context.WithCancelCause(detachedWorkgroupParent{Context: ctx})
	result := &Workgroup{
		ctx:        groupCtx,
		cancel:     cancel,
		parent:     ctx,
		failFast:   failFast,
		workstream: make(chan WorkItem, nWorkers),
		cause:      &workgroupCause{},
		done:       make(chan struct{}),
		parentDone: make(chan struct{}),
	}
	result.stopParent = context.AfterFunc(ctx, result.forwardParentCancellation)
	if context.Cause(ctx) != nil {
		result.forwardParentCancellation()
	}

	result.wg.Add(nWorkers)
	for range nWorkers {
		go result.run()
	}

	return result
}

// Context returns the group context: the same context that is passed to each [WorkItem].
// It is canceled when the group is canceled and when [Workgroup.Wait] returns.
// After a failed Wait, [context.Cause] returns the exact error returned by Wait.
// After a successful Wait, the cause matches [ErrWorkgroupClosed].
func (w *Workgroup) Context() context.Context {
	return w.ctx
}

// Go schedules a work item for execution by the group.
// It blocks while all workers are busy and the queue of pending work items is full.
//
// A nil work item is rejected with [ErrNilWorkItem]. Once cancellation or Wait
// closes submission, Go returns the group's shared terminal cause and the work
// item is not invoked. If the group completes successfully, that cause matches
// [ErrWorkgroupClosed].
//
// Go may be called concurrently with Wait.
func (w *Workgroup) Go(work WorkItem) error {
	w.acceptMu.Lock()
	defer w.acceptMu.Unlock()

	if cause := context.Cause(w.ctx); cause != nil {
		return cause
	}
	if w.closed {
		return w.cause
	}
	if context.Cause(w.parent) != nil {
		w.forwardParentCancellation()
		return context.Cause(w.ctx)
	}
	if work == nil {
		return ErrNilWorkItem
	}

	select {
	case <-w.ctx.Done():
		return context.Cause(w.ctx)
	case w.workstream <- work:
		return nil
	}
}

// Wait stops the group from accepting new work items, waits for all scheduled work items to finish
// and returns the error accumulated according to the group error handling policy:
// the first non-cancellation error for a fail-fast group, or a multi-error
// containing every error for a collecting one.
// If no work item failed but the group has been canceled by its parent context,
// the cause of that cancellation is returned.
//
// Wait cancels the group context before returning, thus it must be called to release resources
// held by the group, even when errors are of no interest.
// It is safe to call Wait concurrently or more than once: every call waits for
// all accepted work and returns the same error value.
func (w *Workgroup) Wait() error {
	w.stop.Do(func() {
		w.acceptMu.Lock()
		defer w.acceptMu.Unlock()

		w.closed = true
		close(w.workstream)
	})
	w.wg.Wait()

	w.finish.Do(func() {
		w.stopWatchingParent()
		if w.cause.hasCause() {
			w.waitErr = w.cause
		}
		w.cancel(w.cause)
		close(w.done)
	})
	<-w.done

	return w.waitErr
}

func (w *Workgroup) run() {
	defer w.wg.Done()

	for work := range w.workstream {
		if context.Cause(w.parent) != nil {
			w.forwardParentCancellation()
		}
		if err := work(w.ctx); err != nil {
			w.collect(err)
		}
	}
}

func (w *Workgroup) collect(err error) {
	if w.failFast {
		if w.cause.addFailFast(err) {
			w.cancel(w.cause)
		}
		return
	}

	w.cause.addCollecting(err)
}

func (w *Workgroup) forwardParentCancellation() {
	w.parentOnce.Do(func() {
		w.recordParentCause()
		close(w.parentDone)
	})
}

func (w *Workgroup) stopWatchingParent() {
	if w.stopParent() {
		w.parentOnce.Do(func() {
			w.recordParentCause()
			close(w.parentDone)
		})
	}

	<-w.parentDone
}

func (w *Workgroup) recordParentCause() {
	cause := context.Cause(w.parent)
	if cause == nil {
		return
	}

	if w.failFast {
		w.cause.addFailFast(cause)
	} else {
		w.cause.addCollecting(cause)
	}
	w.cancel(w.cause)
}
