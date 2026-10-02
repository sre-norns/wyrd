# System transport and host adoption

PR 03 provides the shared system transport required for identity/v0.7.0. This
matrix was audited against Urth `fc6afce` and Exp-Bench `aecce18`. Product changes
ship at the M8 product adoption gates with published dependency pins; these
unchanged checkouts are not yet canonical-wire compatible.

## Shared routes

All paths below have prefix `/v1/system`. Mount with `httpapi.MountSystem` after
`Mount`. `client.System()` has the listed methods. Lists use SystemPage; its
items are canonical resources, with cursor/limit/total preserved. Single resource
responses and mutation wrappers set the resource ETag. All routes require a live
system-scope session and entitlement; ownership declarations do not grant access.

| Route | Response / SDK | Input and precondition |
| --- | --- | --- |
| GET accounts; GET accounts/:id | SystemAccount / Accounts, Account | SystemQuery for list |
| GET accounts/:id/memberships | SystemMembership page / Memberships | SystemQuery |
| GET accounts/:id/invitations | SystemInvitation page / Invitations | SystemQuery |
| GET accounts/:id/impact-previews | ImpactPreview page / ImpactPreviews | SystemQuery |
| GET accounts/:id/owner-recoveries; GET owner-recoveries/:id | OwnerRecovery / OwnerRecoveries, OwnerRecovery | SystemQuery for list |
| GET deletion-requests; GET deletion-requests/:id | AccountDeletionRequest, nested AccountDeletionApproval / DeletionRequests, DeletionRequest | SystemQuery for list |
| GET activity; GET audit-events; GET changes | SystemActivity page / Activity, AuditEvents, Changes | SystemQuery; last two fix kind |
| GET configuration | SystemConfiguration / Configuration | Query DTO; no fabricated identity or ETag |
| POST accounts | `{resource: SystemAccount, ownerInvitationId, token?, emailDelivery?}` / CreateAccount | SystemAccountCreate; Idempotency-Key |
| POST accounts/:id/owner-invitations | `{resource: SystemInvitation, token?}` / CreateFirstOwnerInvitation | FirstOwnerInvitation; account ETag |
| POST account-invitations/:id/deliveries; POST account-invitations/:id/revocations | SystemInvitation / RequestInvitationDelivery, RevokeInvitation | SystemAction; invitation ETag |
| POST accounts/:id/impact-previews | ImpactPreview / CreateImpactPreview | SystemAction; preview captures current account/target version |
| PATCH accounts/:id | SystemAccount / ChangeLifecycle | SystemAction; account ETag, fresh preview, confirmation, reason |
| PATCH account-memberships/:id | SystemMembership / RevokeMembership | SystemAction; membership ETag and confirmation/preview |
| POST accounts/:id/owner-recoveries | `{resource: OwnerRecovery, token?}` / CreateOwnerRecovery | SystemAction; account ETag |
| PATCH owner-recoveries/:id | OwnerRecovery operation result / CompleteOwnerRecovery | SystemAction; recovery ETag |
| POST accounts/:id/step-up-authorizations | StepUpAuthorization / CreateStepUp | SystemAction; recent password assurance |
| POST accounts/:id/deletion-requests | AccountDeletionRequest / RequestDeletion | SystemAction; account ETag, preview, step-up, confirmation/recovery acknowledgement |
| POST deletion-requests/:id/approvals | AccountDeletionRequest / ApproveDeletion | SystemAction; request ETag, independent operator and step-up |
| PATCH deletion-requests/:id | AccountDeletionRequest / ChangeDeletionRequest | SystemAction; request ETag, cancel/retry policy |

All POSTs use the existing transaction-bound replay mechanism. Secrets appear
only on the original successful mutation. Ordinary reads and replays omit them.
Account-create replay ownership is captured from the typed result; account routes
and record-targeted recovery/deletion/invitation routes resolve account ownership
before cache insertion. Account-owned replays are deleted by identity's purge.

Account/membership/invitation scopes, writable spec and observed status fields
are listed in [resource-contract.md](resource-contract.md). System operation
records have system ownership with targetAccountId references. Session IDs and
material digests remain private; nested limits/approvals use their typed codecs.

