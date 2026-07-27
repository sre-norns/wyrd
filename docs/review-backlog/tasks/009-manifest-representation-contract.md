# 009: Preserve Complete Manifests Across Supported Representations

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Manifests |
| Depends on | — |
| Likely conflicts | 005, 010, 013 |
| Owner | Unclaimed |

## Why This Matters

The manifest types present JSON, YAML, and HTTP response representations as one
Resource contract, but custom encoders omit embedded response links and enforce
different strictness. A successful round trip can therefore lose
hypermedia/navigation data, while a known-Kind factory failure is
misrepresented as an unknown Kind and silently preserved.

Representation bugs are especially costly because they cross package and
process boundaries: once a field is omitted from an API response, a caller
cannot recover it from the in-memory object.

## Architecture Assessment

Custom marshal methods currently mix domain shape, Kind construction, and
representation policy. Deepen a manifest Codec Module with one canonical wire
model and explicit JSON/YAML Adapters. Kind construction belongs behind the
Registry Interface from task 010; Bark response negotiation should select a
codec rather than redefining manifest shape.

JSON and YAML are two real Adapters and justify the seam. XML should only remain
advertised if it passes the same losslessness and validation contract.

## Evidence

- `pkg/manifest/meta.go:243-255`: `ResourceManifest.MarshalJSON` constructs a
  reduced anonymous value that omits embedded `HResponse` links.
- `pkg/manifest/meta.go:332-344`: the YAML marshaler repeats the reduced shape.
- `pkg/manifest/meta.go:257-289`: JSON decoding rejects unknown fields, while
  YAML decoding does not implement the same strictness.
- `pkg/manifest/meta.go:271-278`: any registered factory error is treated as an
  unknown Kind and returned without error.
- `pkg/manifest/model.go:63-85`: model-to-manifest conversion copies Kind,
  metadata, spec, and status but drops `APIVersion` and `HResponse`; it also
  calls `MustKnowKindOf` from a function not named as a panic-capable helper.
- `pkg/manifest/model.go:87-162`: manifest-to-model conversion drops
  `HResponse`, silently accepts a missing registered spec as the Go zero value,
  and reports `SpecType` as the expected type when status conversion fails.
- `ErrNilSpec` and `ErrNilStatus` exist but are not used to define required-field
  behavior.
- `pkg/manifest/links.go:5`: `HLink.Ref` has a malformed YAML struct tag,
  `yaml,omitempty:"ref"`.
- Bark negotiates XML alongside JSON and YAML, but manifest XML behavior is not
  defined by equivalent round-trip tests.
- Unknown Kinds are intentionally retained as raw manifests and must remain
  lossless.

## Failure Sequence

1. A Resource is returned with `self`, `next`, or related links in `HResponse`.
2. Bark selects JSON or YAML.
3. The custom manifest marshaler emits only type metadata and the Resource.
4. The client receives no links even though the in-memory response contains
   them.

Separately, a transient factory/configuration error can be swallowed as if the
Kind were merely unregistered.

## Required Outcome

- One documented wire contract defines all serialized manifest fields.
- JSON and YAML preserve type metadata, Resource data, links, zero values, and
  unknown-Kind payloads without accidental loss.
- Known-Kind construction errors retain their cause and are returned.
- Model/manifest conversion preserves all applicable type metadata and semantic
  links, and fallible Kind lookup is exposed as an error unless the function is
  explicitly named `Must...`.
- Presence requirements for spec and status are explicit; missing required data
  cannot silently become a Go zero value.
- Unknown Kind is a distinct, intentional outcome with a lossless raw payload.
- Strictness for unknown fields is consistent or explicitly representation-
  specific and documented.
- Every advertised media type has equivalent contract coverage; unsupported
  types are not advertised.
- Struct tags are valid and stable.

## Implementation Options and Trade-offs

### Preferred: Canonical Wire DTO plus Explicit Codecs

Convert manifests to/from a private canonical wire value, then let JSON and YAML
codecs encode that value. Codecs receive an explicit Kind factory/Registry.
This removes duplicated shape definitions and isolates representation-specific
parsing.

### Alternative: Repair Custom Marshalers in Place

Add omitted fields and duplicate strict decoding rules in each marshaler. This
is smaller initially, but the representations remain separate sources of truth
and are likely to drift again.

## Implementation Constraints

- Preserve unknown Kinds and their raw spec/status content.
- Do not expose Go concrete type names in the wire format.
- Avoid recursive calls to custom marshalers through aliases.
- Define compatibility for previously omitted link fields as additive.
- Coordinate link ownership with tasks 013 and 019.

## Suggested Implementation Sequence

1. Add golden and semantic round-trip tests for every representation.
2. Separate unknown Kind from factory failure.
3. Define the canonical wire value including links.
4. Implement JSON and YAML Adapters and decide XML support explicitly.
5. Document media types and compatibility.

## Non-Goals

- Changing Resource Kind names.
- Introducing protobuf or another representation.
- Moving HTTP link ownership across packages; that is task 019.

## Acceptance Criteria / Definition of Done

- [ ] JSON and YAML round trips preserve every contract field.
- [ ] Links are visible in encoded responses.
- [ ] Unknown Kinds remain lossless.
- [ ] Known-Kind factory errors are not swallowed.
- [ ] Advertised media types exactly match supported codecs.
- [ ] Strictness and compatibility behavior are documented.

## Required Tests

- Known and unknown Kinds with populated and zero-valued spec/status.
- Model ↔ manifest conversions with APIVersion and links, including an
  unregistered generic type that must return rather than panic.
- Missing spec/status and status type mismatch with correct expected-type
  diagnostics.
- All link relations and valid YAML tags.
- Unknown fields in every representation.
- Factory returns typed/sentinel error and error identity is preserved.
- Encode → decode → encode semantic equivalence.
- Media negotiation through Bark for every advertised type.

## Validation

```sh
go test -race -count=1 ./pkg/manifest ./pkg/bark
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
