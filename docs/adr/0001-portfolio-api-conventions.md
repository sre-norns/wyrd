# ADR 0001: One resource API convention for SRE-Norns products

- **Status:** Proposed
- **Date:** 2026-09-28
- **Applies to:** wyrd (`manifest`, `bark`, `dbstore`), Urth, Exp-Bench. Comserv is
  affected only through wyrd and must keep building unchanged.

## Context

Two products are being brought onto one tenancy model, one UI component library and one
CLI shape. They reach Postgres through the same toolkit, but their HTTP APIs disagree on
nearly every convention a client has to know:

| Concern | Urth | Exp-Bench |
|---|---|---|
| Representation | `apiVersion/kind/metadata/spec/status` (`manifest.ResourceManifest`) | Flat object embedding `expbench.Resource` |
| Field case | camelCase | snake_case |
| Prefix | `/api/v1` | `/v1` |
| Scope | none; names globally unique per table | `account_id`, `project_id` on every resource |
| Concurrency | `version` query parameter on delete; update guarded in store | `revision` as ETag, `If-Match` required |
| Errors | `bark.ErrorResponse` | `application/problem+json` with stable `code` |
| Retries | none | `Idempotency-Key` required on every POST, stored in-transaction |
| Lists | `page`/`pageSize` ≤ 512, `{total, count, data}` | `offset`/`limit` ≤ 1024, `{items, total, offset, limit}`; docs promise cursors |
| Filtering | `?labels=` selector over JSON labels | the same selector, plus pseudo-labels (`status`, `account`, `project`…) mapped to columns |
| Removal | soft delete (tombstone) | lifecycle state, no DELETE routes |

A client, a CLI kit and a UI library cannot be shared across two contracts like that. This
ADR picks one. Where it has to choose, it follows Exp-Bench — the newer and more deliberate
design — except on representation, where the resource envelope is the explicit goal.

The following open review tasks in [`review-backlog`](../review-backlog/tasks/) overlap
this decision and are not duplicated here: 005 (server-owned metadata), 007 (deterministic
pagination), 009 (manifest representation), 013/014 (response handling and negotiation).
This ADR states the contract; those tasks remain the place where the implementation is
made correct.

## Decision

### 1. Every resource is an envelope

```yaml
apiVersion: urth.sre-norns.com/v1
kind: scenarios
metadata:
  uid: 7c0d…            # server-assigned, globally unique, canonical identity
  name: checkout-http   # operator-chosen, unique within its scope (§3)
  account: 3f1a…        # scope, server-assigned (§2)
  project: 91be…        # scope, server-assigned (§2)
  version: 4            # optimistic concurrency token (§5)
  labels: {team: payments}
  creationTimestamp: …
  updateTimestamp: …
spec: {…}               # desired state, written by whoever owns the resource
status: {…}             # observed state, written only by the server
```

- `apiVersion` is `<product>.sre-norns.com/<version>`. The group names the product that
  defines the kind, so two products may define a kind with the same name without ambiguity.
  The bare `v1` Urth emits today is accepted on input for one minor release, with a
  `Warning` response header, and is never emitted again.
- `kind` values are whatever the product registers today. Renaming kinds is not worth the
  churn this ADR would add to it.
- JSON field names are **camelCase**, in keeping with the envelope's Kubernetes lineage and
  with every existing `manifest` tag. Exp-Bench converts when it adopts the envelope.
- Timestamps are RFC 3339 and stored as `TIMESTAMPTZ`.

### 2. Scope is part of identity, and the server owns it

Every kind declares one scope when it is registered: **system**, **account** or
**project**. `metadata.account` and `metadata.project` are populated for the scopes that
have them and are absent otherwise.

Scope is taken from the request — the collection URL and the caller's credentials — never
from the body. A body that names a different scope than the URL is rejected with `400`
rather than silently corrected. Silent correction would make `apply` of a manifest copied
from another project appear to succeed while writing somewhere the author did not intend.

`metadata.account`, `project`, `uid`, `version` and the timestamps are server-owned. The
rules in review task 005 apply to all of them: a create carrying a non-zero server-owned
field is rejected.

### 3. Names are unique within a scope, UIDs everywhere

A name identifies a resource *to a person* and is unique per `(scope, kind)`. Two projects
may each have a scenario called `checkout-http`; two accounts may each have a runner called
`edge-eu`. A UID identifies a resource *to a machine* and is globally unique. Anything that
stores a reference stores the UID.

The store enforces name uniqueness with a composite unique index over the scope columns,
name and deletion tombstone. It is not left to a read-before-write check.

### 4. URLs: scoped collections, shallow items

```text
/v1/accounts/{accountUID}/<collection>     account-scoped collection
/v1/projects/{projectUID}/<collection>     project-scoped collection
/v1/<collection>/{uid}                     any item, canonical and shallow
```

- The prefix is `/v1`. Urth serves `/api/v1` as an alias for one minor release.
- Collection segments are plural kebab-case (`dispatch-failures`, `worker-instances`).
- A project's account is implied by the project, so project-scoped URLs do not repeat it.
- Item URLs carry no scope, but reaching an item still resolves and authorizes its full
  ownership chain. A shallow URL is never a way around a scope check.
- Name-addressed lookup is a filter on the scoped collection (`?name=`), not a second item
  URL.

### 5. Optimistic concurrency uses `metadata.version` as the ETag

- Every item response carries `ETag: "<version>"`.
- `PUT`, `PATCH` and `DELETE` require `If-Match`. A missing header returns `428`; a stale
  one returns `412`.
