# 012: Make Bearer Extraction Total and Standards-Compliant

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | HTTP |
| Depends on | — |
| Likely conflicts | 013, 014 |
| Owner | Codex |

## Why This Matters

A client-controlled `Authorization` header can panic the request path. Other
malformed forms are accepted ambiguously, and authentication failures do not
consistently provide the standard challenge response. This is a remotely
triggerable availability and security-boundary defect.

Bearer parsing is small enough to be total: every byte sequence should produce
either a validated token or a non-sensitive classified error, never a panic.

## Architecture Assessment

Separate a pure authorization-header parser from Gin middleware. The parser is
the deep Module: it owns scheme comparison, token presence, whitespace grammar,
and error categories. Middleware is an Adapter that supplies headers, maps
errors to a 401 response, and stores the validated token/principal.

This seam already has two real consumers: direct parsing tests/other HTTP
stacks and Gin middleware.

## Evidence

Baseline evidence, now resolved:

- At the review baseline, `pkg/bark/http.go:263-276` used `strings.SplitN` and
  indexed the second element without first proving it existed.
- A header containing exactly `Bearer` therefore reaches an out-of-range panic.
- Empty tokens can be admitted depending on whitespace shape.
- Scheme matching is case-sensitive even though HTTP authentication schemes are
  case-insensitive.
- Authentication failure responses do not consistently set an appropriate
  `WWW-Authenticate: Bearer` challenge.
- Baseline Bark coverage was 19.4%, and malformed header space was not
  exercised.

## Failure Sequence

1. An unauthenticated client sends `Authorization: Bearer`.
2. Middleware splits the string into one part.
3. The parser indexes `parts[1]`.
4. Gin's recovery path handles a panic instead of a normal 401, consuming extra
   resources and polluting error telemetry.

## Required Outcome

- Parsing is total for arbitrary header bytes and cannot panic.
- The Bearer scheme is matched case-insensitively.
- Missing header, wrong scheme, missing token, extra fields, and malformed token
  are distinct internal errors with one safe external authentication response.
- Empty or whitespace-only tokens are rejected.
- The token value and full header are never included in client errors or logs.
- Middleware returns 401 and an appropriate `WWW-Authenticate` challenge.
- Successful extraction preserves the token exactly as permitted by the chosen
  Bearer token grammar.

## Implementation Options and Trade-offs

### Preferred: Pure `ParseBearer` plus Thin Middleware

Implement an allocation-conscious parser using explicit whitespace/token
grammar and `strings.EqualFold` for the scheme. Return sentinel/typed parse
errors. Gin middleware maps all parse failures to one public 401 while retaining
safe internal categories for metrics.

### Alternative: Standards Library or Maintained Auth Middleware

Adopt a focused, maintained Bearer-auth parser if it matches Wyrd's required
token and error contract. This reduces local security code but adds a dependency
and still requires safe middleware mapping.

## Implementation Constraints

- Do not echo tokens or raw authorization headers.
- Do not accept a comma-separated credential list as one token.
- Decide and document whether tabs are accepted as HTTP optional whitespace.
- Keep authentication (token validity) outside syntactic extraction.
- Coordinate response mapping with task 013.

## Suggested Implementation Sequence

1. Add the one-part `Bearer` panic regression and a fuzz target.
2. Write a table for valid and invalid whitespace/scheme/token forms.
3. Implement the pure parser.
4. Adapt middleware to challenge and abort exactly once.
5. Review logs and errors for credential disclosure.

## Non-Goals

- JWT validation.
- Token issuance, refresh, or revocation.
- Authorization decisions.

## Acceptance Criteria / Definition of Done

- [x] No header input can panic the parser or middleware.
- [x] Valid scheme casing and token forms are accepted.
- [x] All malformed forms return a safe 401 challenge.
- [x] Credentials never appear in error bodies or logs.
- [x] The parser has a fuzz regression corpus.
- [x] Middleware aborts without executing downstream handlers.

## Required Tests

- Missing, empty, `Bearer`, `Bearer `, wrong scheme, mixed-case scheme, multiple
  spaces/tabs, extra fields, commas, Unicode, and control bytes.
- Successful middleware context propagation.
- `WWW-Authenticate` header and exactly one response write.
- Fuzz arbitrary strings for no panic and no accepted empty token.
- Log/error capture proving token redaction.

## Validation

```sh
go test -race -count=1 ./pkg/bark
go test -fuzz=FuzzParseBearer -fuzztime=30s ./pkg/bark
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:** Added the allocation-conscious `ParseBearer` Module,
  immutable classified `BearerParseError`, RFC 6750 `b64token` validation, and a
  thin Gin Adapter that returns one safe 401 challenge. Moved Bearer behavior
  from the mixed HTTP helper file into `pkg/bark/auth.go`.
- **Tests added/updated:** Added public parser and `httptest` coverage for the
  original panic, scheme casing, spaces, every error category, accepted token
  alphabet/padding, tabs, extra fields, commas, Unicode, controls, redaction,
  exactly-one response write, downstream short-circuiting, successful context
  propagation, and `FuzzParseBearer`.
- **Documentation updated:** Expanded `pkg/bark/README.md` with the accepted
  grammar, middleware response contract, parser contract, and authentication
  non-goals.
- **Compatibility/migration:** `AuthBearerAPI`, `RequireBearerToken`,
  `HTTPHeaderAuth`, and `ErrInvalidAuthHeader` remain available.
  `errors.Is(parseErr, ErrInvalidAuthHeader)` remains true. Inputs previously
  accepted only because they were malformed are now intentionally rejected;
  valid mixed-case schemes are newly accepted.
- **Validation evidence:** `go test -race -count=1 ./pkg/bark` passed;
  `go test -fuzz=FuzzParseBearer -fuzztime=30s ./pkg/bark` passed 349,157
  executions; `go test -race -count=1 ./...` passed; `go vet ./...` passed;
  `git diff --check` passed.
- **Follow-ups:** Task 013/014 own replacing panic-based context access and
  consolidating Bark response/error policy; this task preserves
  `RequireBearerToken` behavior for compatibility.
