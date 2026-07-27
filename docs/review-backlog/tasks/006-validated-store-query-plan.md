# 006: Compile Store Options into One Validated Query Plan

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Queries |
| Depends on | 003 |
| Likely conflicts | 001, 003, 004, 007, 008 |
| Owner | Unclaimed |

## Why This Matters

Store options currently mutate separate find and count GORM chains. The same
option can therefore have different meaning in the two halves of one query,
some invalid combinations panic, and callback errors can disappear. Because an
`Option` cannot return an error, validation happens late or not at all.

This shallow Interface exposes GORM mechanics while hiding important policies:
whether counting is enabled, whether expansions apply to totals, how ordering
is validated, and which errors abort a query.

## Architecture Assessment

`applyOptions` is acting as a query compiler, but the compiler has no explicit
input model or validation phase. Deepen this Module around a private immutable
query plan. Public compatibility options should be thin Adapters that populate
the plan; one compiler should then apply it consistently to count and result
queries.

The plan is also the right seam for selector expressions from task 003 and the
dialect capability checks from task 004. Its Interface test surface should be
semantic plans and returned Resources, not GORM callback order.

## Evidence

- `pkg/dbstore/dbstore.go:82-135`: `applyOptions` maintains two mutable query
  chains and treats a nil count chain as an implicit flag.
- `pkg/dbstore/dbstore.go:103-105`: an expansion callback's returned error is
  discarded.
- `pkg/dbstore/dbstore.go:237-250`: `FindLinked` dereferences the count chain
  even when `Count(false)` disabled it.
- `pkg/dbstore/store.go:82-95`: `ExpandOrdered` only has an effect inside a
  preload callback that `applyOptions` installs when its query is non-empty.
- `pkg/dbstore/store.go:42-47,112-131`: unsupported order values fall through to
  ascending behavior instead of failing.
- `pkg/dbstore/store.go:49-50`: the `Option` function signature has no error result,
  so invalid option state cannot be rejected at construction.
- `pkg/dbstore/dbstore.go:166-187`: a till boundary is inclusive when both
  bounds use `BETWEEN`, but exclusive when only `TillTime` is set.
- Direct Store callers can supply an inverted time range or a nil `Option`; the
  current processor validates neither before evaluating/executing it.

## Failure Sequence

1. A caller combines `FindLinked` with `Count(false)` for a cheaper page query.
2. The option processor sets the count query to nil.
3. `FindLinked` still calls `Count` on that nil chain and panics.

Separately, a failed expansion can be ignored, returning a partially hydrated
Resource as though the requested query completed successfully.

## Required Outcome

- All options compile into one explicit, immutable query plan before SQL runs.
- Counting is represented by a non-nil boolean/capability, never by a nil query.
- Invalid order, page, limit, expansion, selector, and dialect combinations
  return descriptive errors before executing a query.
- Time ranges use one documented boundary convention, such as half-open
  `[from, till)`, independent of which bounds are present.
- Find and count queries share the same filtering semantics while intentionally
  excluding result-only projection, preload, ordering, limit, and offset from
  the count.
- Every callback error is propagated with operation context.
- Ordering is applied when requested regardless of whether an expansion filter
  is empty.
- Existing valid public options keep their source compatibility during
  migration.

## Implementation Options and Trade-offs

### Preferred: Private Query Plan with Compatibility Builders

Define a private `queryPlan` containing validated filter, selector, expansion,
order, page, limit, and `includeTotal` fields. Existing `Option` values become
builders over this structure. Compile once, then render count and result scopes
from the same plan. This centralizes policy without making GORM a public
Interface.

### Alternative: Error-Returning Public Options

Change `Option` to `func(*Query) error`. This exposes validation directly and is
simple internally, but it is a broad source-breaking change for every caller.
It may be suitable for a future major version; it is not necessary for the
first repair.

## Implementation Constraints

- Do not let count queries inherit preloads, ordering, limit, or offset.
- Do not silently clamp invalid input; return a typed validation error.
- Preserve transaction binding when rendering both queries.
- Keep selector validation in its canonical Module from task 003.
- Avoid a public query-builder abstraction until two real Store
  implementations need one.

## Suggested Implementation Sequence

1. Add table tests for every option and invalid option combination.
2. Characterize which fields belong to filters, result shaping, and totals.
3. Introduce and validate `queryPlan` behind existing options.
4. Render count and result queries from the plan.
5. Migrate `Find`, `FindLinked`, label queries, and iteration one at a time.

## Non-Goals

- Replacing GORM.
- Adding arbitrary caller-supplied SQL.
- Designing pagination behavior owned by task 007.

## Acceptance Criteria / Definition of Done

- [ ] No option combination can cause a nil-query panic.
- [ ] Invalid option values fail before SQL execution.
- [ ] Expansion and callback errors are returned.
- [ ] Count and result filters are proven equivalent by contract tests.
- [ ] Existing valid callers compile unchanged.
- [ ] The plan has no exported GORM types.

## Required Tests

- `Count(false)` across every find method, especially `FindLinked`.
- Empty and non-empty ordered expansions.
- From-only, till-only, bounded, equal, and inverted time ranges, with Resources
  exactly on each boundary.
- Invalid order, page, limit, selector, and dialect input.
- Nil and operation-inapplicable options.
- Callback error propagation.
- Count/result filter equivalence with association expansions.
- Transaction-bound plan rendering.

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
