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
| `@gavel_tools//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect` | `gavel_submissions` | `*.sarif` |

Each aspect produces SARIF with a proper `ruleId`, severity and location —
**natively** where the tool ships a SARIF emitter that resolves in the sandbox
(e.g. `ruff --output-format=sarif`, golangci-lint's SARIF output), or through a
small gavel **converter** where it does not. ESLint runs `--format json` piped
through a Go converter (`lint/lang/typescript/eslint/converter`) because the npm
SARIF formatter will not resolve inside the sandbox; Clippy is converted from its
JSON the same way.

## What the platform's parser requires

Gavel's SARIF parser needs, per result: a non-empty **`ruleId`** (it rejects
results without one), a **location** (`artifactLocation.uri` + `region.startLine`),
a **message**, and a **level** (else it defaults to warning). It computes its own
stable fingerprint from `tool:ruleId:file:lineContent`, so it does not depend on
the tool supplying fingerprints — but it **does** depend on `ruleId` being present.

That non-empty-`ruleId` requirement is why gavel-tools wraps each linter itself
rather than delegating to
[aspect-build/rules_lint](https://github.com/aspect-build/rules_lint), whose
reviewdog-based SARIF leaves `ruleId` empty and gets rejected. For a tool
gavel-tools does not wrap, rules_lint stays a valid breadth option to wire on the
side.
