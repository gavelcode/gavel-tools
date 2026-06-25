---
title: The catalog — two-layer config
type: reference
summary: catalog.yaml is the default menu of language→tools; gavel.yaml selects per project.
---

# The catalog — two-layer config

## Layer 1 — `lint/catalog.yaml` (the menu)

Source of truth for *what exists* per language, *how to invoke it*, and *what is
on by default*. Format is **YAML** (declarative data; the Go core parses it with
`yaml.v3`; consistent with `gavel.yaml`; no `bazel query` needed as a `.bzl`
would require).

```yaml
version: 1
languages:
  go:
    tools:
      - { name: golangci-lint, tier: native, aspect: go_golangci_lint_submission_aspect, output_group: gavel_submissions, default: true }
      - { name: archtest,      tier: native, aspect: go_archtest_submission_aspect,      output_group: gavel_submissions, default: true }
  java:
    tools:
      - { name: error-prone, tier: native,     aspect: java_error_prone_submission_aspect, output_group: gavel_submissions, default: true }
      - { name: pmd,         tier: native,     aspect: java_pmd_submission_aspect,         output_group: gavel_submissions, default: true }
      - { name: spotbugs,    tier: native,     aspect: java_spotbugs_submission_aspect,    output_group: gavel_submissions, default: true }
      - { name: archtest,    tier: native,     aspect: java_archtest_submission_aspect,    output_group: gavel_submissions, default: true }
      - { name: checkstyle,  tier: rules_lint, aspect: checkstyle,  output_group: rules_lint_machine, default: false }
  # python, rust, typescript …
```

`tier: native` = a gavel-tools wrapper (native SARIF). `tier: rules_lint` = a
breadth-only tool we do not wrap (see [rules-lint](rules-lint.md)), off by
default. Each entry carries exactly what the core needs to generate the bazelrc
and decide what to run: `tier`, `aspect`, `output_group`, `default`.

## Layer 2 — consumer `gavel.yaml` (the selection)

Selects/overrides per project. No `linters` section → use every `default: true`
entry (basic mode).

```yaml
projects:
  - name: payment-service
    languages: [java]
    linters:
      java: [error-prone, pmd, spotbugs, checkstyle]   # opt in to checkstyle
```

## Core impact

`core/.../catalog/*.go` stops being hardcoded maps and becomes a **loader** of
`@gavel_tools//lint:catalog.yaml` (read via Bazel runfiles — the CLI already uses
runfiles). Wiring detail to resolve in implementation: the CLI needs a Bazel dep
on `@gavel_tools//lint:catalog.yaml`.
