# ADR 0003: Canonical resource cutover

- Status: Accepted (2026-10-02)
- Applies to: wyrd, shared identity, Urth and Exp-Bench (M8).
- Supersedes: the one-release resource-format compatibility requirements in ADR 0001 and the first M8 design.

## Decision

There are no installations or resources to preserve for M8. Ship the canonical resource contract directly. Do not implement a flat read/write compatibility window, vendor flat media types, dual decoders, a bridge release or old-data migration. Update current examples and fixtures with their owning API/client changes. Use disposable fresh databases for verification; this decision does not authorize deleting a developer's data.

A resource is `apiVersion`, `kind`, `metadata`, typed `spec`, optional typed `status`, and optional `_links`. Use camelCase for schema-owned fields. Keep arbitrary user map keys unchanged. IDs/references are UIDs; version is the numeric optimistic concurrency token. Resource groups identify the definition owner:

| Owner | apiVersion |
| --- | --- |
| Shared identity in either product | `identity.sre-norns.com/v1` |
| Exp-Bench research/work resources | `exp-bench.sre-norns.com/v1` |
| Urth monitoring/infrastructure | `urth.sre-norns.com/v1` |

Resolve the exact `(apiVersion, kind)` pair. Unknown pairs fail. A kind's spec/status must match its declared types. Services own registries; package import order must not select the resource definition. Existing wyrd APIs remain available to consumers outside this cutover, including Comserv. That source compatibility does not require a legacy wire mode in M8 services.

Use ordinary JSON and the existing supported YAML interfaces. There is no format-selection rollout or automatic mutation retry in a different format. Flat resource bodies fail validation. OAuth, binary and streaming content types remain separate.

## Metadata, state and authority

`manifest.ObjectMeta` supplies UID, name, account/project ownership, version, labels and timestamps. It is the scoped metadata type already implemented by M1. The wire contract does not require database column names to match it.

Desired configuration and immutable submitted definitions go in spec. Lifecycle goes in `status.phase`; effective values, counters, execution progress and current-version pointers go in status. The server records `status.lastModifiedBy` on versioned mutations as a safe actor projection. `status.authority` describes the recorded mutation's authority; it does not grant permission to a reader. Do not serialize a full principal, credential or provider subject as attribution.

Create rejects server-owned metadata/status. PUT/PATCH edit declared writable metadata/spec fields. PATCH preserves absent, null and zero values. Lifecycle commands remain explicit and authorized. Clients send the ETag they read in If-Match; missing and stale preconditions remain 428 and 412. CLI read-to-apply conversion strips server-owned fields rather than weakening server validation.

Declare allowed scopes per kind. Limits/policies may span system, account and project scopes. System operation target references are distinct from ownership. Operator-addressable resources have scoped unique names. Generated/immutable records may use their UID as a generated name. Do not impose one global name index or tombstone policy on all resource kinds.

## Protocol boundaries

Tenant identity, personal profiles/sign-in methods, safe system record projections, product resources and nested resource snapshots are covered. A route's owner records its type, scope, writable/status fields, nested resources, ETag and secret handling beside its implementation. Mounted-route coverage tests must catch omissions, including product-owned system routes.

OAuth/OIDC, principal/bootstrap DTOs, directory candidates, health/summary queries, operation inputs, transport acknowledgements, SSE framing and binary content are explicit exceptions. They do not acquire fabricated resource identities. Lists remain `{items, limit, next?, total?}`; their resource-valued items use envelopes. Aggregate responses retain their query shape, with canonical nested resources.

One-time credentials use operation-result DTOs with a resource and an operation-only credential. They never enter ordinary resource reads/caches/history. Update REST, Go clients, both web clients, CLI JSON/YAML output/input and MCP resource outputs as one adoption unit. No flat MCP output mode is required.

## Storage, errors and retries

Use shared dbstore query/selector/page mechanisms with product-owned policy. Retain authorization, locks, audit/outbox writes, capacity rules and the existing transaction boundaries. Visibility applies to selection and totals before paging. Scope filtering alone is not authorization. Filter labels separately from canonical fields; remove pseudo-label aliases during product adoption. Cursor pagination is already implemented and is not redesigned here.

Use problem responses with accurate status, stable code, requestId and canonical field paths. Unknown internal errors return a safe generic 500. OAuth keeps its protocol errors. Adapt domain errors explicitly rather than losing field details or turning every failure into 400.

Preserve idempotent mutation semantics in the new format. Replays use the original key/body and redacted canonical response. Test nested camelCase credentials. Derive replay ownership from typed domain outcomes, not a flat response `id`. No old replay/snapshot decoder or schema conversion is required.

## Delivery

Expected releases: root wyrd v0.7.0, identity/v0.7.0 and components v0.5.0. Product server/client adoption follows those publications. There is no additional identity default-switch release. Do not deploy a partially adopted product. Pin released artifacts and verify the downloaded release, not a local workspace replacement.

The first root PR supplies the registry, query composition, safe problem adapter and tested [JSON/YAML examples](../examples/m8/README.md). Identity and product PRs supply their concrete codecs, command mappings, complete route inventories and example updates. This ADR does not claim those adoptions are already implemented.