- `PUT` replaces `metadata.labels` and `spec`. `PATCH` is a JSON Merge Patch
  ([RFC 7396](https://www.rfc-editor.org/rfc/rfc7396)) over the same two fields. Neither
  can write `status`; status transitions are named operations the server owns.
- Periodic liveness writes (for example Urth's worker presence) are not edits and must not
  bump `version`. Otherwise a version that was current a minute ago fails `If-Match` for a
  reason the operator cannot see.

### 6. Errors are problem details

Every failure is `application/problem+json`
([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)):

```json
{"type": "…", "title": "…", "status": 409, "code": "name-taken",
 "detail": "…", "instance": "…", "requestId": "…", "fields": {"metadata.name": "…"}}
```

`code` is stable and documented; clients branch on it, never on `detail`. `fields` points at
the offending input using the same paths the envelope uses.

### 7. POSTs are safe to retry

Every `POST` that creates or changes state accepts an `Idempotency-Key` header (≤ 200
bytes). The key is recorded with a digest of the request's method, path and body. A replay
returns the recorded response; a reused key with a different request returns
`409 idempotency-conflict`, and a key whose first request is still executing returns
`409 idempotency-in-progress`.

The shared middleware (`bark.Idempotent`) reserves the key *before* the handler runs, so two
concurrent copies of a request cannot both execute, and records the outcome *after* it
returns. It does not commit the record in the handler's transaction: a process that dies
between the effect and the record leaves a reservation that expires after its lease, and the
retry then executes again. That is at-most-once while the process lives, not exactly-once. A
product whose effects must be exactly-once records inside its own transaction, as Exp-Bench
does today; the middleware is for the rest.

The key is **required** on user-facing POSTs. A route may be exempted only when it is
idempotent by construction and documents why. Urth's run claim is the model case: a repeat
claim by the same worker for the same dispatch already returns the same lease.

### 8. Lists are bounded, ordered and resumable

```json
{"items": […], "limit": 100, "next": "opaque-cursor", "total": 342}
```

- Query parameters: `labels`, `fields`, `name`, `from`, `till`, `limit`, `cursor`.
- `limit` defaults to 100 and is capped at 1024. Zero never means "unbounded".
- Every list has a total order with a unique tie-breaker, by default
  `creationTimestamp DESC, uid DESC`, so a cursor resumes exactly where the page ended.
- `next` is absent on the last page. It is never inferred from `len(items) == limit`.
- `total` is advisory unless a product documents it as exact. It may be counted in a
  separate statement from the page, and a client must not use it to compute page links.
- `offset` is accepted for compatibility, with Urth's `page`/`pageSize` mapped onto it, for
  one minor release. When both styles are present, `offset` and `limit` take precedence; it
  is not an error, because servers preset a default `pageSize` before reading the request. Offset pages skip or repeat rows when earlier rows change; that is
  the reason the cursor is the contract.

### 9. Labels and fields are filtered separately

`labels` takes the wyrd label-selector grammar and matches only labels. `fields` takes the
same grammar and matches a per-kind allowlist of server-known attributes such as
`status.phase`, `metadata.account` or `metadata.project`.

Exp-Bench's pseudo-labels put both in one namespace, so a user label named `status` can
never be selected. Keeping them apart removes the collision. Exp-Bench's pseudo-labels stay
accepted in `labels` until its clients move, then stop being accepted.

The same selector must return the same set whether it is evaluated in Go or in SQL (review
task 003). Filtering is implemented once, in `dbstore`, and both products use it.

### 10. Removal: tombstones, with lifecycle state where the domain needs it

`DELETE` writes a tombstone (`deletionTimestamp`). The row is kept and the name becomes
reusable. A kind whose domain needs states such as `archived` or `suspended` models them in
`status` and may choose not to offer `DELETE` at all, as Exp-Bench does today. The choice is
declared per kind; it is not a global rule.

## Architectural rules

- A product never hand-builds an envelope, error body, list page or ETag. `manifest`,
  `bark` and `dbstore` provide them, and a gap is fixed there.
- Scope comes from the URL and the principal. A request body cannot set or change it.
- Every stored reference to another resource is a UID.
- Every list has a deterministic order before it is paged.
- `status` is never writable through `PUT` or `PATCH`.
- Changes to wyrd types remain source-compatible for consumers not adopting this contract
  (Comserv). New behaviour is opt-in until each consumer has moved.

## Consequences

### Benefits

- One client, one CLI kit and one UI data layer can serve every product, because
  collections, items, errors, retries and pagination behave the same everywhere.
- Scope in metadata makes multi-tenancy a store property rather than a convention each
  handler remembers.
- Clients can retry safely over unreliable links, which both products' machine clients —
  Urth workers, Exp-Bench agents — rely on.

### Costs

- Both products break their wire format once. Urth changes prefixes, list shape, errors and
  adds scope; Exp-Bench changes its whole representation and field case. The compatibility
  windows above bound that cost to one minor release each.
- Cursor pagination needs an index matching each list's order. Urth's existing
  `created_at` indexes cover the default order; any other order a product offers needs one.
- Idempotency records are a new table in Urth, with a retention sweep.

### Explicitly not decided here

- How identity (users, accounts, projects, sessions, machine credentials) is structured and
  shared. See [ADR 0002](./0002-shared-identity-module.md).
- Kind naming conventions, beyond "unchanged for now".
- Watch/streaming semantics. Both products have live streams, and they stay
  product-specific until there is a second consumer that needs a shared form.
