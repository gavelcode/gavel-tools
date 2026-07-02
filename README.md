# gavel-tools

[![CI](https://github.com/gavelcode/gavel-tools/actions/workflows/ci.yml/badge.svg)](https://github.com/gavelcode/gavel-tools/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/gavelcode/gavel-tools/branch/main/graph/badge.svg)](https://codecov.io/gh/gavelcode/gavel-tools)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Bazel module](https://img.shields.io/badge/bazel-module-43A047.svg)

**Bazel tooling that runs static analyzers as aspects and normalizes their
output to SARIF.** It does two things:

- **Lints** — Starlark aspects that run each language's analyzers as Bazel
  aspects (so they ride the action cache), per-language Go wrappers that
  **normalize each tool's output to SARIF** (native where the tool emits it,
  converted where it does not), and a shared architecture-test library.
- **Scaffolds builds** — macros (e.g. `web_project`) that generate a project's
  whole Bazel build graph so consumers stop hand-wiring it.

Built for the [Gavel](https://github.com/gavelcode/gavel) quality platform, but
usable on its own: the interface is **SARIF files on disk** (output group
`gavel_submissions`) — no Go imports cross the boundary, so any SARIF-aware
consumer can read the results.

> [!NOTE]
> **Alpha.** Aspect labels, the catalog format, and the macro API may still change.

## Usage

gavel-tools is published to the [gavel registry](https://gavelcode.github.io/registry).
Point Bazel at it (alongside the Bazel Central Registry) and depend on it by
version:

```bash
# .bazelrc
common --registry=https://bcr.bazel.build
common --registry=https://gavelcode.github.io/registry
```

```python
# MODULE.bazel — the registry lists every published version; pin the latest
bazel_dep(name = "gavel_tools", version = "X.Y.Z")
```

Run a lint aspect over your targets — findings land as `*.sarif` under
`bazel-bin/`:

```bash
bazel build //... \
  --aspects=@gavel_tools//lint/aspects:defs.bzl%go_golangci_lint_submission_aspect \
  --output_groups=gavel_submissions
```

Or scaffold a frontend's entire build graph (esbuild + tailwind + tsc + eslint)
from one declaration:

```python
load("@gavel_tools//macros:web.bzl", "web_project")
```

## Languages

| Language   | Aspects |
|------------|---------|
| Go         | golangci-lint, archtest |
| Java       | PMD, CPD, SpotBugs, Error Prone, archtest |
| Python     | Ruff, Bandit, pycompile, archtest |
| TypeScript | ESLint, archtest |
| Rust       | Clippy, archtest |

> [!WARNING]
> Two aspects run hermetic through gavel-owned glue coupled to build-rule
> internals, and **fail at lint time, not build time** if that glue drifts: the
> Go **golangci-lint** driver (which needs `--@rules_go//go/config:export_stdlib=True`)
> and the TypeScript **ESLint** pnpm-store repair. **Re-validate the relevant one
> whenever you bump `rules_go` / golangci-lint or `rules_js` / ESLint** — the
> maintenance contracts are in
> [the hermetic analyzer driver](docs/tier-model.md).

## Documentation

The [`docs/`](docs/index.md) bundle explains the design — the hermetic driver,
the SARIF boundary, the language catalog, and the `web_project` macro. It is an
[Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
bundle: each concept is one markdown file with a `type` / `title` / `description`
header, indexed by [`index.md`](docs/index.md).

## License

[Apache 2.0](LICENSE).
