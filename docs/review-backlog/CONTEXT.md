# Wyrd Repository Review Backlog Context

This document is shared context for the agent-ready tasks in [`tasks/`](tasks/).
Read it before claiming a task. Each task repeats its local requirements, while
this document records the intent, vocabulary, and cross-cutting invariants that
must remain consistent across workstreams.

## Review Baseline

The backlog was created from a read-only review of Wyrd at commit
`c54dc1066d8f38db49d5d0d25db143685acb3d6e` (`v0.2.2`, `main`) on
2026-07-27. There were no project ADRs or pre-existing `CONTEXT.md`; intent was
reconstructed from the root and package READMEs, exported interfaces, tests,
history, and current downstream usage.

The adjacent Urth checkout at commit
`3e46d6d79d87af73a6b4c646704af90b9cc00fb2` was inspected as a concrete
consumer of `v0.2.2`. The adjacent Comserv checkout was also inspected because
it consumes the webhook, transaction, REST, and process-lifecycle interfaces.
Downstream paths cited by tasks are supporting evidence, not files to modify as
part of this backlog.

Line numbers in tasks refer to the Wyrd baseline above. They are evidence
anchors, not authority: revalidate them after earlier tasks merge.

## Product Intent

Wyrd is a reusable Go toolkit for programs that manage Kubernetes-like
resources. Its packages are intended to be independently useful and to compose:

1. `manifest` defines resource kinds, metadata, typed specs and statuses,
   labels, selectors, wire representations, and search values.
2. `bark` maps those resource concepts onto Gin HTTP handlers, including
   request decoding, content negotiation, pagination, authentication-token
   extraction, and response writing.
3. `dbstore` persists resources through GORM, applies the same selectors in
   SQL, manages associations and transactions, and advertises SQLite, Postgres,
   and MySQL support.
4. `webhooks` describes webhook resources and delivers change notifications
   over HTTP.
5. `grace` turns process cancellation into orderly shutdown and runs bounded
   concurrent work.

The central promise is semantic composition: a selector that matches a resource
in memory must select the same resource in SQL; metadata sent over HTTP must
survive manifest encoding; a version precondition must protect the exact row
named by the caller; and cancellation must not be mistaken for success.

## Domain Language

- **Resource**: one managed object with type metadata, object metadata, a typed
  spec, an optional typed status, and optional semantic links.
- **Kind**: the serialized discriminator that maps a Resource to its spec and
  status types.
- **Kind Registry**: the mapping used to construct typed manifests for known
  Kinds while allowing unknown Kinds to be preserved for round trips.
- **Object Metadata**: UID, resource version, name, labels, timestamps, and
  deletion state associated with a Resource.
- **Resource Version**: the monotonic value used as an optimistic write
  precondition. It describes stored state, not a client-controlled field.
- **Selector**: a conjunction of label Requirements evaluated identically by
  every in-memory and persistence adapter.
- **Search Query**: resource-name, time-range, selector, ordering, and
  pagination intent. A field must not silently change meaning between methods.
- **Store**: the persistence interface for Resource reads and mutations.
- **Store Transaction**: a single commit-or-rollback unit over Store mutations.
- **Mutation Outcome**: an unambiguous result such as created, updated, not
  found, or version conflict. A boolean whose meaning depends on GORM row-count
  behavior is not an adequate outcome.
- **Page**: one bounded, deterministically ordered slice of a result set.
- **Workgroup**: a bounded-concurrency executor with fail-fast or collect-all
  error policy and an explicit submission/termination lifecycle.
- **Webhook Delivery**: one bounded HTTP attempt to deliver a stable event
  payload to one webhook target.

## Architecture Language

Use these terms consistently in backlog work:

- **Module**: anything with an interface and an implementation.
- **Interface**: everything a caller must know, including types, invariants,
  ordering, errors, configuration, and performance characteristics.
- **Implementation**: behavior hidden inside a Module.
- **Depth**: leverage provided through an Interface. A deep Module hides
  substantial behavior behind a small, coherent Interface.
- **Seam**: a place where behavior can be changed without editing the caller.
- **Adapter**: a concrete implementation at a Seam.
- **Leverage**: capability callers receive from a deep Module.
- **Locality**: concentration of behavior, failure handling, and tests in one
  place.

Apply the deletion test before adding or retaining an abstraction. If deleting
a Module makes complexity disappear, it was probably a shallow pass-through. If
the complexity reappears across callers, the Module is earning its Interface.
The Interface is also the test surface. A Seam with one Adapter is hypothetical;
two Adapters make it real.

## Required Invariants

### Resource authority and mutation

- An explicit Resource ID names the only row a mutation may change.
- A non-zero ID embedded in a value must either equal the explicit target or be
  rejected before SQL executes.
- A Resource Version precondition is checked atomically by the write, not by a
  preceding read.
- A full Resource replacement persists zero values. Partial update behavior uses
  a separately named and documented Interface.
- Created, updated, missing, unchanged, and version-conflict outcomes do not
  depend on dialect-specific `RowsAffected` or implicit GORM upsert behavior.
- UID, Resource Version, timestamps, and deletion state are server-owned.
  Client-supplied values are rejected or stripped by one documented policy.
- Active Resource names remain unique on every advertised database. Restore
  collisions are explicit outcomes, never silent duplication.

### Selectors and queries

