# gavel-tools

Bazel tooling for [Gavel](https://github.com/gavelcode/gavel). It does two things:

- **Lints** — Starlark aspects that run static analyzers as Bazel aspects and
  normalize their output to SARIF, per-language Go wrappers, and a shared
  architecture-test library.
- **Scaffolds builds** — macros (e.g. `web_project`) that generate a project's
  Bazel build graph so consumers stop hand-wiring it.

Consumed as a Bazel module (`bazel_dep(name = "gavel_tools")`). The interface to
Gavel is SARIF files on disk — there are no Go imports across the boundary.

## Languages

| Language   | Aspects |
|------------|---------|
| Go         | golangci-lint, archtest |
| Java       | PMD, CPD, SpotBugs, Error Prone, archtest |
| Python     | Ruff, Bandit, pycompile, archtest |
| TypeScript | ESLint, archtest |
| Rust       | Clippy, archtest |

## Documentation

Design docs live in [`docs/`](docs/index.md) as an OKF-style concept bundle —
one markdown file per concept, each with a `type` / `title` / `description`
header, plus a reserved [`index.md`](docs/index.md) that maps them.

## License

Apache 2.0. See [LICENSE](LICENSE).
