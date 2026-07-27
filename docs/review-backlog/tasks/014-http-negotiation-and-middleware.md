# 014: Complete HTTP Negotiation and Middleware Contracts

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | HTTP |
| Depends on | 013 |
| Likely conflicts | 009, 012, 013 |
| Owner | Unclaimed |

## Why This Matters

Bark advertises content negotiation and composable middleware, but its Accept
parser does not implement list/quality semantics, one API helper continues after
binding failure, names conflate request `Content-Type` with response `Accept`,
and behavior depends on undocumented middleware order. Request bodies also lack
an explicit size boundary.

These gaps turn ordinary interoperability mistakes into incorrect responses,
double work, or avoidable memory/CPU exposure.

## Architecture Assessment

Build negotiation as a pure Module: parse an Accept list into weighted media
ranges, select among codecs, and return a classified not-acceptable result.
Gin middleware should be thin Adapters with explicit preconditions and
abort/return behavior. Compose them through a documented setup function when
ordering is mandatory.

This task should consume the response and codec Interfaces from tasks 013 and
009, not add another response path.

## Evidence

- Bark's Accept handling does not fully split comma-separated ranges or honor
  `q` weights and `q=0` exclusions.
- Parameter removal is based on simple string slicing rather than media-type
  parsing.
- `pkg/bark/http.go:230-248`: query-binding error handling aborts but can continue
  into subsequent handler logic because it does not return immediately.
- `pkg/bark/manifest.go:72-80`: `ResourceAPI` uses Gin's write-on-error
  `BindUri`, then tries to write a second Bark error/status for the same failure.
- `AcceptContentTypeAPI` is named like response negotiation while validating
  the request's `Content-Type`.
- Version/resource-ID middleware relies on ordering; a missing earlier step can
  leave no versioned identifier.
- Server-sent event code sets hop-by-hop `Connection` and `Transfer-Encoding`
  headers manually, which is not valid behavior to force under HTTP/2.
- Request-binding paths do not consistently establish maximum body size.

## Failure Sequence

1. A client sends `Accept: application/json;q=0, application/yaml;q=1`.
2. The simple parser sees JSON first and selects it.
3. Bark emits a representation the client explicitly rejected.

Separately, a malformed query can be aborted and then still reach Store work,
causing side effects or a second write attempt.

## Required Outcome

- Accept parsing supports comma-separated media ranges, wildcards, parameters,
  quality weights, stable specificity rules, and `q=0` exclusion.
- Unsupported response types produce 406; unsupported request bodies produce
  415.
- Middleware names distinguish request content-type validation from response
  representation negotiation.
- Binding failures abort and return before downstream work.
- Middleware uses non-writing bind/parse operations so the configured Responder
  owns the only failure write.
- Request body limits are explicit, configurable, and enforced before decoding.
- Required middleware ordering is removed where possible and otherwise
  assembled/validated in one documented composition point.
- SSE leaves connection framing to `net/http` and behaves under HTTP/1.1 and
  HTTP/2.

## Implementation Options and Trade-offs

### Preferred: Pure Negotiator and Composed Middleware Stack

Use `mime.ParseMediaType` around a dedicated weighted-list parser, then select
from codecs supplied by the task 013 Responder. Expose a constructor that
installs dependent middleware in valid order and rejects incomplete
configuration.

### Alternative: Adopt a Maintained Negotiation Library

Use a focused RFC-compliant library behind Wyrd's narrow Interface. This
reduces parser maintenance but requires dependency and behavior review,
especially around tie-breaking and wildcard defaults.

## Implementation Constraints

- Selection must be deterministic for equal quality/specificity.
- Do not claim XML support unless task 009 validates the codec.
- Body-size failures must not expose partial decoder internals.
- Do not set hop-by-hop headers manually.
- Preserve old middleware names as deprecated wrappers where practical.

## Suggested Implementation Sequence

1. Add RFC-style Accept tables and middleware short-circuit tests.
2. Implement pure negotiation against configured codecs.
3. Split request and response content middleware names/contracts.
4. Add bounded request decoding and composed middleware setup.
5. Exercise SSE over HTTP/1.1 and HTTP/2 test servers.

## Non-Goals

- Content transcoding.
- WebSocket lifecycle support.
- Application-specific versioning policy.

## Acceptance Criteria / Definition of Done

- [ ] Media selection honors quality, exclusion, specificity, and wildcards.
- [ ] 406 and 415 are used for the correct protocol failures.
- [ ] Binding failure cannot run downstream handlers.
- [ ] Request bodies are bounded before decode.
- [ ] Middleware ordering is explicit and tested.
- [ ] SSE tests pass over HTTP/1.1 and HTTP/2.

## Required Tests

- Multi-value Accept headers, invalid `q`, `q=0`, wildcards, parameters, ties,
  absent Accept, and unsupported formats.
- Request `Content-Type` with parameters and unsupported types.
- Malformed query/body proving no Store/handler invocation.
- Malformed URI parameters proving exactly one status/header/body write.
- Body exactly at and above the configured limit.
- Missing/reordered middleware.
- HTTP/1.1 and HTTP/2 SSE headers and flushing.

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
