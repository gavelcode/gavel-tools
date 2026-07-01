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
  analysis, architecture rules). The naive answer is to run it **outside the
  sandbox** (`no-sandbox`); rules_lint dropped golangci-lint for exactly this
  ("fatal bug"). But "needs the build environment" does not mean "needs the
  host": if that environment can be **materialized as declared Bazel inputs**,
  the tool runs sandboxed after all. golangci-lint now does — see below.

`no-sandbox` is a tool, used **only where the analysis genuinely cannot be fed
its environment as inputs** — not a default.

## Materializing the build environment (the hermetic golangci-lint)

golangci-lint loads and type-checks the whole package graph through
`go/packages`. That looks like it forces `no-sandbox` — until you notice
go/packages accepts a **`GOPACKAGESDRIVER`**, and rules_go's
`go_pkg_info_aspect` already emits the entire graph as Bazel artifacts (one
`pkg.json` per package, plus compiled `export` data for deps and the stdlib
under `--@rules_go//go/config:export_stdlib=True`). We declare all of those as
action inputs and point golangci-lint at a **static driver**
(`lint/lang/go/golangci_lint/packagesdriver`) that reads them and never shells
out to Bazel or the network. The result is fully sandboxed: no host `go`, no
module fetches, every input declared, cache-correct.

The driver mirrors rules_go's own (build-tag filtering, stdlib import linking,
test-file splitting) but adds what a sandboxed run needs: it collapses the three
Bazel path placeholders to the exec root, merges the two same-ID `pkg.json` a
`go_test` emits, drops the generated `testmain.go` so it is never linted, and
gives each stdlib package the compiled archive from `go_sdk.libs` (`<pkg>.a`) as
its export data — rules_go leaves that field empty, and without it golangci-lint
cannot load export data for anything that imports the stdlib.

> ⚠️ **Maintenance contract — read before bumping `rules_go` or `golangci-lint`.**
> The Go path is the only analyzer where gavel carries code that shadows an
> upstream: our static driver reimplements the JSON half of rules_go's
> `gopackagesdriver` (~250 lines) because the shipped one shells out to `bazel`
> and cannot run inside a sandbox. That buys full hermeticity, but it couples us
> to **three contracts that are not stable public APIs**:
>
> 1. **rules_go's `pkg.json` format** — the `__BAZEL_*__` path placeholders and
>    `FlatPackage` field names.
> 2. **rules_go's `GoPkgInfo` provider**, loaded from the *internal* path
>    `@rules_go//go/tools/gopackagesdriver:aspect.bzl`, plus the
>    `--@rules_go//go/config:export_stdlib=True` build setting the consumer must
>    pass.
> 3. **golangci-lint's `GOPACKAGESDRIVER` protocol**, which upstream documents as
>    *best-effort / unsupported*.
>
> None of these break at build time — a drift surfaces as `could not import …` or
> `no go files to analyze` at lint time. **So when you upgrade `rules_go` or
> `golangci-lint`, re-run the driver end-to-end** (build the golangci aspect over
> a real Go target and confirm clean SARIF) before trusting the gate. This is the
> recurring tax for keeping golangci-lint *and* a closed sandbox; the considered
> alternatives — `nogo` (lose golangci-lint and `.golangci.yml`) or `no-sandbox`
> (lose hermeticity) — were judged worse. Contrast Rust, which pays ~40 lines of
> SARIF conversion because `rules_rust` ships a hermetic Clippy aspect; nobody
> ships one for golangci-lint, so gavel owns the adapter.

## The no-sandbox tax (paid consciously)

`no-sandbox` is not free. Because the tool reads files Bazel did not declare as
inputs, the action cache can go stale and builds couple to the host environment.
We have paid this twice: the `go_test` golangci aspect served stale SARIF on
body-local edits (fixed with sibling-source tracking), and Bandit picked up a
system Python 3.9 off `PATH` (fixed by resolving the hermetic interpreter). So a
source-only tool that uses `no-sandbox` only out of habit should be **sandboxed**
to shed the tax.

golangci-lint used to pay this tax twice over — it took the host `go` off `PATH`
and could reach the network for modules. Both are now gone: it runs sandboxed
under the pinned SDK against a pre-built package graph (see "Materializing the
build environment" above). The lesson generalizes: before reaching for
`no-sandbox`, ask whether the environment the tool needs is already a Bazel
artifact you can declare.

ESLint proved the same point in the JS ecosystem. It looked source-only, but its
flat config `import`s the consumer's plugins (`react-hooks`, `typescript-eslint`,
…) — a per-project graph, not a fixed set gavel can bundle. The aspect runs on
the consumer's `js_library`/`ts_project` and harvests those plugins from
**`JsInfo.npm_sources`** — the rules_js twin of Go's `GoPkgInfo` — declaring them
as sandboxed inputs so gavel's pinned ESLint resolves them with no host access.
ESLint's npm SARIF formatter does not resolve inside the sandbox, so the wrapper
runs the built-in `json` formatter and converts to SARIF in Go (as Clippy does).

## Tier assignment (audited from the aspect implementations)

All of these are **native** wrappers (they emit each tool's native SARIF — see
[rules-lint](rules-lint.md) for why that matters). The column that matters is
whether `no-sandbox` is load-bearing.

| Tool | Consumes | `no-sandbox` |
|------|----------|--------------|
| **golangci-lint** (go) | whole package graph, via pre-built `pkg.json` + export data | **none** → sandboxed (static driver) |
| **Error Prone** (java) | `transitive_compile_time_jars` + `--classpath` | **load-bearing** (type-aware) |
| **archtest** (all langs) | imports/source for layer rules | **load-bearing** (semantic) |
| **pycompile** (python) | python compile | **load-bearing** (env) |
| SpotBugs (java) | `runtime_output_jars` (bytecode) | semantic-ish (jars are Bazel inputs) |
| Ruff (python) | standalone binary, source AST | **incidental** → should be sandboxed |
| PMD (java) | `srcs + config` only | **incidental** → should be sandboxed |
| **ESLint** (ts) | flat config + the consumer's plugins, via `JsInfo.npm_sources` | **none** → sandboxed (JsInfo harvest) |
| Bandit (python) | source AST + python | **incidental** → should be sandboxed |
| CPD (java) | source (copy-paste) | **incidental** → should be sandboxed |
| Clippy (rust) | wraps `rust_clippy_aspect` (already sandboxed) | n/a |

The source-only wrappers keep native-SARIF fidelity **and** can be sandboxed —
best of both. rules_lint is not in this table; it is a separate, breadth-only
add-on (see [rules-lint](rules-lint.md)).
