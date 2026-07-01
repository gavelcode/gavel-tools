---
title: The SARIF boundary
type: reference
description: How findings flow from aspects to the platform — SARIF files on disk.
---

# The SARIF boundary

Every analyzer emits **SARIF on disk**; the platform ingests SARIF and is
agnostic to which analyzer produced it.

| Aspect label | Output group | File |
|--------------|--------------|------|
| `@gavel_tools//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect` | `gavel_submissions` | `*.sarif` |

Each aspect runs the tool with its **native** SARIF emitter (e.g.
`ruff --output-format=sarif`, `@microsoft/eslint-formatter-sarif`, and the
golangci-lint SARIF output), so the SARIF carries a proper `ruleId`, severity
and location.

## What the platform's parser needs

Gavel's SARIF parser requires, per result: a non-empty **`ruleId`** (it rejects
results without one), a **location** (`artifactLocation.uri` + `region.startLine`),
a **message**, and a **level** (else defaults to warning). It computes its own
stable fingerprint from `tool:ruleId:file:lineContent` — so it does not depend on
the tool supplying fingerprints, but it **does** depend on `ruleId` being present.

This is why gavel-tools wraps each linter natively rather than delegating to
[aspect-build/rules_lint](https://github.com/aspect-build/rules_lint): its
reviewdog-based SARIF leaves `ruleId` empty, which the parser rejects. For a
tool gavel-tools does not wrap, rules_lint remains a valid breadth option to
wire on the side.
