# Canonical identity resources

M8 replaces the identity resource wire format in one cutover. There is no flat
resource reader or writer. This change requires wyrd root v0.7.0. Publish
identity/v0.7.0 only after the remaining M8 system-resource work and validation;
products adopt their backend, components, CLI and SDK changes together.

`resource` owns the identity schemas and explicit domain-to-wire mappings.
Persisted Go models and service interfaces are internal/domain types; directly
marshalling them is not a resource API. `Encode` projects ordinary reads,
`EncodeResult` projects mutation results, and `Decode` validates registered
resources, including resource-valued fields inside lists and query DTOs. The
registry is private and keyed by the exact `identity.sre-norns.com/v1` and kind
pair. Unknown kinds, fields and flat resource bodies fail. Persisted `User`
values cannot be encoded; use `PersonalProfile`.

All resources contain `apiVersion`, `kind`, `metadata`, `spec`, `status` and,
where supplied, `_links`. Metadata contains `uid`, `name`, `account`, `project`,
`version`, `labels`, `creationTimestamp` and `updateTimestamp`. Phase is
`status.phase`; safe attribution is `status.lastModifiedBy` (type/userId/agentId),
and historical authority is `status.authority`. No credential IDs, authorization
snapshots or persisted passwords appear in attribution. User map keys retain
their spelling. Nested limits and deletion approvals are resources, not flat
embedded records. A system operation's `targetAccountId` is a reference, not its
ownership. Personal resources have system ownership but are authorized to their
subject; system ownership alone grants no read authority.

The table below is the field inventory, also exposed by `resource.Definitions`.
All rows have the common metadata/status described above. Read-only projections
use distinct system-prefixed kinds because their safe status differs from the
tenant resource. A field without C/P is read-only on generic resource routes;
system command inputs are separate DTOs. The resource scope declaration does not
replace route and service authorization.

| Model / kind | Ownership | Spec (C=create, P=patch) | Observed status |
| --- | --- | --- | --- |
| `Account` / `accounts` | system | `description` (CP), `ownerEmail` (C) input only, `delivery` (C) input only | `lifecycleReason`, `lifecycleAt` |
| `AccountDeletionApproval` / `deletion-approvals` | system | `requestId` | `userId`, `authenticatedAt`, `assuranceMethod` |
| `AccountDeletionRequest` / `deletion-requests` | system | `targetAccountId`, `reason`, `reference` | `counts`, `approvalMode`, `executeAfter`, `completedAt`, `cancelReason`, `failureCode`, `attempts`, `approvals` |
| `AccountInvitation` / `account-invitations` | account | `email` (C), `role` (C), `delivery` (C) | `emailDelivery`, `expiresAt`, `acceptedBy`, `membershipId` |
| `AccountMembership` / `account-memberships` | account | `userId` (C), `role` (CP) | `email`, `displayName` |
| `AccountPurgeTombstone` / `purge-tombstones` | system | `targetAccountId`, `requestId`, `reason`, `reference` | `approverIds`, `counts` |
| `AgentAuthorization` / `agent-authorizations` | project | `agentId` (C), `roles` (CP) | `activePackages` |
| `AgentIdentity` / `agent-identities` | account | `description` (CP) | `lastSeenAt` |
| `AgentIdentityToken` / `agent-identity-tokens` | account | `agentId`, `expiresAt` (C) | — |
| `ImpactPreview` / `impact-previews` | system | `targetAccountId`, `targetId`, `operation` | `targetRevision`, `counts`, `expiresAt`, `consumedAt` |
| `Limit` / `limits` | system, account, project | `value` (P), `unit`, `periodSeconds` (P), `reason` (P) input only | `effective`, `usage`, `limitingSource`, `overLimit` |
| `OwnerRecovery` / `owner-recoveries` | system | `targetAccountId`, `previousMembershipId`, `replacementEmail`, `reason`, `reference` | `emailDelivery`, `invitationId`, `replacementMembershipId`, `invitationStatus`, `expiresAt` |
| `PersonalProfile` / `personal-profiles` | system | `displayName` (P) | `email` |
| `Project` / `projects` | account | `description` (CP), `target` (CP) | `currentContextId` |
| `ProjectMembership` / `project-memberships` | project | `userId` (C) | `email`, `displayName` |
| `Session` / `sessions` | system, account | — | `scope`, `authenticatedAt`, `authenticationMethod`, `userId`, `clientId`, `origin`, `ipAddress`, `userAgent`, `expiresAt`, `refreshExpiresAt` |
| `SignInMethod` / `sign-in-methods` | system | — | `method`, `lastUsedAt` |
| `StepUpAuthorization` / `step-up-authorizations` | system | `targetAccountId`, `action` | `authenticatedAt`, `assuranceMethod`, `expiresAt`, `consumedAt` |
| `SystemAccount` / `system-accounts` | system | `description` | `ownerSetup`, `counts`, `limits`, `overLimit`, `supportStatus`, `lifecycleReason`, `lifecycleAt` |
| `SystemActivity` / `system-activity` | system | — | `targetAccountId`, `kind`, `action`, `targetId`, `outcome`, `reason`, `reference`, `requestId`, `sessionScope`, `assuranceMethod`, `changeIds` |
| `SystemEntitlement` / `system-entitlements` | system | — | — |
| `SystemInvitation` / `system-account-invitations` | account | `email`, `role`, `delivery` | `emailDelivery`, `expiresAt` |
| `SystemMembership` / `system-account-memberships` | account | `userId`, `role` | `email`, `activeOwners` |

## Requests and lifecycle

