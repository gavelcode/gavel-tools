---
title: The hermetic analyzer driver
type: explanation
description: Why every analyzer runs sandboxed, and the maintenance contract for golangci-lint — the one that took custom code to get there.
---

# The hermetic analyzer driver

Every analyzer in gavel-tools runs **sandboxed** — hermetic, cacheable, offline,
no host access. The principle that got them all there:

> Before reaching for `no-sandbox`, ask whether the environment the tool needs is
> already a Bazel artifact you can declare as an input.

Most analyzers read source in isolation and were sandboxed from the start. The
two that looked like they *forced* `no-sandbox` did not, once their environment
was materialized as declared inputs: golangci-lint needs the whole package graph
(rules_go's `go_pkg_info_aspect` emits it), and ESLint needs the consumer's
flat-config plugin closure (rules_js's `JsInfo.npm_sources` carries it). There is
no `no-sandbox` left anywhere in `lint/aspects/`.

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
