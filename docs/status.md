---
title: Status & next steps
type: explanation
description: What is done in gavel-tools, and what remains.
---

# Status & next steps

## Done

- **Extracted** from the gavel monorepo into this standalone Bazel module,
  consumed via `bazel_dep` + `git_override`. Boundary is SARIF-on-disk.
- **Root organized by kind** — `lint/` + `macros/` (see
  [repository-layout](repository-layout.md)).
- **Tool binaries owned here** (`@golangci_lint`, `@pmd`, …) so the aspects can
  resolve them across the module boundary.
- **Native wrappers for go/java/python/rust/typescript** emit each tool's native
  SARIF; gavel judges all five projects green through them.
- **`web_project` macro** ships (see [web-project](web-project.md)).
- **rules_lint evaluated and de-scoped to breadth-only** (see
  [rules-lint](rules-lint.md)). The earlier "delegate-and-delete the native
  wrappers" plan is **cancelled** — native wrappers are higher fidelity.

## Next

1. **catalog.yaml** — add `lint/catalog.yaml` and convert the core catalog from
   hardcoded maps to a loader (see [catalog](catalog.md)).
2. **Sandbox the source-only wrappers** — ruff, pmd, eslint, bandit, cpd use
   `no-sandbox` only by habit; declare their inputs and drop it to shed the cache
   /host tax while keeping native-SARIF fidelity (see [tier-model](tier-model.md)).
3. **Hermetic type-aware ESLint** — finish the aspect-side type-graph gather +
   the consumer-convention layer (see [web-project](web-project.md); planned).
4. **rules_lint breadth backend** — wire it *only* for tools we do not wrap, when
   that breadth is wanted.
