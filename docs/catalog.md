---
title: The catalog — two-layer tool config
type: reference
description: catalog.yaml is the published menu of what gavel-tools can lint per language; a consumer's gavel.yaml selects from it per project.
resource: https://github.com/gavelcode/gavel-tools/blob/main/lint/catalog.yaml
tags: [catalog, configuration, gavel.yaml]
---

# The catalog — two-layer tool config

Two layers keep *what a tool is* separate from *which tools a project runs*:

| Layer | File | Owned by | Answers |
|-------|------|----------|---------|
| 1 — the menu | `@gavel_tools//lint:catalog.yaml` | gavel-tools | what exists per language, and how to run it |
| 2 — the selection | a consumer's `gavel.yaml` | the consumer | which of those a project runs |

A consumer never redefines what a tool *is* — it only selects from the menu. The
guiding rule: **gavel cannot have a capability gavel-tools does not publish.**

## Layer 1 — the published menu

`catalog.yaml` lists, per language, every tool gavel-tools can run — for each: the
`aspect` that runs it, the `sarif_suffix` it emits, and (only when needed) its
`build_flags` and tool `binary` repo. It is the single source of truth, and its
own header comment documents every field, so this page shows only a taste rather
than copying it:

```yaml
languages:
  go:
    - name: golangci-lint
      aspect: go_golangci_lint_submission_aspect
      sarif_suffix: .golangci.sarif
      build_flags: ["--@rules_go//go/config:export_stdlib=True"]
      binary: golangci_lint_binary
  typescript:
    - name: eslint
      aspect: typescript_eslint_submission_aspect
      sarif_suffix: .eslint.sarif
  # java, python, rust …
```

> [!NOTE]
> The catalog can't drift from the aspects it names: `catalog_test`
> (`//lint:lint_test`) fails if any listed `aspect` is missing from
> `//lint/aspects:defs.bzl`, or if an aspect ships without a catalog entry.

## Layer 2 — the per-project selection

A consumer's `gavel.yaml` picks tools per language with a `tooling` map:

```yaml
projects:
  - name: payment-service
    tooling:
      java: [error-prone, spotbugs]   # a subset of the java menu
      typescript: [eslint, archtest]
```

> [!IMPORTANT]
> Selection is **explicit, never implicit**. A language with no tools listed is an
> error — not a silent "run everything" default; gavel refuses to guess. Listing a
> tool the catalog does not publish for that language is likewise an error.

## How a consumer loads it

The consumer reads `@gavel_tools//lint:catalog.yaml` through Bazel runfiles at
runtime; the `aspects_bzl` label in the file tells it where the aspects live under
whatever name it binds gavel-tools. In gavel this replaced hardcoded per-language
maps with a loader, so the menu is owned in exactly one place — here.
