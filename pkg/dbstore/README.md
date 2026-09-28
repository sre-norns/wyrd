# DB store
Storage interface and SQL-based implementation of resource storage, built on
[GORM](https://gorm.io).

# Usage

```go
store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)

var scenarios []Scenario
total, err := store.Find(ctx, &scenarios, searchQuery)
```

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
