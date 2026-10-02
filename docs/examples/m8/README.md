# Canonical resource examples

These examples define the M8 wire contract. Product adoption is a separate change; earlier servers do not accept these examples yet. There is no legacy-format fallback.

[project.json](project.json) and [project.yaml](project.yaml) are equivalent **read responses**. They contain server-owned UID, account, version and status. The manifest test suite decodes both with the same registered typed schema, validates scope and checks version 1. User label keys remain unchanged. `_links` survives JSON and YAML round trips.

The [project create body](project-create.json) omits those server-owned fields:

```json
{
  "apiVersion": "identity.sre-norns.com/v1",
  "kind": "projects",
  "metadata": {"name": "checkout", "labels": {"team": "payments"}},
  "spec": {"description": "Checkout research", "target": "checkout-service"}
}
```

Send it to the account-scoped collection with an Idempotency-Key. To edit the description, send the [merge patch](project-patch.json) below to the item URL with the **ETag returned by the read** in If-Match:

```json
{"spec": {"description": "Updated checkout research"}}
```

A merge patch is an operation input, not a complete resource for Registry.DecodeJSON. Its route-specific decoder checks writable fields and preserves null/zero values. Scope comes from the authorized route. Never copy a read response wholesale into a create request or make status writable to support apply.

The root toolkit examples use `Registry.Register`, `DecodeJSON`/`DecodeYAML`, `ValidateTypes` and `ValidateScope`. Decoding does not authorize a request or validate ownership of writable fields; services enforce those checks.
