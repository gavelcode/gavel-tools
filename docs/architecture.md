# gavel-tools — architecture & design

> Status: design record. Decisions here drive the implementation phases at the end.

## What this module is

`gavel-tools` is the **single source of all Bazel linting wiring** consumed by the
Gavel platform. It provides three things:

1. **Native aspects** — analyzers that run as Bazel aspects and normalize their
   output to SARIF on disk.
2. **A rules_lint adapter** — a pre-wired preset over [aspect-build/rules_lint]
   for the analyzers we choose to delegate.
3. **The default catalog** — a declarative map of *language → tools* that tells
   the platform what to run by default.

The interface to the Gavel platform is **SARIF files on disk**. No Go imports
cross the boundary. The platform (quality gate, baseline, coverage, architecture
verdict, dashboard) treats gavel-tools purely as a *source of findings*.

## The organizing thesis: the sandbox axis

The split between "what gavel-tools owns natively" and "what it delegates to
rules_lint" is **not** "our wrappers vs theirs". It is one question:

> **Does the analyzer need the real build environment?**

- **No** — it reads source files in isolation (ruff, eslint without types, pmd,
  bandit, cpd). It runs fine **sandboxed**: hermetic, cacheable, reproducible.
  rules_lint does this well, and sandboxing is a feature here, not a limitation.
- **Yes** — it needs the compiler, the module graph, type information, the
  classpath, or the whole package (golangci-lint, Error Prone, type-aware
  analysis, architecture rules). It can only run **outside the sandbox**
  (`no-sandbox`). rules_lint structurally cannot do this — it is why they
  removed golangci-lint ("fatal bug").

`gavel-tools` owns the **no-sandbox / semantic tier**; rules_lint owns the
**sandbox / source-file tier**. This boundary is principled and defensible.

### The no-sandbox tax (paid consciously)

`no-sandbox` is not free. Because the tool reads files Bazel did not declare as
inputs, the action cache can go stale and builds couple to the host environment.
We have already paid this twice: the `go_test` golangci aspect served stale SARIF
on body-local edits (fixed with sibling-source tracking), and Bandit picked up a
system Python 3.9 off `PATH` (fixed by resolving the hermetic interpreter). The
native tier pays this tax deliberately, only where the analysis requires it.

## Tier assignment (audited from the aspect implementations)

| Tool | Consumes | no-sandbox is | Tier |
|------|----------|---------------|------|
| **golangci-lint** (go) | compiler + module graph + whole package | load-bearing | **native** 🟢 unique |
| **Error Prone** (java) | `transitive_compile_time_jars` + `--classpath` | load-bearing (type-aware) | **native** 🟢 (rules_lint lacks it) |
| **archtest** (go/java/py/rust/ts) | imports/source for layer rules | semantic | **native** 🟢 unique |
| **pycompile** (python) | python compile | env | **native** 🟢 (rules_lint lacks it) |
| PMD (java) | `srcs + config` only | incidental (habit) | **rules_lint** 🔵 |
| ESLint (ts) | `srcs + config`, not type-aware | incidental | **rules_lint** 🔵 |
| Ruff (python) | standalone binary, source AST | incidental | **rules_lint** 🔵 |
| Bandit (python) | source AST + python | incidental | **rules_lint** 🔵 |
| CPD (java) | source (copy-paste) | incidental | **rules_lint** 🔵 |
| Clippy (rust) | already wraps `rust_clippy_aspect` (sandboxed) | n/a | **rules_lint** 🔵 |
| SpotBugs (java) | `runtime_output_jars` (bytecode) | semantic-ish | **rules_lint** 🔵 (decided: delegate) |

Delegating the source-only tools is a *win*: it sheds the no-sandbox tax for that
tier and gains rules_lint's broader catalog (flake8, pylint, checkstyle,
clang_tidy, cppcheck, ktlint, vale, yamllint, stylelint, ty, …) for free.

## The SARIF boundary (why delegation is clean)

Both tiers emit SARIF on disk; the platform consumes them identically.

| Tier | Aspect label | Output group | File |
|------|--------------|--------------|------|
| native | `@gavel_tools//aspects:defs.bzl%<lang>_<tool>_submission_aspect` | `gavel_submissions` | `*.sarif` |
| rules_lint | `@gavel_tools//rules_lint:linters.bzl%<tool>` | `rules_lint_machine` | `*.report` (SARIF) |

rules_lint runs each linter's native output then converts it to SARIF via its
`sarif_parser` toolchain (reviewdog/errorformat under the hood) into the
`rules_lint_machine` output group. Run with
`--@aspect_rules_lint//lint:fail_on_violation` **off** so the build stays green
and Gavel evaluates the gate itself.

