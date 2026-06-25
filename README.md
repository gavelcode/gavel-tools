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

Design docs live in [`docs/`](docs/) as a per-concept bundle (one file per
concept, each with a `title` / `type` / `summary` header):

- [Repository layout](docs/repository-layout.md) — the `lint/` + `macros/` structure
- [The sandbox axis](docs/tier-model.md) — which tools run sandboxed vs `no-sandbox`, and why
- [SARIF boundary](docs/sarif-boundary.md) — how findings flow to the platform
- [rules_lint](docs/rules-lint.md) — why it's a breadth add-on, not a substitute
- [Catalog](docs/catalog.md) — the two-layer config
- [web_project](docs/web-project.md) — the frontend build macro
- [Status](docs/status.md) — what's done, what's next

## License

Apache 2.0. See [LICENSE](LICENSE).