Create bodies contain the exact type pair, writable metadata and `spec`. They
omit IDs, ownership, versions, timestamps, links and status. Route parameters
resolve account/project ownership after authorization. Operator-addressable
accounts have globally unique names; project and agent names are unique within
an account (case-insensitive, trimmed). Generated records use their UID when no
name is submitted. Account/project/agent/membership/grant edits can change name
and labels; credential/invitation lifecycle updates cannot.

PATCH accepts `application/json` or `application/merge-patch+json`. It changes
only supplied writable `metadata`/`spec` fields. An optional type pair must match.
Absent fields remain unchanged; strings may be set to empty; nullable fields
such as `spec.displayName` accept null. Label objects merge by key and a null
label deletes that key; `metadata.labels: null` removes all labels. `spec: null`
and null nonnullable scalar values are rejected. Other request media types are
415. JSON is the identity HTTP response representation; CLI supports JSON/YAML.

Every edit requires `If-Match` from the read ETag: missing is 428, stale is 412.
The Go SDK derives it from the decoded revision, or accepts an explicit
`RequestOptions.IfMatch`. CLI `DecodeObject` validates a read document, then
returns only its supplied writable fields; pass those as `RequestOptions.Patch`.
`Output.Encode` writes canonical JSON/YAML and never a one-time token.
`Output.EncodeResult` is the explicit mutation-output path for issued credentials.

Lifecycle is an explicit `{"operation":"revoke"}` command, never a write to
`status.phase`. Sessions, invitations, agent tokens and sign-in methods allow
revoke. Memberships, agents and grants allow activate/deactivate/revoke; projects
allow activate/deactivate/suspend. Existing service policy still checks authority,
last-owner protection, related resources, and current state. Account lifecycle
keeps its confirmed `SystemAction` workflow with preview, reason and confirmation.
Use `client.Resource[T](c).Transition(ctx, path, value, operation)` for lifecycle;
an ordinary resource edit sends writable configuration only. Existing credential
SDK update methods translate a revoked domain value to the explicit command.

## Queries and errors

List results remain `{items, limit, next?, total?}`. `labels=status=label` selects
a label called status; `fields=status.phase=active` selects lifecycle state.
There are no pseudo-label aliases. Metadata fields and scalar stored spec/status
fields declared by the identity schema are selectable. Unknown fields fail 400;
numeric comparisons on text fields fail 400. Invitation phase is computed using
its expiry at query time. Derived counters, JSON arrays and transient projections
are not selectable merely because they can be read.

Queries compose `dbstore.FilterQuery` with identity `Visibility`, scope predicates,
and existing `PageBy` keysets inside the caller's transaction. Scope/visibility
precede filtering, totals and paging. Name matching is case-insensitive, time
ranges are `[from,till)`, and nonzero offsets are rejected. Internal row loads use
`dbstore.GetByUID` in the same transaction before service authorization; unknown
IDs on update still fail rather than creating a resource.

The SDK forwards both `SearchQuery.Fields` and `Selector`. CLI lists accept
`--fields 'status.phase=active'` and `--selector 'team_name=sre'`. Resource failures
use `bark.Problem`, `requestId` and canonical validation paths. Unknown internal
failures are safe 500s; OAuth retains its protocol-specific error shape.

## Operation results and replay

Account mutation results are `{resource, ownerInvitation?}`. Invitation, agent
credential and recovery results are `{resource, token?}`; an owner invitation is
itself an operation result. System account creation additionally supplies
`ownerInvitationId` and optional `emailDelivery`. Ordinary reads never contain
these operation credentials, even if the domain object still holds one.

POST retries must reuse the original idempotency key and request body. Responses
and headers are stored in the current transaction; replay returns the original
resource effect with credentials removed. Token keys in nested operation results
are redacted in camelCase as well as protocol-defined snake_case. User labels
are preserved, including a label named `token`.

Host system handlers must supply typed `HTTPOutcome.AccountID` for a successful
account create (`resource.OutcomeOwner` derives it before serialization). There
is no parser for a flat response ID. Hosts using `httpapi.Authenticate` can use
`httpapi.SendResource`, which records this ownership in the capture context and
serializes the response. Product audit callbacks still receive domain values;
they must use the codec when writing canonical snapshots during product adoption.

## Coverage and adoption gates

[routes.json](routes.json) records every `/v1` route mounted by identity, its
response type, and resource/command/query input classification. Tests fail if a
route is added without classification. OAuth/discovery/browser forms remain
protocol exceptions. Principal, service configuration, accessible-account
summaries and directory candidates are query exceptions; any nested registered
resource is still canonical. Personal profile handlers and sign-in methods are
covered even though they bypass the generic input handler.

The remaining system-focused M8 PR must verify every product-mounted system
route, system SDK/command adapter, bulk lifecycle attribution, deletion/recovery
replay cleanup, and canonical snapshot handling at the host boundary before the
identity release gate closes. The schema definitions here provide those hosts
with typed projections; a compiled host using old bespoke handlers is not proof
of wire adoption. Urth/Exp-Bench and norns-components still need their coordinated
adoption PRs; do not deploy this module independently with their old clients.

## Examples

- [Project create](../examples/project-create.json): POST to `/v1/accounts/{account}/projects` with an `Idempotency-Key`.
- [Project read](../examples/project-read.yaml): CLI JSON/YAML read and apply; use version 4 as `If-Match: "4"` after reading the real resource.
- [Project patch](../examples/project-patch.json): preserve omitted target, clear description, delete one label and add another.
- [Profile patch](../examples/profile-patch.json): clear the nullable display name with the current ETag.
- [Invitation create](../examples/invitation-create.json): POST to `/v1/accounts/{account}/invitations`; consume the one-time token from the operation result.
- [Revoke](../examples/revoke.json): PATCH a supported lifecycle endpoint with its current ETag.

Tests parse these files through the public request/CLI codecs.
