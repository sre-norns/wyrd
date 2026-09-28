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
provider tests without external accounts.

`mail.New` selects SMTP, Mailgun, or development-file delivery. `mail.Render`
renders embedded text and escaped HTML templates. `mail.Mailer` accepts a rendered
message; the compatibility `mail.Sender` delivers its text part. `pages.Config`
sets branding, privacy URL and trusted application CSS per render. The CSP hashes
the exact CSS and script. Do not pass user-authored CSS as `ThemeCSS`.

Run `RunInvitationMailWorker`, `RunProjectAccessMailWorker`, and `RunPurgeWorker`
with the application lifecycle context when those features are enabled.

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
