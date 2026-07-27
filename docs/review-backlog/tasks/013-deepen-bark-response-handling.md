# 013: Deepen Bark Response and Error Handling

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | HTTP |
| Depends on | — |
| Likely conflicts | 007, 009, 012, 014 |
| Owner | Unclaimed |

## Why This Matters

Bark has parallel top-level and contextual response helpers that have already
drifted. One ignores the requested error status, created responses disagree
about `Location`, and raw internal errors are commonly classified as bad
requests and written to clients. This breaks protocol correctness and risks
leaking database or infrastructure detail.

Call sites also rely on `MustGet` middleware state, turning integration ordering
mistakes into request panics.

## Architecture Assessment

Response policy needs a deep `Responder` Module that owns public error shape,
status classification, representation selection, headers, and exactly-once
writing. A caller-supplied domain-neutral `ErrorMapper` is the seam between
application errors and HTTP; JSON/YAML codecs are real representation Adapters.
Package functions and contextual methods become compatibility wrappers over the
same implementation.

The internal cause belongs in logs/telemetry, not in the public response DTO.
The Interface test surface is observable HTTP output.

## Evidence

- `pkg/bark/contextual.go:37-39`: `AbortWithError` ignores its `code` argument
  and always emits HTTP 400.
- Bark's top-level and contextual response files duplicate status, encoding,
  link, and pagination behavior.
- The contextual created-response path does not provide the same `Location`
  behavior as the top-level helper.
- Generic errors are treated as HTTP 400 and `err.Error()` can become a
  client-visible message.
- Exported standard errors such as `ErrResourceNotFound` are mutable pointers;
  callers can change shared status/message/links and race with request handling.
- `MustGet`-style context helpers panic when expected middleware has not run.
- Error response fields lack an explicit, stable serialization contract.
- `StatusResponse.Ready`, pagination totals/count/data, and other meaningful
  zero values use `omitempty`, so false readiness or an empty collection can
  lose fields clients rely on.
- Bark currently has only 19.4% statement coverage.

## Failure Sequence

1. A handler passes an internal database error to a contextual helper with
   status 500.
2. The helper ignores 500, writes 400, and includes the raw error text.
3. Monitoring records a client fault while the client learns internal storage
   details and may avoid a valid retry.

## Required Outcome

- One response writer powers top-level and contextual helpers.
- Every supplied/mapped status is preserved unless invalid, in which case the
  call fails safely before writing.
- Public error bodies use stable tagged fields and safe messages/codes.
- Standard error templates are immutable values/factories; request-specific
  links or messages cannot mutate package-global state.
- Internal causes remain available for logging and tracing through `Unwrap`,
  but are never exposed by default.
- Error classification is explicit and injectable without importing
  application domain packages.
- Created responses consistently set a valid `Location` when one is available.
- Missing middleware state returns a classified internal error or safe fallback,
  not a panic.
- A response is written/aborted at most once.
- Success/error envelopes define whether meaningful zero and empty values are
  present, identically across supported representations.

## Implementation Options and Trade-offs

### Preferred: Configured `Responder` with Error Mapper

Build a `Responder` from representation codecs, a safe `ErrorMapper`, and an
internal error observer/logger. Retain current functions and contextual methods
as thin wrappers around a default Responder. This creates locality without
forcing all consumers to migrate at once.

### Alternative: Shared Private Functions Only

Deduplicate current helpers into private functions while keeping global policy.
This fixes immediate drift with less API work but still prevents applications
from defining causal error mapping cleanly.

## Implementation Constraints

- Never infer 4xx versus 5xx from arbitrary error text.
- Never write `err.Error()` publicly unless the error explicitly marks its
  message safe.
- Do not log/write a response twice when middleware and handlers both fail.
- Preserve valid existing wire shapes through versioned/additive changes.
- Reuse manifest codecs from task 009 rather than reimplementing serialization.

## Suggested Implementation Sequence

1. Add end-to-end `httptest` coverage for every current helper pair.
2. Define safe public `Problem`/error DTO and causal mapped error.
3. Implement one writer and a default conservative mapper.
4. Delegate top-level and contextual helpers.
5. Replace panic-based context access and document middleware requirements.

## Non-Goals

- Defining every application's domain-to-status policy in Bark.
- Pagination mechanics owned by task 007.
- Authentication parsing owned by task 012.

## Acceptance Criteria / Definition of Done

- [ ] Contextual and top-level helpers have identical observable behavior.
- [ ] Requested/mapped statuses are honored.
- [ ] Internal error detail is absent from public bodies by default.
- [ ] Causes remain inspectable internally.
- [ ] Created responses use consistent `Location` semantics.
- [ ] Missing context/middleware state cannot panic a request.
- [ ] Compatibility wrappers delegate to one implementation.

## Required Tests

- Every supported success/error status through both helper styles.
- Sentinel and wrapped application errors through custom/default mappers.
- Public-body redaction with internal cause capture.
- Invalid status, already-written response, and missing middleware state.
- Created `Location` with escaped path components.
- JSON/YAML response parity and stable field tags.
- False readiness, zero totals/counts, and empty result collections.
- Concurrent use and attempted customization of standard error templates.

## Validation

```sh
go test -race -count=1 ./pkg/bark ./pkg/manifest
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
