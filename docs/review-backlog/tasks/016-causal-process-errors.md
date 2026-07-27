# 016: Preserve Real Failures and Make Actionable Errors Causal

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Lifecycle |
| Depends on | — |
| Likely conflicts | 015, 017 |
| Owner | Unclaimed |

## Why This Matters

Grace's launch helpers suppress an entire joined error when any branch contains
`context.Canceled`. A real worker failure joined with cancellation can therefore
look like clean shutdown. Meanwhile the package's actionable error Interface
does not itself implement Go's `error` contract, and a constructor erases the
action method from its declared return type.

Both behaviors discard intent precisely where callers need it to choose logs,
exit codes, or remediation.

## Architecture Assessment

Separate causal error classification from process termination. A pure
classifier should decide whether an error tree contains a real failure versus
expected shutdown. Process helpers are thin Adapters that log/exit based on that
result.

Define one `ActionableError` Interface embedding `error` and exposing the
remediation. Concrete wrappers should implement `Unwrap`, keeping the original
cause available to standard tooling.

## Evidence

- `pkg/grace/launch.go:11-23`: `FatalOnError` and `SuccessRequired` suppress an
  error whenever `errors.Is(err, context.Canceled)` is true.
- `errors.Join(realFailure, context.Canceled)` satisfies that test, hiding the
  real failure.
- Grace's exported `Error` Interface exposes an action but does not embed the
  standard `error` Interface.
- `RaiseError` returns a narrower type that does not expose the action method at
  compile time, even though the concrete value implements it.
- Process helpers call logging/exit behavior directly, which is difficult to
  contract-test without subprocesses.

## Failure Sequence

1. A worker returns a database corruption error.
2. Fail-fast cancellation causes another worker to return
   `context.Canceled`.
3. The group joins both errors.
4. `FatalOnError` sees cancellation anywhere in the tree and reports success,
   hiding the corruption failure.

## Required Outcome

- Cancellation is considered clean only when no non-cancellation failure exists
  in the causal tree.
- Real failures remain available through wrapping/joining and drive non-success
  process outcomes.
- Error classification is pure and independently testable.
- `ActionableError` embeds `error`, returns stable actionable guidance, and
  supports `Unwrap`.
- Constructors return an Interface that retains both message/cause and action.
- Existing helpers remain as compatibility Adapters with documented exit
  behavior.
- Logging does not duplicate the same failure at multiple layers by default.

## Implementation Options and Trade-offs

### Preferred: Causal Classifier plus `ActionableError`

Walk the error tree, including joined errors, and classify cancellation-only,
deadline-only, and real-failure outcomes. Wrap causes in a concrete actionable
error returned as an Interface embedding `error`. Let launch helpers consume the
classification.

### Alternative: Require Callers to Pre-Classify

Remove cancellation logic from process helpers and require callers to pass nil
for expected shutdown. This is simple but repeats nuanced joined-error handling
in every application and weakens Grace's leverage.

## Implementation Constraints

- Handle both single `Unwrap() error` and joined `Unwrap() []error` trees.
- Preserve `errors.Is`/`errors.As` for original causes.
- Do not treat deadlines as success without an explicit policy.
- Keep `os.Exit`/fatal behavior at the outermost process Adapter.
- Coordinate first-cause semantics with task 015.

## Suggested Implementation Sequence

1. Add joined cancellation/real-failure regression tables.
2. Define pure classification outcomes.
3. Repair the actionable error Interface and constructors.
4. Delegate process helpers to classification.
5. Add subprocess tests only for final exit behavior.

## Non-Goals

- A general logging framework.
- Assigning HTTP statuses to errors.
- Retry policy.

## Acceptance Criteria / Definition of Done

- [ ] Joined real failures cannot be suppressed by cancellation.
- [ ] Cancellation-only shutdown remains identifiable.
- [ ] Actionable errors work as standard causal errors.
- [ ] Constructor return types expose actions.
- [ ] Classification has no process side effects.
- [ ] Compatibility helper exit behavior is tested and documented.

## Required Tests

- Nil, cancellation, deadline, real, wrapped, and multiply joined error trees.
- Multiple real failures plus cancellation.
- `errors.Is`/`errors.As` through actionable wrappers.
- Compile-time Interface assertions.
- Subprocess tests for success/failure exit behavior.
- No duplicate logging through an injectable observer.

## Validation

```sh
go test -race -count=1 ./pkg/grace
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