- `Selector.Matches` is the executable semantic reference for SQL adapters.
- Every operator has validated key and value cardinality before evaluation or
  SQL compilation.
- Missing keys, empty values, negative operators, numeric comparison, and
  malformed numeric label values behave identically in memory, SQLite,
  Postgres, and MySQL.
- An unsupported dialect or untranslatable selector fails closed with a typed
  error; it never emits an empty predicate or silently broadens a query.
- Search Query fields keep one meaning. Unsupported combinations are rejected,
  not ignored or reinterpreted.
- Pages are bounded and deterministically ordered with a unique tie-breaker.
- Cancellation and partial iteration are observable; they are not reported as
  successful completion.

### Manifest and HTTP behavior

- Known Kinds decode to their registered spec/status types. Unknown Kinds retain
  lossless data for round trips.
- Registry infrastructure failures are not flattened into “unknown Kind.”
- JSON and YAML enforce the same known-field and shape policy. Any advertised
  XML behavior is either contract-tested or removed from the supported surface.
- Resource links and error fields survive every supported representation with
  stable field names.
- Malformed external input returns a bounded client error and never panics.
- HTTP status, public message, internal cause, and retryability remain distinct.
  Internal database or network details are not exposed by default.
- `Accept` negotiation follows media-range and quality semantics. Request
  `Content-Type` validation is a separate concern.
- Pagination links use the known total and never advertise a page known not to
  exist.

### Concurrency and process lifecycle

- Workgroup submission and termination have one linearization point. Concurrent
  `Go`, cancellation, and `Wait` calls cannot panic or accept work that is then
  silently dropped.
- Fail-fast cancellation, rejected submissions, `Context`, and `Wait` expose
  the same canonical first failure.
- Mixed error trees containing cancellation and a real failure preserve the
  real failure.
- Signal registration has explicit ownership and cleanup. Repeated or concurrent
  construction does not cause an undocumented panic.
- Libraries return errors and cancellation causes where practical; process exit
  policy remains at executable composition.

### Webhook delivery

- Target URLs and HTTP clients are validated before an attempt.
- Every attempt is bounded by a context or configured timeout.
- All 2xx statuses are successful; failure results retain status,
  retryability, `Retry-After`, and a size-limited diagnostic body.
- Response bodies are closed and drained sufficiently for connection reuse.
- Automatic retry requires a stable delivery identity and documented receiver
  deduplication. Durable at-least-once delivery belongs to an outbox/dispatcher
  Module, not an uncoordinated loop inside the HTTP adapter.

### Package composition

- `manifest` should remain usable without importing persistence implementation
  details.
- A consumer selecting one SQL dialect should not have to compile every driver.
- `webhooks` should not import Gin transitively merely to set standard HTTP
  headers.
- New Seams require real variation. Prefer internal dialect Adapters and
  consumer-owned narrow interfaces over broad speculative public interfaces.

## Known Downstream Constraints

- Urth passes an explicit ID and `WithVersion` to status-transition writes; those
  transitions require atomic compare-and-swap behavior.
- Urth currently works around `dbstore.Update` skipping zero values by using
  `CreateOrUpdate` for full Resource edits. That workaround narrows optimistic
  concurrency to a read-time check and must be removed when the mutation
  Interface is repaired.
- Urth uses both top-level Bark helpers and the contextual response wrapper, so
  a migration needs compatibility wrappers and contract tests.
- Comserv uses manual Store transactions and calls webhooks from both foreground
  and notification paths. Transaction and webhook changes need explicit
  migration notes.
- Urth and Comserv use `NewSignalHandlingContext`, `FatalOnError`, and
  `SuccessRequired` in executables. Keep a staged deprecation path even though
  Wyrd is pre-1.0.

## Agent Workflow

1. Choose a `ready` task whose dependencies are `done`.
2. Read the full task, this context, and any task completion records it depends
   on.
3. Revalidate evidence against the current branch and inspect downstream
   call sites named by the task.
4. Check `git status --short` and the task index for likely conflicts.
5. Mark the task `in-progress` and record an owner/branch before editing.
6. Add a failing test through the affected Interface before changing its
   Implementation.
7. Keep changes within Required Outcome and Non-Goals.
8. Fill the completion record, mark the task `done`, and update the index.

When a task makes an enduring compatibility or product decision not settled
here, record an ADR rather than burying it in implementation comments.

## Validation Baseline

The review ran with Go 1.26.5:

```text
go mod verify                         pass
go test -race -count=1 ./...         flaky: passed once, then failed
go vet ./...                          pass
git diff --check                      pass
```

The repeated race-suite failure is
`TestStringSet_Join/some-set`: `StringSet.Join` preserves randomized map
iteration while the test expects insertion order. A focused
`go test -count=100 ./pkg/manifest -run '^TestStringSet_Join$'` reproduces
multiple output orders. Task 011 owns the value contract; task 020 owns the
flakiness gate.

Statement coverage from `go test -count=1 -cover ./...`:

```text
pkg/bark       19.4%
pkg/dbstore    63.1%
pkg/grace      67.8%
pkg/manifest   67.0%
pkg/webhooks    0.0%
```

The locally installed `staticcheck` could not analyze the module because that
binary was built with Go 1.25.3 while the module requires Go 1.26.5. The
repository's `make staticcheck` downloads `@latest`, so the checked tool is not
reproducibly pinned. Task 020 owns the quality-gate contract.
