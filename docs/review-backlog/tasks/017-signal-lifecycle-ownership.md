# 017: Own Signal Registration and Cleanup Explicitly

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Lifecycle |
| Depends on | — |
| Likely conflicts | 015, 016 |
| Owner | Unclaimed |

## Why This Matters

Grace registers process signals into package-global state, never unregisters
them, and closes shared channels in ways that can panic on repeated or
concurrent construction. Tests and embedded applications cannot own or clean up
the subscription, and hard-coded process exit behavior prevents graceful
composition.

Signal handling is a process-wide resource. Its owner and cleanup must be
explicit.

## Architecture Assessment

Introduce a lifecycle Module that returns a context/events channel and an
idempotent `Stop`/cleanup function. Prefer `signal.NotifyContext` for first-signal
cancellation. If second-signal forced exit remains, isolate it behind a small
injectable process-exit Adapter and keep registration local to one lifecycle
instance.

The two real Adapters are the operating-system signal source and an injected
test source. Global convenience functions may delegate to a documented default
owner, but must not hide irreversible state.

## Evidence

- `pkg/grace/signals.go` stores signal state in package globals.
- Channels are closed from lifecycle paths that can be constructed/called more
  than once, making double close possible.
- `signal.Notify` registrations are not paired with `signal.Stop`.
- Forced shutdown calls process exit directly.
- The signal paths have no focused tests, despite global/concurrent behavior.

## Failure Sequence

1. Two tests or components construct the signal handler in one process.
2. Both reference the same package-global channel/state.
3. Cleanup or signal handling closes the shared channel twice.
4. The process panics, or later tests receive stale signals because the
   registration was never stopped.

## Required Outcome

- Each lifecycle instance owns its signal registration and cleanup.
- Cleanup is idempotent and calls the appropriate signal unregister/stop
  operation.
- First-signal cancellation has an inspectable cause.
- Optional second-signal forced exit is explicit, documented, and injectable in
  tests.
- Multiple sequential instances do not leak state or signals.
- Concurrent construction either works independently or returns a clear
  ownership error; it never races or panics.
- Package APIs do not require callers to close channels they do not own.

## Implementation Options and Trade-offs

### Preferred: `signal.NotifyContext` Lifecycle

Return a derived context plus an idempotent stop function. Add a separate
optional second-signal watcher configured with an `ExitFunc` Adapter. This uses
standard-library lifecycle semantics and makes cleanup obvious.

### Alternative: Owned Signal Channel Struct

Wrap `signal.Notify`, a private channel, context cancellation, and `sync.Once`
cleanup in a struct. This gives full second-signal control but requires more
custom synchronization.

## Implementation Constraints

- Never close a channel owned by `os/signal` or a caller.
- Do not invoke process exit while holding locks or in unit tests.
- Stop all signal notification goroutines on cleanup.
- Preserve the original signal as cancellation cause where Go APIs permit.
- Coordinate final process classification with task 016.

## Suggested Implementation Sequence

1. Add sequential/concurrent construction and idempotent cleanup tests.
2. Introduce an owned lifecycle using an injected signal source.
3. Implement the operating-system Adapter with `NotifyContext` or paired
   `Notify`/`Stop`.
4. Isolate optional forced exit behind configuration.
5. Deprecate unsafe global helpers.

## Non-Goals

- Application shutdown timeouts.
- Ordering application-specific cleanup functions.
- Windows service-control integration beyond supported Go signals.

## Acceptance Criteria / Definition of Done

- [ ] Signal registrations are always stoppable and stopped.
- [ ] Cleanup is idempotent.
- [ ] Sequential and allowed concurrent instances do not share mutable state.
- [ ] First-signal cause is observable.
- [ ] Forced exit is optional and injectable.
- [ ] Race/subprocess tests cannot panic or leak signal handlers.

## Required Tests

- First signal cancellation and cause.
- Second signal behavior with a fake exit function.
- Stop before/after signal and repeated stop.
- Sequential and concurrent instances.
- No events after cleanup.
- Subprocess integration for real signal delivery without killing the test
  runner.

## Validation

```sh
go test -race -count=100 ./pkg/grace
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Compatibility/migration:**
- **Validation evidence:**
- **Follow-ups:**

