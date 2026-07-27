# 004: Honor the Advertised SQL Dialect and Schema Contract

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Queries |
| Depends on | — |
| Likely conflicts | 001, 003, 006, 008, 020 |
| Owner | Unclaimed |

## Why This Matters

The root documentation advertises the databases supported by GORM, while Wyrd
directly includes SQLite, Postgres, and MySQL drivers. Only SQLite is exercised.
Several paths are not portable: MySQL label enumeration uses SQLite's
`json_each`, active-name uniqueness relies on null behavior that differs by
dialect, custom schema names are ignored by some methods, and connection
overrides are silently malformed or discarded.

These are data-integrity and startup-contract failures, not merely missing
optimizations.

## Architecture Assessment

Dialect differences are handled by switches embedded in query expressions and a
single Config method that partially assembles three unrelated connection
formats. These are real Seams because there are three advertised Adapters, but
their interfaces are implicit and unvalidated.

Deepen dialect configuration, JSON query, and schema validation Modules. Each
Adapter must explicitly claim its capabilities and pass the same contract suite.
Unknown dialect behavior must fail closed. This gives callers leverage without
requiring them to know database-specific NULL, JSON, uniqueness, or DSN rules.

## Evidence

- `README.md:101-105`: dbstore is advertised as supporting GORM databases.
- `go.mod:12-15`: SQLite, Postgres, and MySQL drivers are direct dependencies.
- `pkg/dbstore/dbstore_test.go:71-80`: Store behavior is tested only on SQLite.
- `pkg/dbstore/gorm_json.go:274-283`: both `mysql` and `sqlite` emit
  `json_each`, which is not MySQL's JSON table interface.
- `pkg/manifest/meta.go:172-190`: active-name uniqueness uses a composite
  `(name, deleted_at)` index with a Postgres-specific `NULLS NOT DISTINCT`
  option.
- `pkg/dbstore/dbstore_test.go:771-790`: SQLite fixtures permit behavior that
  depends on NULL-distinct composite indexes.
- `pkg/dbstore/dbstore.go:28-38,62-72`: `SchemaConfig` promises custom mappings,
  but construction does not validate them.
- `pkg/dbstore/dbstore.go:51-60`: the default `ManifestModel` configuration is
  an exported mutable package variable, so unrelated callers can change the
  defaults observed by later Store construction.
- `pkg/dbstore/dbtransaction.go:60-66`: `GetByName` hard-codes `name`.
- `pkg/dbstore/config.go:35-70`: DSN-only passes the guard but an empty URL is
  still parsed; `Port` formats the pointer; overrides affect only Postgres and
  are space-joined without dialect escaping.
- `pkg/dbstore/config_test.go:36-41,64-69`: tests expect DSN-only failure and
  assert only the Adapter name, so the broken port is not exercised.

## Failure Sequence

1. A caller configures `SchemaConfig.NameColumnName = "resource_name"`.
2. Store construction succeeds.
3. `GetByName` queries `name`, producing an opaque database error or reading the
   wrong column.

Separately, two active Resources with the same name can coexist where the
composite unique index treats NULL deletion timestamps as distinct.

## Required Outcome

- The supported dialect list is explicit and limited to SQLite, Postgres, and
  MySQL unless a new Adapter passes the contract suite.
- Store construction validates a complete immutable SchemaConfig or applies one
  documented zero-value default.
- Default schema configuration is returned by value from a constructor and
  cannot be mutated process-wide.
- Every CRUD, version, soft-delete, restore, association, selector, and label
  catalog path honors configured quoted columns.
- Active Resource names are unique on every supported dialect; soft-deleted
  history and restore collisions have defined typed outcomes.
- JSON extraction and enumeration use dialect-correct SQL.
- Connection configuration has one unambiguous URL/DSN contract, correct
  override precedence, numeric port handling, safe escaping, and password
  redaction.
- Unsupported capabilities fail at construction or query compilation, not after
  a broadened or partial query.

## Implementation Options and Trade-offs

### Preferred: Real Dialect Adapters

Define private dialect capabilities for connection parsing, JSON operations,
safe numeric conversion, uniqueness migration, and quoting. Construct one
validated Adapter in `NewDBStore`. Use a default SchemaConfig constructor rather
than an externally mutable global value.

For active-name uniqueness, use a dialect-appropriate generated active-name
column or partial/functional unique index maintained by migrations. Keep the
logical invariant identical even when SQL differs.

### Alternative: Narrow Supported Persistence

Declare Postgres as the only production Adapter and move SQLite to test/local
status while removing MySQL claims and driver dependency. This is simpler but
contradicts current project intent and downstream usage expectations; it requires
an explicit compatibility decision and documentation before implementation.

## Implementation Constraints

- Do not emulate uniqueness solely with a check-then-insert race.
- Do not log or embed plaintext credentials in errors.
- Multi-dialect contract tests must use live engines for data behavior.
- Preserve GORM parameter binding and identifier quoting.
- Coordinate selector semantics with task 003 and CI services with task 020.

## Suggested Implementation Sequence

1. Add live database fixtures and a shared Store contract suite.
2. Validate/freeze SchemaConfig and correct all hard-coded columns.
3. Define the configuration contract and dialect-owned parsing.
4. Implement dialect-correct JSON and uniqueness behavior.
5. Exercise create/delete/restore/name-reuse races on all Adapters.
6. Publish a support table and migration notes.

## Non-Goals

- Database pool sizing, TLS policy, or connection retries.
- Supporting every GORM dialect.
- Full label indexing/performance redesign.

## Acceptance Criteria / Definition of Done

- [ ] SQLite, Postgres, and MySQL pass one public Store contract suite.
- [ ] Duplicate active names are impossible on all supported databases.
- [ ] Restore collision is explicit and leaves data unchanged.
- [ ] Custom SchemaConfig works across every operation.
- [ ] DSN/URL and overrides behave exactly as documented.
- [ ] Unknown dialects/capabilities fail closed.
- [ ] Support documentation no longer overclaims.

## Required Tests

- Duplicate active name, soft delete/recreate, restore with and without collision.
- Custom names for ID, name, version, labels, and timestamps across CRUD/query.
- MySQL label enumeration and JSON selector smoke tests.
- DSN-only/URL-only, special-character credentials, override precedence, port,
  unsupported override, and redacted errors.
- Live connectivity plus behavior tests for every advertised Adapter.

## Validation

```sh
go test -race -count=1 ./pkg/dbstore
# Run the documented live SQLite/Postgres/MySQL contract target.
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
