# 003: Make Selector Semantics Identical in Memory and SQL

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Queries |
| Depends on | — |
| Likely conflicts | 004, 006, 008 |
| Owner | Unclaimed |

## Why This Matters

Wyrd explicitly promises that dbstore turns the same label selectors used in
memory into SQL. Negative requirements currently disagree on missing keys, and
numeric requirements disagree on malformed numeric label values. A Resource can
therefore be admitted by an in-memory policy and disappear from the Store query,
or a malformed label can fail an entire Postgres query while the in-memory
Selector simply reports no match.

Programmatic `Requirement` construction also accepts invalid keys and wrong
operator cardinalities, leaving later code to choose arbitrary values from a
map.

## Architecture Assessment

Selector meaning is spread across `Requirement.Matches`, the Kubernetes parser
Adapter, dbstore's switch, and dialect-specific JSON SQL generation. The SQL
implementation is not an Adapter of one canonical semantic Module; it
reinterprets operators.

Deepen a selector compiler Module: validate a Requirement once, retain
`Matches` as the reference behavior, and compile a dialect-neutral expression
that real SQLite, Postgres, and MySQL Adapters render. The shared truth-table
contract becomes the Interface test surface.

## Evidence

- `README.md:76-77,92-95`: selectors work like kubectl and dbstore translates
  the same selectors to SQL.
- `pkg/manifest/selector.go:51-61`: `NewRequirement` validates only the operator.
- `pkg/manifest/selector.go:80-111`: `NotIn` and `NotEquals` match an absent key;
  malformed numeric labels do not match.
- `pkg/manifest/selector.go:133-143`: single-value formatting indexes an
  arbitrary value and can panic when cardinality is wrong.
- `pkg/dbstore/dbstore.go:428-472`: SQL compilation emits plain negative and
  numeric expressions.
- `pkg/dbstore/gorm_json.go:73-103,153-185,215-250`: missing JSON paths become
  SQL NULL and numeric strings are cast without a safe type check.
- `pkg/dbstore/dbstore_test.go:444-468`: the SQLite `notin` fixture currently
  enshrines the divergence by excluding a Resource with no key.

## Failure Sequence

1. A policy evaluates `env != production` against a Resource with no `env`
   label and accepts it.
2. The same Selector is passed to dbstore.
3. SQL evaluates `NULL <> 'production'` as unknown and omits the Resource.

For `size > 10`, a non-numeric stored label returns false in memory but can abort
Postgres casting or be coerced differently by another database.

## Required Outcome

- Every supported operator has one documented missing-key, empty-value, and
  cardinality behavior.
- `NewRequirement` validates label key syntax and operator-specific value counts.
- Single-value operators accept exactly one value; existence operators accept
  none; set operators reject invalid empty sets according to Kubernetes-compatible
  semantics.
- In-memory, parsed Kubernetes, LabelSelector-derived, SQLite, Postgres, and
  MySQL evaluation return the same matching Resource IDs.
- Non-numeric stored values are non-matches for numeric comparisons and never
  abort or broaden a query.
- Unsupported operators or dialects return typed errors before executing SQL.
- Expression and bind ordering is deterministic.

## Implementation Options and Trade-offs

### Preferred: Dialect-Neutral Selector Expression

Compile validated Requirements into a private expression tree with explicit
`KeyExists`, string comparison, set membership, and safe numeric comparison.
Each real SQL dialect Adapter renders that tree. This gives locality and lets one
contract suite exercise every Adapter.

### Alternative: Normalize Labels Relationally

Store labels in a child table with typed/validated columns and compile selectors
to joins. This gives portable SQL and better indexing but adds a data migration,
write amplification, and association complexity. It is a later performance
option, not required to repair semantics.

## Implementation Constraints

- Do not change Kubernetes-compatible negative-selector meaning to match current
  SQLite tests.
- Validate before generating SQL; an empty clause is never an error fallback.
- Preserve parameter binding; no values or keys may be interpolated as SQL.
- Coordinate dialect contract coverage with task 004.

## Suggested Implementation Sequence

1. Build a table of valid/invalid Requirements and expected `Matches` behavior.
2. Add a shared Store contract test that derives expected IDs through `Matches`.
3. Introduce the dialect-neutral compiler and one Adapter at a time.
4. Correct existing fixtures that encode divergent semantics.
5. Add fuzz/property tests for parsed and programmatic selectors.

## Non-Goals

- Changing selector syntax.
- Adding OR semantics; a Selector remains a conjunction.
- Query-plan/index tuning beyond preventing pathological casts.

## Acceptance Criteria / Definition of Done

- [ ] All adapters pass the same selector truth table.
- [ ] Missing-key negative requirements match consistently.
- [ ] Invalid numeric labels cannot fail a query.
- [ ] Invalid keys/cardinalities fail at construction.
- [ ] Unsupported compilation fails closed.
- [ ] Existing public selector strings remain compatible when valid.

## Required Tests

- Every operator against present, absent, empty, matching, and non-matching keys.
- Zero, one, and multiple values for every operator class.
- Numeric labels: negative, bounds, overflow, and non-numeric.
- `Requirement.Matches` expected IDs versus live SQLite/Postgres/MySQL results.
- Deterministic `String`, expression, and bind ordering.
- Fuzz parse → requirements → string → parse semantic equivalence.

## Validation

```sh
go test -race -count=1 ./pkg/manifest ./pkg/dbstore
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

