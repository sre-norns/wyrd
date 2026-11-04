# DB store
Storage interface and SQL-based implementation of resource storage, built on
[GORM](https://gorm.io).

# Usage

```go
store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)

var scenarios []Scenario
total, err := store.Find(ctx, &scenarios, searchQuery)
```

## Connecting

`Config` (kong flags; `DB_URL`, `DB_USER`, `DB_PASS`, `DB_NAME` in the
environment) selects the driver from the URL scheme. `Dialector()` returns the
`gorm.Dialector`:

```go
dialector, err := dbstore.Config{URL: os.Getenv("DB_URL"), Password: os.Getenv("DB_PASS")}.Dialector()
db, err := gorm.Open(dialector, &gorm.Config{})
```

The user name and password in `URL` must be percent-encoded. Encode every
reserved character (`/ # ? @ : %` and others). Python's `urllib.parse.quote`
leaves `/` unencoded by default, so pass `safe=""`. To avoid encoding, leave
the credentials out of the URL and set `User` and `Password`.

For Postgres, `User`, `Password`, `DBName` and `Port` override the matching
parts of the URL, and any `user`, `password`, `dbname` or `port` query option.
An empty override keeps the value in the URL. Other query options are kept as
written, such as `sslmode` and a Cloud SQL socket
(`postgres://svc@localhost/db?host=/cloudsql/project:region:instance`).
`Dialector()` checks the final Postgres settings, so a syntax problem fails
there and not in `gorm.Open`. A Postgres `URL` cannot be combined with `DSN`.
MySQL and SQLite ignore the overrides.

Errors from `Dialector()` never contain the URL, the user name, or the
password. They say what to fix. `errors.Is(err, dbstore.ErrInvalidDBUrl)`
reports a URL that does not parse.

## Scope

Resources carry `metadata.account` and `metadata.project` (see
[manifest](../manifest)). Names are unique within a scope, not across the table.

```go
ref := manifest.ScopeRef{Account: accountID, Project: projectID}

// Reads, updates and deletes see only that scope; creates are placed in it.
store.Find(ctx, &scenarios, query, dbstore.InScope(ref))
store.Create(ctx, &scenario, dbstore.InScope(ref)) // ErrScopeMismatch if the body names another
```

A database migrated by wyrd v0.3.0 or earlier still has a table-wide unique name
index. Call `dbstore.DropLegacyNameIndexes(db, models...)` once, after
`AutoMigrate`, when adopting scoped resources.

## Visibility

A `Visibility` decides which rows the caller may see (`Filter`) and write
(`Admit`). A store built with one applies it to every operation -- finds and
counts, gets, the name and label catalogues, updates, deletes, transactions --
so tenancy is a property of the store rather than a filter each handler must
remember:

```go
tenantStore := store.WithVisibility(myVisibility)
```

`CreateOrUpdate` refuses a UID that exists but is not visible (`ErrNotVisible`).
A filter alone cannot: gorm's `Save` falls back to an upsert when its update
matches nothing, and would overwrite the hidden row.

A control loop that must see everything uses the store without a visibility.

## Listing a page

`FindPage` lists newest first and continues from an opaque cursor, so a page
never repeats or skips rows when earlier rows change:

```go
page, err := store.FindPage(ctx, &scenarios, manifest.SearchQuery{Limit: 50, Cursor: previous.Next})
// page.Next is empty on the last page; page.Total is nil when Count(false) was passed.
```

A limit of 0 means `DefaultPageLimit` (100); `MaxPageLimit` (1024) is the cap.

## Field selectors

`SearchQuery.Fields` selects on columns rather than labels, in the label selector
grammar. `metadata.uid`, `.name`, `.version`, `.account` and `.project` are
always selectable; declare more per kind:

```go
var scenarioFields = dbstore.Fields(dbstore.FieldColumns{"status.phase": "status_phase"})
store.Find(ctx, &scenarios, manifest.SearchQuery{Fields: fields}, scenarioFields)
```

An undeclared field is `ErrUnknownField`, never silently ignored.

## Idempotency records

`IdempotencyStore` backs `bark.Idempotent`. Migrate `dbstore.IdempotencyRecord`
with your models, and `Sweep` old records on a schedule.

## Testing against Postgres

Tests that need Postgres skip unless `WYRD_TEST_POSTGRES_URL` is set. Each gets a
private schema, dropped afterwards.

## Composing a resource query in an existing transaction

`FilterQuery` applies visibility, exact optional scope, case-insensitive name
search, a half-open time range, label selectors and declared field selectors to
an existing GORM query. It leaves ordering and pagination to `PageBy`. Use that
same returned query for selection and totals. This preserves the caller's
transaction and domain predicates; it does not introduce another commit.

`QueryPolicy.Fields` maps public field paths to server-owned column names.
`QueryPolicy.Predicates` registers computed attributes such as effective expiry
or linked-record filters. Build their SQL from trusted code and bind client
values. `CompareField` provides shared comparison/null semantics for scalar SQL
expressions. Numeric operators require numeric expressions.

`QueryPolicy.Scope` is exact: an account-only scope matches account records,
not projects inside the account. For an authorized cross-scope list, omit Scope
and supply Visibility. A nil Visibility is only for an explicitly authorized
service query. Scope filtering does not replace authorization.

No limit/offset is applied by FilterQuery; nonzero offsets fail. Existing cursor
keysets and PageBy provide the page and total. The M8 query tests cover combined
filters, computed expiry, row visibility, continuation, transaction rollback and
PostgreSQL behaviour.
