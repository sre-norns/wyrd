# ADR 0002: Identity and tenancy as a shared module, extracted from Exp-Bench

- **Status:** Accepted (2026-09-28)
- **Date:** 2026-09-28
- **Depends on:** [ADR 0001](./0001-portfolio-api-conventions.md)

## Context

Exp-Bench already has what Urth lacks entirely:

- users and accounts, with `owner`/`admin`/`member` roles;
- projects owned by accounts, with explicit project membership;
- account-owned machine identities with revocable tokens and per-project grants;
- a first-party OAuth authorization server, using PKCE for the web UI, the device grant
  for the CLI, and short-lived access tokens with rotating refresh tokens;
- server-side sessions a user can list and revoke;
- email/password sign-in with register and reset links, Google and GitHub sign-in, and
  account invitations;
- mail over SMTP, Mailgun or a development directory;
- system administration.

Urth's REST API has no user authentication. Its runner-enrolment endpoint is open. It has
no notion of an account or project.

Urth needs the same model, and the two products must behave the same way to a person who
uses both. Porting a copy would give two security-sensitive implementations that drift. The
decision is to extract Exp-Bench's implementation once and have both products import it.

The code is not self-contained today. Four couplings shape the boundary:

1. **Auditing.** Every write, identity writes included, goes through `record()`
   (`internal/server/storage.go`), which writes Exp-Bench's `Change` and `AuditEvent`
   rows. It has special cases for research kinds.
2. **Account purge.** `system_purge.go` deletes an account by walking
   `accountResourceTables`, a hard-coded list that mixes identity tables with research
   tables, and a second list of tables whose `credentials` rows must go first.
3. **Credentials.** One `credentials` table stores verifiers for sessions, machine tokens
   and invitations, and also Exp-Bench's work-package lease tokens.
4. **Kind knowledge in authorization.** `authorization.go` hard-codes which kinds an agent
   may read and resolves ownership per kind in a `switch`.

There is also one schema-revision counter for the whole database.

## Decision

### 1. A separate module in the wyrd repository

The module is `github.com/sre-norns/wyrd/identity`, with its own `go.mod`. Consumers that
need only `manifest`/`dbstore`/`bark` do not take on OAuth, bcrypt, OIDC and Mailgun
dependencies. Comserv is the case that matters today.

It lives in wyrd rather than in a new repository because it is built on wyrd's `manifest`
and `dbstore` contracts from ADR 0001. Keeping them in one repository lets a change to the
store contract and its main consumer land together.

| Package | Contents | Moved from `experibench/` |
|---|---|---|
| `identity/model` | User, Account, AccountMembership, AccountInvitation, Project, ProjectMembership, Session, MachineIdentity, MachineToken, MachineGrant, Principal, PersonalProfile | `pkg/expbench/*.go`, internal models in `internal/server/storage.go` |
| `identity/authn` | OAuth authorization server, credential verifiers, register/reset links, rate limiting | `authentication.go`, `identity_access.go`, `rate.go` |
| `identity/providers` | Google, GitHub, and a new generic OIDC provider | `providers.go`, `upstream_flow.go`, `provider_confirmation.go`, `sign_in_methods.go` |
| `identity/authz` | Principal resolution, `Visibility` and `Authorize` implementations, system authority | `authorization.go`, `system_authority.go` |
| `identity/sessions`, `identity/invitations` | Session listing and revocation; the invitation flow and delivery worker | `sessions.go`, `invitation_*.go`, `invitations.go`, `project_access_mail.go` |
| `identity/mail` | `Mailer` interface; SMTP, Mailgun, development providers; templates | `cmd/api-server/identity_mail*.go` |
| `identity/pages` | Embedded, themeable HTML for sign-in, register, reset, device approval, invitations | `cmd/api-server/*_pages.go`, `pages/` |
| `identity/httpapi` | `Mount(router, Config)` and the `Authenticate` middleware | the matching routes in `cmd/api-server/routes.go` |
| `identity/fakeidp` | Test OIDC provider | `internal/fakeidp` |

Account deletion and purge move as well: approvals, owner recovery, step-up authorization
and tombstones. They are identity lifecycle, and the purge is where coupling 2 is resolved.

### 2. The authority model moves unchanged

These Exp-Bench rules are product-neutral and become the portfolio's:

- **Principals** are a *user* (OAuth session), a *machine* (machine token) or a *system
  administrator* (a user session carrying system scope).
- **Account roles** are `owner`, `admin` and `member`. Every account keeps at least one
  active owner.
- **An account role never grants access to project content.** Reading or changing a
  project's resources requires explicit project membership. Account admins manage
  membership; they are not implicitly members.
- **System authority** operates the service. It does not grant project authority.
- **Machine identities belong to one account.** They act in a project only through a
  `MachineGrant` for that project, and only in the roles the grant names.
- **A user session is bound to one account.** Account selection happens before the session
  is issued. Switching accounts in a UI therefore re-authorizes; it does not flip a client
  variable.
- Token secrets are shown once, at creation. Only verifiers are stored.

### 3. Products extend it through registration, not edits

