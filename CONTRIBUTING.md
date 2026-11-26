# Contributing to gavel-tools

Thanks for your interest. gavel-tools is a Bazel module (bzlmod) that runs
static analyzers as aspects and emits SARIF. Everything builds, runs, and tests
through Bazel.

## Prerequisites

- [Bazelisk](https://github.com/bazelbuild/bazelisk) (resolves the right Bazel
  version automatically). No other system dependencies — toolchains are
  managed by Bazel.

## Build & test

```bash
bazel build //...
bazel test //...
```

If you change Go code, regenerate the `BUILD.bazel` files:

```bash
bazel run //dev:gazelle
```

## Layout

```
modules/<lang>/        # one Bazel module per language: its aspects and linter binaries
lint/
├── aspects/           # core aspects (no language ruleset) and shared helpers
├── archtest/          # shared architecture-test library
├── cmd/gavel-lint/    # the one multi-call helper binary every aspect runs
├── toolchain/         # delivers gavel-lint: prebuilt in releases, source in dev
└── lang/<lang>/<tool>/ # per-tool Go package (`Main`) that invokes the tool → SARIF
docs/                  # design docs (OKF bundle) — see docs/index.md
```

See [`docs/repository-layout.md`](docs/repository-layout.md) for the full map and
[`docs/sarif-boundary.md`](docs/sarif-boundary.md) for how findings flow out.

## Adding a linter or language

1. Add the aspect in `modules/<language>/lint/aspects/<language>.bzl` and re-export
   it from that module's `lint/aspects/defs.bzl` (the stable public entry point).
   An aspect that loads no language ruleset belongs in the core `lint/aspects/`.
2. Add the per-tool wrapper under `lint/lang/<language>/<tool>/` (a Go package
   exposing `Main(args) int` that runs the tool and produces SARIF — natively
   where the tool emits it, via a converter where it does not), register it as a
   subcommand in `lint/cmd/gavel-lint/main.go`, and have the aspect run
   `gavel-lint <tool> …` through the toolchain (`gavel_lint_binary(ctx)` in
   `lint/aspects/common.bzl`). Never add a per-tool `go_binary`.
3. Declare the tool's binary repository in the language module's `MODULE.bazel`
   (never the core's), and list the tool in `lint/catalog.yaml` with its `module`.
4. For architecture validation, extend `lint/archtest/`.
5. Add tests, and document the tool in the relevant `docs/` concept file.

The organizing principle is **hermeticity** — read
[`docs/tier-model.md`](docs/tier-model.md) before deciding how a tool runs.

## Pull requests

- Keep each PR to one logical change, and run the full gate before pushing:
  `bash .github/ci/check.sh` (tests, coverage, formatting, lint, architecture,
  vulnerabilities, …) must pass — it is exactly what CI runs.
  `bash .github/ci/fmt.sh` applies the auto-fixes.
- Follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat`, `fix`, `docs`, `refactor`, …) with an imperative subject. Explain the
  *why* in the body, not the diff.
- No `Co-Authored-By` trailers.
