# NNN: Task Title

Shared context: [`CONTEXT.md`](CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` / `P1` / `P2` |
| Workstream | Mutations / Queries / Manifests / HTTP / Lifecycle / Delivery / Architecture / Verification |
| Depends on | Task IDs or — |
| Likely conflicts | Task IDs or — |
| Owner | Unclaimed |

## Why This Matters

Describe the observable bug, safety risk, or architecture friction and its user
impact. Use the domain and architecture language from
[`CONTEXT.md`](CONTEXT.md). When copying this template under `tasks/`, change
that link to `../CONTEXT.md`.

## Architecture Assessment

Identify the current Module and its Interface, explain why it is shallow or why
behavior lacks locality, and apply the deletion test. Name real Seams and
Adapters only where behavior varies. State the leverage expected from the
deepened Module and how its Interface becomes the test surface.

## Evidence

- `path/to/file.go:line`: current behavior and why it matters.
- Include downstream evidence separately when it is useful.

Line numbers are starting points. Revalidate them against the current branch.

## Failure Sequence

1. State the caller action or runtime event.
2. Show the current implementation decision.
3. Show the incorrect observable outcome.

## Required Outcome

State externally observable behavior and important invariants. Be specific enough
that an implementer does not need to make a product decision.

## Implementation Options and Trade-offs

### Preferred: Deep Module

Describe the preferred Interface and what behavior moves behind it. Explain
locality, leverage, compatibility, and performance.

### Alternative

Describe a credible alternative and its cost. Do not list a patch that fails the
Required Outcome.

## Implementation Constraints

- Compatibility and dependency direction.
- Trust, durability, ordering, cancellation, and resource limits.
- Decisions in shared context or ADRs that must not be weakened.

## Suggested Implementation Sequence

1. Add a failing regression or contract test through the affected Interface.
2. Introduce the smallest coherent Interface migration.
3. Exercise the named failure seam, not only the success path.
4. Update public and downstream migration documentation.

## Non-Goals

- List tempting adjacent changes that must not be folded into the task.

## Acceptance Criteria / Definition of Done

- [ ] Observable Required Outcome is implemented.
- [ ] Regression tests cover success, failure, and edge behavior.
- [ ] Public documentation matches the Interface.
- [ ] Compatibility/deprecation steps are explicit.
- [ ] No unrelated changes are included.
- [ ] Targeted and full validation pass.

## Required Tests

- Name concrete scenarios and the most appropriate existing or new test module.

## Validation

```sh
# Add targeted commands first.
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Compatibility/migration:**
- **Validation evidence:**
- **Follow-ups:**

