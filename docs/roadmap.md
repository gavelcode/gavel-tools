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
2. **Type-aware ESLint (finish)** — `web_project` already exposes the tsconfig and
   type deps on `JsInfo`; feed them into the eslint aspect and set
   `parserOptions.project` so `@typescript-eslint`'s type-aware rules run in the
   sandbox. Verify against a consumer that enables those rules — there is no TS
   target in this repo to test it (see [web-project](web-project.md)).
