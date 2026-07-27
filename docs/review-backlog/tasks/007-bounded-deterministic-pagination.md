# 007: Make Pagination and Iteration Bounded and Deterministic

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Queries |
| Depends on | 006 |
| Likely conflicts | 006, 008, 013 |
| Owner | Unclaimed |

## Why This Matters

Pagination is split between dbstore offsets, Bark link construction, and a
batch iterator. None owns the whole contract. Store queries have no guaranteed
unique order, the iterator can become unbounded or skip rows while the dataset
changes, cancellation is reported as success, and HTTP helpers can advertise a
next page that cannot exist.

The result is both a correctness and resilience problem: long-running jobs can
loop over unstable pages, and clients cannot distinguish completion from
cancellation.

## Architecture Assessment

Pagination should be a deep policy Module with a small Interface: a validated
page request, stable ordering/cursor, exact progress, and optional totals.
Storage and HTTP are separate Adapters. The Store Adapter retrieves a page; the
Bark Adapter turns the returned page metadata into links. Neither should infer
facts the other already knows.

Offset pagination can remain for compatibility, but mutable scans need a stable
keyset or snapshot strategy. The deletion test is whether removing the current
duplicated page/link arithmetic leaves one authoritative policy.

## Evidence

- `pkg/dbstore/iterate.go:10-36`: `ForEach` derives progress from a total, checks
  cancellation only between pages, and advances by offset without stable order.
- `pkg/dbstore/iterate.go:16-18`: a fetch failure returns the total count rather
  than the number actually processed.
- `pkg/dbstore/iterate.go:20-24`: `Count(false)` leaves total zero, so iteration
  terminates after the first page.
- `pkg/dbstore/iterate.go:27-29`: the named error is shadowed; cancellation
  breaks the loop and returns nil.
- `pkg/dbstore/iterate_test.go`: current tests encode cancellation as successful
  completion.
- `pkg/dbstore/dbstore.go`: ordinary page queries have no default unique order.
- `pkg/dbstore/dbstore.go:319-331`: total and page data are fetched in separate
  statements without a documented snapshot, so concurrent mutation can make
  them disagree.
- `ForEach` accepts the complete mutation-capable `Store` Interface even though
  it only needs `Find`, increasing fake and caller coupling.
- `pkg/bark/http.go:137-168` and `pkg/bark/contextual.go:79-113`: next-link logic is
  duplicated and uses `len(items) == pageSize` even when an exact total is
  available.

## Failure Sequence

1. A worker iterates Resources in offset pages while another process inserts or
   deletes an earlier row.
2. The next offset addresses a shifted window.
3. A Resource is skipped or processed twice, and there is no stable cursor in
   the callback contract to detect it.

If cancellation arrives, the loop may stop and still report nil, causing the
caller to commit partial work as complete.

## Required Outcome

- Page size is positive, bounded by a documented maximum, and overflow-safe.
- Every paged query has a stable, unique ordering with a deterministic
  tie-breaker.
- Returned page metadata includes enough information to construct correct
  previous/next links without guessing from slice length.
- The consistency level between an optional total and its page is explicit;
  callers can distinguish an exact same-snapshot total from advisory metadata.
- Exact totals, when requested, produce no next link beyond the final page.
- Iteration returns the exact processed count and the first causal error.
- Context cancellation is observable as `context.Canceled` or its causal
  replacement.
- Nil callbacks and invalid iteration configuration return validation errors,
  not panics.
- Iteration behavior is documented for concurrent mutation; stable keyset
  traversal is preferred where supported.
- A limit of zero cannot accidentally request an unbounded scan.

## Implementation Options and Trade-offs

### Preferred: Keyset Iterator plus Explicit Page Result

Retain offset/page input for public list APIs, but return a `PageResult` carrying
items, position, page size, optional total, and `HasNext`. Implement bulk
iteration with a stable `(sortKey, primaryKey)` cursor. This prevents offset
drift and makes Bark link construction mechanical.

### Alternative: Transactional Snapshot with Offset Pages

Run the complete iteration in a repeatable-read transaction and retain offsets.
This is simpler conceptually but holds a long-lived transaction, is
dialect-dependent, and can increase contention. It should be an explicit
caller choice rather than the default.

## Implementation Constraints

- Keep exact-total counting optional; absence of a total must be explicit.
- Depend on a narrow read-only page-finder Interface rather than the full Store.
- Do not construct HTTP URLs in dbstore.
- Do not hold locks or database transactions while invoking user callbacks
  unless the Interface explicitly promises it.
- Check cancellation before fetching and before invoking each callback batch.
- Coordinate query validation with task 006 and Bark response handling with
  task 013.

## Suggested Implementation Sequence

1. Add failing tests for cancellation, zero limits, final-page links, and
   mutation during traversal.
2. Define validated page request/result values.
3. Give Store queries a stable unique order.
4. Implement cursor-based `ForEach` and correct processed-count semantics.
5. Replace Bark's duplicate page inference with `PageResult` metadata.

## Non-Goals

- Streaming arbitrary SQL result sets.
- Providing distributed exactly-once processing.
- Requiring totals for every page.

## Acceptance Criteria / Definition of Done

- [ ] Page validation rejects unbounded or overflowing requests.
- [ ] Stable traversal neither skips nor repeats rows in mutation tests.
- [ ] Cancellation returns a causal error and exact progress.
- [ ] Final-page links are correct with and without totals.
- [ ] Result and link pagination logic exists in one place each.
- [ ] Existing offset callers have a documented migration path.

## Required Tests

- Empty, first, middle, exact-full final, and partial final pages.
- Totals enabled and disabled.
- Zero, negative, excessive, and overflowed page values.
- Nil handler and nil/invalid finder configuration.
- Cancellation before fetch, between fetch and callback, and during callbacks.
- Inserts/deletes while a keyset traversal runs.
- Duplicate sort values requiring a primary-key tie-breaker.
- Fetch and callback errors with exact processed counts.

## Validation

```sh
go test -race -count=1 ./pkg/dbstore ./pkg/bark
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
