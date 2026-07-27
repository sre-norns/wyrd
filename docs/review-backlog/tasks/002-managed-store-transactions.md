# 002: Execute Store Transactions Through One Safe Lifecycle

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Mutations |
| Depends on | — |
| Likely conflicts | 001, 006 |
| Owner | Unclaimed |

## Why This Matters

Every transaction caller must currently remember the same begin/defer
rollback/execute/commit sequence and invent error precedence. The one helper
intended to protect panics cannot be called by external users because it accepts
an unexported concrete type. If it were used internally, it would recover the
panic, roll back, and silently swallow the panic.

That Interface makes transaction leaks, missed rollbacks, hidden panics, and
lost rollback errors normal caller mistakes.

## Architecture Assessment

`Begin` exposes the entire transaction state machine rather than hiding it.
`RollbackOnPanic` is a shallow and unusable fragment of that state machine. Apply
the deletion test: removing it loses no useful behavior for external callers.

Create one deep transaction executor whose small Interface owns begin, callback,
commit, rollback, cancellation, panic, and error precedence. The raw transaction
remains an escape hatch, but ordinary callers gain leverage and tests exercise
the same seam they use.

## Evidence

- `pkg/dbstore/store.go:206-225`: callers own commit/rollback and `Rollback`
  cannot report an error.
- `pkg/dbstore/dbstore.go:212-221`: `Begin` returns a raw StoreTransaction.
- `pkg/dbstore/dbtransaction.go:17-24`: rollback error is discarded; commit
  mutates the internal GORM handle.
- `pkg/dbstore/dbtransaction.go:103-109`: `RollbackOnPanic` accepts the
  unexported `*gormStoreTransaction`, recovers, and does not re-panic.
- Downstream Comserv `../comserv/pkg/comserv/service.go:342-397` repeats the manual
  transaction sequence around association writes.

## Failure Sequence

1. A transaction performs one or more successful writes.
2. The caller returns an error or panics before its hand-written cleanup is
   correct.
3. The transaction leaks or commits/rolls back ambiguously. If the current panic
   helper is used internally, the panic disappears and execution resumes with
   zero return values.

## Required Outcome

- A managed transaction Interface begins a transaction, invokes one callback,
  commits exactly once only when the callback succeeds, and otherwise rolls back.
- Context cancellation before commit is a rollback outcome.
- A panic triggers rollback and re-panics with the original value and stack
  semantics intact.
- Begin, callback, commit, and rollback failures follow one documented
  precedence/join policy.
- Terminal calls are idempotent or return a stable typed state error.
- Callers can still access raw transactions for exceptional workflows, with
  explicit documentation.

## Implementation Options and Trade-offs

### Preferred: Callback Transaction Module

Add `WithTransaction(ctx, func(StoreTransaction) error) error` (name illustrative)
on DBStore or a narrow transaction executor. Keep lifecycle implementation
private, join a rollback error to the triggering error without replacing it, and
re-panic after best-effort rollback.

This produces locality and makes failure injection straightforward through an
internal transaction Adapter.

### Alternative: Repair the Raw Transaction Interface

Make `Rollback` return an error and replace `RollbackOnPanic` with a helper that
accepts the exported `Transaction`, rolls back, and re-panics. This makes the
current pattern possible but leaves lifecycle duplication in every caller and
provides less leverage.

## Implementation Constraints

- Do not retry transactions implicitly; retries require idempotency knowledge
  owned by callers.
- Preserve the original callback error and panic as primary.
- Never commit after context cancellation or callback failure.
- Managed transactions must expose the Store operations actually needed inside
  the callback; do not force type assertions to the GORM implementation.

## Suggested Implementation Sequence

1. Add an internal fake transaction Adapter that can fail every lifecycle step.
2. Specify the error/panic precedence table in tests and docs.
3. Implement the callback executor.
4. Migrate one existing downstream manual transaction as a compatibility proof.
5. Deprecate or delete the inaccessible panic helper.

## Non-Goals

- Nested transactions or savepoints.
- Distributed transactions.
- Automatic serialization/deadlock retries.

## Acceptance Criteria / Definition of Done

- [ ] Callback success commits and callback failure rolls back.
- [ ] Cancellation rolls back and is returned.
- [ ] Panic identity is preserved after rollback.
- [ ] Rollback and commit failures remain observable.
- [ ] No external caller needs the unexported transaction type.
- [ ] Raw and managed transaction ownership is documented.

## Required Tests

- Committed data is visible; callback-error and panic data are absent.
- Context canceled before callback and during callback.
- Begin, commit, and rollback failure injection.
- Panic plus rollback failure preserves both, with the panic still primary.
- Concurrent or repeated terminal calls have stable behavior.

## Validation

```sh
go test -race -count=1 ./pkg/dbstore
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
