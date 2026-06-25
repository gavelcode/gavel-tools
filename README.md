# gavel-tools

[![CI](https://github.com/gavelcode/gavel-tools/actions/workflows/ci.yml/badge.svg)](https://github.com/gavelcode/gavel-tools/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/gavelcode/gavel-tools/branch/main/graph/badge.svg)](https://codecov.io/gh/gavelcode/gavel-tools)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Bazel module](https://img.shields.io/badge/bazel-module-43A047.svg)

**Bazel tooling that runs static analyzers as aspects and normalizes their
output to SARIF.** It does two things:

- **Lints** — Starlark aspects that run each language's analyzers as Bazel
  aspects (so they ride the action cache), per-language Go wrappers that emit
  each tool's **native SARIF**, and a shared architecture-test library.
- **Scaffolds builds** — macros (e.g. `web_project`) that generate a project's
  whole Bazel build graph so consumers stop hand-wiring it.

Built for the **Gavel** quality platform, but usable on its own: the interface
is **SARIF files on disk** (output group
`gavel_submissions`) — no Go imports cross the boundary, so any SARIF-aware
consumer can read the results.

> **Status: alpha.** Aspect labels, the catalog format, and the macro API may
> still change.

## Usage

Add the module (bzlmod):

```python
# MODULE.bazel
bazel_dep(name = "gavel_tools", version = "0.1.0")
git_override(
    module_name = "gavel_tools",
    remote = "https://github.com/gavelcode/gavel-tools.git",
    commit = "…",  # pin a commit
)
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

## Documentation

The [`docs/`](docs/index.md) bundle explains the design — the sandbox axis, the
SARIF boundary, the language catalog, and the `web_project` macro. Each concept
is one markdown file with a `type` / `title` / `description` header, mapped by
[`index.md`](docs/index.md).

## License

[Apache 2.0](LICENSE).
