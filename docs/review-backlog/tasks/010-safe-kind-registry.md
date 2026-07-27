# 010: Replace the Global Kind Map with a Safe Registry Module

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Manifests |
| Depends on | — |
| Likely conflicts | 009, 011, 019 |
| Owner | Unclaimed |

## Why This Matters

Kind registration is a mutable package-global map used by codecs and
reflection. Concurrent registration/lookup races, tests leak registrations into
one another, typed nil exemplars panic, and reverse lookup scans the complete
map. A library consumer cannot create an isolated registry for a plugin,
tenant, or test.

The current Interface is convenient but shallow: global state, reflection
rules, uniqueness policy, and lookup errors are inseparable.

## Architecture Assessment

Create a `Registry` Module that owns both Kind-to-factory and type-to-Kind
indexes, validation, and concurrency policy. The existing package-level
functions become a default-registry Adapter for compatibility. Explicit
registries used by codecs and isolated tests are a second real use case, so this
is not a speculative seam.

A build-then-freeze registry is the deepest model when kinds are static; a
copy-on-write snapshot is preferable if runtime extension is required. Choose
one and document the lifecycle.

## Evidence

- `pkg/manifest/meta.go:15-16`: Kind metadata is stored in a mutable global map
  with no synchronization.
- `pkg/manifest/meta.go:18-51`: public register, lookup, and unregister
  operations mutate/read that map directly.
- `pkg/manifest/meta.go:23-31`: exemplar reflection dereferences pointer types
  without rejecting a typed nil pointer.
- `pkg/manifest/meta.go:127-132`: `KindOf` follows the same nil-sensitive
  reflection path.
- Reverse lookup iterates registrations instead of using a reverse index.
- Package tests depend on global registration ordering and cleanup.

## Failure Sequence

1. One goroutine decodes a manifest and looks up its Kind.
2. Another goroutine registers or unregisters a test/plugin Kind.
3. The unsynchronized map is read and written concurrently, causing a race or
   runtime panic.

A typed nil exemplar reaches reflection and panics rather than returning a
validation error.

## Required Outcome

- Registry operations are race-free with a documented mutation lifecycle.
- Registration validates nil/typed-nil exemplars, Kind syntax, duplicate Kind,
  and duplicate Go type.
- Kind-to-factory and type-to-Kind lookups are direct and consistent.
- Lookups distinguish absent registrations from invalid registry state and
  factory failures.
- Codecs can depend on an explicit registry/factory.
- Existing package-level functions delegate to a default registry during a
  compatibility period.
- Tests can construct isolated registries without global cleanup.

## Implementation Options and Trade-offs

### Preferred: Immutable Snapshot Registry

Use a builder that validates both indexes, then freeze the result for lock-free
lookups. Runtime extension creates a new snapshot and atomically publishes it
only where explicitly needed. This makes read behavior simple and deterministic.

### Alternative: Mutex-Protected Mutable Registry

Guard bidirectional maps with `sync.RWMutex`. This preserves runtime register and
unregister semantics with less API migration, but allows behavior to change
during decoding and requires careful callback/lock boundaries.

## Implementation Constraints

- Never invoke a caller factory while holding a registry lock.
- Do not derive public Kind identity from package paths or type strings.
- Do not let the reverse index diverge on failed registration/unregistration.
- Preserve default global behavior long enough to migrate downstream users.
- Keep factory construction errors causal.

## Suggested Implementation Sequence

1. Add typed-nil, duplicate, isolated, and concurrent registry tests.
2. Introduce explicit Registry and bidirectional indexes.
3. Adapt codecs and reflection helpers to registry methods.
4. Delegate package-level functions to the default Registry.
5. Deprecate global mutation and document the lifecycle.

## Non-Goals

- Dynamic network-based schema discovery.
- Loading Go plugins.
- Changing serialized Kind names.

## Acceptance Criteria / Definition of Done

- [ ] Race tests pass under concurrent lookup and allowed mutation.
- [ ] Typed nil and duplicate registrations return descriptive errors.
- [ ] Reverse lookup is indexed and deterministic.
- [ ] Isolated registries require no global cleanup.
- [ ] Codecs accept an explicit Registry/factory.
- [ ] Compatibility wrappers and deprecation path are documented.

## Required Tests

- Nil interface, typed nil pointer, pointer/value, and invalid exemplar inputs.
- Duplicate Kind and duplicate concrete type.
- Factory error identity and no lock held during factory invocation.
- Parallel lookup/register behavior permitted by the chosen lifecycle.
- Default-registry compatibility and isolated-test independence.
- `go test -race -count=100 ./pkg/manifest`.

## Validation

```sh
go test -race -count=100 ./pkg/manifest
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
