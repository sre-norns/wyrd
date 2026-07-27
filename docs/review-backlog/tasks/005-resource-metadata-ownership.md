# 005: Enforce Server-Owned Resource Metadata at the Write Seam

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P0` |
| Workstream | Mutations |
| Depends on | 001 |
| Likely conflicts | 001, 009, 011 |
| Owner | Unclaimed |

## Why This Matters

Object Metadata documents UID, Resource Version, timestamps, and deletion state
as system-populated and read-only. HTTP decoding, manifest-to-model conversion,
and Store creation currently copy those values without a mandatory validation or
sanitization step. A client can attempt to choose identity, fake a version or
timestamp, or submit deletion state.

Metadata initialization is also inconsistent: `ResourceModel` supplies a
default name from UID, while `StatefulResource` relies only on embedded hooks.
Validation allows an empty name and is never automatically invoked by Bark or
dbstore.

## Architecture Assessment

Metadata invariants exist as comments, optional `Validate` methods, and GORM
hooks spread across the manifest and Store implementations. Callers must remember
which fields to clear and when to validate. The current Modules are shallow
because the Interface accepts values that violate its own invariants.

Put create/update preparation behind the authoritative mutation seam from task
001. A deep metadata Module should distinguish client input from persisted
Resource state, validate once, assign server fields, and produce the exact value
the Store may write.

## Evidence

- `pkg/manifest/meta.go:160-190`: UID, Resource Version, and timestamps are
  documented as system-populated/read-only but remain normal serializable fields.
- `pkg/manifest/meta.go:193-203`: GORM hooks assign UID and increment version,
  but do not reject client values.
- `pkg/manifest/meta.go:212-229`: validation is optional and explicitly allows
  an empty name.
- `pkg/manifest/model.go:87-90,113-116`: manifest-to-model conversion copies all
  Object Metadata.
- `pkg/manifest/model.go:164-173`: only `ResourceModel` has the extra
  name-from-UID initialization.
- Caller-defined types such as `type Pet manifest.ResourceModel[PetSpec]` and
  `type Webhook manifest.ResourceModel[WebhookSpec]` do not inherit
  `ResourceModel.BeforeCreate`; common use therefore loses that extra default
  while retaining promoted `ObjectMeta` behavior.
- `pkg/bark/manifest.go:40-60`: request middleware parses a full ResourceManifest
  and stores it without metadata validation.
- `pkg/dbstore/dbtransaction.go:26-29`: Store creation passes the value directly
  to GORM.

## Failure Sequence

1. A create request includes a chosen UID, high Resource Version, CreatedAt, and
   a valid DeletedAt value.
2. Bark decodes and the model conversion copies the metadata.
3. dbstore persists fields that the public Interface calls server-owned, or
   fails with a database-specific collision instead of a client validation
   outcome.

## Required Outcome

- Create input cannot control UID, Resource Version, timestamps, or deletion
  state. The documented policy is to reject non-zero server fields so mistakes
  are visible.
- Update input carries concurrency intent separately from mutable Resource
  content; it cannot rewrite identity or system timestamps.
- Resource name and labels are validated at the write seam.
- Empty-name behavior is one rule for stateless and stateful Resources.
  Preserve the existing generated-name behavior for compatibility, but perform
  it in one place and document it.
- Metadata behavior must not depend on whether a consumer uses the generic type
  directly or declares a named model from its underlying type.
- UID generation and initial Resource Version happen exactly once.
- Every successful mutation advances Resource Version according to task 001;
  failed validation/conflict does not.
- Validation errors identify fields and are safe to return to clients.

## Implementation Options and Trade-offs

### Preferred: Separate Input and Persisted Metadata

Introduce create/update input types that contain only caller-owned metadata and
map them to persisted Object Metadata through one preparation Module. Keep
wire-compatible decoding adapters during migration. This is the strongest
Interface: invalid ownership is difficult to represent.

### Alternative: Central Sanitizer/Validator

Retain current structs but require `PrepareCreate`/`PrepareReplace` at every
Store write seam and reject non-zero protected fields. This is less type-safe but
can be introduced compatibly. GORM field-permission tags may add defense in
depth, but cannot be the only enforcement because they often ignore rather than
report bad input.

## Implementation Constraints

- Do not silently trust client UID/version because a caller bypasses Bark.
- Do not put HTTP-specific error types in manifest or dbstore.
- Preserve unknown-Kind round trips; ownership applies when persistence is
  requested, not when a manifest is merely decoded.
- Use task 011's canonical validation errors.

## Suggested Implementation Sequence

1. Add create tests with every protected metadata field set.
2. Specify and document generated-name compatibility.
3. Add client-input/preparation types or the central validator.
4. Integrate it with the authoritative mutation Module.
5. Adapt Bark to map safe validation errors without leaking Store details.

## Non-Goals

- Authentication/authorization of which principal may mutate a Resource.
- Namespaces or multi-tenancy.
- User-defined defaulting/admission webhooks.

## Acceptance Criteria / Definition of Done

- [ ] Client input cannot persist protected metadata.
- [ ] Stateful and stateless Resources initialize metadata identically.
- [ ] Names/labels are validated before SQL.
- [ ] Version changes only on successful writes.
- [ ] HTTP and direct Store callers observe the same domain validation outcome.
- [ ] Compatibility and migration behavior is documented.

## Required Tests

- Create with each protected field individually and together.
- Empty name for ResourceModel and StatefulResource.
- Empty name through direct generic values and caller-defined named model types.
- Client-provided UID collision does not expose database details.
- Invalid name/labels never execute SQL.
- Replacement cannot change UID/CreatedAt/DeletedAt directly.
- Direct Store and Bark integration tests return equivalent safe outcomes.

## Validation

```sh
go test -race -count=1 ./pkg/manifest ./pkg/dbstore ./pkg/bark
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
