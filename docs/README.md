---
title: gavel-tools docs
type: explanation
summary: What gavel-tools is, and an index of the concept docs.
---

# gavel-tools — docs

`gavel-tools` does two things for the Gavel platform:

1. **Lints** — analyzers that run as Bazel aspects and normalize their output to
   **SARIF files on disk**.
2. **Scaffolds builds** — macros (e.g. `web_project`) that generate a project's
   Bazel build graph so consumers stop hand-wiring it.

The interface to the platform is SARIF on disk: no Go imports cross the boundary,
and the platform (quality gate, baseline, coverage, architecture verdict,
dashboard) treats gavel-tools purely as a *source of findings*.

## Concept docs

| Doc | What it covers |
|-----|----------------|
| [repository-layout](repository-layout.md) | The `lint/` + `macros/` structure, labels, tool-binary ownership |
| [tier-model](tier-model.md) | The sandbox axis: which tools run sandboxed vs `no-sandbox`, and why |
| [sarif-boundary](sarif-boundary.md) | How findings flow from aspects to the platform |
| [rules-lint](rules-lint.md) | Why rules_lint is a breadth add-on, not a substitute (measured) |
| [catalog](catalog.md) | The two-layer config: `catalog.yaml` menu + `gavel.yaml` selection |
| [web-project](web-project.md) | The `web_project` macro for frontend builds |
| [status](status.md) | What is done, what is next |