## Exp-Bench remaining boundaries

Replace the 26 shared registrations in `cmd/api-server/system_routes.go` with
MountSystem; retain these 13 product-owned registrations:

| Routes (same prefix) | Boundary and adoption requirement |
| --- | --- |
| GET overview | Summary DTO containing limits and recent SystemActivity resources; encode nested resources through a composite product/identity codec |
| GET health | SystemHealth query DTO; preserve diagnostics and authorization |
| GET capacity | CapacityResponse query with a counts map; no resource identity |
| POST impact-previews | Product policy preview (no account path); SystemAction input, canonical ImpactPreview output, retain target policy checks |
| GET limits; GET limits/:id; PATCH limits/:id | Canonical identity Limit resources; SystemPolicyAction input, read ETag, product effective-limit policy |
| GET research-roles; GET research-roles/:id; PATCH research-roles/:id | Product ResearchRole schema and SystemPolicyAction, read ETag, effective configuration preserved |
| GET review-policies; GET review-policies/:id; PATCH review-policies/:id | Product ReviewPolicy schema and SystemPolicyAction, read ETag, review policy provenance preserved |

The event stream is **GET /v1/events**, registered in `cmd/api-server/routes.go`
with tenant account-session authorization; SubscribeLiveEvents rejects system
sessions. SSE framing/invalidation DTOs are protocol exceptions. Preserve that
authorization boundary and verify invalidation payloads in product stream tests.
It must remain outside the buffered transactional response capture.

`pkg/client/systemService.go` currently decodes flat bodies with json.Unmarshal.
Delegate shared methods to identity's SystemClient and migrate call sites to pass
the read resource (or explicit If-Match) for mutations. Rename the product's
ChangeDeletion call sites to ChangeDeletionRequest when delegating. Remaining
product methods need the composite decoder; changing only the server is unsafe.
`cmd/expbctl` system outputs must use the canonical output codec and explicit
operation-output path for issued invitations/recovery credentials. Update website
system adapters and command fixtures together; command DTOs remain exceptions.

`internal/server/identity_adapter.go` currently passes Audit.Resource into its
product record function. That function selects fields from domain JSON into a
small Change.Summary; it does not persist a full resource snapshot today. Keep
that declared summary policy and domain authorization inputs. Use Audit.Snapshot
where fresh resource snapshots are persisted, rather than directly serializing
Audit.Resource. Preserve the SystemAction metadata-only account projection,
account/project ownership, request IDs, authority, change links and transaction
rollback. Product resource snapshots need their own declared codec in PR 07. Remove redundant
idempotency_records auxiliary registration (identity now owns it).

## Urth boundary

`pkg/apiserver/routes.go` mounts identity's tenant/profile/OAuth routes and has no
bespoke `/v1/system` registration to remove. MountSystem is opt-in; this PR does
not add an administration UI to Urth. Its existing tenant account lifecycle
route still uses canonical SystemAccount/ImpactPreview responses.

`pkg/urth/tenancy.go` uses Audit.Resource as typed AgentIdentity/AgentAuthorization
policy input and does not persist identity history. Keep that domain boundary.
If history is added, use Audit.Snapshot. Product CLI, website and monitoring
codecs/examples are updated with identity/components adoption in PR 05.

## Verification gates

`httpapi/inventory_test.go` classifies every route from Mount + MountSystem
(76 total). `client/system_test.go` requires a matching SDK path for each of the
26 system routes, checks caller replay keys and read-version If-Match headers.
PostgreSQL integration covers canonical account/recovery retries, private-token
redaction, cross-operator cancellation and independent purge approval, nested
resources, attribution, audit snapshots and account-owned replay cleanup.
Purge-hook rollback preserves replay data; unrelated account records survive.
CLI/HTTP tests validate all current examples.

After publication, product PRs must prove their complete route inventories,
actual SDK/CLI/UI workflows, snapshots and first-party fixtures with released
identity and components pins. This module's compile checks do not close those
product or full-M8 gates.
