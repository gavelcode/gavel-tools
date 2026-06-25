---
title: The SARIF boundary
type: reference
description: How findings flow from aspects to the platform — SARIF files on disk.
---

# The SARIF boundary

Every analyzer emits **SARIF on disk**; the platform ingests SARIF and is
agnostic to which analyzer produced it.

| Source | Aspect label | Output group | File |
|--------|--------------|--------------|------|
| native (gavel-tools) | `@gavel_tools//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect` | `gavel_submissions` | `*.sarif` |
| rules_lint (breadth) | `@aspect_rules_lint//lint:<tool>.bzl%<tool>` | `rules_lint_machine` | `*.report` (SARIF) |

Native aspects run the tool with its **native** SARIF emitter (e.g.
`ruff --output-format=sarif`, `@microsoft/eslint-formatter-sarif`,
`golangci-lint ... --out-format sarif`), so the SARIF carries a proper
`ruleId`, severity and location.

rules_lint (used only for tools we do not wrap) converts each tool's text output
to SARIF via its `sarif_parser` toolchain (`reviewdog/errorformat`) into
`rules_lint_machine`. That conversion is lossier — see [rules-lint](rules-lint.md).
Run it with `--@aspect_rules_lint//lint:fail_on_violation` **off** so the build
stays green and Gavel evaluates the gate itself.

## What the platform's parser needs

Gavel's SARIF parser requires, per result: a non-empty **`ruleId`** (it rejects
results without one), a **location** (`artifactLocation.uri` + `region.startLine`),
a **message**, and a **level** (else defaults to warning). It computes its own
stable fingerprint from `tool:ruleId:file:lineContent` — so it does not depend on
the tool supplying fingerprints, but it **does** depend on `ruleId` being present.
