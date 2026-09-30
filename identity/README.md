# Identity and tenancy

`github.com/sre-norns/wyrd/identity` is a separate Go module. It owns identity
inside each product's PostgreSQL database. Products do not share users, secrets,
sessions, or an OAuth server across deployments.

The module provides email/password access, PKCE and device authorization, rotating
refresh tokens, Google/GitHub/generic OIDC sign-in, profiles, sessions, invitations,
accounts, project membership, machine credentials and grants, system authority,
account lifecycle, purge approvals, delivery workers, mail templates, and HTML pages.

## Use

Start with `identity.DefaultConfig()`. Set `ProductName`, `WebClientID`, `Clients`,
`Issuer`, and the mail sender for the product. Client redirect URIs must match the
product's registered web and CLI clients. Configure the service before serving
requests or starting workers; configuration is not a live reload API.

```go
err := identity.Register(db, identity.Extensions{
    Kinds: map[string]identity.Kind{
        "Probe": {
            Table: "probes", IDColumn: "uid", Scope: "project",
            MachineRead: true,
        },
    },
    GrantRoles: []model.RoleType{"runner"},
    CredentialPurposes: map[string]string{"probe-lease": "probes"},
})
// Check err, migrate product tables, and call identity.Migrate(db).
service := identity.NewService(db)
// Check service.Configure(config).
httpapi.Mount(router, service, httpapi.Config{
    ProductName: "Urth", PrivacyURL: "/privacy", ThemeCSS: productTheme,
})
```

`httpapi.Mount` mounts `/oauth/*`, discovery, and the identity `/v1` endpoints,
including `/profile/accounts`, `/principal`, `/sessions`, accounts and projects.
Mount it before product routes. `httpapi.Authenticate(service)` supplies bearer
principal resolution for other routes. Route handlers and service methods enforce
authorization; authentication alone does not authorize a product operation.

Use `identity.Visibility{DB: db}` with wyrd's `dbstore.Visibility` option. Use
`identity.Authorize(ctx, db, resource, write)` in service operations. Both support
legacy identity metadata and `manifest.ObjectMeta`. Unknown kinds fail closed.
`WithServicePrincipal` is an explicit internal control-loop capability; never
construct it from request data or a system-administrator session.

`identity.Options` is an embeddable Kong option group. An embedding field can use
`embed:"" envprefix:"URTH_"`. `Options.Apply` configures durations, providers and
mail on top of branded defaults. Exp-Bench retains its existing flag adapter and
legacy environment aliases.

## Extensions and transactions

Register once before migration or request handling. Registry maps and slices are
copied. Callbacks must be safe for concurrent use. Table and ID-column names must
be simple SQL identifiers. Register actual model names, including their plural
SQL table names and account/project/system scope. Account-owned tables must have
`account_id`; project-owned tables also need `project_id`.

- `Auditor.Record` receives the current database transaction. An audit error aborts
  the mutation. Products write their audit and change-feed records here.
- `Rejected` records a denied mutation in its failure transaction. `HTTPRejected`
  records an HTTP rejection after a transaction rolls back and must start from the
  supplied database handle, not a transaction retained in the request context.
- `Kind.MachineRead`, `Admit`, and `MachineFilter` express product read policy.
  Hooks are trusted application code; keep their account and grant checks explicit.
- `Materialize`, `PrepareProject`, `DecorateGrant`, `ResourceLimit`, and
  `EffectiveLimit` connect product defaults, validation and read projections.
- `CredentialPurposes` registers product credential owners. `ReplaceCredential`
  and `VerifyCredential` retain verifier ownership in identity. For manifest
  tables set `Kind.IDColumn` to `uid`; the default is `id`.
- Purge walks registered account/project tables and credential owners. `PurgeHook`
  runs in the purge transaction. Its database effects roll back on failure.
  Hooks must not perform irreversible external actions. `AuxiliaryTables` adds
  product tables with an `account_id`; system history remains outside account purge.
- `ImpactCounts`, `ImpactTarget`, and `PreviewTables` add product impact-preview
  information. The material digest covers registered rows and supports both
  legacy identity fields and manifest metadata.

An account administrator does not gain project-content access. Account sessions
stay bound to one account. Machines need project grants. System-administrator
sessions do not gain project authority. Revocation checks use current database
state rather than authority cached in a token.

## Providers, mail and pages

Set `Config.Providers["oidc"]` with `IssuerURL`, `ClientID`, and `ClientSecret`.
Register `<issuer>/oauth/providers/oidc/callback` with the upstream provider.
Discovery, signature/audience checks, PKCE, nonce verification, and verified email
requirements use the same flow as Google. Production upstream issuers require
HTTPS. Local HTTP issuers require development mode. `fakeidp` supports isolated
provider tests without external accounts. For local development and browser tests,
`go run github.com/sre-norns/wyrd/identity/cmd/fake-idp` serves it on a loopback
address (`-listen`, `-client-id` and `-client-secret`, or `FAKE_IDP_LISTEN`,
`FAKE_IDP_CLIENT_ID` and `FAKE_IDP_CLIENT_SECRET`). Its Google issuer,
`http://<listen>/google`, also serves OpenID discovery and works as the `oidc` issuer.