| Extension | Product supplies | Resolves |
|---|---|---|
| **Kinds** | Each kind's scope (ADR 0001 §2), table and machine-readable flag, registered at start-up | Coupling 4. `Visibility` filters any registered kind by scope and principal, with no per-kind `switch`. |
| **Grant roles** | Its role catalogue, e.g. Exp-Bench's `experimenter`, `integrator`…; Urth's `runner` | Machine grants carry product roles the module stores but does not interpret. |
| **Purge** | Nothing extra: every registered account- or project-scoped kind is purged by scope. A kind with side effects outside Postgres registers a `PurgeHook` that runs in the purge transaction. | Coupling 2. A new product table cannot be left out of purge by forgetting to add it to a list. |
| **Auditing** | An `Auditor` receiving `(principal, action, target, outcome, requestID)` inside the write transaction | Coupling 1. Exp-Bench adapts its `Change`/`AuditEvent` recorder; Urth may start with a minimal one. |
| **Credentials** | Its own credential purposes (Exp-Bench: work-package lease) registered with the verifier store | Coupling 3. The table stays shared; the module owns hashing and verification, products own meaning. |
| **Branding** | Product name, logo, theme CSS, mail sender and footer | One set of pages and templates serves both products. |
| **Notifications** | Calls to `Mailer` with its own templates | Urth mails about runners without a second mail stack. |
| **Configuration** | An environment prefix (`EXPBENCH_`, `URTH_`) | `identity.Config` embeds in each product's kong CLI struct. |

### 4. Enforcement: the store filters, the service decides

- `authz` supplies the `dbstore` visibility hook from ADR 0001. Every read and write of a
  registered kind is constrained to what the principal may see. A handler cannot forget
  the filter, because it never applies it.
- Whether an action is *allowed* — create in this project, grant this role, revoke this
  session — is decided in the product's service layer by calling `authz.Authorize`. This
  keeps Exp-Bench's current split: HTTP middleware authenticates, services authorize.
- Background loops that act for the service itself, such as Urth's reconciler and outbox
  relay, run as an explicit **system principal** with a context marker. Code that bypasses
  visibility has to declare it.

### 5. Each product keeps its own database and authorization server

The module creates its tables in the product's database, under the names Exp-Bench uses
today. Exp-Bench therefore needs **no data migration** to adopt it. The module keeps its own
schema-revision row, separate from the product's.

There is no single sign-on across products. A person has a separate user in each
installation. A shared identity service is a larger decision than this one: it would add a
deployment dependency, and it would put cross-product trust into every product's threat
model. Nothing here prevents it later: the module is already a complete authorization
server and could run standalone.

### 6. Identity resources adopt the envelope last

The module first moves with Exp-Bench's current flat representation, so that extraction and
re-representation are not one change. It adopts ADR 0001's envelope in a coordinated release
with Exp-Bench's own conversion. Urth consumes identity routes only through the module's
client and the shared UI package, so the switch is invisible to Urth's code.

### 7. Exp-Bench adopts first

The extraction is proven by Exp-Bench running on the module with its unit, integration and
end-to-end suites unchanged, against a copy of a real database, before Urth imports it. A
behaviour change found at that point is fixed in the module, not worked around in Urth.

## Architectural rules

- The module never imports a product. Products register with it.
- No product code writes identity tables directly. It goes through module services.
- Every account- or project-scoped kind is registered with its scope. An unregistered kind
  cannot be served by a scoped route, and so cannot escape purge or visibility.
- A code path that bypasses visibility runs as the system principal and says so.
- Secrets appear only in the response that created them. Logs, audits and mail never
  contain them.

## Consequences

### Benefits

- One implementation of the most security-sensitive code in the portfolio, reviewed and
  tested once.
- Urth gains sign-in, IdPs, sessions, invitations, roles and mail, with no second design.
- Account purge and visibility become complete by construction, through the kind registry,
  instead of by keeping lists up to date.
- A person moving between the products meets the same sign-in pages, roles and session
  model.

### Costs

- The extraction is the largest single change in the unification programme, and it touches
  Exp-Bench code that currently works. The mitigation is §7: Exp-Bench adopts it first and
  is the proof.
- The wyrd repository gains a second module. Its tags are prefixed (`identity/v0.1.0`), and
  a change spanning both modules needs two coordinated tags.
- The module is PostgreSQL-only, as Exp-Bench is today. Products using it cannot run on
  SQLite.
- Exp-Bench's audit recorder becomes an adapter behind an interface, one level of
  indirection further from the writes it records.

## M2 implementation note (2026-09-29)

The implementation keeps the transaction-owning services in the root `identity`
package. `model`, `authz`, `mail`, `pages`, `httpapi`, and `fakeidp` are separate
packages. Authentication, providers, sessions and invitations share credential
rotation, account provisioning, audit and delivery transactions. Splitting their
implementation into the proposed sibling packages would require a second shared
internal engine or cyclic imports. Their service interfaces remain separately
accessible through `identity.Service`; there is one implementation.

The first module release preserves Agent table/route names and legacy project
fields. Machine names are public aliases. The M8 envelope conversion removes this
compatibility boundary in a coordinated consumer release. Compatibility bridges
also let Exp-Bench retain its existing private test fixtures while deleting the
moved implementation.

Registered kinds replace research-kind authorization and purge knowledge. Products
supply audit projections, materialization, policy limits, grant read models and
impact counts. The purge digest supports legacy and manifest metadata. The module
has its own revision table and its own CI audit. It retains the source product's
license in `identity/LICENSE`; the root module license does not change.

The initial tag is `identity/v0.1.0`. Merge and publish it before resolving the
consumer's final `go.sum`. M2 adds no Urth runtime dependency.
