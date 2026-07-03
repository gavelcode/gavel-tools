---
title: The hermetic analyzer driver
type: explanation
description: Why every analyzer runs sandboxed, and the two contracts — golangci-lint's driver and ESLint's pnpm store — that custom code holds in place.
resource: https://github.com/gavelcode/gavel-tools/tree/main/lint/aspects
tags: [hermeticity, sandbox, golangci-lint, eslint, maintenance-contract]
---

# The hermetic analyzer driver

Every analyzer in gavel-tools runs **sandboxed** — hermetic, cacheable, offline,
no host access. The principle that got them all there:

> Before reaching for `no-sandbox`, ask whether the environment the tool needs is
> already a Bazel artifact you can declare as an input.

Most analyzers read source in isolation and were sandboxed from the start. Two
looked like they *forced* `no-sandbox` and did not — but each costs a little
custom code and a standing maintenance contract:

- **golangci-lint** needs the whole package graph; rules_go's `go_pkg_info_aspect`
  emits it, and a static driver reads it.
- **ESLint** needs the consumer's flat-config plugin closure **and** a pnpm store
  that resolves inside the sandbox; `JsInfo` carries the closure, and the wrapper
  repairs the store.

There is no `no-sandbox` left anywhere in `lint/aspects/`. The two contracts
below are the price — and both fail the same treacherous way: **not at build
time, but at lint time**, surfacing as `could not import …` or `Cannot find
module …` long after the build goes green.

## The hermetic golangci-lint

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

No published integration manages this. Aspect's `rules_lint` shipped a golangci
aspect, hit the same transitive-sources problem, declared it a *fatal bug* and
[removed golangci-lint support entirely](https://github.com/aspect-build/rules_lint/pull/207).
rules_go's built-in `nogo` runs at compile time with correct caching but only
executes `go/analysis` analyzers, not the full golangci suite; the
proofs-of-concept that port golangci's linters into nogo warn against production
use, and golangci-lint's
[own tracking issue for Bazel support](https://github.com/golangci/golangci-lint/issues/1473)
has sat open since 2020. Every attempt gives up one of three things —
golangci-lint itself, correct caching, or the sandbox. The static driver is how
gavel keeps all three.

The driver mirrors rules_go's own (build-tag filtering, stdlib import linking,
test-file splitting) but adds what a sandboxed run needs: it collapses the three
Bazel path placeholders to the exec root, merges the two same-ID `pkg.json` a
`go_test` emits, drops the generated `testmain.go` so it is never linted, and
gives each stdlib package the compiled archive from `go_sdk.libs` (`<pkg>.a`) as
its export data — rules_go leaves that field empty, and without it golangci-lint
cannot load export data for anything that imports the stdlib.

> [!WARNING]
> **Maintenance contract — read before bumping `rules_go` or `golangci-lint`.**
>
> The Go path is the only analyzer where gavel carries code that *shadows* an
> upstream: the static driver reimplements the JSON half of rules_go's
> `gopackagesdriver` because the shipped one shells out to `bazel` and cannot run
> inside a sandbox. That buys full hermeticity, but couples us to **three
> contracts that are not stable public APIs**:
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
> None of these break at build time. **So when you upgrade `rules_go` or
> `golangci-lint`, re-run the driver end-to-end** — build the golangci aspect over
> a real Go target and confirm a clean SARIF — before trusting the gate. This is
> the recurring tax for keeping golangci-lint *and* a closed sandbox; the
> alternatives — `nogo` (lose golangci-lint and `.golangci.yml`) or `no-sandbox`
> (lose hermeticity) — were judged worse. Contrast Rust, which pays only SARIF
> conversion because `rules_rust` ships a hermetic Clippy aspect; nobody ships one
> for golangci-lint, so gavel owns the adapter.

## The hermetic ESLint

ESLint resolves the consumer's plugins from `JsInfo.npm_sources` and its own
runtime from the pinned tool — both are Bazel artifacts, so the run is sandboxed
with no host `node_modules`. One wrinkle needs custom code: when a **downstream
module** consumes gavel-tools, rules_js materializes the pnpm store without its
`s/` layer, so every `../../s/<pkg>` symlink inside the store dangles and ESLint
cannot load even its own dependencies. The wrapper
(`lint/lang/typescript/eslint/wrapper`) reconstructs the `s/` store across the
runfiles and keeps the rules_js Node fs-patch on (`JS_BINARY__PATCH_NODE_FS=1`),
so Node resolves *within* the runfiles instead of following symlinks out to the
raw, unrepaired output tree.

> [!WARNING]
> **Maintenance contract — read before bumping `rules_js` or ESLint.**
>
> The store repair couples us to **two things that are not stable public APIs**:
> the rules_js pnpm store layout (the `s/` directory and the `../../s/<pkg>`
> symlink shape) and the `JS_BINARY__PATCH_NODE_FS` fs-patch behaviour.
>
> Two properties make this uniquely easy to break *silently*:
>
> 1. **It fails at lint time, not build time** — a drift surfaces as
>    `executionSuccessful: false` / `Cannot find module …`, which gavel records as
>    a failed tool execution, not a build error.
> 2. **It only reproduces cross-module.** In gavel-tools' *own* build the store is
>    materialized correctly, so no native unit or e2e test can catch it — only a
>    **downstream consumer running the aspect** can. That consumer is gavel's
>    `gavel judge` dogfooding gate in CI.
>
> **So when you bump `rules_js` or ESLint, run the ESLint aspect end-to-end from a
> downstream consumer** — build the aspect over a real TypeScript target and
> confirm the SARIF is `executionSuccessful` — before trusting the gate. A stale
> path in this repair once shipped broken for months precisely because nothing
> exercised it end-to-end.
