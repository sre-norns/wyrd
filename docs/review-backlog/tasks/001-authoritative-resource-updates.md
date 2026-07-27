# 001: Make Resource Updates Authoritative and Explicit

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Mutations |
| Depends on | — |
| Likely conflicts | 004, 005, 006 |
| Owner | Unclaimed |

## Why This Matters

The Store Interface says `Update` changes the Resource identified by its `id`
argument. The implementation never reads that argument. It lets GORM infer a
target from the primary key embedded in `newValue`, so a caller can update the
wrong row, update multiple same-version rows, or receive a missing-WHERE error
even after supplying a valid target.

The same implementation uses GORM's struct `Updates`, which skips zero values.
Clearing a boolean, string, number, collection, or status field can therefore
appear successful without changing stored state. Urth already carries a
workaround that gives up atomic write-time version checking for ordinary
Resource edits.

## Architecture Assessment

Resource mutation is currently a shallow wrapper over GORM. Its Interface
accepts an explicit ID, Resource Version options, and a full value, but callers
must know GORM primary-key inference, zero-value omission, hooks, `Save` upsert
fallback, and dialect-specific `RowsAffected` behavior to predict the result.

Deepen the mutation Module so target resolution, compare-and-swap, replacement
semantics, version advancement, and outcomes have locality. Its Interface should
be the test surface. Deleting that Module should cause these rules to reappear in
every caller; that is the leverage it must provide.

## Evidence

- `pkg/dbstore/store.go:169-178`: the Store Interface says `Update` takes the ID
  of the entry to update.
- `pkg/dbstore/dbstore.go:290-302`: the public wrapper forwards the ID.
- `pkg/dbstore/dbtransaction.go:31-38`: the implementation never references
  `id`; `Model(newValue).Updates(newValue)` chooses the target and skips zero
  fields.
- `pkg/dbstore/dbtransaction.go:41-49`: `CreateOrUpdate` treats
  `RowsAffected == 1` as an `exists` boolean around GORM `Save`.
- `pkg/dbstore/dbtransaction.go:68-86`: delete and restore also collapse
  missing, stale-version, already-restored, and invariant outcomes into a
  boolean derived from `RowsAffected`.
- `pkg/dbstore/dbstore_test.go:66-913`: exported Store tests cover construction
  and queries but not direct Update/CreateOrUpdate/Delete/Restore outcomes.
- Downstream, Urth
  `../urth/pkg/urth/service.go:1362-1378` documents that `Update` silently drops zero
  values and works around it with `CreateOrUpdate`.

## Failure Sequence

1. A caller asks to update Resource A but passes a value whose embedded UID is B.
2. `Update` ignores A and GORM targets B from the value.
3. B changes while A remains unchanged.

An even wider variant passes a zero UID with `WithVersion(1)`: the only explicit
predicate can become `version = 1`, allowing multiple rows to change. The method
then returns `false, nil` because `RowsAffected` is greater than one, after the
damage is committed.

## Required Outcome

- The explicit ID and configured ID column name the only row `Update` may
  mutate.
- A different non-zero ID embedded in the value is rejected before SQL runs.
- An expected Resource Version is included in the same `UPDATE` predicate.
- A full Resource replacement persists zero values. If patch behavior remains
  useful, expose it through a separately named Interface with an explicit field
  mask.
- Exactly one successful write advances Resource Version once.
- Missing ID, version conflict, created, updated, and unchanged are stable typed
  outcomes; dialect-specific row counts do not redefine them.
- Delete and restore use the same explicit target and typed-outcome rules;
  version conflict is not reported as “missing.”
- A single-Resource method treats `RowsAffected > 1` as an invariant violation
  and rolls back where possible.
- `CreateOrUpdate` does not silently insert after an attempted update unless the
  caller explicitly requested upsert semantics.

## Implementation Options and Trade-offs

### Preferred: Explicit Compare-and-Swap Replacement

Add a deep mutation Interface such as `Replace(id, expectedVersion, value)` that
builds one guarded update using quoted configured columns, an explicit list of
server-writable fields, and `version = version + 1`. Return a typed
`MutationOutcome`. Keep `Update` and `CreateOrUpdate` as deprecated compatibility
adapters until consumers migrate.

This is one round trip, keeps optimistic concurrency atomic, and concentrates
zero-value and ownership rules. GORM hooks must not be relied on to smuggle
essential predicate or version behavior into the query.

### Alternative: Transactional Load, Validate, and Replace

Lock the target row, validate embedded identity/version, write an explicit full
replacement, and commit. This is easier to reason about for complex schemas but
costs a read, holds a lock longer, and still needs a typed outcome. It is
appropriate only if one guarded update cannot express all required model rules.

## Implementation Constraints

- Use `SchemaConfig.IDColumnName` and `VersionColumnName`; do not hard-code
  manifest defaults.
- Preserve atomic status-transition behavior used by Urth.
- Do not expose raw GORM errors as mutation outcomes.
- Resource ownership rules from task 005 must fit behind this write seam.
- Provide a staged migration for the current boolean-returning Interface.

## Suggested Implementation Sequence

1. Add failing tests where explicit and embedded IDs differ, where embedded ID
   is empty, and where a zero value must clear stored state.
2. Define `MutationOutcome` and explicit replacement semantics.
3. Implement the guarded single-row write for DBStore and StoreTransaction.
4. Adapt/deprecate current methods and migrate downstream call sites.
5. Add multi-dialect contract coverage through task 004's harness.

## Non-Goals

- Bulk updates.
- JSON merge-patch or arbitrary field-mask syntax.
- Retrying application-level version conflicts.

## Acceptance Criteria / Definition of Done

- [ ] A mutation can change only the explicit target ID.
- [ ] Stale Resource Versions cannot write.
- [ ] Zero-valued fields are persisted by replacement.
- [ ] Single-row invariant violations cannot be reported as benign `false`.
- [ ] Upsert outcomes are independent of GORM `Save` row-count behavior.
- [ ] Urth's zero-value workaround can migrate back to an atomic write.
- [ ] Public and downstream migration documentation is complete.

## Required Tests

- Explicit A / embedded B rejects with neither row changed.
- Explicit A / embedded zero changes only A.
- Matching and stale Resource Version compare-and-swap.
- Clearing bool, string, numeric, slice/map, and embedded status fields.
- Missing ID, unchanged replacement, driver error, and forced multi-row result.
- Delete of missing/current/stale versions and restore of missing/active/deleted
  Resources, including name collision.
- The same scenarios through `DBStore` and an open `StoreTransaction`.

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
