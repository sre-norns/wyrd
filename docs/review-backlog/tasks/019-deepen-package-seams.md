# 019: Reduce Package Coupling and Optional Dependency Cost

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P2` |
| Workstream | Architecture |
| Depends on | 001–018 |
| Likely conflicts | all package-interface tasks |
| Owner | Unclaimed |

## Why This Matters

Core manifest types import persistence concerns, webhook delivery imports Bark
and therefore Gin for header constants, and dbstore unconditionally imports all
database drivers. Consumers pay dependency, build, binary, and sometimes CGO
cost for capabilities they do not use. HTTP links embedded in manifest types
also blur whether a Resource is a domain value or a transport response.

Changing package boundaries before behavior is stable would cause churn. After
tasks 001–018 define the contracts, these seams can be deepened deliberately.

## Architecture Assessment

The desired dependency direction is:

```text
manifest core
    ↑
storage contracts ← dbstore/GORM + selected dialect adapters
    ↑
application composition
    ↓
bark HTTP adapters     webhooks HTTP delivery     grace process lifecycle
```

Core Modules should not import outer Adapters. Keep an Interface only when at
least two real implementations/consumers exist: explicit/default Registry,
JSON/YAML codecs, HTTP/fake webhook callers, and selected database dialects all
meet that test. Avoid inventing generic repository/transport Interfaces merely
to make the diagram symmetric.

## Evidence

- `pkg/manifest` imports GORM for persistence-facing model behavior.
- `pkg/dbstore` imports SQLite, Postgres, and MySQL drivers unconditionally.
- `pkg/webhooks` imports `pkg/bark` for generic HTTP header names, transitively
  importing Gin.
- `HResponse`/links are embedded in manifest response types, mixing HTTP
  navigation semantics into the core Resource model.
- `manifest.Model` declares getters that the generic `ResourceModel` and
  `StatefulResource` types do not implement; it is used mainly by iterator test
  fakes while Store itself accepts `any`.
- `pkg/dbstore/README.md` is effectively a placeholder, so package ownership and
  intended seams are not documented.
- Root examples compose packages successfully, demonstrating that stable
  compatibility Adapters are needed during movement.
- Urth and Comserv are active downstream consumers of Wyrd v0.2.2 behavior.

## Failure Sequence

1. A CLI imports manifest only to validate and encode Resources.
2. The manifest/storage coupling and unconditional driver graph pull in GORM and
   database drivers it never opens.
3. Builds are slower/larger and can inherit platform/CGO constraints unrelated
   to the CLI's use case.

The inverse coupling makes core changes harder because transport and persistence
concerns share types and hooks.

## Required Outcome

- Manifest core has no GORM dependency.
- Persistence model/hooks live in dbstore or a dedicated storage Adapter.
- Database drivers are opt-in packages or explicit constructors so consumers
  import only supported dialects they use.
- Webhooks uses `net/http` directly and has no Bark/Gin dependency.
- HTTP link/response ownership is either moved to Bark or represented by a
  transport-neutral value with a documented reason.
- Public Interfaces stay small, behavior-oriented, and backed by at least two
  real uses.
- The obsolete/inconsistent `manifest.Model` contract is either implemented for
  a demonstrated consumer or removed in favor of the narrow values actually
  required.
- Compatibility aliases/wrappers and a pre-v1 migration plan protect Urth and
  Comserv.
- Package documentation states ownership, dependencies, and extension points.

## Implementation Options and Trade-offs

### Preferred: Incremental Adapter Extraction

Move GORM models/hooks to dbstore, split dialect registration into subpackages,
remove trivial cross-package constant imports, and let Bark wrap manifests with
links. Preserve old constructors/types through deprecated aliases and measure
the import/binary impact at each step.

### Alternative: New v1 Module Layout

Design clean v1 packages and migrate consumers in one coordinated change. This
can eliminate compatibility scaffolding but creates a large flag day and makes
behavioral regressions harder to isolate.

## Implementation Constraints

- Complete behavior-defining tasks 001–018 first.
- Do not use import cycles or duplicate domain types to force separation.
- Do not hide driver imports behind blank imports in the core package.
- Preserve database schema and wire compatibility unless separately migrated.
- Validate downstream Urth and Comserv before removing any Adapter.

## Suggested Implementation Sequence

1. Record the target dependency direction and compatibility plan in an ADR.
2. Remove Bark from webhooks and measure the graph.
3. Extract GORM models/hooks from manifest with compatibility aliases.
4. Make SQL driver selection explicit.
5. Resolve link ownership after manifest/Bark codec contracts stabilize.
6. Update package docs and migrate downstream consumers.

## Non-Goals

- Replacing GORM, Gin, or `net/http`.
- Creating an Interface for every package.
- Rewriting all packages into one framework.

## Acceptance Criteria / Definition of Done

- [ ] Core-to-adapter dependency direction matches the documented graph.
- [ ] Manifest can build/test without GORM and database drivers.
- [ ] Webhooks can build/test without Gin/Bark.
- [ ] Consumers select only needed SQL drivers.
- [ ] Every new Interface passes the one-adapter hypothetical/two-adapter real
  test.
- [ ] Urth and Comserv migration validation is recorded.
- [ ] Package documentation and ADRs match the implementation.

## Required Tests

- `go list -deps` assertions or architecture checks for forbidden edges.
- Build/test minimal manifest-only and webhook-only consumer fixtures.
- Selected-driver integration tests.
- Schema/wire compatibility fixtures before and after extraction.
- Urth and Comserv compile/test against the migration branch.
- Binary/dependency comparison recorded as evidence, not a hard arbitrary goal.

## Validation

```sh
go list -deps ./pkg/manifest ./pkg/webhooks
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
