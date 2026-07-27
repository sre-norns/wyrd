# 020: Make Quality Gates Reproducible and Contract-Focused

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Verification |
| Depends on | — |
| Likely conflicts | all test and workflow changes |
| Owner | Unclaimed |

## Why This Matters

The current race suite and vet pass, yet they miss every P0 defect in this
backlog. Storage behavior is primarily tested on SQLite despite three advertised
dialects, webhooks has zero coverage, a misspelled selector test never runs, and
audit tooling uses moving `@latest` versions. A local staticcheck binary built
with Go 1.25.3 cannot analyze this Go 1.26.5 module.

Green CI must mean stable public contracts were exercised, not merely that the
available unit tests compiled.

## Architecture Assessment

Verification should mirror the deep Interfaces: selector truth tables across
dialects, response output through HTTP, lifecycle interleavings under race, and
delivery outcomes through `httptest`. Build one shared contract suite per
Interface and run it against real Adapters.

Pin the toolchain and analysis tools so developers and CI execute the same
checks. Coverage is a discovery/ratchet signal; it is not a substitute for
semantic assertions.

## Evidence

- An initial `go test -race -count=1 ./...` passed, but a final rerun failed
  `TestStringSet_Join/some-set`; a focused 100-count run reproduces randomized
  output order. `go vet ./...` passes.
- Baseline coverage is: Bark 19.4%, dbstore 63.1%, Grace 67.8%, manifest 67.0%,
  webhooks 0.0%.
- Storage tests predominantly exercise SQLite; Postgres/MySQL selector, JSON,
  schema, and DSN branches lack equivalent live contracts.
- `pkg/manifest/label_selector_test.go:112` begins with `Tes`, so Go never runs
  it.
- `Makefile` invokes staticcheck and govulncheck with `@latest`, making audit
  results time-dependent.
- The installed staticcheck was built with Go 1.25.3 and refuses the module's Go
  1.26.5 version.
- GitHub Actions use a moving runner label in one workflow while others pin an
  Ubuntu release.
- CI has no explicit gofmt/go-mod-tidy diff gate or downstream compatibility
  job.

## Failure Sequence

1. A contributor runs `make audit` and resolves `@latest` tool versions.
2. CI or another contributor resolves different versions later.
3. One environment fails or reports different findings without any repository
   change.

Meanwhile a SQLite-only selector test passes even though the same query aborts
on Postgres.

## Required Outcome

- Go version and analysis tools are pinned and updated intentionally.
- One documented command reproduces CI locally.
- Formatting, module tidiness, vet, compatible staticcheck, race tests, and
  vulnerability scanning have explicit gates.
- SQLite, Postgres, and MySQL run shared live Store contracts for every claimed
  dialect behavior.
- HTTP, webhook, codec, validation, and lifecycle Interfaces have focused
  contract/fuzz/stress coverage.
- Disabled/misspelled tests are detected or eliminated.
- Order-dependent/flaky tests are stressed and fixed at the underlying contract,
  not retried until green.
- Coverage is reported per package and ratcheted from an agreed baseline without
  incentivizing low-value assertions.
- Important compatibility is tested against Urth and Comserv at defined
  milestones.
- CI runner/tool configuration is stable and documented.

## Implementation Options and Trade-offs

### Preferred: Pinned Tools Module plus Shared Contract Suites

Pin tool versions in a dedicated tools module or explicit Make variables, use a
version-matched Go toolchain, and have CI invoke the same Make target as local
development. Run database services in a dialect matrix and reuse one Store
contract package.

### Alternative: Versioned Container Image

Publish a development/CI image containing Go, tools, and database clients. This
is highly reproducible but adds image maintenance and can obscure what ordinary
Go contributors need. It can complement, not replace, version declarations.

## Implementation Constraints

- Do not retain `@latest` in required CI commands.
- Live database contracts must not be replaced by SQL string snapshots.
- Keep fuzz seeds deterministic in ordinary CI; run bounded fuzz time.
- Do not introduce a high global coverage threshold before critical paths have
  meaningful assertions; establish package baselines and ratchet.
- Make integration-test secrets ephemeral and least-privileged.

## Suggested Implementation Sequence

1. Repair the disabled test and capture current tool/coverage baselines.
2. Pin Go-compatible staticcheck and govulncheck versions.
3. Add format/tidy/audit targets and make CI call them.
4. Extract Store contracts and add the three-dialect service matrix.
5. Add fuzz/stress/HTTP contracts named by tasks 001–018.
6. Add downstream compatibility checks at release candidates.

## Non-Goals

- Proving correctness solely through coverage percentage.
- Running unbounded fuzzing on every pull request.
- Supporting unadvertised Go/database versions indefinitely.

## Acceptance Criteria / Definition of Done

- [ ] A clean checkout can run the documented CI-equivalent command.
- [ ] Required tool versions are pinned and Go-version compatible.
- [ ] Format and module drift fail CI.
- [ ] All advertised SQL dialects run one shared live contract suite.
- [ ] Critical HTTP, lifecycle, codec, and delivery contracts run in CI.
- [ ] No test is silently disabled by a misspelled prefix.
- [ ] Repeated test runs do not expose known randomized-order assumptions.
- [ ] Coverage baselines and ratchet policy are recorded.
- [ ] Downstream compatibility checks have explicit cadence/ownership.

## Required Tests

- Bootstrap/verify pinned tools on a clean runner.
- Deliberate gofmt and go.mod drift fixtures in workflow tests or script tests.
- Store contract matrix against live SQLite, Postgres, and MySQL.
- Bounded fuzz targets for auth, selectors, validation, and manifest codecs.
- Repeated focused tests for code that traverses maps or depends on concurrency.
- Repeated race/stress target for Grace.
- `httptest` contracts for all Bark and webhook public response paths.
- Urth and Comserv compile/test jobs at release milestones.

## Validation

```sh
make audit
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
