---
title: The sandbox axis
type: explanation
description: One question decides how each analyzer runs — does it need the real build environment?
---

# The sandbox axis

The organizing question for every analyzer is **not** "is it ours or rules_lint's".
It is:

> **Does the analyzer need the real build environment?**

- **No** — it reads source files in isolation (ruff, eslint without types, pmd,
  bandit, cpd). It runs fine **sandboxed**: hermetic, cacheable, reproducible.
- **Yes** — it needs the compiler, the module graph, type information, the
  classpath, or the whole package (golangci-lint, Error Prone, type-aware
  analysis, architecture rules). It can only run **outside the sandbox**
  (`no-sandbox`). This is structural — it is why rules_lint removed golangci-lint
  ("fatal bug").

`no-sandbox` is a tool, used **only where the analysis requires it** — not a
default.

## The no-sandbox tax (paid consciously)

`no-sandbox` is not free. Because the tool reads files Bazel did not declare as
inputs, the action cache can go stale and builds couple to the host environment.
We have paid this twice: the `go_test` golangci aspect served stale SARIF on
body-local edits (fixed with sibling-source tracking), and Bandit picked up a
system Python 3.9 off `PATH` (fixed by resolving the hermetic interpreter). So a
source-only tool that uses `no-sandbox` only out of habit should be **sandboxed**
to shed the tax.

## Tier assignment (audited from the aspect implementations)

All of these are **native** wrappers (they emit each tool's native SARIF — see
[rules-lint](rules-lint.md) for why that matters). The column that matters is
whether `no-sandbox` is load-bearing.

| Tool | Consumes | `no-sandbox` |
|------|----------|--------------|
| **golangci-lint** (go) | compiler + module graph + whole package | **load-bearing** |
| **Error Prone** (java) | `transitive_compile_time_jars` + `--classpath` | **load-bearing** (type-aware) |
| **archtest** (all langs) | imports/source for layer rules | **load-bearing** (semantic) |
| **pycompile** (python) | python compile | **load-bearing** (env) |
| SpotBugs (java) | `runtime_output_jars` (bytecode) | semantic-ish (jars are Bazel inputs) |
| Ruff (python) | standalone binary, source AST | **incidental** → should be sandboxed |
| PMD (java) | `srcs + config` only | **incidental** → should be sandboxed |
| ESLint (ts) | `srcs + config` | **incidental** → should be sandboxed |
| Bandit (python) | source AST + python | **incidental** → should be sandboxed |
| CPD (java) | source (copy-paste) | **incidental** → should be sandboxed |
| Clippy (rust) | wraps `rust_clippy_aspect` (already sandboxed) | n/a |

The source-only wrappers keep native-SARIF fidelity **and** can be sandboxed —
best of both. rules_lint is not in this table; it is a separate, breadth-only
add-on (see [rules-lint](rules-lint.md)).
