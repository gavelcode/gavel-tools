---
title: The SARIF boundary
type: reference
description: How findings flow from aspects to the platform — SARIF files on disk, carrying the ruleId the parser requires.
tags: [sarif, findings, boundary, platform]
---

# The SARIF boundary

Every analyzer emits **SARIF on disk**; the platform ingests SARIF and is
agnostic to which analyzer produced it.

| Aspect label | Output group | File |
|--------------|--------------|------|
| `@<module>//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect` (`<module>` per [`catalog.yaml`](../lint/catalog.yaml)) | `gavel_submissions` | `*.sarif` |

Each aspect produces SARIF with a proper `ruleId`, severity and location —
**natively** where the tool ships a SARIF emitter that resolves in the sandbox
(e.g. `ruff --output-format=sarif`, golangci-lint's SARIF output), or through a
small gavel **converter** where it does not. ESLint runs `--format json` piped
through a Go converter (`lint/lang/typescript/eslint/converter`) because the npm
SARIF formatter will not resolve inside the sandbox; Clippy is converted from its
JSON the same way.

## What the platform's parser requires

The per-result contract (non-empty `ruleId`, location, message, level, failed-run
reporting) is specified once in [bridge](bridge.md#sarif-contract) and applies to
gavel-tools' own aspects and to project-written bridges alike. gavel computes its
own stable fingerprint, so it does not depend on tool fingerprints — but it
**does** depend on `ruleId` being present.

That non-empty-`ruleId` requirement is why gavel-tools wraps each linter itself
rather than delegating to
[aspect-build/rules_lint](https://github.com/aspect-build/rules_lint), whose
reviewdog-based SARIF leaves `ruleId` empty and gets rejected. For a tool
gavel-tools does not wrap, rules_lint stays a valid breadth option to wire on the
side.

For a tool gavel-tools does not wrap, write a [bridge](bridge.md) instead: an aspect
returning the same output group through a stable API.