> **Open question to validate before deleting any native wrapper:** rules_lint's
> SARIF is reviewdog-derived (line-level). Does it carry enough — rule id,
> severity, message, stable location — for Gavel's fingerprint/baseline/severity?
> Measure this with a prototype on one language (ruff is the cleanest) before
> delegating the rest.

## Repository structure (target, flattened)

`tools/` was a monorepo artifact; in a standalone module the whole repo *is*
tooling, so it is removed. The layout reflects the tiers:

```
gavel-tools/
├── MODULE.bazel, BUILD.bazel, go.mod, go.sum, README.md, LICENSE
├── catalog.yaml                 # default catalog (the menu) — see below
│
├── aspects/defs.bzl             # NATIVE (no-sandbox) aspects
├── archtest/                    #   shared Go library (layer rules)
├── go/golangci_lint/            #   golangci-lint wrapper + repositories.bzl
├── java/error_prone/            #   Error Prone wrapper + repositories.bzl
├── python/pycompile/            #   pycompile wrapper
│
└── rules_lint/                  # SANDBOX tier — the rules_lint glue lives HERE
    └── linters.bzl              #   pre-wired preset: pmd, ruff, bandit, eslint,
                                 #   cpd, clippy, spotbugs (+ opt-in extras)
```

Labels become `@gavel_tools//aspects:...`, `@gavel_tools//rules_lint:...`,
`@gavel_tools//:catalog.yaml` — no redundant `tools/` segment.

## Tool binary ownership

gavel-tools declares the linter tool binary repos (`@golangci_lint`, plus the
rules_lint tool binaries) in its `MODULE.bazel` via `use_repo_rule`. This is
required: repos a consumer declares are **not visible across the module
boundary** to the aspect that references them. Versions live here, centrally —
consumers do not manage them.

## Catalog: the two-layer config

### Layer 1 — `gavel-tools/catalog.yaml` (the menu)

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
      - { name: archtest,     tier: native,     aspect: java_archtest_submission_aspect,    output_group: gavel_submissions, default: true }
      - { name: pmd,          tier: rules_lint, aspect: pmd,        output_group: rules_lint_machine, default: true }
      - { name: spotbugs,     tier: rules_lint, aspect: spotbugs,   output_group: rules_lint_machine, default: true }
      - { name: checkstyle,   tier: rules_lint, aspect: checkstyle, output_group: rules_lint_machine, default: false }
  # python, rust, typescript …
```

Each entry carries exactly what the core needs to generate the bazelrc and decide
what to run: `tier`, `aspect`, `output_group`, `default`.

### Layer 2 — consumer `gavel.yaml` (the selection)

Selects/overrides per project. No `linters` section → use every `default: true`
entry (basic mode).

```yaml
projects:
  - name: payment-service
    languages: [java]
    linters:
      java: [error-prone, pmd, checkstyle]   # enable extra checkstyle, drop spotbugs
```

### Core impact

`core/.../catalog/*.go` stops being hardcoded maps and becomes a **loader** of
`@gavel_tools//:catalog.yaml` (read via Bazel runfiles — the CLI already uses
runfiles). Wiring detail to resolve in implementation: the CLI needs a Bazel dep
on `@gavel_tools//:catalog.yaml`.

## Implementation roadmap

**Phase 1 — flatten & reorganize (now, reversible, no behavior change).**
Move `tools/*` to the root, keep *all* current native aspects (including the
ones slated for delegation). Update Gavel's label generation
(`core/.../catalog/aspect.go`, `installer`), the committed `.gavel/*`, the four
example repos, and the local registry copy. Re-push gavel-tools, re-pin, judge
5/5 green. `rules_lint/` and `catalog.yaml` are added in later phases.

**Phase 2 — rules_lint prototype + fidelity check.** Build
`rules_lint/linters.bzl` for one language (ruff). Consume `rules_lint_machine`
from Gavel's collector and **measure SARIF fidelity against the baseline**. Go /
no-go on the delegation strategy.

**Phase 3 — catalog.yaml.** Introduce `catalog.yaml`; convert the core catalog
from hardcoded maps to a loader. Add the optional `linters` section to
`gavel.yaml`.

**Phase 4 — delegate & delete.** Per language, wire the rules_lint backend,
validate, then remove the corresponding native wrapper (pmd, ruff, bandit,
eslint, cpd, clippy, spotbugs). Never delete before the replacement is proven.

## Decisions captured

- Native tier = no-sandbox/semantic; rules_lint tier = sandbox/source-file. The
  axis is environment-need, not authorship.
- SpotBugs is delegated to rules_lint.
- The rules_lint glue and the default catalog both live **in gavel-tools**.
- Catalog format is **YAML**; configuration is two-layer (catalog = menu,
  `gavel.yaml` = selection).
- Flatten first keeping everything; delegate-and-delete only after rules_lint
  SARIF fidelity is proven per language.

[aspect-build/rules_lint]: https://github.com/aspect-build/rules_lint