`mail.New` selects SMTP, Mailgun, or development-file delivery. `mail.Render`
renders embedded text and escaped HTML templates. `mail.Mailer` accepts a rendered
message; the compatibility `mail.Sender` delivers its text part. `pages.Config`
(also `httpapi.Config`) sets branding, privacy URL and trusted application CSS per
render. The CSP hashes the exact CSS and script. Do not pass user-authored CSS as
`ThemeCSS`.

The page colours are `@sre-norns/components` theme tokens (`--primary-container`,
`--on-surface`, …) with the Exp-Bench theme's values as defaults, so `ThemeCSS` is
a product theme's `:root` block. Five colours have no kit token and are named
`--page-*`: `--page-glow`, `--page-field-outline`, `--page-success`,
`--page-success-container` and `--page-error-container`. Set them too; the
defaults are cyan-tinted.

`pages.Config.Copy` carries the wording that belongs to a product: tagline,
headlines, description, the invitation's description, what its machine tokens
are called, and `DeviceRetry` -- how to start another device sign-in once one
expired, naming the product's CLI. Empty fields keep `pages.DefaultCopy`, the wording the pages were
written with.

`Copy.HeadlineVariants` are the rewordings the pages crossfade the headline
through, at an unpredictable pace. The headline carries them in a `data-variants`
attribute, so the pages' script stays one static, CSP-hashed file for every
product. Fewer than two variants keep the headline still. The default variants
apply only with the default headline, so a product that sets its own headline
and no variants keeps it still.

Run `RunInvitationMailWorker`, `RunProjectAccessMailWorker`, and `RunPurgeWorker`
with the application lifecycle context when those features are enabled.

## Lists

Every list follows ADR 0001 §8: `?limit=&cursor=` in, `{items, limit, next?, total?}`
out. `limit` defaults to 100 and is capped at 1024. Paging is by cursor only:
`offset`, `page` and `pageSize` are answered with a 400 problem `offset-unsupported`,
and a cursor issued for another list order (another invitation `sort`, the other
`direction`) with `invalid-cursor`.

Lists are newest first (`created_at`, then `id`), except where the order is the point:
`/profile/accounts` is by account name, and account invitations take `sort` and
`direction`, the computed sorts (`status`, `email_status`, `last_attempt_at`)
included. System lists (`SystemPage`) have the same shape plus `generated_at`;
`total` is absent where derived filters make it uncountable. Paging is applied on top
of the authorised query (`listQuery`), so a cursor can never widen what a caller sees.
Products page their own lists with `dbstore.PageBy` and `NewestFirst`, and their system
lists with `SystemPageOf` / `NewSystemPage`.

## Project access read models

`Service.Directory()` and `Mount` serve the read models behind project access
management, over identity records only:

- `GET /v1/projects/{id}/member-candidates?q=`: active members of the project's account, with their project membership status;
- `GET /v1/projects/{id}/agent-candidates?q=`: active machine identities of the account, with their grant status;
- `GET /v1/agent-identities/{id}/project-authorizations`: a machine identity's grants, with project name and status.

Candidates are visible to whoever may read the project's memberships;
authorizations to whoever may read the machine identity. Searches match literal
substrings (`strpos`, so `%` and `_` are not wildcards) and sort by name under
the database's collation, the key read back from SQL so cursors agree with it
(`PageByText`). A product that served these paths itself must drop its routes
when it upgrades: gin refuses to register a path twice, at startup.

## Compatibility and release

The first release preserves Exp-Bench's tables, text identity IDs, JSON fields,
route names, problem URNs, cookie names, and OAuth behavior. `MachineIdentity`,
`MachineToken`, and `MachineGrant` are aliases of the existing Agent types. Agent
routes/tables and legacy project fields remain until the coordinated M8 envelope
conversion. Product compatibility aliases do not contain a second implementation.

`Migrate` requires PostgreSQL. It uses `identity_schema_revisions` independently
of the product's schema revision and rejects a newer identity revision. Existing
Exp-Bench databases need no table rename or data rewrite. Migrate identity before
serving traffic; product migrations remain product-owned.

Publish the nested module with tag **`identity/v0.1.0`**, after merging the wyrd
branch. Then run `go mod tidy` in the Exp-Bench adoption branch to add the published
identity checksums and run its CI. A consumer cannot fetch this new module before
the tag exists. Local development uses a temporary Go workspace; no local replace
belongs in a released consumer module.

## Verification

```sh
WYRD_TEST_POSTGRES_URL='postgres://.../isolated?sslmode=disable' make audit
```

The database tests use private schemas. They test account/project isolation,
machine-grant revocation, manifest visibility, credential purposes, migration
versioning, purge-hook rollback, and OIDC nonce validation. The Exp-Bench adoption
retains its integration and browser suites. The repository CI audits this module
separately so OAuth dependencies do not enter the root wyrd module.

The transactional engine stays in the root identity package. `model`, `authz`,
`mail`, `pages`, `httpapi`, and `fakeidp` expose the independent contracts and
adapters. See ADR 0002 for the implementation adjustment to its proposed package
layout.

## License

This extracted module retains Exp-Bench's PolyForm Noncommercial 1.0.0 license
in `identity/LICENSE`. The parent wyrd module remains Apache-2.0. This extraction
does not relicense the source product's code.
