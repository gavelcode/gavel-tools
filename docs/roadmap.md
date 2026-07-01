---
title: Roadmap
type: explanation
description: What is not yet built in gavel-tools; shipped state lives in the code and the published versions.
---

# Roadmap

Shipped functionality is described by the concept docs in this bundle and by the
versions published to the [registry](https://gavelcode.github.io/registry) — this
file carries only what is *not yet built*, so it does not drift against the code.

## Next

1. **catalog.yaml** — add `lint/catalog.yaml` and convert the core catalog from
   hardcoded maps to a loader (see [catalog](catalog.md)).
2. **Hermetic type-aware ESLint** — finish the aspect-side type-graph gather +
   the consumer-convention layer (see [web-project](web-project.md); planned).
3. **rules_lint breadth backend** — wire it *only* for tools we do not wrap, when
   that breadth is wanted.
