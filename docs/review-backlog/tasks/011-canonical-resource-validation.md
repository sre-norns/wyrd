# 011: Canonicalize Resource and Selector Validation

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Manifests |
| Depends on | — |
| Likely conflicts | 003, 005, 010 |
| Owner | Unclaimed |

## Why This Matters

Wyrd defines Kubernetes-like names, labels, selectors, and validation errors,
but several rules are incomplete and validation is not consistently enforced at
construction or persistence seams. Invalid metadata can enter memory and the
database, then fail later in routing, filtering, or downstream Kubernetes
integrations.

The current error aggregation also loses causal identity, making it difficult
for HTTP and CLI Adapters to classify a validation failure without parsing text.

## Architecture Assessment

Validation is a domain Module, not a collection of helper regexes. Centralize
canonical primitives for names, label keys/values, selector cardinality, and
object metadata. Return structured field errors that implement `error` and
support `errors.Is`/`errors.As`. Construction and write seams should compose
those primitives rather than reimplementing rules.

When Kubernetes semantics are intended, using the upstream validation package
is better leverage than maintaining a subtly different copy. A local Adapter
can keep Wyrd's stable error shape.

## Evidence

- `pkg/manifest/types.go:9-50`: local DNS/subdomain regex validation does not
  enforce all Kubernetes segment constraints and can admit malformed dotted
  names.
- `pkg/manifest/error_set.go`: `ErrorSet` aggregates text but does not expose
  causes through `Unwrap`.
- `pkg/manifest/selector.go:51-61`: `NewRequirement` validates the operator but
  not label-key syntax or operator-specific value cardinality.
- `pkg/manifest/selector.go:133-143`: formatting single-value operators indexes
  an arbitrary set value and assumes one exists.
- `StringSet` is map-backed, so public mutability and unordered traversal can
  make output nondeterministic unless every call sorts/copies.
- `pkg/manifest/string_set.go:34-49` returns map iteration order from
  `StringSet.Join`; `pkg/manifest/string_set_test.go:87-99` expects insertion
  order. A 100-count focused run produces multiple orders and fails.
- `ObjectMeta.Validate` permits empty metadata, and persistence/conversion paths
  do not consistently invoke it.
- `pkg/manifest/label_selector_test.go:112`: `TesLabelSelectorParsing` is
  misspelled, so a contradictory selector assertion is never executed.
- Directly constructed `ErrorSet` values can contain nil elements; formatting
  dereferences every element and can panic.

## Failure Sequence

1. A Resource with a locally accepted but Kubernetes-invalid name is persisted.
2. Its name is later used in a Kubernetes object, route, or selector.
3. The downstream boundary rejects it after storage and related work have
   already succeeded.

Programmatic selector construction can likewise create a wrong-cardinality
requirement that later panics while formatting.

## Required Outcome

- Resource names, DNS subdomains, label keys, and label values follow one
  documented canonical rule set.
- Selector operators enforce key syntax and exact value cardinality at
  construction.
- Validation errors carry field paths, stable categories, messages, and causal
  identity.
- Aggregate errors work with `errors.Is` and `errors.As`.
- Aggregate construction either filters or rejects nil causes and formatting is
  total.
- Validation is invoked at public construction/conversion and Store write seams
  where invalid state would otherwise become durable.
- Set/map inputs are defensively copied where immutability is promised, and
  externally visible formatting is deterministic.
- `StringSet.Join` has one explicit ordering contract; callers do not need to
  guess between it and `JoinSorted`.
- Disabled tests are repaired and made meaningful.

## Implementation Options and Trade-offs

### Preferred: Upstream Kubernetes Rules behind Wyrd Errors

Delegate compatible primitive checks to Kubernetes validation packages and map
their results to Wyrd's structured field errors. This maximizes correctness and
reduces duplicated policy while retaining a stable public Interface.

### Alternative: Complete the Local Validator

Implement every segment length, character, cardinality, and path rule locally.
This avoids another direct dependency but creates an ongoing parity burden in a
repository that already depends on Kubernetes APIs.

## Implementation Constraints

- Do not silently normalize invalid names or labels.
- Preserve multiple validation failures in deterministic field order.
- Avoid exposing upstream concrete error types as Wyrd's permanent API.
- Coordinate selector cardinality behavior with task 003.
- Coordinate server-owned field checks with task 005.

## Suggested Implementation Sequence

1. Repair the misspelled selector test and add canonical valid/invalid tables.
2. Define structured field and aggregate error contracts.
3. Wrap upstream primitive validators.
4. Enforce Requirement cardinality and immutable set handling.
5. Add validation to constructors, converters, and write seams.

## Non-Goals

- Schema validation of arbitrary user-defined spec fields.
- Automatic name/label repair.
- Authorization policy.

## Acceptance Criteria / Definition of Done

- [ ] Canonical Kubernetes-compatible fixtures pass.
- [ ] Invalid names, labels, and requirements cannot become durable.
- [ ] Validation errors expose stable field paths and categories.
- [ ] `errors.Is`/`errors.As` work through aggregates.
- [ ] Selector formatting cannot panic on constructed input.
- [ ] No test remains accidentally disabled by naming.

## Required Tests

- DNS/name boundary lengths, consecutive/leading/trailing dots and hyphens.
- Qualified label keys and all value edge cases.
- Every selector operator with zero, one, and multiple values.
- Deterministic aggregate ordering and `errors.Is`/`errors.As`.
- Nil entries in aggregate construction/formatting.
- Defensive-copy mutation tests.
- Repeated StringSet joining with a contract-consistent expected order.
- Constructor/converter/Store enforcement integration tests.

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
