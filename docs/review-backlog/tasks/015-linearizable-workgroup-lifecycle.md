# 015: Make Workgroup Lifecycle Linearizable

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Lifecycle |
| Depends on | — |
| Likely conflicts | 016, 017 |
| Owner | Codex |

## Why This Matters

`Workgroup.Go` can send on a channel that `Wait` concurrently closes. This is a
race between public methods and can panic a production process even when every
worker function behaves correctly. Cancellation and fail-fast error selection
are also nondeterministic: submission can appear successful after cancellation,
and the error returned by `Wait` can disagree with the error reported by `Go`.

A concurrency primitive must make its accepted/rejected work and terminal cause
unambiguous under every interleaving.

## Architecture Assessment

The Workgroup needs one lifecycle state machine and one linearization point for
submission versus closure. Channel ownership, wait-group counters, cancellation,
and first-error policy currently live in separate mechanisms. Consolidate them
behind a deep Module whose Interface says exactly when work is accepted and
which cause wins.

Do not close a submission channel from a method that races with senders unless
closure is serialized with every send. A mutex/condition or dispatcher-owned
channel are viable internal implementations; callers should see only the
lifecycle contract.

## Evidence

- `pkg/grace/workgroup.go:100-106`: `Go` selects and sends work to the internal
  channel.
- `pkg/grace/workgroup.go:118-126`: `Wait` closes that same channel after its
  own lifecycle checks.
- A concurrent send and close is not made mutually exclusive and can panic.
- When both context cancellation and channel send are selectable, `Go` may
  return nil even though the worker later discards the item.
- `pkg/grace/workgroup.go:146-153`: error collection and cancellation occur
  across separate critical sections/events, so competing failures can disagree
  about the first cause.
- Current tests do not stress concurrent `Go` and `Wait` under the race detector.
- A nil parent context reaches `context.WithCancelCause` and a nil accepted
  `WorkItem` is invoked as a function; both panic instead of failing at the
  constructor/submission seam.

## Failure Sequence

1. A producer calls `Go` while another goroutine calls `Wait`.
2. `Go` passes its preliminary state check.
3. `Wait` closes the work channel.
4. `Go` selects the send case and panics with `send on closed channel`.

## Required Outcome

- Concurrent `Go`, cancellation, and `Wait` are linearizable and cannot panic.
- Every `Go` call has one observable outcome: accepted for execution or rejected
  with the terminal cause.
- Accepted work is not silently discarded after `Go` returned nil. If queued
  cancellation is a supported outcome, it must be represented explicitly in
  the terminal result instead.
- `Wait` prevents later acceptance and waits for every previously accepted job.
- A documented deterministic policy selects the terminal error, preferably the
  first non-cancellation worker failure.
- `Go`, context state, and `Wait` report the same causal terminal error.
- Multiple/concurrent `Wait` calls have defined behavior.
- Nil functions and invalid configuration fail safely.
- Nil parent contexts follow one documented policy rather than panicking.

## Implementation Options and Trade-offs

### Preferred: Mutex-Protected Lifecycle with Direct Launch

Track open/closing/closed state, accepted-job count, and first cause under one
mutex. `Go` linearizes acceptance and increments before launch; `Wait`
linearizes closure then waits. This avoids a closeable submission channel and
keeps the state proof local.

### Alternative: Single Dispatcher Owns the Work Channel

All commands, closure, and cause selection pass through one dispatcher
goroutine. Ownership is very clear, but command submission still needs careful
context/dispatcher shutdown semantics and adds a goroutine to every Workgroup.

## Implementation Constraints

- Never call user work while holding the lifecycle mutex.
- Establish accepted-job accounting before launching a goroutine.
- Do not turn a real failure into `context.Canceled`.
- Do not rely on scheduler selection order for error precedence.
- Panic recovery for user work is a separate policy and must not be added
  implicitly.

## Suggested Implementation Sequence

1. Write a repeated stress regression for concurrent `Go`/`Wait`/cancel.
2. State the lifecycle and first-cause contract in package documentation.
3. Implement one linearized state machine.
4. Make all public methods read the same stored terminal cause.
5. Run race and high-count stress tests.

## Non-Goals

- Distributed work queues.
- Automatic retries.
- Implicit recovery from panics in caller functions.

## Acceptance Criteria / Definition of Done

- [x] Stress/race tests cannot produce send-on-closed or wait-group misuse.
- [x] Accepted work always finishes before `Wait` returns.
- [x] Rejected work returns the shared terminal cause.
- [x] First real failure selection is deterministic.
- [x] Cancellation-only behavior remains recognizable as cancellation.
- [x] Concurrent/repeated `Wait` behavior is documented and tested.

## Required Tests

- Thousands of concurrent `Go`, `Wait`, and cancel interleavings.
- Cancellation before submission, after acceptance, and during work.
- Two simultaneous worker failures with controlled ordering.
- Multiple concurrent waiters.
- Zero work, nil work, and invalid worker/queue configuration.
- Nil parent context.
- `go test -race -count=100 ./pkg/grace`.

## Validation

```sh
go test -race -count=100 ./pkg/grace
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:** Serialized submission with channel closure; guaranteed invocation
  of every accepted item; cached one result for all waiters; added a stable causal
  multi-error shared by `Go`, `Context`, and `Wait`; explicitly forwarded and
  cleaned up parent cancellation; rejected nil work safely; treated nil parents
  as `context.Background`.
- **Tests added/updated:** Added repeated concurrent `Go`/`Wait`/cancel stress,
  accepted-work accounting, post-`Wait` rejection, cancellation before/during/
  after acceptance, controlled failure precedence, mixed cancellation and real
  failure, concurrent waiters, nil work, nil parent, parent metadata, and
  negative worker-count coverage. Updated the original fail-fast assertion to
  require every accepted item to run.
- **Documentation updated:** Defined submission/closure linearization, accepted
  work, canonical cause precedence, concurrent/repeated `Wait`, successful
  closure, nil inputs, and invalid worker-count behavior in Go documentation and
  `pkg/grace/README.md`.
- **Compatibility/migration:** `Go` is now safe during/after `Wait`; after a
  successful close its error matches `ErrWorkgroupClosed`. `Go(nil)` returns
  `ErrNilWorkItem`. Already accepted queued work is now invoked with the canceled
  context instead of being silently dropped. Callers should use `errors.Is` when
  inspecting Workgroup results.
- **Validation evidence:** `go test -race -count=100 ./pkg/grace` passed
  (32.029s); `go test -race -count=1 ./...` passed; `go vet ./...` and
  `git diff --check` passed.
- **Follow-ups:** The first full-suite run reproduced the already-backlogged
  order-sensitive `TestStringSet_Join/some-set` failure (tasks 011/020); the
  immediate rerun passed. No new Workgroup follow-up remains.
