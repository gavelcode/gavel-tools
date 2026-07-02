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
bazel run //:gazelle
```

## Layout

```
lint/
├── aspects/           # lint aspects: one <lang>.bzl each; defs.bzl re-exports them
├── archtest/          # shared architecture-test library
└── lang/<lang>/<tool>/ # per-tool Go wrapper that invokes the tool → SARIF
macros/                # build macros (e.g. web_project)
docs/                  # design docs (OKF bundle) — see docs/index.md
```

See [`docs/repository-layout.md`](docs/repository-layout.md) for the full map and
[`docs/sarif-boundary.md`](docs/sarif-boundary.md) for how findings flow out.

## Adding a linter or language

1. Add the aspect in `lint/aspects/<language>.bzl` and re-export it from
   `lint/aspects/defs.bzl` (the stable public entry point).
2. Add the per-tool wrapper under `lint/lang/<language>/<tool>/` (Go binary that
   runs the tool and produces SARIF — natively where the tool emits it, via a
   converter where it does not).
3. Declare the tool's binary repository in `MODULE.bazel`.
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
