# 008: Give Label Catalogs a Truthful Query Interface

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Queries |
| Depends on | 003, 006 |
| Likely conflicts | 003, 004, 006, 007 |
| Owner | Unclaimed |

## Why This Matters

`SearchQuery` documents `Name` as a Resource-name search, but label catalog
methods reinterpret it as a label-key or label-value pattern. They also ignore
other fields whose names suggest Resource filtering. A caller cannot tell which
parts of a query are honored, and `%` or `_` in input silently becomes a SQL
wildcard.

An Interface that accepts fields it ignores invites authorization and UX bugs:
the displayed label vocabulary may come from a broader Resource set than the
caller intended.

## Architecture Assessment

Resource search and label-catalog search are different domain operations. They
should not share a broad parameter bag. Introduce a dedicated catalog query
value whose fields have one meaning across dialect Adapters. Reuse the canonical
Resource filter/selector plan from tasks 003 and 006 rather than copying it.

This deepens both Interfaces: Resource search no longer contains method-specific
exceptions, and label enumeration becomes precise enough to test.

## Evidence

- `pkg/manifest/search.go:10-23`: `SearchQuery.Name` is described as a fuzzy Resource
  name filter.
- `pkg/dbstore/dbstore.go`: Resource search builds a `LIKE` expression from
  caller input without escaping literal `%` and `_`.
- `pkg/dbstore/dbstore.go:356-381`: `FindLabels` reuses `Name` as a label-key
  filter.
- `pkg/dbstore/dbstore.go:383-410`: `FindLabelValues` reuses `Name` as a label-value
  filter and does not apply all Resource search fields.
- `pkg/dbstore/dbstore_test.go`: comments acknowledge that querying by labels
  “makes no sense” for these methods instead of defining a separate contract.
- Dialect-specific JSON enumeration in `pkg/dbstore/gorm_json.go` is not covered
  by a shared catalog contract.

## Failure Sequence

1. A caller builds a `SearchQuery` with a tenant Selector and `Name: "cost_%"`.
2. The same value is passed to `FindLabelValues`.
3. The Selector is ignored and `_`/`%` are interpreted as SQL patterns.
4. Values outside the intended Resource set are returned.

## Required Outcome

- Label key and label value enumeration use a dedicated query type.
- The query distinguishes literal prefix/substring matching from an explicit
  pattern language.
- Literal `%`, `_`, and dialect escape characters behave identically across
  SQLite, Postgres, and MySQL.
- Resource scoping uses the canonical validated filter/selector plan.
- Pagination, ordering, distinctness, and optional totals are documented.
- Unsupported filters fail explicitly instead of being ignored.
- Compatibility shims for the old `SearchQuery` calls emit a migration path.

## Implementation Options and Trade-offs

### Preferred: `LabelCatalogQuery` Composed with `ResourceFilter`

Define a narrow value containing catalog search text/mode, page request, and an
optional validated `ResourceFilter`. Use one dialect-neutral catalog operation
with database Adapters for JSON enumeration. This keeps field meaning local and
reuses existing authorization scoping.

### Alternative: Separate Key and Value Query Types

Use `LabelKeyQuery` and `LabelValueQuery`. This is maximally explicit and may be
valuable if their filtering diverges, but duplicates pagination and Resource
scope fields today. Start with one type unless real behavior requires two.

## Implementation Constraints

- Bind all input as parameters; do not interpolate JSON paths or patterns.
- Define case sensitivity rather than inheriting database collation defaults.
- Do not silently fall back to unscoped enumeration.
- Preserve deterministic ordering before pagination.
- Coordinate SQL parity with task 004.

## Suggested Implementation Sequence

1. Write the public catalog contract and cross-dialect fixtures.
2. Add literal wildcard and Resource-scope regression tests.
3. Introduce the dedicated query type and compatibility Adapter.
4. Render the validated plan per dialect.
5. Deprecate the overloaded methods in documentation.

## Non-Goals

- Full-text search over labels.
- Label-frequency analytics.
- A caller-defined SQL pattern language.

## Acceptance Criteria / Definition of Done

- [ ] Every accepted field has the same meaning in both catalog operations.
- [ ] Resource scoping is honored or rejected explicitly.
- [ ] Literal wildcard characters behave portably.
- [ ] Results are distinct, stable, and correctly paginated.
- [ ] Old callers receive a documented compatibility path.
- [ ] All three supported dialects pass the same contract.

## Required Tests

- Literal `%`, `_`, escape, Unicode, empty, and case-variant search text.
- Scoped versus unscoped Resource sets.
- Duplicate label keys/values and deterministic ordering.
- Empty, first, middle, and final pages with optional totals.
- Unsupported query-field rejection.
- Live SQLite, Postgres, and MySQL contract tests.

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
