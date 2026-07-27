# Wyrd Repository Review Backlog

This directory turns the repository-wide review into agent-ready tasks. It is
deliberately more explicit than a flat TODO list: every task records impact,
source evidence, failure sequences, architecture assessment, implementation
options, constraints, ordering, tests, and a Definition of Done.

Read [`CONTEXT.md`](CONTEXT.md) before claiming a task. This backlog is based on
Wyrd `v0.2.2`; revalidate evidence after earlier tasks merge.

## Headline Findings

A race-suite run can pass despite a known randomized-order failure, and it does
not cover several live Interface failures:

- `dbstore.Update` does not use the ID it promises to target and cannot reliably
  persist zero-valued fields.
- SQL negative and numeric selector behavior differs from in-memory matching.
- malformed `Authorization: Bearer` input can panic an HTTP request.
- the contextual Bark error writer ignores its requested status, while current
  helpers can expose internal errors as client-visible bodies.
- concurrent Workgroup submission and `Wait` can send on a closed channel.
- manifest encoders omit semantic links.
- `StringSet.Join` exposes randomized map order while its test assumes insertion
  order, making the suite flaky.
- webhook delivery has no tests and does not classify retryable failures.

These are grouped into coherent tasks rather than patched as isolated nil checks.
The preferred designs deepen Modules so that mutation, query, response, lifecycle,
and delivery policies have locality and one testable Interface.

## Agent Workflow

1. Choose a `ready` task whose dependencies are `done`.
2. Read its entire file and [`CONTEXT.md`](CONTEXT.md).
3. Revalidate source and downstream evidence.
4. Check `git status --short` and likely conflicts.
5. Mark the task `in-progress` and record an owner/branch.
6. Add the named regression or contract test first.
7. Implement only the Required Outcome.
8. Fill the completion record, mark the task `done`, and update this index.

Status values:

- `ready`: may be claimed now.
- `blocked`: waiting on listed dependencies or a recorded decision.
- `in-progress`: claimed; inspect owner and conflicts before editing.
- `done`: acceptance criteria and completion record are satisfied.

## Task Index

| ID | Priority | Status | Task | Depends on | Likely conflicts |
|---|---|---|---|---|---|
| 001 | P0 | ready | [Make Resource updates authoritative and explicit](tasks/001-authoritative-resource-updates.md) | — | 004, 005, 006 |
| 002 | P0 | ready | [Execute Store transactions through one safe lifecycle](tasks/002-managed-store-transactions.md) | — | 001, 006 |
| 003 | P0 | ready | [Make selector semantics identical in memory and SQL](tasks/003-selector-semantic-parity.md) | — | 004, 006, 008 |
| 004 | P0 | ready | [Honor the advertised SQL dialect and schema contract](tasks/004-sql-dialect-and-schema-contract.md) | — | 001, 003, 006, 008, 020 |
| 005 | P0 | blocked | [Enforce server-owned Resource metadata at the write seam](tasks/005-resource-metadata-ownership.md) | 001 | 001, 009, 011 |
| 006 | P1 | blocked | [Compile Store options into one validated query plan](tasks/006-validated-store-query-plan.md) | 003 | 001, 003, 004, 007, 008 |
| 007 | P1 | blocked | [Make pagination and iteration bounded and deterministic](tasks/007-bounded-deterministic-pagination.md) | 006 | 006, 008, 013 |
| 008 | P1 | blocked | [Give label catalogs a truthful query interface](tasks/008-label-catalog-query-contract.md) | 003, 006 | 003, 004, 006, 007 |
| 009 | P1 | ready | [Preserve complete manifests across supported representations](tasks/009-manifest-representation-contract.md) | — | 005, 010, 013 |
| 010 | P1 | ready | [Replace the global Kind map with a safe Registry module](tasks/010-safe-kind-registry.md) | — | 009, 011, 019 |
| 011 | P1 | ready | [Canonicalize Resource and selector validation](tasks/011-canonical-resource-validation.md) | — | 003, 005, 010 |
| 012 | P0 | done | [Make bearer extraction total and standards-compliant](tasks/012-harden-bearer-authentication.md) | — | 013, 014 |
| 013 | P0 | ready | [Deepen Bark response and error handling](tasks/013-deepen-bark-response-handling.md) | — | 007, 009, 012, 014 |
| 014 | P1 | blocked | [Complete HTTP negotiation and middleware contracts](tasks/014-http-negotiation-and-middleware.md) | 013 | 009, 012, 013 |
| 015 | P0 | ready | [Make Workgroup lifecycle linearizable](tasks/015-linearizable-workgroup-lifecycle.md) | — | 016, 017 |
| 016 | P1 | ready | [Preserve real failures and make actionable errors causal](tasks/016-causal-process-errors.md) | — | 015, 017 |
| 017 | P1 | ready | [Own signal registration and cleanup explicitly](tasks/017-signal-lifecycle-ownership.md) | — | 015, 016 |
| 018 | P1 | ready | [Define bounded and observable webhook delivery](tasks/018-resilient-webhook-delivery.md) | — | 009, 019, 020 |
| 019 | P2 | blocked | [Reduce package coupling and optional dependency cost](tasks/019-deepen-package-seams.md) | 001–018 | all package-interface tasks |
| 020 | P1 | ready | [Make quality gates reproducible and contract-focused](tasks/020-reproducible-contract-verification.md) | — | all test and workflow changes |

Priority meanings:

- **P0**: data correctness, concurrency safety, or security failure; fix before
  treating the affected Interface as safe for production use.
- **P1**: resilience, interoperability, operability, or contract completeness.
- **P2**: architecture and maintainability improvement after behavior is stable.

## Workstreams and Ordering

```text
Mutations:       001 ─→ 005
                  └──↔ 002 managed transactions

Queries:         003 ─→ 006 ─→ 007
                  │      └───→ 008
                 004 ─────────↗

Manifests:       009 representation contract
                  ↕ coordinates with
                 010 Registry, 011 validation, 005 metadata

HTTP:            012 ─┐
                 013 ─┴─→ 014
                   ↑
                 007 pagination links

Lifecycle:       015 ─┐
                 016 ─┼─→ documented process lifecycle
                 017 ─┘

Delivery:        018

Verification:    020 supports every workstream

Architecture:    stabilized behavior from 001–018 → 019
```

Tasks in separate workstreams may proceed concurrently when their conflict
metadata does not overlap. A likely conflict is not a dependency; coordinate
file ownership or merge order.

## Completion Gate

Every task must satisfy its own Definition of Done and normally finish with:

```sh
go mod verify
go test -race -count=1 ./...
go vet ./...
git diff --check
```

Until task 020 pins a compatible staticcheck version, record whether staticcheck
ran and the exact tool version rather than silently omitting it. Storage tasks
that claim multi-dialect behavior must run live SQLite, Postgres, and MySQL
contract tests; a GORM dry-run SQL assertion is supporting evidence, not a
substitute. Concurrency tasks need repeated race/stress tests. HTTP and webhook
tasks need `httptest` tests through their public Interfaces.

## Backlog Maintenance

- Keep each task self-contained enough for an agent receiving only that task.
- Update status, dependencies, and the completion record as work lands.
- Move newly discovered scope into a separate task instead of silently widening
  an active task.
- Preserve compatibility wrappers until downstream migration steps in the task
  are complete.
- Record enduring design choices in ADRs and link them from
  [`CONTEXT.md`](CONTEXT.md).
