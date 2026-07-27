# 018: Define Bounded and Observable Webhook Delivery

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Delivery |
| Depends on | — |
| Likely conflicts | 009, 019, 020 |
| Owner | Unclaimed |

## Why This Matters

The webhook client accepts invalid construction, can panic on a nil HTTP client,
recognizes only selected success statuses, and discards the response information
needed to decide whether a failure is retryable. Bodies are closed without
being drained, reducing connection reuse under load. There are no package tests.

Downstream Comserv logs failed delivery and loses the event. Blind retries would
be equally unsafe without a stable delivery identity, because receivers may
apply the same event twice.

## Architecture Assessment

Make one HTTP delivery attempt a deep Module: validate target/configuration,
encode the event, enforce deadline/body bounds, accept protocol success, and
return a typed causal result containing status and retry hints. Durable
scheduling/outbox behavior belongs to the composing application, but this
Module must provide enough classification for it.

The existing caller Interface remains a useful injection seam downstream. The
default HTTP implementation and test fake are two real Adapters.

## Evidence

- `pkg/webhooks/caller.go:17-27`: `NewHTTPCaller` can be created with a nil HTTP client;
  use then dereferences it.
- The constructor returns an error value even though it performs little or no
  validation.
- Target URL assembly does not require a supported `http`/`https` scheme and
  valid host.
- Success handling lists selected status codes rather than accepting the full
  2xx class.
- Response bodies are closed but not drained to a bounded discard before close,
  preventing reliable keep-alive reuse.
- Errors do not retain HTTP status, bounded response excerpt, `Retry-After`, or
  retryability classification.
- `pkg/webhooks` imports Bark for generic header constants, pulling Gin into a
  delivery package.
- `ResourceDiff` lacks stable tags and `Principal`/`Principle` naming is
  inconsistent.
- `WebhookSpec.Schema` models a URL scheme under the wrong term, making the
  public/wire contract harder to understand and migrate safely.
- `go test -cover ./pkg/webhooks` reports 0.0%.

## Failure Sequence

1. A receiver returns 204 or another valid 2xx status not in the hard-coded
   accepted list.
2. Wyrd reports delivery failure.
3. The caller cannot distinguish that result from a timeout or 503 because the
   error is unstructured.
4. The event is either lost or retried without a stable idempotency key.

## Required Outcome

- Construction validates/injects a non-nil client, absolute target URL,
  supported scheme, host, and explicit timeout policy.
- Every 2xx response is success; redirects follow an explicit safe policy.
- Failure returns a typed causal error with operation, target identity (without
  secrets), status, retryability, parsed `Retry-After`, and a bounded sanitized
  body excerpt.
- Response bodies are drained only to a safe bound and always closed.
- Events have stable serialization tags and a caller-supplied or generated
  delivery ID suitable for receiver deduplication.
- URL configuration uses correct terminology (`scheme`) with a wire-compatible
  migration from `schema`.
- The one-attempt caller does not perform hidden retries.
- Durable retry/outbox guidance documents which classified outcomes are safe to
  reschedule.
- Webhooks no longer imports Bark just for header constants.

## Implementation Options and Trade-offs

### Preferred: Typed One-Attempt `HTTPCaller`

Keep retries outside the transport. Configure a concrete caller with validated
target, client, clock/body bounds, and redirect policy. Return `DeliveryError`
with `Unwrap` and retry metadata. Applications can place stable events in a
transactional outbox and use the typed result to schedule another attempt.

### Alternative: Retry-Capable Caller

Implement bounded exponential retries inside the caller. This reduces caller
code but hides latency, cannot make event persistence atomic, and is unsafe
without receiver idempotency. Only consider it as an explicit wrapper around the
one-attempt Interface.

## Implementation Constraints

- Never include URL userinfo, tokens, or unbounded remote bodies in errors.
- Respect request context cancellation and client deadlines.
- Do not retry POST automatically without a stable delivery ID and explicit
  policy.
- Limit draining and diagnostic body reads.
- Maintain compatibility for existing event fields with corrected tags/names.

## Suggested Implementation Sequence

1. Add `httptest` tables for every 2xx class, failures, timeout, redirect, and
   malformed configuration.
2. Define stable event and typed delivery-result contracts.
3. Validate construction and implement bounded body lifecycle.
4. Remove Bark dependency and document retry classification.
5. Add a downstream outbox integration example/contract without embedding it
   in the transport.

## Non-Goals

- Building a durable queue inside Wyrd.
- Exactly-once delivery over HTTP.
- Signing/authentication policy beyond configurable headers or RoundTripper.

## Acceptance Criteria / Definition of Done

- [ ] Invalid caller configuration fails at construction.
- [ ] All and only 2xx responses are transport success.
- [ ] Failure status/retry hints remain causal and inspectable.
- [ ] Bodies are bounded, drained safely, and closed.
- [ ] Events carry stable delivery identity and wire tags.
- [ ] No hidden retry occurs.
- [ ] Webhook package has meaningful contract coverage and no Bark dependency.

## Required Tests

- Every 2xx family boundary plus representative 3xx/4xx/5xx.
- Nil client, malformed/relative/non-HTTP URLs, and URL credential redaction.
- Timeout and caller cancellation.
- Seconds/date/invalid `Retry-After`.
- Very large/streaming error bodies and connection reuse.
- Redirect policy and cross-host credential behavior.
- Event JSON golden files and delivery ID propagation.
- Legacy `schema` and corrected `scheme` configuration compatibility.

## Validation

```sh
go test -race -count=1 ./pkg/webhooks
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
